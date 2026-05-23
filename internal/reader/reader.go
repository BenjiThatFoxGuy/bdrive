package reader

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/gotd/td/tg"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/crypt"
	"github.com/tgdrive/teldrive/pkg/types"
)

// FileRef identifies a file stored in Telegram channels.
type FileRef struct {
	ID        string
	ChannelID int64
	Encrypted bool
}

// Reader is a seekable, streaming byte reader for teldrive files.
// It implements io.ReadSeeker + io.Closer so callers can use it directly
// or wrap it with varc.Cache.OpenReadSeeker for disk-backed caching.
type Reader struct {
	ctx         context.Context
	file        *FileRef
	parts       []types.Part
	totalSize   int64
	position    int64
	reader      io.ReadCloser
	readerStart int64
	readerLen   int64
	config      *config.TGConfig
	client      *tg.Client
	cache       cache.Cacher
	closeOnce   sync.Once
	closeErr    error
	botID       string
}

// NewReader creates a seekable Reader for the full file.
// Unlike the old version there is no [start, end] range parameter — the
// Reader always represents the entire file. Call io.LimitReader or varc to
// restrict the output range.
func NewReader(ctx context.Context,
	client *tg.Client,
	cache cache.Cacher,
	file *FileRef,
	parts []types.Part,
	config *config.TGConfig,
	botID string,
) (*Reader, error) {

	var totalSize int64
	for _, p := range parts {
		if file.Encrypted {
			totalSize += p.DecryptedSize
		} else {
			totalSize += p.Size
		}
	}
	if totalSize == 0 {
		return nil, fmt.Errorf("empty file")
	}

	return &Reader{
		ctx:   ctx,
		parts: parts,
		file:  file,
		totalSize: totalSize,
		config: config,
		client: client,
		cache:  cache,
		botID:  botID,
	}, nil
}

// Read reads up to len(p) bytes from the current file position.
func (r *Reader) Read(p []byte) (int, error) {
	if r.position >= r.totalSize {
		return 0, io.EOF
	}

	if r.reader == nil {
		if err := r.openReader(); err != nil {
			return 0, err
		}
	}

	// Don't read past the current part reader's range.
	readable := r.readerLen - (r.position - r.readerStart)
	if readable <= 0 {
		r.closeCurrentReader()
		return r.Read(p)
	}
	if int64(len(p)) > readable {
		p = p[:readable]
	}

	n, err := r.reader.Read(p)
	r.position += int64(n)

	if err == io.EOF {
		r.closeCurrentReader()
		// There may be more parts — only propagate EOF at the very end.
		if r.position < r.totalSize {
			err = nil
		}
	}

	return n, err
}

// Seek repositions the file offset for the next Read call.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.position + offset
	case io.SeekEnd:
		abs = r.totalSize + offset
	default:
		return r.position, fmt.Errorf("reader: invalid whence %d", whence)
	}
	if abs < 0 {
		abs = 0
	}
	if abs > r.totalSize {
		abs = r.totalSize
	}

	// Keep the current reader if the new position falls inside its range.
	if r.reader != nil {
		if abs < r.readerStart || abs >= r.readerStart+r.readerLen {
			r.closeCurrentReader()
		}
	}

	r.position = abs
	return r.position, nil
}

// Close closes the reader and releases underlying Telegram connections.
func (r *Reader) Close() error {
	r.closeOnce.Do(func() {
		r.closeCurrentReader()
	})
	return r.closeErr
}

// openReader resolves the current position to a part and creates the
// corresponding part reader (raw or decrypted).
func (r *Reader) openReader() error {
	var cumSize int64
	for _, p := range r.parts {
		partSize := p.Size
		if r.file.Encrypted {
			partSize = p.DecryptedSize
		}
		if r.position < cumSize+partSize {
			offsetInPart := r.position - cumSize
			bytesLeftInPart := partSize - offsetInPart

			chunkSrc := &chunkSource{
				channelId:   r.file.ChannelID,
				partId:      p.ID,
				client:      r.client,
				concurrency: r.config.Stream.Concurrency,
				cache:       r.cache,
				key:         cache.KeyFileLocation(r.config.SessionInstance, r.botID, r.file.ID, p.ID),
			}

			partEnd := offsetInPart + bytesLeftInPart - 1

			if r.file.Encrypted {
				ciph, err := crypt.NewCipher(r.config.Uploads.EncryptionKey, p.Salt)
				if err != nil {
					return fmt.Errorf("reader: new cipher: %w", err)
				}
				decReader, err := ciph.DecryptDataSeek(r.ctx,
					func(ctx context.Context, underlyingOffset, underlyingLimit int64) (io.ReadCloser, error) {
						end := min(p.Size-1, underlyingOffset+underlyingLimit-1)
						return newTGMultiReader(r.ctx, underlyingOffset, end, r.config, chunkSrc)
					}, offsetInPart, bytesLeftInPart)
				if err != nil {
					return fmt.Errorf("reader: decrypt: %w", err)
				}
				r.reader = decReader
			} else {
				rawReader, err := newTGMultiReader(r.ctx, offsetInPart, partEnd, r.config, chunkSrc)
				if err != nil {
					return fmt.Errorf("reader: tg multi reader: %w", err)
				}
				r.reader = rawReader
			}

			r.readerStart = r.position
			r.readerLen = bytesLeftInPart
			return nil
		}
		cumSize += partSize
	}

	return io.EOF
}

func (r *Reader) closeCurrentReader() {
	if r.reader != nil {
		r.closeErr = r.reader.Close()
		r.reader = nil
	}
}

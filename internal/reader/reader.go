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

// Reader is a byte-addressable reader for teldrive files. ReadAt is stateless
// and is the primary API used by the disk cache; Read/Seek are convenience
// methods for direct streaming without cache.
type Reader struct {
	ctx       context.Context
	file      *FileRef
	parts     []types.Part
	totalSize int64
	position  int64
	config    *config.TGConfig
	client    *tg.Client
	cache     cache.Cacher
	mu        sync.Mutex
	closed    bool
	botID     string
}

// NewReader creates a byte-addressable Reader for the full file.
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
		ctx:       ctx,
		parts:     parts,
		file:      file,
		totalSize: totalSize,
		config:    config,
		client:    client,
		cache:     cache,
		botID:     botID,
	}, nil
}

// Read reads up to len(p) bytes from the current file position.
func (r *Reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	off := r.position
	r.mu.Unlock()

	n, err := r.ReadAt(p, off)

	r.mu.Lock()
	if !r.closed {
		r.position += int64(n)
	}
	r.mu.Unlock()
	return n, err
}

// ReadAt reads len(p) bytes from the file starting at off. It does not use or
// mutate the sequential Read cursor, so it is safe for cache range reads.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("reader: negative offset %d", off)
	}
	if off >= r.totalSize {
		return 0, io.EOF
	}
	want := len(p)
	if off+int64(want) > r.totalSize {
		want = int(r.totalSize - off)
	}

	total := 0
	for total < want {
		part, partStart, partSize, err := r.partAt(off + int64(total))
		if err != nil {
			return total, err
		}
		offsetInPart := off + int64(total) - partStart
		span := min(int64(want-total), partSize-offsetInPart)
		n, err := r.readPartAt(p[total:total+int(span)], part, offsetInPart)
		total += n
		if err != nil {
			return total, err
		}
		if n != int(span) {
			return total, io.ErrUnexpectedEOF
		}
	}
	if want < len(p) {
		return total, io.EOF
	}
	return total, nil
}

// Seek repositions the file offset for the next Read call.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
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

	r.position = abs
	return r.position, nil
}

// Close marks the reader closed. Individual range reads close their Telegram
// readers as soon as each ReadAt call completes.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *Reader) partAt(off int64) (types.Part, int64, int64, error) {
	var cumSize int64
	for _, p := range r.parts {
		partSize := p.Size
		if r.file.Encrypted {
			partSize = p.DecryptedSize
		}
		if off < cumSize+partSize {
			return p, cumSize, partSize, nil
		}
		cumSize += partSize
	}
	return types.Part{}, 0, 0, io.EOF
}

func (r *Reader) readPartAt(p []byte, part types.Part, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunkSrc := &chunkSource{
		channelId:   r.file.ChannelID,
		partId:      part.ID,
		client:      r.client,
		concurrency: r.config.Stream.Concurrency,
		cache:       r.cache,
		key:         cache.KeyFileLocation(r.config.SessionInstance, r.botID, r.file.ID, part.ID),
	}

	var rc io.ReadCloser
	var err error
	span := int64(len(p))
	if r.file.Encrypted {
		ciph, err := crypt.NewCipher(r.config.Uploads.EncryptionKey, part.Salt)
		if err != nil {
			return 0, fmt.Errorf("reader: new cipher: %w", err)
		}
		rc, err = ciph.DecryptDataSeek(r.ctx,
			func(ctx context.Context, underlyingOffset, underlyingLimit int64) (io.ReadCloser, error) {
				end := min(part.Size-1, underlyingOffset+underlyingLimit-1)
				return newTGMultiReader(ctx, underlyingOffset, end, r.config, chunkSrc)
			}, off, span)
		if err != nil {
			return 0, fmt.Errorf("reader: decrypt: %w", err)
		}
	} else {
		rc, err = newTGMultiReader(r.ctx, off, off+span-1, r.config, chunkSrc)
		if err != nil {
			return 0, fmt.Errorf("reader: tg multi reader: %w", err)
		}
	}
	defer rc.Close()
	n, err := io.ReadFull(rc, p)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return n, err
	}
	return n, err
}

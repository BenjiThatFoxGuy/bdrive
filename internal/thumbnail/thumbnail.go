package thumbnail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ErrInputTooLarge is returned when the video exceeds the configured maximum.
var ErrInputTooLarge = errors.New("input exceeds maximum size for thumbnail generation")

// Generator extracts a single JPEG frame from a video file using ffmpeg.
type Generator struct {
	maxInputSize int64
	timeout      time.Duration
	sem          chan struct{}
}

// NewGenerator creates a Generator that limits concurrent ffmpeg invocations to
// maxConcurrent and skips files larger than maxInputSize bytes.
func NewGenerator(maxInputSize int64, timeout time.Duration, maxConcurrent int) *Generator {
	return &Generator{
		maxInputSize: maxInputSize,
		timeout:      timeout,
		sem:          make(chan struct{}, maxConcurrent),
	}
}

// Generate reads the full video from input, writes it to a temp file, and runs
// ffmpeg to extract a single JPEG frame (360 px wide, quality ~80). The caller
// must decrypt encrypted input before passing it here.
func (g *Generator) Generate(ctx context.Context, input io.Reader, inputSize int64) ([]byte, error) {
	if g.maxInputSize > 0 && inputSize > g.maxInputSize {
		return nil, ErrInputTooLarge
	}

	// Acquire concurrency slot.
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	tmpDir, err := os.MkdirTemp("", "teldrive-thumb-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.tmp")
	outputPath := filepath.Join(tmpDir, "output.jpg")

	f, err := os.Create(inputPath)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, input); err != nil {
		f.Close()
		return nil, fmt.Errorf("write input: %w", err)
	}
	f.Close()

	// Try extracting a frame at 1 s; fall back to 0 for very short videos.
	ffCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	if err := runFFmpeg(ffCtx, inputPath, outputPath, "1"); err != nil {
		os.Remove(outputPath)
		if err2 := runFFmpeg(ffCtx, inputPath, outputPath, "0"); err2 != nil {
			return nil, fmt.Errorf("ffmpeg: %w", err2)
		}
	}

	return os.ReadFile(outputPath)
}

// GenerateFromURL runs ffmpeg directly against an HTTP(S) URL. ffmpeg handles
// its own seeking, so we never download the whole file into memory or disk.
// This only works for unencrypted files whose stream endpoint supports Range.
func (g *Generator) GenerateFromURL(ctx context.Context, url string) ([]byte, error) {
	// Acquire concurrency slot.
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	tmpDir, err := os.MkdirTemp("", "teldrive-thumb-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "output.jpg")

	ffCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	if err := runFFmpeg(ffCtx, url, outputPath, "1"); err != nil {
		os.Remove(outputPath)
		if err2 := runFFmpeg(ffCtx, url, outputPath, "0"); err2 != nil {
			return nil, fmt.Errorf("ffmpeg url: %w", err2)
		}
	}

	return os.ReadFile(outputPath)
}

func runFFmpeg(ctx context.Context, input, output, seekTo string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-ss", seekTo,
		"-i", input,
		"-vframes", "1",
		"-vf", "scale=360:-2",
		"-q:v", "5",
		"-f", "image2",
		"-y",
		output,
	)
	return cmd.Run()
}

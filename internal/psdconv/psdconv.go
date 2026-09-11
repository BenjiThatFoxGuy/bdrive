// Package psdconv converts PSD files to PNG images for the image preview
// pipeline. Conversion runs in a goroutine so the caller's request thread
// is never blocked by the decode.
package psdconv

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"

	"github.com/oov/psd"
)

// ToPNG reads a PSD from r and encodes the flattened (composited) image as
// PNG into the returned buffer. The work runs synchronously but is designed
// to be called from a goroutine; cancel ctx to abort early.
func ToPNG(ctx context.Context, r io.Reader) (*bytes.Buffer, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("psdconv: read: %w", err)
	}

	// Check for cancellation before the expensive decode.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	img, _, err := psd.Decode(bytes.NewReader(data), &psd.DecodeOptions{
		SkipMergedImage: false,
	})
	if err != nil {
		return nil, fmt.Errorf("psdconv: decode: %w", err)
	}

	// psd.Decode returns a *psd.PSD whose Config gives us the composited image.
	composited := img.Config.Rect
	if composited.Dx() == 0 || composited.Dy() == 0 {
		return nil, fmt.Errorf("psdconv: empty image (%dx%d)", composited.Dx(), composited.Dy())
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Build the composited NRGBA image from the picker.
	nrgba := image.NewNRGBA(composited)
	for y := composited.Min.Y; y < composited.Max.Y; y++ {
		for x := composited.Min.X; x < composited.Max.X; x++ {
			nrgba.Set(x, y, img.Picker.At(x, y))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, nrgba); err != nil {
		return nil, fmt.Errorf("psdconv: png encode: %w", err)
	}

	return &buf, nil
}

// IsPSD returns true if the MIME type or file extension indicates a PSD file.
func IsPSD(mimeType, fileName string) bool {
	if mimeType == "image/vnd.adobe.photoshop" || mimeType == "image/x-photoshop" {
		return true
	}
	if len(fileName) > 4 {
		ext := fileName[len(fileName)-4:]
		return ext == ".psd" || ext == ".PSD"
	}
	return false
}

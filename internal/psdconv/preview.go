package psdconv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"strings"

	"github.com/oov/psd"
	"github.com/oov/psd/composite"
)

const (
	previewMaxDimension    = 2048
	previewMaxSourceBytes  = 512 << 20
	previewMaxCanvasPixels = 64 << 20
	previewMaxLayerPixels  = 128 << 20
	previewMaxLayers       = 1024
)

var ErrUnsupportedPreview = errors.New("PSD is outside the server layer-preview limits")

type PreviewLayer struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Visible  bool           `json:"visible"`
	Group    bool           `json:"group"`
	Children []PreviewLayer `json:"children,omitempty"`
}

type PreviewMetadata struct {
	Width  int            `json:"width"`
	Height int            `json:"height"`
	Layers []PreviewLayer `json:"layers"`
}

type cancellableReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancellableReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// InspectLayers reads the metadata without decoding any merged or layer pixels.
// Run this and RenderLayers in an isolated worker before exposing uploaded files
// to the decoder: the dependency can allocate from lengths declared in PSD blocks.
func InspectLayers(ctx context.Context, input io.ReadSeeker) (PreviewMetadata, error) {
	var meta PreviewMetadata
	if err := ctx.Err(); err != nil {
		return meta, err
	}
	size, err := input.Seek(0, io.SeekEnd)
	if err != nil {
		return meta, err
	}
	if size < 26 || size > previewMaxSourceBytes {
		return meta, ErrUnsupportedPreview
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return meta, err
	}
	var header [26]byte
	if _, err := io.ReadFull(input, header[:]); err != nil {
		return meta, err
	}
	width := uint64(binary.BigEndian.Uint32(header[18:22]))
	height := uint64(binary.BigEndian.Uint32(header[14:18]))
	if string(header[:4]) != "8BPS" || binary.BigEndian.Uint16(header[4:6]) != 1 ||
		binary.BigEndian.Uint16(header[22:24]) != 8 || binary.BigEndian.Uint16(header[24:26]) != 3 ||
		width == 0 || height == 0 || width > 30000 || height > 30000 || width*height > previewMaxCanvasPixels {
		return meta, ErrUnsupportedPreview
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return meta, err
	}
	decoded, _, err := psd.Decode(cancellableReader{ctx, input}, &psd.DecodeOptions{SkipLayerImage: true, SkipMergedImage: true})
	if err != nil {
		return meta, fmt.Errorf("read PSD layer metadata: %w", err)
	}
	meta.Width, meta.Height = int(width), int(height)
	var total int64
	var count int
	var visit func([]psd.Layer, int) ([]PreviewLayer, error)
	visit = func(layers []psd.Layer, depth int) ([]PreviewLayer, error) {
		if depth > 64 || count+len(layers) > previewMaxLayers {
			return nil, ErrUnsupportedPreview
		}
		count += len(layers)
		result := make([]PreviewLayer, 0, len(layers))
		for i := len(layers) - 1; i >= 0; i-- {
			l := &layers[i]
			for _, rect := range []image.Rectangle{l.Rect, l.Mask.Rect} {
				if rect.Min.X < -30000 || rect.Min.Y < -30000 || rect.Max.X > 60000 || rect.Max.Y > 60000 || rect.Dx() < 0 || rect.Dy() < 0 || rect.Dx() > 30000 || rect.Dy() > 30000 {
					return nil, ErrUnsupportedPreview
				}
				total += int64(rect.Dx()) * int64(rect.Dy())
				if total > previewMaxLayerPixels {
					return nil, ErrUnsupportedPreview
				}
			}
			var children []PreviewLayer
			if len(l.Layer) > 0 {
				children, err = visit(l.Layer, depth+1)
				if err != nil {
					return nil, err
				}
			}
			name := strings.TrimSpace(l.Name)
			if name == "" {
				name = "(unnamed)"
			}
			result = append(result, PreviewLayer{ID: l.SeqID, Name: name, Visible: l.Visible(), Group: l.Folder(), Children: children})
		}
		return result, nil
	}
	meta.Layers, err = visit(decoded.Layer, 0)
	return meta, err
}

// RenderLayers composites the requested visibility changes and emits a lossless
// RGBA preview. Only the preview is scaled; the source is never modified.
func RenderLayers(ctx context.Context, input io.ReadSeeker, overrides map[int]bool) ([]byte, error) {
	meta, err := InspectLayers(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(meta.Layers) == 0 {
		return nil, ErrUnsupportedPreview
	}
	valid := make(map[int]struct{})
	var record func([]PreviewLayer)
	record = func(layers []PreviewLayer) {
		for _, l := range layers {
			valid[l.ID] = struct{}{}
			record(l.Children)
		}
	}
	record(meta.Layers)
	for id := range overrides {
		if _, ok := valid[id]; !ok {
			return nil, fmt.Errorf("unknown PSD layer ID %d", id)
		}
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	factor := math.Min(1, float64(previewMaxDimension)/float64(max(meta.Width, meta.Height)))
	opt := &composite.Options{}
	opt.TransformMatrix[0], opt.TransformMatrix[4] = factor, factor
	tree, err := composite.New(ctx, cancellableReader{ctx, input}, opt)
	if err != nil {
		return nil, fmt.Errorf("decode PSD layers: %w", err)
	}
	var apply func([]composite.Layer)
	apply = func(layers []composite.Layer) {
		for i := range layers {
			l := &layers[i]
			if on, ok := overrides[l.SeqID]; ok {
				l.Visible = on
			}
			apply(l.Children)
		}
	}
	apply(tree.Root.Children)
	w, h := int(math.Ceil(float64(meta.Width)*factor)), int(math.Ceil(float64(meta.Height)*factor))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if err = tree.Renderer.Render(ctx, img); err != nil {
		return nil, fmt.Errorf("composite PSD layers: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, img); err != nil {
		return nil, fmt.Errorf("encode PSD preview: %w", err)
	}
	return encoded.Bytes(), nil
}

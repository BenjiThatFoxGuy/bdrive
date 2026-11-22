package psdconv

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"
)

func TestLayerPreviewPNG(t *testing.T) {
	fixture := layeredFixture()
	metadata, err := InspectLayers(context.Background(), bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Width != 2 || metadata.Height != 2 || len(metadata.Layers) != 2 {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	var id int
	for _, layer := range metadata.Layers {
		if layer.Name == "red" {
			id = layer.ID
		}
	}
	for _, tc := range []struct {
		name      string
		overrides map[int]bool
		want      color.NRGBA
	}{
		{"saved visibility", nil, color.NRGBA{R: 255, A: 255}},
		{"hide red", map[int]bool{id: false}, color.NRGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := RenderLayers(context.Background(), bytes.NewReader(fixture), tc.overrides)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
				t.Fatalf("preview dimensions: %v", img.Bounds())
			}
			if got := color.NRGBAModel.Convert(img.At(0, 0)); got != tc.want {
				t.Fatalf("pixel: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLayerPreviewRejectsUnsupportedBlend(t *testing.T) {
	for _, mode := range []string{"diss", "????"} {
		t.Run(mode, func(t *testing.T) {
			fixture := bytes.Replace(layeredFixture(), []byte("8BIMnorm"), []byte("8BIM"+mode), 1)
			if _, err := InspectLayers(context.Background(), bytes.NewReader(fixture)); err != ErrUnsupportedPreview {
				t.Fatalf("inspect unsupported blend %q: got %v, want %v", mode, err, ErrUnsupportedPreview)
			}
		})
	}
}

func TestLayerPreviewLinearDodge(t *testing.T) {
	fixture := layeredFixture()
	// The second layer is the top (blue) layer; the first is the red backdrop.
	copy(fixture[bytes.LastIndex(fixture, []byte("8BIMnorm")):], "8BIMlddg")
	meta, err := InspectLayers(context.Background(), bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	overrides := make(map[int]bool)
	for _, l := range meta.Layers {
		overrides[l.ID] = true
	}
	data, err := RenderLayers(context.Background(), bytes.NewReader(fixture), overrides)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := color.NRGBAModel.Convert(img.At(0, 0)), (color.NRGBA{R: 255, B: 255, A: 255}); got != want {
		t.Fatalf("Linear Dodge over blue: got %v, want %v", got, want)
	}
}

func layeredFixture() []byte {
	var file, info bytes.Buffer
	u16 := func(b *bytes.Buffer, v uint16) { _ = binary.Write(b, binary.BigEndian, v) }
	u32 := func(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.BigEndian, v) }
	section := func(b *bytes.Buffer, s []byte) { u32(b, uint32(len(s))); b.Write(s) }
	file.WriteString("8BPS")
	u16(&file, 1)
	file.Write(make([]byte, 6))
	u16(&file, 3)
	u32(&file, 2)
	u32(&file, 2)
	u16(&file, 8)
	u16(&file, 3)
	section(&file, nil)
	section(&file, nil)

	u16(&info, 2)
	for _, layer := range []struct {
		name  string
		flags byte
	}{{"red", 0}, {"blu", 2}} {
		for _, edge := range []uint32{0, 0, 2, 2} {
			u32(&info, edge)
		}
		u16(&info, 4)
		for _, id := range []uint16{0, 1, 2, 65535} {
			u16(&info, id)
			u32(&info, 6)
		}
		info.WriteString("8BIMnorm")
		info.Write([]byte{255, 0, layer.flags, 0})
		var extra bytes.Buffer
		section(&extra, nil)
		section(&extra, nil)
		extra.WriteByte(3)
		extra.WriteString(layer.name)
		section(&info, extra.Bytes())
	}
	for _, v := range []byte{255, 0, 0, 255, 0, 0, 255, 255} {
		u16(&info, 0)
		info.Write([]byte{v, v, v, v})
	}
	var layers bytes.Buffer
	section(&layers, info.Bytes())
	section(&layers, nil)
	section(&file, layers.Bytes())
	u16(&file, 0)
	file.Write(make([]byte, 12))
	return file.Bytes()
}

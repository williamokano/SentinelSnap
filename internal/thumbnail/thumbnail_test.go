package thumbnail_test

import (
	"bytes"
	"image"
	_ "image/jpeg"
	"image/png"
	"testing"

	"github.com/williamokano/sentinelsnap/internal/thumbnail"
)

func makePNG(w, h int) *bytes.Buffer {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return &buf
}

func TestEncode_downscales(t *testing.T) {
	src := makePNG(800, 600)
	var out bytes.Buffer
	if err := thumbnail.Encode(src, &out, 200); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, _, err := image.Decode(&out)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > 200 || b.Dy() > 200 {
		t.Errorf("expected thumbnail ≤ 200×200, got %d×%d", b.Dx(), b.Dy())
	}
	// Aspect ratio preserved: 800/600 = 4/3, so 200×150.
	if b.Dx() != 200 || b.Dy() != 150 {
		t.Errorf("expected 200×150, got %d×%d", b.Dx(), b.Dy())
	}
}

func TestEncode_noUpscale(t *testing.T) {
	src := makePNG(100, 80)
	var out bytes.Buffer
	if err := thumbnail.Encode(src, &out, 400); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, _, err := image.Decode(&out)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > 100 || b.Dy() > 80 {
		t.Errorf("should not upscale: got %d×%d", b.Dx(), b.Dy())
	}
}

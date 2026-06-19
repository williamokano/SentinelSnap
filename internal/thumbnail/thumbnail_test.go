package thumbnail_test

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
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

// jpegWithOrientation encodes a w×h JPEG and splices in an EXIF APP1 segment
// carrying the given Orientation value, mimicking what a phone camera writes.
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Content is irrelevant to these tests (they assert dimensions); a solid
	// fill keeps the JPEG valid.
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 120, G: 80, B: 40, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	jb := buf.Bytes()

	// Big-endian ("MM") TIFF with a single IFD0 entry: Orientation (0x0112),
	// type SHORT, count 1, value `orientation`.
	exif := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, // TIFF header, IFD0 at offset 8
		0x00, 0x01, // 1 directory entry
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, byte(orientation >> 8), byte(orientation), 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // next IFD offset = 0
	}
	segLen := len(exif) + 2
	app1 := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}, exif...)

	// Insert APP1 right after the SOI marker (the first two bytes, 0xFFD8).
	out := append([]byte{}, jb[:2]...)
	out = append(out, app1...)
	return append(out, jb[2:]...)
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

// Orientation 6 means "rotate 90° clockwise to display". A 400×200 landscape
// source must therefore come out as a 200×400 portrait thumbnail.
func TestEncode_appliesExifOrientation(t *testing.T) {
	src := jpegWithOrientation(t, 400, 200, 6)
	var out bytes.Buffer
	if err := thumbnail.Encode(bytes.NewReader(src), &out, 400); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, _, err := image.Decode(&out)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 200 || b.Dy() != 400 {
		t.Errorf("orientation 6 should swap to 200×400 portrait, got %d×%d", b.Dx(), b.Dy())
	}
}

// Without an orientation tag the dimensions are left untouched.
func TestEncode_orientationNoneKeepsDimensions(t *testing.T) {
	src := jpegWithOrientation(t, 400, 200, 1)
	var out bytes.Buffer
	if err := thumbnail.Encode(bytes.NewReader(src), &out, 400); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, _, err := image.Decode(&out)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 400 || b.Dy() != 200 {
		t.Errorf("orientation 1 should keep 400×200, got %d×%d", b.Dx(), b.Dy())
	}
}

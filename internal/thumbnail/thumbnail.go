// Package thumbnail generates a JPEG thumbnail from an image reader.
// It uses only the standard library: image/* for decode and image/jpeg for encode.
// Scaling uses a simple area-average (box) filter which is fast and produces
// acceptable quality for small target sizes (≤ 500 px).
//
// JPEG inputs carrying an EXIF Orientation tag (as phone cameras do — they
// store the sensor's raw landscape pixels plus a tag telling viewers how to
// rotate) are corrected: Go's image decoders ignore EXIF, so a re-encoded
// thumbnail would otherwise appear rotated relative to the browser-rendered
// original. The tag is parsed and the rotation/flip is baked into the output.
package thumbnail

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
)

// Encode decodes an image from r, scales it to fit within maxSide × maxSide
// (preserving aspect ratio, never upscaling), applies any EXIF orientation,
// and writes a JPEG to w. If the source already fits within maxSide it is
// re-encoded as-is (still orientation-corrected).
func Encode(r io.Reader, w io.Writer, maxSide int) error {
	// The bytes are needed twice — once to decode the pixels, once to read the
	// EXIF orientation that the image decoders discard. Buffering is cheap
	// relative to the decoded bitmap already held in memory.
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("thumbnail: read: %w", err)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("thumbnail: decode: %w", err)
	}

	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return fmt.Errorf("thumbnail: empty image")
	}

	scale := 1.0
	if sw > maxSide {
		scale = float64(maxSide) / float64(sw)
	}
	if float64(sh)*scale > float64(maxSide) {
		scale = float64(maxSide) / float64(sh)
	}

	var out image.Image
	if scale >= 1.0 {
		out = src
	} else {
		dw := int(math.Round(float64(sw) * scale))
		dh := int(math.Round(float64(sh) * scale))
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
		out = boxScale(src, dw, dh)
	}

	// Rotate/flip last, on the already-downscaled (small) image — cheap, and
	// keeps the box filter sampling the original pixel grid.
	out = applyOrientation(out, exifOrientation(data))

	return jpeg.Encode(w, out, &jpeg.Options{Quality: 82})
}

// boxScale scales src to dw×dh using a box (area-average) filter.
// Each destination pixel averages the source pixels that map to its area,
// producing better quality than nearest-neighbour for downscaling.
func boxScale(src image.Image, dw, dh int) *image.NRGBA {
	sb := src.Bounds()
	sw := float64(sb.Dx())
	sh := float64(sb.Dy())
	xRatio := sw / float64(dw)
	yRatio := sh / float64(dh)

	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))

	for dy := 0; dy < dh; dy++ {
		srcY0 := float64(dy) * yRatio
		srcY1 := srcY0 + yRatio
		for dx := 0; dx < dw; dx++ {
			srcX0 := float64(dx) * xRatio
			srcX1 := srcX0 + xRatio

			var rSum, gSum, bSum, aSum, weight float64

			// Iterate over the integer-aligned source pixels that overlap the
			// sampling box, weighting by the area of the overlap.
			iy0 := int(srcY0)
			iy1 := int(math.Ceil(srcY1))
			ix0 := int(srcX0)
			ix1 := int(math.Ceil(srcX1))

			for sy := iy0; sy < iy1; sy++ {
				wy := overlapWeight(float64(sy), float64(sy+1), srcY0, srcY1)
				if wy == 0 {
					continue
				}
				py := sb.Min.Y + sy
				if py < sb.Min.Y || py >= sb.Max.Y {
					continue
				}
				for sx := ix0; sx < ix1; sx++ {
					wx := overlapWeight(float64(sx), float64(sx+1), srcX0, srcX1)
					if wx == 0 {
						continue
					}
					px := sb.Min.X + sx
					if px < sb.Min.X || px >= sb.Max.X {
						continue
					}
					w := wx * wy
					r, g, b, a := src.At(px, py).RGBA()
					// RGBA() returns pre-multiplied 16-bit values; normalise to [0,1].
					fa := float64(a) / 0xffff
					var fr, fg, fb float64
					if a > 0 {
						fr = float64(r) / float64(a)
						fg = float64(g) / float64(a)
						fb = float64(b) / float64(a)
					}
					rSum += fr * w
					gSum += fg * w
					bSum += fb * w
					aSum += fa * w
					weight += w
				}
			}

			var c color.NRGBA
			if weight > 0 {
				c = color.NRGBA{
					R: clampUint8(rSum / weight * 255),
					G: clampUint8(gSum / weight * 255),
					B: clampUint8(bSum / weight * 255),
					A: clampUint8(aSum / weight * 255),
				}
			}
			dst.SetNRGBA(dx, dy, c)
		}
	}
	return dst
}

// overlapWeight returns the length of the overlap between [a,b) and [lo,hi).
func overlapWeight(a, b, lo, hi float64) float64 {
	start := math.Max(a, lo)
	end := math.Min(b, hi)
	if end <= start {
		return 0
	}
	return end - start
}

func clampUint8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// exifOrientation returns the EXIF Orientation value (1–8) for a JPEG, or 1
// (no transform needed) for non-JPEG input or when the tag is absent or
// malformed. It walks the JPEG marker segments to find APP1/Exif and reads
// the orientation tag from IFD0.
func exifOrientation(data []byte) int {
	const normal = 1
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return normal // not a JPEG
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return normal
		}
		marker := data[i+1]
		// Start of scan / end of image: no more metadata segments follow.
		if marker == 0xDA || marker == 0xD9 {
			return normal
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 {
			return normal
		}
		segStart := i + 4
		segEnd := i + 2 + segLen
		if segEnd > len(data) {
			return normal
		}
		if marker == 0xE1 { // APP1 — may hold the Exif block
			if o, ok := orientationFromExif(data[segStart:segEnd]); ok {
				return o
			}
		}
		i = segEnd
	}
	return normal
}

// orientationFromExif reads the Orientation tag (0x0112) from an APP1 payload.
func orientationFromExif(p []byte) (int, bool) {
	if len(p) < 6 || string(p[0:6]) != "Exif\x00\x00" {
		return 0, false
	}
	tiff := p[6:]
	if len(tiff) < 8 {
		return 0, false
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	ifdOff := int(bo.Uint32(tiff[4:8]))
	if ifdOff < 8 || ifdOff+2 > len(tiff) {
		return 0, false
	}
	count := int(bo.Uint16(tiff[ifdOff : ifdOff+2]))
	entry := ifdOff + 2
	for k := 0; k < count; k++ {
		if entry+12 > len(tiff) {
			return 0, false
		}
		if bo.Uint16(tiff[entry:entry+2]) == 0x0112 { // Orientation, SHORT
			v := int(bo.Uint16(tiff[entry+8 : entry+10]))
			if v >= 1 && v <= 8 {
				return v, true
			}
			return 0, false
		}
		entry += 12
	}
	return 0, false
}

// applyOrientation returns img transformed per the EXIF orientation value.
// Values: 1 none, 2 flip-H, 3 rot180, 4 flip-V, 5 transpose, 6 rot90-CW,
// 7 transverse, 8 rot90-CCW.
func applyOrientation(img image.Image, o int) image.Image {
	switch o {
	case 2:
		return remap(img, false, func(x, y, w, h int) (int, int) { return w - 1 - x, y })
	case 3:
		return remap(img, false, func(x, y, w, h int) (int, int) { return w - 1 - x, h - 1 - y })
	case 4:
		return remap(img, false, func(x, y, w, h int) (int, int) { return x, h - 1 - y })
	case 5:
		return remap(img, true, func(x, y, w, h int) (int, int) { return y, x })
	case 6:
		return remap(img, true, func(x, y, w, h int) (int, int) { return h - 1 - y, x })
	case 7:
		return remap(img, true, func(x, y, w, h int) (int, int) { return h - 1 - y, w - 1 - x })
	case 8:
		return remap(img, true, func(x, y, w, h int) (int, int) { return y, w - 1 - x })
	default:
		return img
	}
}

// remap builds a new image by sending each source pixel (x,y) to the
// destination coordinate returned by dst. When swap is true the output's
// width and height are exchanged (the 90°/270° and transpose cases).
func remap(src image.Image, swap bool, dst func(x, y, w, h int) (int, int)) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if swap {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx, ny := dst(x, y, w, h)
			out.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

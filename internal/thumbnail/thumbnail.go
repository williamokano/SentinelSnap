// Package thumbnail generates a JPEG thumbnail from an image reader.
// It uses only the standard library: image/* for decode and image/jpeg for encode.
// Scaling uses a simple area-average (box) filter which is fast and produces
// acceptable quality for small target sizes (≤ 500 px).
package thumbnail

import (
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
// (preserving aspect ratio, never upscaling), and writes a JPEG to w.
// If the source already fits within maxSide, it is re-encoded as-is.
func Encode(r io.Reader, w io.Writer, maxSide int) error {
	src, _, err := image.Decode(r)
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

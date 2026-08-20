package render

import (
	"image"
	"image/color"

	xdraw "golang.org/x/image/draw"
)

// scaleSquare resizes src to a size x size RGBA using high-quality resampling,
// center-cropping to a square first so artwork is never distorted.
func scaleSquare(src image.Image, size int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// Center-crop to square.
	side := w
	if h < w {
		side = h
	}
	ox := b.Min.X + (w-side)/2
	oy := b.Min.Y + (h-side)/2
	cropRect := image.Rect(ox, oy, ox+side, oy+side)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, xdraw.Over, nil)
	return dst
}

// averageColor returns the mean RGB of an image by sampling a grid of pixels.
func averageColor(img image.Image) color.RGBA {
	b := img.Bounds()
	if b.Empty() {
		return color.RGBA{0x6d, 0x6d, 0x72, 0xff}
	}
	const steps = 24
	sx := max1(b.Dx() / steps)
	sy := max1(b.Dy() / steps)
	var rSum, gSum, bSum, n uint64
	for y := b.Min.Y; y < b.Max.Y; y += sy {
		for x := b.Min.X; x < b.Max.X; x += sx {
			r, g, bl, _ := img.At(x, y).RGBA()
			rSum += uint64(r >> 8)
			gSum += uint64(g >> 8)
			bSum += uint64(bl >> 8)
			n++
		}
	}
	if n == 0 {
		return color.RGBA{0x6d, 0x6d, 0x72, 0xff}
	}
	return color.RGBA{uint8(rSum / n), uint8(gSum / n), uint8(bSum / n), 0xff}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// scaleColor multiplies an RGB color by f (clamped) keeping alpha opaque.
func scaleColor(c color.RGBA, f float64) color.RGBA {
	return color.RGBA{
		R: clamp8(float64(c.R) * f),
		G: clamp8(float64(c.G) * f),
		B: clamp8(float64(c.B) * f),
		A: 0xff,
	}
}

// mixColor linearly interpolates between a and b by t in [0,1].
func mixColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: clamp8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: clamp8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: clamp8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 0xff,
	}
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

package styleimage

import "image"

// reduce writes the part sr of src into dst, which is no larger: each
// pixel of dst is the average of the part of src it covers, partly
// covered pixels counting for the part they cover. Averaging is what a
// large reduction needs to keep thin lines, such as a logo's, whole.
//
// It reads src a row at a time and keeps three rows of dst's width beside
// dst, so it costs next to nothing over the decoded picture. A kernel
// scaler such as golang.org/x/image/draw's keeps a buffer of dst's width
// by src's height at 32 bytes a pixel, which for a large source is more
// than the decoded picture itself.
//
// The average is taken of premultiplied colour, so a transparent pixel's
// colour, which nobody sees, does not bleed into its neighbours.
func reduce(dst *image.NRGBA, src image.Image, sr image.Rectangle) {
	tw, th := dst.Bounds().Dx(), dst.Bounds().Dy()
	kw, kh := sr.Dx(), sr.Dy()
	at := pixels(src)

	// Positions are in units where a source column is tw wide and a
	// destination column kw wide, so every boundary is a whole number. A
	// source column falls in one destination column or across the
	// boundary of two.
	type share struct {
		col       int
		near, far float32
	}
	shares := make([]share, kw)
	for x := range shares {
		lo, hi := int64(x)*int64(tw), int64(x+1)*int64(tw)
		col := int(lo / int64(kw))
		edge := int64(col+1) * int64(kw)
		if hi <= edge || col+1 >= tw {
			shares[x] = share{col: min(col, tw-1), near: float32(tw) / float32(kw)}
		} else {
			shares[x] = share{col: col, near: float32(edge-lo) / float32(kw), far: float32(hi-edge) / float32(kw)}
		}
	}

	row := make([]float32, 4*tw)
	current := make([]float32, 4*tw)
	next := make([]float32, 4*tw)
	out := 0
	for y := 0; y < kh && out < th; y++ {
		clear(row)
		for x := 0; x < kw; x++ {
			r, g, b, a := at(sr.Min.X+x, sr.Min.Y+y)
			s := shares[x]
			i := 4 * s.col
			row[i] += s.near * r
			row[i+1] += s.near * g
			row[i+2] += s.near * b
			row[i+3] += s.near * a
			if s.far != 0 {
				row[i+4] += s.far * r
				row[i+5] += s.far * g
				row[i+6] += s.far * b
				row[i+7] += s.far * a
			}
		}
		// The same in the other direction: a source row is th high, a
		// destination row kh.
		lo, hi := int64(y)*int64(th), int64(y+1)*int64(th)
		edge := int64(out+1) * int64(kh)
		near, far := float32(th)/float32(kh), float32(0)
		if hi > edge {
			near, far = float32(edge-lo)/float32(kh), float32(hi-edge)/float32(kh)
		}
		for i, v := range row {
			current[i] += near * v
			next[i] += far * v
		}
		if hi >= edge || y == kh-1 {
			put(dst, out, current)
			current, next = next, current
			clear(next)
			out++
		}
	}
}

// put writes a row of premultiplied averages, in sixteen bit units, as a
// row of dst.
func put(dst *image.NRGBA, y int, row []float32) {
	pix := dst.Pix[dst.PixOffset(dst.Bounds().Min.X, dst.Bounds().Min.Y+y):]
	for i := 0; i+3 < len(row); i += 4 {
		a := row[i+3]
		if a <= 0 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = 0, 0, 0, 0
			continue
		}
		pix[i] = channel(row[i] / a * 255)
		pix[i+1] = channel(row[i+1] / a * 255)
		pix[i+2] = channel(row[i+2] / a * 255)
		pix[i+3] = channel(a / 257)
	}
}

func channel(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}

// pixels reads a picture's pixels as premultiplied sixteen bit colour.
// Every picture the decoders return has RGBA64At, which reads a pixel
// without allocating.
func pixels(src image.Image) func(x, y int) (r, g, b, a float32) {
	if p, ok := src.(image.RGBA64Image); ok {
		return func(x, y int) (float32, float32, float32, float32) {
			c := p.RGBA64At(x, y)
			return float32(c.R), float32(c.G), float32(c.B), float32(c.A)
		}
	}
	return func(x, y int) (float32, float32, float32, float32) {
		r, g, b, a := src.At(x, y).RGBA()
		return float32(r), float32(g), float32(b), float32(a)
	}
}

// Command stylefixtures draws the pictures the style tests and the
// gallery upload (testdata/style): logos of three shapes, a banner
// photograph, and a photograph that carries a location and is stored on
// its side, as a phone saves one. They are drawn here, from plain shapes,
// so that nothing in the repository is anybody's mark or anybody's
// photograph.
//
//	go run ./tools/stylefixtures
//	cwebp -quiet testdata/style/logo-square.png -o testdata/style/logo-square.webp
//	magick testdata/style/banner.jpg -resize 1200x -interlace JPEG -quality 82 testdata/style/banner-progressive.jpg
//
// The last two lines are by hand, once: Go's standard library and
// golang.org/x/image read a WebP and a progressive JPEG and write
// neither.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

func main() {
	dir := filepath.Join("testdata", "style")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	write("logo-square.png", encodePNG(logo(400, 400)))
	write("logo-wide.png", encodePNG(logo(900, 240)))
	write("logo-tall.png", encodePNG(logo(240, 720)))
	write("banner.jpg", encodeJPEG(landscape(4000, 2250), 82))
	// As a phone held upright saves it: the pixels lie on their side,
	// and the file says to turn them a quarter clockwise. It also says
	// where it was taken.
	write("photo-located-sideways.jpg", withEXIF(encodeJPEG(sideways(landscape(900, 1200)), 85), 6))
	// A banner far wider than it is tall: the part kept of it is small,
	// and cutting it must not copy the rest.
	write("banner-very-wide.jpg", encodeJPEG(landscape(24000, 400), 82))
	// Files that are small and whose decoding is not: a plain sixteen bit
	// picture of 25 megapixels, which decodes at eight bytes a pixel, and
	// the frame header of a progressive CMYK JPEG of the same size, whose
	// decoder would keep every coefficient of four components. The header
	// is enough, since it is refused on its header.
	var vast bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&vast, image.NewNRGBA64(image.Rect(0, 0, 5000, 5000))); err != nil {
		log.Fatal(err)
	}
	write("vast-16bit.png", vast.Bytes())
	write("vast-progressive-cmyk.jpg", progressiveCMYKHeader(5000, 5000))
}

// progressiveCMYKHeader is a JPEG's start, a progressive frame header of
// four components at full sampling, the header of its first scan (a
// decoder reading only the header looks that far to learn how four
// components are to be read), and its end.
func progressiveCMYKHeader(w, h int) []byte {
	out := []byte{0xff, 0xd8, 0xff, 0xc2, 0x00, 8 + 3*4, 8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 4}
	for id := byte(1); id <= 4; id++ {
		out = append(out, id, 0x11, 0)
	}
	out = append(out, 0xff, 0xda, 0x00, 6+2*4, 4)
	for id := byte(1); id <= 4; id++ {
		out = append(out, id, 0)
	}
	out = append(out, 0, 0, 0)
	return append(out, 0xff, 0xd9)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(img image.Image, quality int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

var (
	ink   = color.NRGBA{0x18, 0x22, 0x30, 0xff}
	amber = color.NRGBA{0xe0, 0x9a, 0x2b, 0xff}
)

// logo is a mark of plain shapes on a transparent ground: a ring with a
// dot in it, and beside or under it three bars standing in for a name.
// It is drawn dark, as most logos are, which is what the plate behind a
// logo is for in the dark display mode.
func logo(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	side := min(w, h)
	cx, cy := float64(side)/2, float64(side)/2
	outer, inner, dot := float64(side)*0.42, float64(side)*0.30, float64(side)*0.14
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			switch {
			case d <= dot:
				img.SetNRGBA(x, y, amber)
			case d <= outer && d >= inner:
				img.SetNRGBA(x, y, ink)
			}
		}
	}
	bar := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if image.Pt(x, y).In(img.Bounds()) {
					img.SetNRGBA(x, y, ink)
				}
			}
		}
	}
	switch {
	case w > h: // the bars run beside the ring
		left := side + side/8
		for i, length := range []float64{1, 0.72, 0.5} {
			top := h/4 + i*h/5
			bar(left, top, left+int(float64(w-left-side/8)*length), top+h/10)
		}
	case h > w: // the bars run under it
		for i, length := range []float64{1, 0.72, 0.5} {
			top := side + side/6 + i*(h-side)/4
			bar(w/8, top, w/8+int(float64(w-w/4)*length), top+(h-side)/9)
		}
	}
	return img
}

// landscape is a picture with a sky, a sun and three ranges of hills,
// which reads as a photograph at a glance and has a plain top and bottom,
// so that which part a crop kept can be seen.
func landscape(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	mix := func(a, b color.NRGBA, t float64) color.NRGBA {
		f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
		return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
	}
	skyTop, skyLow := color.NRGBA{0x2c, 0x5a, 0x8c, 0xff}, color.NRGBA{0xf2, 0xd9, 0xb0, 0xff}
	sun := color.NRGBA{0xff, 0xf1, 0xc9, 0xff}
	hills := []color.NRGBA{{0x6f, 0x8f, 0x9b, 0xff}, {0x3f, 0x6b, 0x6a, 0xff}, {0x1f, 0x45, 0x44, 0xff}}
	fw, fh := float64(w), float64(h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/fw, float64(y)/fh
			c := mix(skyTop, skyLow, math.Min(fy/0.62, 1))
			if d := math.Hypot((fx-0.7)*fw, (fy-0.42)*fh) / fh; d < 0.09 {
				c = sun
			} else if d < 0.16 {
				c = mix(sun, c, (d-0.09)/0.07)
			}
			for i, hill := range hills {
				n := float64(i)
				ridge := 0.5 + 0.11*n + 0.05*math.Sin(fx*(5+3*n)+n*1.7) + 0.025*math.Sin(fx*(13+5*n)+n)
				if fy > ridge {
					c = hill
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// sideways lays an upright picture on its side, the way a camera held
// upright stores it: what was the top is at the right.
func sideways(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.SetNRGBA(y, w-1-x, src.NRGBAAt(x, y))
		}
	}
	return dst
}

// withEXIF puts an Exif segment after a JPEG's first marker: an
// orientation, and a GPS directory with a latitude and a longitude. The
// place is a made up one in the sea.
func withEXIF(jpg []byte, orientation uint16) []byte {
	var tiff bytes.Buffer
	le := binary.LittleEndian
	put := func(v any) { _ = binary.Write(&tiff, le, v) }
	entry := func(tag, kind uint16, count, value uint32) {
		put(tag)
		put(kind)
		put(count)
		put(value)
	}
	tiff.WriteString("II")
	put(uint16(42))
	put(uint32(8))
	// The first directory: the orientation, and where the GPS one is.
	const gpsAt = 8 + 2 + 2*12 + 4
	put(uint16(2))
	entry(0x0112, 3, 1, uint32(orientation))
	entry(0x8825, 4, 1, gpsAt)
	put(uint32(0))
	// The GPS directory: N 1°2'3", E 4°5'6".
	const valuesAt = gpsAt + 2 + 4*12 + 4
	put(uint16(4))
	entry(0x0001, 2, 2, uint32('N'))
	entry(0x0002, 5, 3, valuesAt)
	entry(0x0003, 2, 2, uint32('E'))
	entry(0x0004, 5, 3, valuesAt+24)
	put(uint32(0))
	for _, n := range []uint32{1, 2, 3, 4, 5, 6} {
		put(n)
		put(uint32(1))
	}
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := []byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{}, jpg[:2]...)
	out = append(out, segment...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

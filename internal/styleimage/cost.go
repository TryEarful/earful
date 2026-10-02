package styleimage

import (
	"image"
	"image/color"
	"math"
)

// decodeCost is what decoding a picture will allocate, in bytes, worked
// out from its header the way each decoder allocates: the decoded picture
// itself, and whatever the decoder keeps beside it until it is done. It
// errs on the side of more, since its use is to refuse a picture before
// it can take the memory of the whole instance.
func decodeCost(kind string, b []byte, config image.Config) (int64, error) {
	w, h := float64(config.Width), float64(config.Height)
	switch kind {
	case kindJPEG:
		frame, ok := jpegFrame(b)
		// A header of another size than the decoder reported is not the
		// one it will decode by, so what it says cannot be trusted.
		if !ok || frame.width != config.Width || frame.height != config.Height {
			return 0, ErrUnreadable
		}
		return frame.cost(config), nil
	case kindPNG:
		cost := w * h * bytesPerPixel(config.ColorModel)
		// An interlaced PNG is decoded pass by pass, each pass a picture of
		// its own, and the passes are then laid into the whole one.
		if len(b) > 28 && b[28] == 1 {
			cost *= 2
		}
		return ceil(cost), nil
	default:
		switch config.ColorModel {
		case color.YCbCrModel:
			// A lossy WebP is a 4:2:0 picture in whole macroblocks.
			return ceil(padded(w, 16) * padded(h, 16) * 1.5), nil
		case color.NYCbCrAModel:
			// The same with an alpha plane, which may itself be stored
			// losslessly and is then decoded at four bytes a pixel first.
			return ceil(padded(w, 16)*padded(h, 16)*1.5 + w*h*5), nil
		default:
			// A lossless WebP is decoded at four bytes a pixel, and its
			// transforms may hold a second picture of that size.
			return ceil(w * h * 8), nil
		}
	}
}

// bytesPerPixel is what a decoded picture of a colour model takes.
func bytesPerPixel(model color.Model) float64 {
	switch model {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := model.(color.Palette); ok {
		return 1
	}
	return 4
}

// jpegComponent is one component of a JPEG frame, with its sampling.
type jpegComponent struct{ h, v int }

// jpegHeader is what a JPEG's frame header says about decoding it.
type jpegHeader struct {
	width, height int
	progressive   bool
	components    []jpegComponent
}

// jpegFrame reads a JPEG's frame header: the segments before it are
// skipped by their lengths, as the decoder skips them. It must end each
// segment where image/jpeg does, or the two could find different frame
// headers, so wherever the decoder reads a file differently it gives up:
// on stray bytes between segments, which the decoder passes over, and on
// the markers that carry no length, which it steps past without reading
// one.
func jpegFrame(b []byte) (jpegHeader, bool) {
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xff {
			return jpegHeader{}, false
		}
		marker := b[i+1]
		// A run of 0xff is padding before a marker.
		if marker == 0xff {
			i++
			continue
		}
		// An escaped 0xff, the temporary marker and the restart markers.
		if marker == 0x00 || marker == 0x01 || 0xd0 <= marker && marker <= 0xd7 {
			return jpegHeader{}, false
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if size < 2 || i+2+size > len(b) {
			return jpegHeader{}, false
		}
		segment := b[i+4 : i+2+size]
		switch marker {
		case 0xc0, 0xc1, 0xc2:
			// Precision, height, width, the count of components, then
			// three bytes for each: its id, its sampling, its table.
			if len(segment) < 6 {
				return jpegHeader{}, false
			}
			n := int(segment[5])
			if n == 0 || len(segment) < 6+3*n {
				return jpegHeader{}, false
			}
			header := jpegHeader{
				height:      int(segment[1])<<8 | int(segment[2]),
				width:       int(segment[3])<<8 | int(segment[4]),
				progressive: marker == 0xc2,
			}
			for c := 0; c < n; c++ {
				sampling := segment[6+3*c+1]
				hs, vs := int(sampling>>4), int(sampling&0x0f)
				if hs == 0 || vs == 0 {
					return jpegHeader{}, false
				}
				header.components = append(header.components, jpegComponent{h: hs, v: vs})
			}
			return header, true
		case 0xda, 0xd9:
			// The scan data, or the end, before any frame header.
			return jpegHeader{}, false
		}
		i += 2 + size
	}
	return jpegHeader{}, false
}

// cost follows image/jpeg: each component is a plane of bytes at its own
// sampling, in whole blocks; a progressive frame also keeps every
// component's coefficients, 64 of four bytes for each block of 64 pixels,
// until its last scan; four components are turned into a CMYK picture
// after decoding, and a JPEG of RGB rather than YCbCr into an RGBA one.
func (f jpegHeader) cost(config image.Config) int64 {
	hmax, vmax := 1, 1
	for _, c := range f.components {
		hmax, vmax = max(hmax, c.h), max(vmax, c.v)
	}
	w, h := float64(config.Width), float64(config.Height)
	area := padded(w, 8*float64(hmax)) * padded(h, 8*float64(vmax))
	planes := 0.0
	for _, c := range f.components {
		planes += area * float64(c.h*c.v) / float64(hmax*vmax)
	}
	cost := planes
	if f.progressive {
		cost += 4 * planes
	}
	if len(f.components) == 4 || config.ColorModel == color.RGBAModel {
		cost += 4 * w * h
	}
	return ceil(cost)
}

// padded rounds n up to a whole number of units.
func padded(n, unit float64) float64 { return math.Ceil(n/unit) * unit }

func ceil(f float64) int64 { return int64(math.Ceil(f)) }

// Package styleimage turns a picture a creator uploads into the picture
// Earful stores and serves (ADR-0018): identified by its content, checked
// against its slot's limits, decoded, turned upright, scaled down and
// encoded again. Nothing of the uploaded file survives but its pixels, so
// what a respondent's browser fetches carries no metadata: a photograph's
// location, its camera and its embedded thumbnail stay with the creator.
//
// It is domain logic with no HTTP and no database in it. A Slot says what
// one place a picture goes needs of it, so a further place (the picture
// on the thanks page, a picture in a question, ADR-0021) is one more Slot
// and no new code.
package styleimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// The types a stored picture can have. An upload may also be a WebP; it
// is stored as one of these two, which every browser has always drawn.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
)

// maxPixels bounds the picture's size. A file of a megabyte can declare
// dimensions that would take gigabytes to decode; the dimensions are read
// before any pixel is, and a picture past this is refused. 25 megapixels
// is above what a phone's camera saves within a slot's byte limit.
const maxPixels = 25_000_000

// MaxDecodeBytes bounds the memory one decode may take. Pixels alone do
// not say it: a progressive JPEG keeps every coefficient of every
// component until its last scan, and a sixteen bit PNG takes eight bytes
// a pixel, so a small file within maxPixels can still decode to more
// than the instance has. Decode cost works out what the decoder will
// allocate from the file's header, and a picture past this is refused
// before any of it is. It is what maxPixels allows at four bytes a pixel,
// so every ordinary picture the pixel limit admitted is still admitted.
const MaxDecodeBytes = 96 << 20

// jpegQuality is the quality a JPEG is stored at: above it a banner
// grows with no visible gain, below it flat colour starts to ring.
const jpegQuality = 85

// Slot is one place a picture goes, and what that place needs.
type Slot struct {
	// MaxUpload is the largest file accepted, in bytes.
	MaxUpload int64
	// MaxWidth and MaxHeight bound the stored picture. A larger one is
	// scaled down to fit; a smaller one is never scaled up.
	MaxWidth, MaxHeight int
	// Crop is the shape the picture is cut to, as width over height,
	// about its centre, before it is scaled. Zero keeps the picture's
	// own shape.
	CropWidth, CropHeight int
	// Photo stores the picture as a JPEG on a white ground whatever it
	// came as. Otherwise a picture with any transparency is a PNG, and
	// one without is whichever of the two is smaller.
	Photo bool
}

var (
	// Banner is the strip at the head of a respondent's page. It is cut
	// to the three to one the page draws it at, so what is stored is
	// what is shown and no more: the export holds the picture a
	// respondent saw, and a tall photograph does not cost its unseen
	// height in every page view.
	Banner = Slot{MaxUpload: 2 << 20, MaxWidth: 1600, MaxHeight: 1600, CropWidth: 3, CropHeight: 1, Photo: true}
	// Logo is the mark on the plate. It keeps its shape, since a logo
	// cut to fit is no longer the logo, and its transparency.
	Logo = Slot{MaxUpload: 1 << 20, MaxWidth: 512, MaxHeight: 512}
	// Thanks is the picture above the thanks page's heading, in place of
	// the owl. It keeps its shape and its transparency, as a logo does,
	// and is larger, since it stands alone on the page.
	Thanks = Slot{MaxUpload: 1 << 20, MaxWidth: 800, MaxHeight: 800}
)

// Why a picture is refused. Each is the creator's to fix, and says so in
// the words they are shown.
var (
	// ErrTooLarge: the file is over its slot's byte limit.
	ErrTooLarge = errors.New("the file is over its size limit")
	// ErrType: the file is not a PNG, a JPEG or a WebP. What a file is
	// is read from its content, never from its name: an SVG is a
	// document that can carry script and fetch from other origins, and
	// a GIF may be an animation.
	ErrType = errors.New("use a PNG, JPEG or WebP image")
	// ErrDimensions: the picture declares more pixels than are decoded.
	ErrDimensions = errors.New("that image has too many pixels, so use a smaller one")
	// ErrUnreadable: the file says it is an image and does not decode as
	// one.
	ErrUnreadable = errors.New("that file could not be read as an image")
)

// Image is a picture as it is stored and served.
type Image struct {
	Bytes       []byte
	ContentType string
	Width       int
	Height      int
	// SHA256 is the hash of Bytes, in hex: the picture's address.
	SHA256 string
}

// Ext is the file extension of the stored type, with its dot.
func (i Image) Ext() string { return Ext(i.ContentType) }

// Ext is the file extension of a stored type, with its dot.
func Ext(contentType string) string {
	if contentType == TypeJPEG {
		return ".jpg"
	}
	return ".png"
}

// Cost checks an uploaded file as far as its header goes, and returns
// what decoding it will take in bytes of memory, so that a caller can
// wait for that much room before calling Prepare. A file Prepare would
// refuse before decoding is refused here with the same error.
func Cost(upload []byte, slot Slot) (int64, error) {
	_, cost, err := check(upload, slot)
	return cost, err
}

// check is Cost, with the format it read.
func check(upload []byte, slot Slot) (string, int64, error) {
	if int64(len(upload)) > slot.MaxUpload {
		return "", 0, ErrTooLarge
	}
	kind := sniff(upload)
	if kind == "" {
		return "", 0, ErrType
	}
	config, err := decodeConfig(kind, upload)
	if err != nil {
		return "", 0, ErrUnreadable
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxPixels/config.Height {
		return "", 0, ErrDimensions
	}
	cost, err := decodeCost(kind, upload, config)
	if err != nil {
		return "", 0, err
	}
	if cost > MaxDecodeBytes {
		return "", 0, ErrDimensions
	}
	return kind, cost, nil
}

// Prepare makes the stored picture from an uploaded file.
func Prepare(upload []byte, slot Slot) (Image, error) {
	kind, _, err := check(upload, slot)
	if err != nil {
		return Image{}, err
	}
	src, err := decode(kind, upload)
	if err != nil {
		return Image{}, ErrUnreadable
	}
	orientation := 1
	if kind == kindJPEG {
		orientation = jpegOrientation(upload)
	}
	out := shape(src, orientation, slot)
	return encode(out, slot)
}

const (
	kindPNG  = "png"
	kindJPEG = "jpeg"
	kindWebP = "webp"
)

// sniff names the format by the file's first bytes, or "" for anything
// that is not one of the three accepted.
func sniff(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return kindPNG
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return kindJPEG
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return kindWebP
	}
	return ""
}

func decodeConfig(kind string, b []byte) (image.Config, error) {
	r := bytes.NewReader(b)
	switch kind {
	case kindPNG:
		return png.DecodeConfig(r)
	case kindJPEG:
		return jpeg.DecodeConfig(r)
	default:
		return webp.DecodeConfig(r)
	}
}

func decode(kind string, b []byte) (image.Image, error) {
	r := bytes.NewReader(b)
	switch kind {
	case kindPNG:
		return png.Decode(r)
	case kindJPEG:
		return jpeg.Decode(r)
	default:
		return webp.Decode(r)
	}
}

// shape turns the decoded picture into the one to store: cut to the
// slot's shape, scaled down to the slot's size, and turned upright. The
// cut comes first, as a window on the decoded picture, and the scaling
// (reduce) writes only the kept part at its stored size, reading the
// decoded picture a row at a time, so nothing near the decoded picture's
// size is allocated a second time; the turning comes last and copies the
// small picture. The sizes are worked out for the upright picture, which
// is the one the limits are about.
func shape(src image.Image, orientation int, slot Slot) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// The upright picture's size.
	uw, uh := w, h
	if orientation >= 5 {
		uw, uh = h, w
	}
	// The part of the upright picture that is kept.
	cw, ch := uw, uh
	if slot.CropWidth > 0 && slot.CropHeight > 0 {
		if cw*slot.CropHeight > ch*slot.CropWidth {
			cw = ch * slot.CropWidth / slot.CropHeight
		} else {
			ch = cw * slot.CropHeight / slot.CropWidth
		}
		cw, ch = max(cw, 1), max(ch, 1)
	}
	// The same part in the decoded picture: the cut is about the centre,
	// which every orientation keeps where it is, so only its sides swap.
	kw, kh := cw, ch
	if orientation >= 5 {
		kw, kh = ch, cw
	}
	kept := image.Rect(0, 0, kw, kh).Add(b.Min).Add(image.Pt((w-kw)/2, (h-kh)/2))
	// How much the kept part shrinks to fit the slot. Never above one.
	num, den := 1, 1
	if cw*slot.MaxHeight > ch*slot.MaxWidth {
		if cw > slot.MaxWidth {
			num, den = slot.MaxWidth, cw
		}
	} else if ch > slot.MaxHeight {
		num, den = slot.MaxHeight, ch
	}
	scale := func(n int) int { return max((n*num+den/2)/den, 1) }

	scaled := image.NewNRGBA(image.Rect(0, 0, scale(kw), scale(kh)))
	if num == den {
		draw.Draw(scaled, scaled.Bounds(), src, kept.Min, draw.Src)
	} else {
		reduce(scaled, src, kept)
	}
	return orient(scaled, orientation)
}

// orient applies an EXIF orientation (1 to 8) to a picture, so that it
// is stored the way up it was taken. A camera records which way it was
// held rather than turning the pixels, and the record is metadata, which
// is not kept; without this a photograph taken upright would be served
// on its side.
func orient(src *image.NRGBA, orientation int) *image.NRGBA {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // mirrored left to right
				dx, dy = w-1-x, y
			case 3: // turned half way
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored top to bottom
				dx, dy = x, h-1-y
			case 5: // mirrored about the falling diagonal
				dx, dy = y, x
			case 6: // a quarter turn clockwise brings it upright
				dx, dy = h-1-y, x
			case 7: // mirrored about the rising diagonal
				dx, dy = h-1-y, w-1-x
			case 8: // a quarter turn anticlockwise brings it upright
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return dst
}

// encode writes the picture in the type its slot stores. The encoders
// write pixels and nothing else.
func encode(img *image.NRGBA, slot Slot) (Image, error) {
	var out []byte
	contentType := TypePNG
	switch {
	case slot.Photo:
		b, err := encodeJPEG(img)
		if err != nil {
			return Image{}, err
		}
		out, contentType = b, TypeJPEG
	case !opaque(img):
		b, err := encodePNG(img)
		if err != nil {
			return Image{}, err
		}
		out = b
	default:
		asPNG, err := encodePNG(img)
		if err != nil {
			return Image{}, err
		}
		asJPEG, err := encodeJPEG(img)
		if err != nil {
			return Image{}, err
		}
		out = asPNG
		if len(asJPEG) < len(asPNG) {
			out, contentType = asJPEG, TypeJPEG
		}
	}
	sum := sha256.Sum256(out)
	return Image{
		Bytes:       out,
		ContentType: contentType,
		Width:       img.Bounds().Dx(),
		Height:      img.Bounds().Dy(),
		SHA256:      hex.EncodeToString(sum[:]),
	}, nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeJPEG lays the picture on white first: a JPEG has no transparency,
// and the encoder would otherwise write whatever colour lies under a
// transparent pixel, which is usually black.
func encodeJPEG(img *image.NRGBA) ([]byte, error) {
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func opaque(img *image.NRGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}

// jpegOrientation reads the EXIF orientation of a JPEG: 1 to 8, and 1
// where the file does not say. It walks the segments before the picture
// to the Exif one and reads the one tag it needs from the first
// directory; anything it does not understand is "does not say".
func jpegOrientation(b []byte) int {
	const upright = 1
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xff {
			return upright
		}
		marker := b[i+1]
		// The picture's own data starts here; EXIF comes before it.
		if marker == 0xda || marker == 0xd9 {
			return upright
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if size < 2 || i+2+size > len(b) {
			return upright
		}
		segment := b[i+4 : i+2+size]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		i += 2 + size
	}
	return upright
}

func tiffOrientation(t []byte) int {
	const upright = 1
	if len(t) < 8 {
		return upright
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[0:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[1]) | int(b[0])<<8 }
		u32 = func(b []byte) int { return int(b[3]) | int(b[2])<<8 | int(b[1])<<16 | int(b[0])<<24 }
	default:
		return upright
	}
	offset := u32(t[4:8])
	if offset < 8 || offset+2 > len(t) {
		return upright
	}
	entries := u16(t[offset : offset+2])
	for e := 0; e < entries; e++ {
		at := offset + 2 + e*12
		if at+12 > len(t) {
			return upright
		}
		if u16(t[at:at+2]) == 0x0112 {
			if v := u16(t[at+8 : at+10]); v >= 1 && v <= 8 {
				return v
			}
			return upright
		}
	}
	return upright
}

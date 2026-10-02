package styleimage_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/TryEarful/earful/internal/styleimage"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "style", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func decodeStored(t *testing.T, img styleimage.Image) image.Image {
	t.Helper()
	var decoded image.Image
	var err error
	switch img.ContentType {
	case styleimage.TypePNG:
		decoded, err = png.Decode(bytes.NewReader(img.Bytes))
	case styleimage.TypeJPEG:
		decoded, err = jpeg.Decode(bytes.NewReader(img.Bytes))
	default:
		t.Fatalf("stored as %q", img.ContentType)
	}
	if err != nil {
		t.Fatalf("the stored picture does not decode: %v", err)
	}
	if decoded.Bounds().Dx() != img.Width || decoded.Bounds().Dy() != img.Height {
		t.Fatalf("stored as %dx%d, said to be %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy(), img.Width, img.Height)
	}
	return decoded
}

func TestBanner_APhotographIsCutToThreeToOneAndScaledDown(t *testing.T) {
	t.Parallel()
	img, err := styleimage.Prepare(fixture(t, "banner.jpg"), styleimage.Banner)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	decodeStored(t, img)
	if img.ContentType != styleimage.TypeJPEG {
		t.Errorf("a banner is stored as %q, want a JPEG", img.ContentType)
	}
	if img.Width != 1600 || img.Height != 533 {
		t.Errorf("stored at %dx%d, want 1600x533", img.Width, img.Height)
	}
	if len(img.SHA256) != 64 {
		t.Errorf("hash %q is not a SHA-256 in hex", img.SHA256)
	}
}

func TestBanner_ASmallPictureIsNotScaledUp(t *testing.T) {
	t.Parallel()
	small := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		t.Fatal(err)
	}
	img, err := styleimage.Prepare(buf.Bytes(), styleimage.Banner)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if img.Width != 600 || img.Height != 200 {
		t.Errorf("stored at %dx%d, want 600x200", img.Width, img.Height)
	}
}

func TestBanner_ATransparentPictureIsLaidOnWhite(t *testing.T) {
	t.Parallel()
	img, err := styleimage.Prepare(fixture(t, "logo-wide.png"), styleimage.Banner)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	r, g, b, _ := decodeStored(t, img).At(1, 1).RGBA()
	if r>>8 < 0xf0 || g>>8 < 0xf0 || b>>8 < 0xf0 {
		t.Errorf("a transparent corner was stored as %02x%02x%02x, want white", r>>8, g>>8, b>>8)
	}
}

func TestPhotograph_IsTurnedUprightAndLosesItsLocation(t *testing.T) {
	t.Parallel()
	upload := fixture(t, "photo-located-sideways.jpg")
	if !bytes.Contains(upload, []byte("Exif")) {
		t.Fatal("the fixture carries no EXIF, so the test would prove nothing")
	}
	img, err := styleimage.Prepare(upload, styleimage.Logo)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if bytes.Contains(img.Bytes, []byte("Exif")) {
		t.Error("the stored picture still carries EXIF")
	}
	// The fixture is 1200 wide and 900 high on its side: upright it is
	// taller than wide, with the sky at the top.
	if img.Width >= img.Height {
		t.Fatalf("stored at %dx%d, want it upright (taller than wide)", img.Width, img.Height)
	}
	decoded := decodeStored(t, img)
	_, _, topBlue, _ := decoded.At(img.Width/4, 2).RGBA()
	_, _, bottomBlue, _ := decoded.At(img.Width/4, img.Height-3).RGBA()
	topRed, _, _, _ := decoded.At(img.Width/4, 2).RGBA()
	if topBlue <= topRed || topBlue <= bottomBlue {
		t.Error("the sky is not at the top: the picture was not turned upright")
	}
}

func TestLogo_KeepsItsShapeAndItsTransparency(t *testing.T) {
	t.Parallel()
	for name, want := range map[string][2]int{
		"logo-square.png": {400, 400},
		"logo-wide.png":   {512, 137},
		"logo-tall.png":   {171, 512},
	} {
		img, err := styleimage.Prepare(fixture(t, name), styleimage.Logo)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if img.ContentType != styleimage.TypePNG {
			t.Errorf("%s: stored as %q, want a PNG, which keeps transparency", name, img.ContentType)
		}
		if img.Width != want[0] || img.Height != want[1] {
			t.Errorf("%s: stored at %dx%d, want %dx%d", name, img.Width, img.Height, want[0], want[1])
		}
		if _, _, _, a := decodeStored(t, img).At(0, 0).RGBA(); a != 0 {
			t.Errorf("%s: the corner is no longer transparent", name)
		}
	}
}

func TestLogo_AWebPIsStoredAsAPNG(t *testing.T) {
	t.Parallel()
	img, err := styleimage.Prepare(fixture(t, "logo-square.webp"), styleimage.Logo)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	decodeStored(t, img)
	if img.ContentType != styleimage.TypePNG {
		t.Errorf("stored as %q, want a PNG", img.ContentType)
	}
}

func TestPrepare_TheSamePictureHasTheSameAddress(t *testing.T) {
	t.Parallel()
	a, err := styleimage.Prepare(fixture(t, "logo-square.png"), styleimage.Logo)
	if err != nil {
		t.Fatal(err)
	}
	b, err := styleimage.Prepare(fixture(t, "logo-square.png"), styleimage.Logo)
	if err != nil {
		t.Fatal(err)
	}
	if a.SHA256 != b.SHA256 {
		t.Error("the same upload was stored under two addresses")
	}
}

func TestPrepare_Refusals(t *testing.T) {
	t.Parallel()

	var animation bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}

	// A picture of plain colour declares 30 megapixels in a few kilobytes.
	var vast bytes.Buffer
	if err := png.Encode(&vast, image.NewGray(image.Rect(0, 0, 6000, 5000))); err != nil {
		t.Fatal(err)
	}

	// Noise does not compress, so this is over a megabyte.
	noise := image.NewNRGBA(image.Rect(0, 0, 700, 700))
	random := rand.New(rand.NewPCG(1, 2))
	for i := range noise.Pix {
		noise.Pix[i] = byte(random.IntN(256))
	}
	var heavy bytes.Buffer
	if err := png.Encode(&heavy, noise); err != nil {
		t.Fatal(err)
	}

	cut := fixture(t, "banner.jpg")[:4000]

	for name, c := range map[string]struct {
		upload []byte
		want   error
	}{
		"an SVG":                     {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), styleimage.ErrType},
		"a GIF":                      {animation.Bytes(), styleimage.ErrType},
		"text":                       {[]byte("this is not a picture"), styleimage.ErrType},
		"nothing":                    {nil, styleimage.ErrType},
		"too many pixels":            {vast.Bytes(), styleimage.ErrDimensions},
		"over the byte limit":        {heavy.Bytes(), styleimage.ErrTooLarge},
		"a JPEG cut short":           {cut, styleimage.ErrUnreadable},
		"a PNG header and no more":   {[]byte("\x89PNG\r\n\x1a\n"), styleimage.ErrUnreadable},
		"a WebP header and no more":  {[]byte("RIFF\x00\x00\x00\x00WEBP"), styleimage.ErrUnreadable},
		"a JPEG marker and no more":  {[]byte{0xff, 0xd8, 0xff, 0xe0}, styleimage.ErrUnreadable},
		"a PNG named by its content": {append([]byte("\x89PNG\r\n\x1a\n"), []byte("but not really")...), styleimage.ErrUnreadable},
	} {
		if _, err := styleimage.Prepare(c.upload, styleimage.Logo); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

// A small file can declare a decoding far larger than its pixels say. Each
// of these is within the pixel limit and would take more memory than one
// decode is allowed, so it is refused on its header, before the decoder
// allocates anything.
func TestPrepare_AFileWhoseDecodingIsTooLargeIsRefusedOnItsHeader(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"vast-16bit.png", "vast-progressive-cmyk.jpg"} {
		upload := fixture(t, name)
		if _, err := styleimage.Cost(upload, styleimage.Logo); !errors.Is(err, styleimage.ErrDimensions) {
			t.Errorf("%s: cost refused with %v, want %v", name, err, styleimage.ErrDimensions)
		}
		if _, err := styleimage.Prepare(upload, styleimage.Logo); !errors.Is(err, styleimage.ErrDimensions) {
			t.Errorf("%s: refused with %v, want %v", name, err, styleimage.ErrDimensions)
		}
	}
}

// smallJPEG is an ordinary JPEG of a few pixels, as an encoder writes it.
func smallJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// afterStart is a JPEG with bytes laid in straight after its start marker.
func afterStart(jpg []byte, inserted ...byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	out = append(out, inserted...)
	return append(out, jpg[2:]...)
}

// The cost is read from the frame header, so it is only as good as
// finding the header the decoder will use. The decoder takes some markers
// to have no length after them, and passes over stray bytes between
// segments. A file with such a marker before its frame header could show
// the cost a different header from the one decoded, so it is refused;
// fill bytes before a marker, which both read alike, are not.
func TestCost_AMarkerWithNoLengthBeforeTheFrameHeaderIsRefused(t *testing.T) {
	t.Parallel()
	jpg := smallJPEG(t)
	// After each marker, two bytes that read as an empty segment if a length
	// is taken to follow, and as stray bytes if not. The decoder accepts the
	// first three files; the standard gives the last marker no length
	// either, and the decoder refuses it.
	for name, upload := range map[string][]byte{
		"a restart marker": afterStart(jpg, 0xff, 0xd0, 0x00, 0x02),
		"the last restart": afterStart(jpg, 0xff, 0xd7, 0x00, 0x02),
		"an escaped 0xff":  afterStart(jpg, 0xff, 0x00, 0x00, 0x02),
		"a temporary mark": afterStart(jpg, 0xff, 0x01, 0x00, 0x02),
	} {
		if _, err := styleimage.Cost(upload, styleimage.Logo); !errors.Is(err, styleimage.ErrUnreadable) {
			t.Errorf("%s: cost gave %v, want %v", name, err, styleimage.ErrUnreadable)
		}
	}
	for name, upload := range map[string][]byte{
		"as an encoder writes it": jpg,
		"with fill bytes":         afterStart(jpg, 0xff, 0xff),
	} {
		if _, err := styleimage.Cost(upload, styleimage.Logo); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := styleimage.Prepare(upload, styleimage.Logo); err != nil {
			t.Errorf("%s: prepare: %v", name, err)
		}
	}
}

// What an ordinary picture costs fits within one decode's allowance, a
// progressive photograph's included, so the limit refuses only what it
// is for.
func TestCost_OrdinaryPicturesAreWithinTheAllowance(t *testing.T) {
	t.Parallel()
	for name, slot := range map[string]styleimage.Slot{
		"banner.jpg":             styleimage.Banner,
		"banner-progressive.jpg": styleimage.Banner,
		"banner-very-wide.jpg":   styleimage.Banner,
		"logo-square.png":        styleimage.Logo,
		"logo-square.webp":       styleimage.Logo,
		"logo-wide.png":          styleimage.Logo,
	} {
		upload := fixture(t, name)
		cost, err := styleimage.Cost(upload, slot)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if cost <= 0 || cost > styleimage.MaxDecodeBytes {
			t.Errorf("%s: costs %d bytes, want between 1 and %d", name, cost, styleimage.MaxDecodeBytes)
		}
		if _, err := styleimage.Prepare(upload, slot); err != nil {
			t.Errorf("%s: prepare: %v", name, err)
		}
	}
}

// A banner keeps three to one of its picture. Of a very wide picture that
// is a small part, and the cut is made before anything is copied, so
// preparing it takes about its decoding and no second picture of the
// whole. Not parallel: it reads the process's allocation count.
func TestBanner_AVeryWidePictureIsCutBeforeAnythingIsCopied(t *testing.T) {
	upload := fixture(t, "banner-very-wide.jpg")
	cost, err := styleimage.Cost(upload, styleimage.Banner)
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	img, err := styleimage.Prepare(upload, styleimage.Banner)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if img.Width != 1200 || img.Height != 400 {
		t.Errorf("stored at %dx%d, want 1200x400", img.Width, img.Height)
	}
	// The decoding, and the kept part at four bytes a pixel for each of
	// the few copies the encoders make of it.
	allowed := cost + 8<<20
	if spent := int64(after.TotalAlloc - before.TotalAlloc); spent > allowed {
		t.Errorf("preparing took %d MB, want at most %d MB", spent>>20, allowed>>20)
	}
}

package styleimage

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"testing"
)

// The frame header the cost reads must be the one the decoder reads. When
// the two agree on where each segment ends they find the same header; this
// holds the cost to it besides, by refusing a header whose size is not the
// size the decoder reported.
func TestDecodeCost_AFrameHeaderOfAnotherSizeIsRefused(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCost(kindJPEG, b.Bytes(), config); err != nil {
		t.Fatalf("the header's own size: %v", err)
	}
	config.Width, config.Height = 4000, 4000
	if _, err := decodeCost(kindJPEG, b.Bytes(), config); !errors.Is(err, ErrUnreadable) {
		t.Errorf("another size: got %v, want %v", err, ErrUnreadable)
	}
}

package export_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/TryEarful/earful/internal/export"
)

// noise does not compress, so its size in the archive is its size.
func noise(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWriter_StopsAsSoonAsItPassesItsLimit: the pictures go into the
// archive as they are read, and the picture that takes it past its limit
// stops it there, before the rest of the workspace is read.
func TestWriter_StopsAsSoonAsItPassesItsLimit(t *testing.T) {
	t.Parallel()
	w := export.NewWriter(1 << 20)
	if err := w.Image(export.ImagePath("a", ".png"), noise(t, 600<<10)); err != nil {
		t.Fatalf("a picture under the limit: %v", err)
	}
	if err := w.Image(export.ImagePath("b", ".png"), noise(t, 600<<10)); !errors.Is(err, export.ErrTooLarge) {
		t.Fatalf("the picture past the limit: got %v, want %v", err, export.ErrTooLarge)
	}
}

// TestWriter_PicturesAreStoredAsTheyAre: a picture is compressed already,
// so the archive stores it without deflating it again; the documents
// beside it are deflated.
func TestWriter_PicturesAreStoredAsTheyAre(t *testing.T) {
	t.Parallel()
	w := export.NewWriter(1 << 20)
	path := export.ImagePath("abc", ".jpg")
	if err := w.Image(path, noise(t, 4<<10)); err != nil {
		t.Fatal(err)
	}
	built, err := w.Finish(export.Archive{FormatVersion: export.FormatVersion}, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(built), int64(len(built)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		want := uint16(zip.Deflate)
		if f.Name == path {
			want = zip.Store
		}
		if f.Method != want {
			t.Errorf("%s: method %d, want %d", f.Name, f.Method, want)
		}
	}
}

// TestWriter_APictureShownTwiceIsWrittenOnce: two versions may show the
// same picture, and the archive holds it once, at its address.
func TestWriter_APictureShownTwiceIsWrittenOnce(t *testing.T) {
	t.Parallel()
	w := export.NewWriter(1 << 20)
	path := export.ImagePath("abc", ".png")
	for range 2 {
		if err := w.Image(path, []byte("picture")); err != nil {
			t.Fatal(err)
		}
	}
	built, err := w.Finish(export.Archive{FormatVersion: export.FormatVersion}, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(built), int64(len(built)))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range zr.File {
		if f.Name == path {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the picture is in the archive %d times, want once", count)
	}
}

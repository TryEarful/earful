package uitext_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/text"
)

func written(t *testing.T) (en, es uitext.Localizer) {
	t.Helper()
	catalog, err := uitext.Load(text.FS, uitext.Options{Strict: true, Languages: []string{"en", "es"}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return catalog.Localizer("en"), catalog.Localizer("es")
}

// English dates were written by Go, from layouts. They are written from
// messages now, and come out as Go wrote them, in every month.
func TestEnglishDatesAreAsGoWroteThem(t *testing.T) {
	en, _ := written(t)
	for month := time.January; month <= time.December; month++ {
		for _, day := range []int{1, 9, 28} {
			at := time.Date(2026, month, day, 7, 5, 0, 0, time.UTC)
			for name, pair := range map[string][2]string{
				"Day":      {en.Day(at), at.Format("2 January 2006")},
				"DateTime": {en.DateTime(at), at.Format("2 Jan 2006, 15:04")},
				"ShortDay": {en.ShortDay(at), at.Format("2 Jan")},
			} {
				if pair[0] != pair[1] {
					t.Errorf("%s: %q, Go wrote %q", name, pair[0], pair[1])
				}
			}
		}
	}
}

func TestEnglishNumbersAreAsGoWroteThem(t *testing.T) {
	en, _ := written(t)
	for _, v := range []float64{0, 0.04, 3, 3.25, 9.96, -41.5, 66.666, 100} {
		for got, want := range map[string]string{
			en.Decimal(v, 1): fmt.Sprintf("%.1f", v),
			en.Signed(v, 0):  fmt.Sprintf("%+.0f", v),
			en.Euros(v):      fmt.Sprintf("€%.2f", v),
		} {
			if got != want {
				t.Errorf("%v: %q, Go wrote %q", v, got, want)
			}
		}
	}
	for _, n := range []int{0, 7, 42, 100} {
		if got, want := en.Percent(n), fmt.Sprintf("%d%%", n); got != want {
			t.Errorf("%q, Go wrote %q", got, want)
		}
	}
	for bytes, want := range map[int64]string{
		0: "", -1: "", 1: "1 KB", 3000: "3 KB", 1<<20 - 1: "1024 KB", 1 << 20: "1.0 MB", 5_400_000: "5.1 MB",
	} {
		if got := en.Size(bytes); got != want {
			t.Errorf("%d bytes: %q, want %q", bytes, got, want)
		}
	}
	for seconds, want := range map[int]string{0: "", -3: "", 45: "45s", 60: "1m 00s", 125: "2m 05s", 3599: "59m 59s"} {
		if got := en.Duration(seconds); got != want {
			t.Errorf("%d seconds: %q, want %q", seconds, got, want)
		}
	}
}

func TestSpanishDatesAndNumbers(t *testing.T) {
	_, es := written(t)
	at := time.Date(2026, time.September, 29, 15, 4, 0, 0, time.UTC)
	for got, want := range map[string]string{
		es.Day(at):          "29 de septiembre de 2026",
		es.DateTime(at):     "29 sept 2026, 15:04",
		es.ShortDay(at):     "29 sept",
		es.Decimal(3.25, 1): "3,2",
		es.Signed(41.5, 0):  "+42",
		es.Signed(-12, 0):   "-12",
		es.Percent(42):      "42 %",
		es.Euros(1.5):       "1,50 €",
		es.Size(5_400_000):  "5,1 MB",
		es.Size(3000):       "3 kB",
		es.Duration(125):    "2 min 05 s",
		es.Duration(45):     "45 s",
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}

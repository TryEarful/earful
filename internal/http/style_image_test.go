package http_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// The pictures of a survey's style (ADR-0018): a logo and a banner,
// uploaded on the Style tab, stored beside the survey and served from
// this origin.

func styleFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "style", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// pictureForm is the Style tab's form with its picture fields, as the
// page posts it: the words, the logo's alternative text, and no box
// ticked.
func pictureForm(alt string) url.Values {
	form := workshopStyle()
	form.Set("logo_alt", alt)
	return form
}

func postPictures(t *testing.T, app *apptest.App, creator *http.Client, id string, form url.Values, files ...apptest.Upload) (*http.Response, string) {
	t.Helper()
	resp := app.PostMultipart(t, creator, "/surveys/"+id+"/style", form, files...)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

func logoUpload(t *testing.T) apptest.Upload {
	return apptest.Upload{Field: "logo", Name: "logo.png", Data: styleFixture(t, "logo-square.png")}
}

// ownLogo is a logo no other test uploads: a square of random shades.
// A picture's address is the hash of its bytes, and the tests share a
// database, so a test about whether an address is served needs bytes
// that only it has stored.
func ownLogo(t *testing.T) apptest.Upload {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	if _, err := rand.Read(img.Pix); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return apptest.Upload{Field: "logo", Name: "logo.png", Data: buf.Bytes()}
}

func bannerUpload(t *testing.T) apptest.Upload {
	return apptest.Upload{Field: "banner", Name: "banner.jpg", Data: styleFixture(t, "banner.jpg")}
}

var (
	logoImgRe   = regexp.MustCompile(`<img[^>]*\bjs-style-logo\b[^>]*>`)
	bannerImgRe = regexp.MustCompile(`<img[^>]*\bjs-style-banner\b[^>]*>`)
	srcRe       = regexp.MustCompile(`src="([^"]+)"`)
	madeWithRe  = regexp.MustCompile(`(?s)<span class="[^"]*\bjs-made-with\b[^"]*">.*?</span>`)
)

// pictureSrc is the address of the first picture on a page that re
// finds, or empty.
func pictureSrc(page string, re *regexp.Regexp) string {
	m := srcRe.FindStringSubmatch(re.FindString(page))
	if m == nil {
		return ""
	}
	return m[1]
}

// fetch gets an address and returns the response with its body read.
func fetch(t *testing.T, client *http.Client, address string) (*http.Response, []byte) {
	t.Helper()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", address, err)
	}
	return resp, body
}

// styledSurvey is a survey with one question, published once with no
// style, so that what a draft holds and what respondents see can differ.
func styledSurvey(t *testing.T, app *apptest.App, creator *http.Client, title string) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, title, true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)
	return id
}

// TestStyleImages_PreviewedThenPublished: an uploaded logo and banner
// are the draft's. The Style tab and the preview show them at once, from
// the creator's own address; respondents see them, at the public
// address, only once a version is published with them, and until then
// the public address does not have them.
func TestStyleImages_PreviewedThenPublished(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images"))
	id := styledSurvey(t, app, creator, "With pictures")

	resp, saved := postPictures(t, app, creator, id, pictureForm("Corner Workshop"), ownLogo(t), bannerUpload(t))
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("saving pictures: status %d\n%s", resp.StatusCode, saved)
	}
	if got := strings.Count(saved, "js-style-image-current"); got != 2 {
		t.Errorf("the Style tab shows %d current pictures, want the logo and the banner", got)
	}
	if !strings.Contains(saved, `name="logo_alt" class="js-style-logo-alt" value="Corner Workshop"`) {
		t.Errorf("the Style tab does not show the logo's description again")
	}
	if !bodyContains(saved, "Only use a logo you have the right to use") {
		t.Errorf("the Style tab does not say whose logo may be used")
	}

	preview := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	logo, banner := pictureSrc(preview, logoImgRe), pictureSrc(preview, bannerImgRe)
	if !strings.HasPrefix(logo, "/surveys/"+id+"/style-image/") || !strings.HasPrefix(banner, "/surveys/"+id+"/style-image/") {
		t.Fatalf("the preview's pictures are at %q and %q, want the creator's address", logo, banner)
	}
	if !strings.Contains(logoImgRe.FindString(preview), `alt="Corner Workshop"`) {
		t.Errorf("the logo has no alternative text: %s", logoImgRe.FindString(preview))
	}
	if !strings.Contains(bannerImgRe.FindString(preview), `alt=""`) {
		t.Errorf("the banner is decoration and should have empty alternative text: %s", bannerImgRe.FindString(preview))
	}
	for _, address := range []string{logo, banner} {
		got, body := fetch(t, creator, app.Server.URL+address)
		if got.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("the creator cannot fetch %s: status %d", address, got.StatusCode)
		}
		// A draft's picture is kept only to ask again whether it is
		// current, which it is told without the picture being sent.
		if cache := got.Header.Get("Cache-Control"); cache != "private, no-cache" {
			t.Errorf("the creator's picture is cached as %q", cache)
		}
		again, err := http.NewRequest(http.MethodGet, app.Server.URL+address, nil)
		if err != nil {
			t.Fatal(err)
		}
		again.Header.Set("If-None-Match", got.Header.Get("ETag"))
		kept, err := creator.Do(again)
		if err != nil {
			t.Fatal(err)
		}
		kept.Body.Close()
		if kept.StatusCode != http.StatusNotModified {
			t.Errorf("a draft picture the creator's browser has: status %d, want 304", kept.StatusCode)
		}
	}

	// Nothing published shows them yet.
	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if pictureSrc(live, logoImgRe) != "" || pictureSrc(live, bannerImgRe) != "" {
		t.Fatalf("the draft's pictures reached respondents before they were published")
	}
	sha := logo[strings.LastIndex(logo, "/")+1:]
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+"/style-image/"+sha); got.StatusCode != http.StatusNotFound {
		t.Fatalf("a picture only the draft has is public: status %d", got.StatusCode)
	}
	// Nor is it found by asking whether a kept copy is current, or by
	// asking for its headers: whether it may be fetched is asked each time.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		ask, err := http.NewRequest(method, app.Server.URL+"/style-image/"+sha, nil)
		if err != nil {
			t.Fatal(err)
		}
		ask.Header.Set("If-None-Match", `"`+sha+`"`)
		resp, err := http.DefaultClient.Do(ask)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s with a tag for a draft's picture: status %d, want 404", method, resp.StatusCode)
		}
	}

	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing the pictures was refused:\n%s", body)
	}
	live = mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	logo, banner = pictureSrc(live, logoImgRe), pictureSrc(live, bannerImgRe)
	if !strings.HasPrefix(logo, "/style-image/") || !strings.HasPrefix(banner, "/style-image/") {
		t.Fatalf("the published pictures are at %q and %q, want the public address", logo, banner)
	}

	got, body := fetch(t, &http.Client{}, app.Server.URL+logo)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("the public logo: status %d", got.StatusCode)
	}
	if kind := got.Header.Get("Content-Type"); kind != "image/png" {
		t.Errorf("the logo is served as %q, want image/png", kind)
	}
	if cache := got.Header.Get("Cache-Control"); cache != "public, max-age=31536000, immutable" {
		t.Errorf("the public picture is cached as %q", cache)
	}
	if sniff := got.Header.Get("X-Content-Type-Options"); sniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", sniff)
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("the served logo does not decode: %v", err)
	}
	// A browser that has it asks again with the tag and is told to keep it.
	again, err := http.NewRequest(http.MethodGet, app.Server.URL+logo, nil)
	if err != nil {
		t.Fatal(err)
	}
	again.Header.Set("If-None-Match", got.Header.Get("ETag"))
	kept, err := http.DefaultClient.Do(again)
	if err != nil {
		t.Fatal(err)
	}
	kept.Body.Close()
	if kept.StatusCode != http.StatusNotModified {
		t.Errorf("a picture the browser has: status %d, want 304", kept.StatusCode)
	}
	// Its headers alone say its type and its size, and send no picture.
	head, err := http.Head(app.Server.URL + logo)
	if err != nil {
		t.Fatal(err)
	}
	headBody, _ := io.ReadAll(head.Body)
	head.Body.Close()
	if head.StatusCode != http.StatusOK || head.Header.Get("Content-Type") != "image/png" ||
		head.Header.Get("Content-Length") != strconv.Itoa(len(body)) || len(headBody) != 0 {
		t.Errorf("HEAD: status %d, type %q, length %q (the picture is %d bytes), %d bytes sent",
			head.StatusCode, head.Header.Get("Content-Type"), head.Header.Get("Content-Length"), len(body), len(headBody))
	}

	got, body = fetch(t, &http.Client{}, app.Server.URL+banner)
	if got.StatusCode != http.StatusOK || got.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("the public banner: status %d, type %q", got.StatusCode, got.Header.Get("Content-Type"))
	}
	stored, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the served banner does not decode: %v", err)
	}
	if size := stored.Bounds().Size(); size.X != 1600 || size.Y != 533 {
		t.Errorf("a 4000 pixel photograph is served at %dx%d, want 1600x533", size.X, size.Y)
	}
	if len(body) > 2<<20 {
		t.Errorf("the served banner is %d bytes", len(body))
	}
	// The page leaves the picture's room before it arrives.
	if tag := bannerImgRe.FindString(live); !strings.Contains(tag, `width="1600"`) || !strings.Contains(tag, `height="533"`) {
		t.Errorf("the banner's size is not written out: %s", tag)
	}
}

// TestStyleImages_PageStaysFirstParty is ADR-0006 with pictures on the
// page: a logo and a banner are fetched from this origin and no other.
func TestStyleImages_PageStaysFirstParty(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-origin"))
	id := styledSurvey(t, app, creator, "First party pictures")
	form := styleForm("ocean")
	form.Set("header_name", "Corner Workshop")
	form.Set("logo_alt", "Corner Workshop")
	postPictures(t, app, creator, id, form, logoUpload(t), bannerUpload(t))
	app.Publish(t, creator, id)

	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if pictureSrc(page, logoImgRe) == "" || pictureSrc(page, bannerImgRe) == "" {
		t.Fatalf("the page has no pictures, so the test would prove nothing")
	}
	for _, external := range findExternalURLs(page) {
		t.Errorf("a page with pictures references a third-party origin: %s", external)
	}
	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data:;") {
		t.Errorf("the policy for pictures changed: %s", csp)
	}
}

// TestStyleImages_OneMark: a survey with a logo of its own is named as
// Earful's in words alone; one without keeps the owl.
func TestStyleImages_OneMark(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-mark"))
	id := styledSurvey(t, app, creator, "One mark")

	// A banner alone is not a mark.
	postPictures(t, app, creator, id, pictureForm(""), bannerUpload(t))
	app.Publish(t, creator, id)
	page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if made := madeWithRe.FindString(page); !strings.Contains(made, "<svg") || !bodyContains(made, "Powered by Earful") {
		t.Fatalf("a survey with no logo lost the owl or the words:\n%s", made)
	}

	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), logoUpload(t))
	app.Publish(t, creator, id)
	respondent := jarClient(t)
	page = mustGet(t, respondent, app.Server.URL+"/s/"+id)
	made := madeWithRe.FindString(page)
	if strings.Contains(made, "<svg") {
		t.Errorf("a survey with its own logo still shows the owl:\n%s", made)
	}
	if !bodyContains(made, "Powered by Earful") {
		t.Errorf("a survey with its own logo no longer says it is powered by Earful:\n%s", made)
	}
	if !bodyContains(page, "Help") {
		t.Errorf("the footer lost its Help link")
	}
	// The footer shows the logo again, small, and does not say it twice.
	if footer := footerOf(page); !strings.Contains(footer, "js-style-footer-logo") || !strings.Contains(footer, `alt=""`) {
		t.Errorf("the creator's footer does not show the logo:\n%s", footer)
	}

	// The pages after the questions show the logo beside the name.
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "an answer")
	_, thanks := submitAfterReading(t, app, respondent, id, form)
	if header := headerOf(thanks); pictureSrc(header, logoImgRe) == "" || !bodyContains(header, "Corner Workshop") {
		t.Errorf("the thanks page's header lacks the logo or the name:\n%s", header)
	}
	if strings.Contains(headerOf(thanks), "js-style-banner") {
		t.Errorf("the thanks page shows the banner; its header is the compact one")
	}
}

// TestStyleImages_StoredWithoutMetadata: what is served is never the
// uploaded file. A photograph that says where it was taken, and lies on
// its side as a phone saves it, is served upright and says nothing.
func TestStyleImages_StoredWithoutMetadata(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-exif"))
	id := styledSurvey(t, app, creator, "Located")

	photograph := styleFixture(t, "photo-located-sideways.jpg")
	if !bytes.Contains(photograph, []byte("Exif")) {
		t.Fatal("the fixture carries no EXIF, so the test would prove nothing")
	}
	resp, saved := postPictures(t, app, creator, id, pictureForm("A hillside"),
		apptest.Upload{Field: "logo", Name: "IMG_0001.jpg", Data: photograph})
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("saving a photograph: status %d\n%s", resp.StatusCode, saved)
	}
	app.Publish(t, creator, id)

	page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	_, body := fetch(t, &http.Client{}, app.Server.URL+pictureSrc(page, logoImgRe))
	if bytes.Contains(body, []byte("Exif")) || bytes.Contains(body, []byte("GPS")) {
		t.Errorf("the served picture still carries metadata")
	}
	served, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the served picture does not decode: %v", err)
	}
	if size := served.Bounds().Size(); size.X >= size.Y {
		t.Errorf("served at %dx%d: the photograph was not turned upright", size.X, size.Y)
	}
}

// TestStyleImages_AWebPIsServedAsAPNG: a WebP is accepted and stored as a
// type every browser draws.
func TestStyleImages_AWebPIsServedAsAPNG(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-webp"))
	id := styledSurvey(t, app, creator, "WebP")

	resp, saved := postPictures(t, app, creator, id, pictureForm("Corner Workshop"),
		apptest.Upload{Field: "logo", Name: "logo.webp", Data: styleFixture(t, "logo-square.webp")})
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("saving a WebP: status %d\n%s", resp.StatusCode, saved)
	}
	got, body := fetch(t, creator, app.Server.URL+pictureSrc(saved, regexp.MustCompile(`<img[^>]*\bjs-style-image-current\b[^>]*>`)))
	if got.Header.Get("Content-Type") != "image/png" {
		t.Errorf("a WebP is served as %q, want image/png", got.Header.Get("Content-Type"))
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("the stored picture does not decode as a PNG: %v", err)
	}
}

// TestStyleImages_RefusesWhatItShouldNot: a file that is not a picture
// Earful stores, a picture too large, and a logo with no description are
// each answered with a message beside the field, and leave the draft as
// it was.
func TestStyleImages_RefusesWhatItShouldNot(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-refused"))
	id := styledSurvey(t, app, creator, "Refusals")

	var vast bytes.Buffer
	if err := png.Encode(&vast, image.NewGray(image.Rect(0, 0, 6000, 5000))); err != nil {
		t.Fatal(err)
	}
	// Noise does not compress: over a megabyte, and under the form's cap.
	noise := image.NewNRGBA(image.Rect(0, 0, 640, 640))
	seed := uint32(1)
	for i := range noise.Pix {
		seed = seed*1664525 + 1013904223
		noise.Pix[i] = byte(seed >> 24)
	}
	var heavy bytes.Buffer
	if err := png.Encode(&heavy, noise); err != nil {
		t.Fatal(err)
	}
	if heavy.Len() <= 1<<20 {
		t.Fatalf("the heavy fixture is only %d bytes", heavy.Len())
	}

	for name, c := range map[string]struct {
		upload apptest.Upload
		alt    string
		want   string
	}{
		"an SVG":          {apptest.Upload{Field: "logo", Name: "logo.svg", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)}, "A logo", "Logo: use a PNG, JPEG or WebP image"},
		"a GIF":           {apptest.Upload{Field: "logo", Name: "logo.gif", Data: []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")}, "A logo", "Logo: use a PNG, JPEG or WebP image"},
		"text named .png": {apptest.Upload{Field: "logo", Name: "logo.png", Data: []byte("this is not a picture")}, "A logo", "Logo: use a PNG, JPEG or WebP image"},
		"too many pixels": {apptest.Upload{Field: "banner", Name: "vast.png", Data: vast.Bytes()}, "", "Banner: that image has too many pixels"},
		"too many bytes":  {apptest.Upload{Field: "logo", Name: "heavy.png", Data: heavy.Bytes()}, "A logo", "Logo: keep the file under 1 MB"},
		"a broken JPEG":   {apptest.Upload{Field: "banner", Name: "cut.jpg", Data: styleFixture(t, "banner.jpg")[:5000]}, "", "Banner: that file could not be read as an image"},
		"no description":  {apptest.Upload{Field: "logo", Name: "logo.png", Data: styleFixture(t, "logo-square.png")}, "  ", "Logo description: say what the logo shows"},
	} {
		resp, page := postPictures(t, app, creator, id, pictureForm(c.alt), c.upload)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, resp.StatusCode)
		}
		if !bodyContains(page, c.want) {
			t.Errorf("%s: the page does not say %q:\n%s", name, c.want, page)
		}
		// The summary leads to the field, which is far below it on a phone.
		if !regexp.MustCompile(`<a class="js-style-error-link" href="#style-(banner|logo|invalid)"`).MatchString(page) ||
			!regexp.MustCompile(`id="style-(banner|logo|invalid)"[^>]*aria-invalid="true"|aria-invalid="true"[^>]*id="style-(banner|logo|invalid)"`).MatchString(strings.Join(strings.Fields(page), " ")) {
			t.Errorf("%s: the summary does not link to the field in error", name)
		}
		if !bodyContains(page, "Choose your images again") {
			t.Errorf("%s: the page does not say the file must be chosen again", name)
		}
		// What was typed is handed back.
		if !strings.Contains(page, `name="header_name" value="Corner Workshop"`) {
			t.Errorf("%s: the refused form lost what was typed", name)
		}
		if strings.Contains(page, "js-style-image-current") {
			t.Errorf("%s: a refused picture is shown as the current one", name)
		}
	}
	// Nothing of the refused forms was saved: not the pictures, not the words.
	tab := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style")
	if strings.Contains(tab, "js-style-image-current") || strings.Contains(tab, "Corner Workshop") {
		t.Errorf("a refused save changed the draft:\n%s", tab)
	}
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Publish version 2") {
		t.Errorf("a refused save left something to publish")
	}
}

// TestStyleImages_KeptReplacedRemoved: a field left empty keeps its
// picture, whatever else on the form changes; a new file replaces it; the
// box removes it.
func TestStyleImages_KeptReplacedRemoved(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-kept"))
	id := styledSurvey(t, app, creator, "Kept")
	currentRe := regexp.MustCompile(`<img[^>]*\bjs-style-image-current\b[^>]*>`)

	_, saved := postPictures(t, app, creator, id, pictureForm("Corner Workshop"), logoUpload(t), bannerUpload(t))
	first := currentRe.FindAllString(saved, -1)
	if len(first) != 2 {
		t.Fatalf("the Style tab shows %d pictures, want 2", len(first))
	}

	// The words change and no file is chosen: both pictures stay.
	form := pictureForm("Corner Workshop")
	form.Set("header_name", "Corner Workshop Cooperative")
	form.Set("theme", "forest")
	_, saved = postPictures(t, app, creator, id, form)
	if got := currentRe.FindAllString(saved, -1); len(got) != 2 || got[0] != first[0] || got[1] != first[1] {
		t.Fatalf("saving words changed the pictures:\n%v\nwant\n%v", got, first)
	}
	// A form with no picture fields at all, as an older page posts, keeps them too.
	saveStyle(t, app, creator, id, "slate")
	postStyle(t, app, creator, id, workshopStyle())
	if got := currentRe.FindAllString(mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style"), -1); len(got) != 2 {
		t.Fatalf("a form without picture fields dropped the pictures: %v", got)
	}

	// A new logo replaces the old one and leaves the banner.
	_, saved = postPictures(t, app, creator, id, pictureForm("Corner Workshop"),
		apptest.Upload{Field: "logo", Name: "wide.png", Data: styleFixture(t, "logo-wide.png")})
	replaced := currentRe.FindAllString(saved, -1)
	if len(replaced) != 2 || replaced[0] != first[0] || replaced[1] == first[1] {
		t.Fatalf("replacing the logo:\n%v\nwas\n%v", replaced, first)
	}

	// The box removes the banner; the logo stays.
	form = pictureForm("Corner Workshop")
	form.Set("banner_remove", "1")
	_, saved = postPictures(t, app, creator, id, form)
	if got := currentRe.FindAllString(saved, -1); len(got) != 1 || got[0] != replaced[1] {
		t.Fatalf("removing the banner left %v", got)
	}
	// Removing the logo removes the need for its description.
	form = pictureForm("")
	form.Set("logo_remove", "1")
	resp, saved := postPictures(t, app, creator, id, form)
	if resp.StatusCode != http.StatusOK || strings.Contains(saved, "js-style-image-current") {
		t.Fatalf("removing the logo: status %d\n%s", resp.StatusCode, saved)
	}
	preview := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	if pictureSrc(preview, logoImgRe) != "" || pictureSrc(preview, bannerImgRe) != "" {
		t.Errorf("the preview still shows a removed picture")
	}
}

// TestStyleImages_FrozenPerVersion: a response is thanked with the logo
// of the version it answered, and a version's pictures stay public after
// a later version replaces them.
func TestStyleImages_FrozenPerVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-frozen"))
	id := app.CreateSurvey(t, creator, "Frozen pictures", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), logoUpload(t))
	app.Publish(t, creator, id)

	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	first := pictureSrc(page, logoImgRe)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "from version 1")

	postPictures(t, app, creator, id, pictureForm("Corner Workshop"),
		apptest.Upload{Field: "logo", Name: "wide.png", Data: styleFixture(t, "logo-wide.png")})
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing a second logo was refused:\n%s", body)
	}

	_, thanks := submitAfterReading(t, app, respondent, id, form)
	if got := pictureSrc(thanks, logoImgRe); got != first {
		t.Errorf("a response to version 1 was thanked with %q, want version 1's logo %q", got, first)
	}
	second := pictureSrc(mustGet(t, jarClient(t), app.Server.URL+"/s/"+id), logoImgRe)
	if second == "" || second == first {
		t.Fatalf("version 2's logo is %q, version 1's %q", second, first)
	}
	for _, address := range []string{first, second} {
		if got, _ := fetch(t, &http.Client{}, app.Server.URL+address); got.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want both versions' logos public", address, got.StatusCode)
		}
	}
}

// TestStyleImages_NotFound: the public address has only what a published
// version of a survey that still exists shows, and the creator's address
// only the pictures of a survey in the creator's own workspace.
func TestStyleImages_NotFound(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-404"))
	id := styledSurvey(t, app, creator, "Found and not")
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), ownLogo(t))
	app.Publish(t, creator, id)

	public := pictureSrc(mustGet(t, jarClient(t), app.Server.URL+"/s/"+id), logoImgRe)
	sha := public[strings.LastIndex(public, "/")+1:]
	own := "/surveys/" + id + "/style-image/" + sha

	stranger := app.Login(t, apptest.UniqueEmail("style-images-stranger"))
	for name, c := range map[string]struct {
		client  *http.Client
		address string
	}{
		"another workspace, the creator's address": {stranger, own},
		"a hash nothing has":                       {&http.Client{}, "/style-image/" + strings.Repeat("0", 64)},
		"not a hash":                               {&http.Client{}, "/style-image/logo.png"},
		"a hash in capitals":                       {&http.Client{}, "/style-image/" + strings.ToUpper(sha)},
		"not a hash, the creator's address":        {creator, "/surveys/" + id + "/style-image/..%2f..%2fetc"},
	} {
		if got, _ := fetch(t, c.client, app.Server.URL+c.address); got.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, got.StatusCode)
		}
	}
	// Signed out, the creator's address asks for a sign in and shows nothing.
	if got, body := fetch(t, jarClient(t), app.Server.URL+own); got.Request.URL.Path != "/login" || bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Errorf("the creator's address served a picture to nobody: landed on %s", got.Request.URL.Path)
	}

	// A closed survey's page still shows whose it was.
	app.PostForm(t, creator, "/surveys/"+id+"/close", url.Values{}).Body.Close()
	closed := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if pictureSrc(closed, logoImgRe) != public {
		t.Errorf("the closed page does not show the logo:\n%s", headerOf(closed))
	}
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+public); got.StatusCode != http.StatusOK {
		t.Errorf("a closed survey's logo: status %d, want 200", got.StatusCode)
	}

	// A deleted survey's is public no longer.
	app.PostForm(t, creator, "/surveys/"+id+"/delete", url.Values{}).Body.Close()
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+public); got.StatusCode != http.StatusNotFound {
		t.Errorf("a deleted survey's logo: status %d, want 404", got.StatusCode)
	}
}

// TestStyleImages_FetchingIsNotAnOpen: a picture is fetched by everybody
// who opens the page and by anybody who has its address. It counts
// nothing (ADR-0003, ADR-0009).
func TestStyleImages_FetchingIsNotAnOpen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-stats"))
	id := styledSurvey(t, app, creator, "Counted")
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), logoUpload(t))
	app.Publish(t, creator, id)

	logo := pictureSrc(mustGet(t, jarClient(t), app.Server.URL+"/s/"+id), logoImgRe)
	before := extractStat(t, mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/stats"), "Opened")
	for range 5 {
		if got, _ := fetch(t, &http.Client{}, app.Server.URL+logo); got.StatusCode != http.StatusOK {
			t.Fatalf("fetching the logo: status %d", got.StatusCode)
		}
	}
	if after := extractStat(t, mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/stats"), "Opened"); after != before {
		t.Errorf("fetching a picture moved Opened from %d to %d", before, after)
	}
}

// TestStyleImages_AtMostTen: a survey stores ten pictures. Past that an
// upload first clears the ones nothing shows, and is refused only when
// every one is shown by a version or the draft.
func TestStyleImages_AtMostTen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-ten"))
	id := styledSurvey(t, app, creator, "Ten")

	// Each is a different picture: a square of its own shade.
	logo := func(n int) apptest.Upload {
		img := image.NewGray(image.Rect(0, 0, 32, 32))
		for i := range img.Pix {
			img.Pix[i] = uint8(n * 9)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return apptest.Upload{Field: "logo", Name: "logo" + strconv.Itoa(n) + ".png", Data: buf.Bytes()}
	}
	// Ten versions, each with its own logo: ten pictures, all shown.
	for n := 1; n <= 10; n++ {
		if resp, page := postPictures(t, app, creator, id, pictureForm("Logo "+strconv.Itoa(n)), logo(n)); resp.StatusCode != http.StatusOK {
			t.Fatalf("logo %d: status %d\n%s", n, resp.StatusCode, page)
		}
		app.Publish(t, creator, id)
	}
	resp, page := postPictures(t, app, creator, id, pictureForm("Logo 11"), logo(11))
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(page, "this survey already keeps 10 images") {
		t.Fatalf("an eleventh picture: status %d\n%s", resp.StatusCode, page)
	}
	// One of the ten again is not an eleventh.
	if resp, page := postPictures(t, app, creator, id, pictureForm("Logo 3"), logo(3)); resp.StatusCode != http.StatusOK {
		t.Fatalf("a picture already stored: status %d\n%s", resp.StatusCode, page)
	}

	// Pictures that were replaced before any version showed them make room.
	other := styledSurvey(t, app, creator, "Ten, unpublished")
	for n := 1; n <= 12; n++ {
		if resp, page := postPictures(t, app, creator, other, pictureForm("Logo "+strconv.Itoa(n)), logo(n)); resp.StatusCode != http.StatusOK {
			t.Fatalf("draft logo %d: status %d\n%s", n, resp.StatusCode, page)
		}
	}
}

// TestStyleImages_Localized: the logo's description is translated with
// the style's other words, and a reader in that language hears it.
func TestStyleImages_Localized(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-lang"))
	id := app.CreateSurvey(t, creator, "Described", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	form := styleForm("")
	form.Set("logo_alt", "Corner Workshop logo")
	if resp, page := postPictures(t, app, creator, id, form, logoUpload(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the logo: status %d\n%s", resp.StatusCode, page)
	}
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()

	languages := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(languages, "Logo description in Dutch") || !bodyContains(languages, "Corner Workshop logo") {
		t.Fatalf("the languages page does not ask for the logo's description:\n%s", languages)
	}
	// A language without the description is not published.
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{"t_" + identity: {"Iets?"}}).Body.Close()
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 1") {
		t.Fatalf("a language without the logo's description was published:\n%s", body)
	}
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity:  {"Iets?"},
		"style_logo_alt": {"Logo van Corner Workshop"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publishing was refused:\n%s", body)
	}

	dutch := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id+"?lang=nl")
	if tag := logoImgRe.FindString(dutch); !strings.Contains(tag, `alt="Logo van Corner Workshop"`) {
		t.Errorf("a Dutch reader's logo is described as: %s", tag)
	}
	english := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if tag := logoImgRe.FindString(english); !strings.Contains(tag, `alt="Corner Workshop logo"`) {
		t.Errorf("the logo as written is described as: %s", tag)
	}
}

// TestStyleImages_CrossWorkspaceDenied: nobody uploads a picture to a
// survey that is not theirs.
func TestStyleImages_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("style-images-owner"))
	id := styledSurvey(t, app, owner, "Not yours")
	stranger := app.Login(t, apptest.UniqueEmail("style-images-other"))

	resp, _ := postPictures(t, app, stranger, id, pictureForm("Somebody else"), logoUpload(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a stranger's upload: status %d, want 404", resp.StatusCode)
	}
	if tab := mustGet(t, owner, app.Server.URL+"/surveys/"+id+"/style"); strings.Contains(tab, "js-style-image-current") {
		t.Errorf("a stranger's upload reached the survey")
	}
}

// TestStyleImages_TooLargeAForm: a form over the route's cap is refused
// whole, with a page that says how large a form may be.
func TestStyleImages_TooLargeAForm(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-cap"))
	id := styledSurvey(t, app, creator, "Capped")

	resp, page := postPictures(t, app, creator, id, pictureForm("A logo"),
		apptest.Upload{Field: "logo", Name: "vast.png", Data: make([]byte, 7<<20)})
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !bodyContains(page, "6 MB") {
		t.Errorf("a 7 MB form: status %d\n%s", resp.StatusCode, page)
	}
}

// TestStyleImages_TurnedAwayWhileOthersAreSaved: while as many Style
// forms are being handled as the instance allows, another is refused at
// once, before its body is read, with a page that says to try again,
// and nothing of it is saved.
func TestStyleImages_TurnedAwayWhileOthersAreSaved(t *testing.T) {
	t.Parallel()
	full := semaphore.NewWeighted(1)
	if !full.TryAcquire(1) {
		t.Fatal("could not fill the bound")
	}
	app := apptest.New(t, apptest.Options{StyleSaves: full})
	creator := app.Login(t, apptest.UniqueEmail("style-images-busy"))
	id := styledSurvey(t, app, creator, "Busy")

	resp, page := postPictures(t, app, creator, id, pictureForm("A logo"), logoUpload(t))
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q, want 503 with a Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if !bodyContains(page, "save yours again in a moment") {
		t.Errorf("the refusal does not say to try again:\n%s", page)
	}
	if tab := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style"); strings.Contains(tab, "js-style-image-current") {
		t.Errorf("a refused form's logo was kept")
	}

	full.Release(1)
	resp, _ = postPictures(t, app, creator, id, pictureForm("A logo"), logoUpload(t))
	if resp.StatusCode >= 400 {
		t.Errorf("once there is room, status %d", resp.StatusCode)
	}
}

// holdStyleForm sends the head of a Style form as the creator and waits
// until the server asks for its body, which it does only once the form
// holds its places. The body is not sent; closing the connection ends
// the form.
func holdStyleForm(t *testing.T, app *apptest.App, creator *http.Client, id string) net.Conn {
	t.Helper()
	server, err := url.Parse(app.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", server.Host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var cookies []string
	for _, c := range creator.Jar.Cookies(server) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	head := "POST /surveys/" + id + "/style HTTP/1.1\r\n" +
		"Host: " + server.Host + "\r\n" +
		"Cookie: " + strings.Join(cookies, "; ") + "\r\n" +
		"Content-Type: multipart/form-data; boundary=held\r\n" +
		"Content-Length: 100000\r\n" +
		"Expect: 100-continue\r\n\r\n"
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, " 100 ") {
		t.Fatalf("the server did not ask for the body: %q, %v", line, err)
	}
	return conn
}

// saveStyleSoon posts a Style form until it is no longer turned away, or
// fails after a while: a form's places are given back as its handler
// returns, which may be a moment after its answer is read.
func saveStyleSoon(t *testing.T, app *apptest.App, creator *http.Client, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, page := postPictures(t, app, creator, id, pictureForm("A logo"), logoUpload(t))
		if resp.StatusCode < 400 {
			return
		}
		if resp.StatusCode != http.StatusServiceUnavailable || time.Now().After(deadline) {
			t.Fatalf("the form was not saved: status %d\n%s", resp.StatusCode, page)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestStyleImages_OneFormAtATimeEach: a creator with a Style form being
// handled is turned away from saving another, with a way back to the
// Style tab, while somebody else saves theirs; once the first form ends,
// the creator can save again.
func TestStyleImages_OneFormAtATimeEach(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-one-each"))
	id := styledSurvey(t, app, creator, "One at a time")
	other := app.Login(t, apptest.UniqueEmail("style-images-other"))
	otherID := styledSurvey(t, app, other, "Somebody else's")

	held := holdStyleForm(t, app, creator, id)
	resp, page := postPictures(t, app, creator, id, pictureForm("A logo"), logoUpload(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a second form at once: status %d, want 503\n%s", resp.StatusCode, page)
	}
	if !bodyContains(page, "choosing any images again") {
		t.Errorf("the refusal does not say to choose the images again:\n%s", page)
	}
	if !strings.Contains(page, `href="/surveys/`+id+`/style"`) || !strings.Contains(page, "js-error-back") {
		t.Errorf("the refusal has no way back to the Style tab:\n%s", page)
	}
	if resp, page := postPictures(t, app, other, otherID, pictureForm("A logo"), logoUpload(t)); resp.StatusCode >= 400 {
		t.Errorf("somebody else was turned away: status %d\n%s", resp.StatusCode, page)
	}

	_ = held.Close()
	saveStyleSoon(t, app, creator, id)
}

// TestStyleImages_ABodyThatNeverComesGivesUpItsPlace: a Style form whose
// body stops arriving is ended when its time is up, and gives back both
// its sender's place and its place among the forms handled at once.
func TestStyleImages_ABodyThatNeverComesGivesUpItsPlace(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{
		StyleSaves:    semaphore.NewWeighted(1),
		StyleBodyTime: 300 * time.Millisecond,
	})
	creator := app.Login(t, apptest.UniqueEmail("style-images-trickle"))
	id := styledSurvey(t, app, creator, "Trickled")

	held := holdStyleForm(t, app, creator, id)
	if _, err := io.WriteString(held, "--held\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = held.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(held).ReadString('\n')
	if err == nil && !strings.Contains(line, " 400 ") {
		t.Fatalf("a body that stopped: %q, want 400 or the connection closed", line)
	}

	// The only place among the forms at once, and the creator's own, are
	// free again.
	saveStyleSoon(t, app, creator, id)
}

// TestStyleImages_Exported: the workspace export holds each picture a
// version showed, once, and each version's style names its files.
func TestStyleImages_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-images-export"))
	id := app.CreateSurvey(t, creator, "Exported pictures", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), logoUpload(t), bannerUpload(t))
	app.Publish(t, creator, id)
	// A second version keeps the logo and drops the banner.
	form := pictureForm("Corner Workshop")
	form.Set("banner_remove", "1")
	postPictures(t, app, creator, id, form)
	app.Publish(t, creator, id)
	// A picture only the draft has is not in the archive.
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"),
		apptest.Upload{Field: "logo", Name: "wide.png", Data: styleFixture(t, "logo-wide.png")})

	link := waitForExport(t, app, creator)
	resp, raw := fetch(t, creator, app.Server.URL+link)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download: status %d", resp.StatusCode)
	}
	files := openArchive(t, raw)
	var archive export.Archive
	if err := json.Unmarshal(files["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	var pictures []string
	for name := range files {
		if strings.HasPrefix(name, "images/") {
			pictures = append(pictures, name)
		}
	}
	if len(pictures) != 2 {
		t.Fatalf("the archive holds %v, want the logo and the banner once each", pictures)
	}
	for _, survey := range archive.Surveys {
		if survey.ID != id {
			continue
		}
		if len(survey.Versions) != 2 {
			t.Fatalf("versions = %d, want 2", len(survey.Versions))
		}
		var logos []string
		for _, version := range survey.Versions {
			header := version.Style.Header
			if header == nil || header.Logo == nil {
				t.Fatalf("version %d has no logo: %+v", version.Number, version.Style)
			}
			logos = append(logos, header.Logo.File)
			if header.Logo.Alt != "Corner Workshop" || header.Logo.Width != 400 || header.Logo.Height != 400 {
				t.Errorf("version %d logo = %+v", version.Number, header.Logo)
			}
			if !strings.HasSuffix(header.Logo.File, ".png") {
				t.Errorf("version %d logo file = %q, want a .png", version.Number, header.Logo.File)
			}
			if _, err := png.Decode(bytes.NewReader(files[header.Logo.File])); err != nil {
				t.Errorf("version %d: %s is not in the archive as a PNG: %v", version.Number, header.Logo.File, err)
			}
			switch version.Number {
			case 1:
				if header.Banner == nil || !strings.HasSuffix(header.Banner.File, ".jpg") || header.Banner.Alt != "" {
					t.Fatalf("version 1 banner = %+v", header.Banner)
				}
				if _, err := jpeg.Decode(bytes.NewReader(files[header.Banner.File])); err != nil {
					t.Errorf("%s is not in the archive as a JPEG: %v", header.Banner.File, err)
				}
			case 2:
				if header.Banner != nil {
					t.Errorf("version 2 banner = %+v, want none", header.Banner)
				}
			}
		}
		if logos[0] != logos[1] {
			t.Errorf("two versions with one logo name two files: %v", logos)
		}
		return
	}
	t.Fatalf("the survey is missing from the export")
}

// colourAt is a pixel's colour, eight bits a channel.
func colourAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// TestThemeSheet_HasPictures: the theme sheet draws a logo and a banner
// of its own, so both are seen in every theme.
func TestThemeSheet_HasPictures(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	sheet := mustGet(t, &http.Client{}, app.Server.URL+"/dev/theme-sheet?theme=ocean")
	for name, re := range map[string]*regexp.Regexp{"logo": logoImgRe, "banner": bannerImgRe} {
		address := pictureSrc(sheet, re)
		got, body := fetch(t, &http.Client{}, app.Server.URL+address)
		if got.StatusCode != http.StatusOK {
			t.Fatalf("the sheet's %s at %q: status %d", name, address, got.StatusCode)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("the sheet's %s does not decode: %v", name, err)
		}
		if name == "logo" && colourAt(img, 0, 0).A != 0 {
			t.Errorf("the sheet's logo has no transparent ground, so it does not show what the plate is for")
		}
	}
}

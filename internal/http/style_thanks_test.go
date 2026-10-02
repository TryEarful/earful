package http_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// The picture on a survey's thanks page (ADR-0018): Earful's happy owl
// unless the style chooses one of the three drawings, the creator's own
// picture, or none.

var (
	thanksImageRe = regexp.MustCompile(`<img[^>]*\bjs-thanks-image\b[^>]*>`)
	drawingRe     = regexp.MustCompile(`data-drawing="([a-z]+)"`)
)

// thanksPictureOf names the picture above a thanks page's heading: "owl",
// a drawing's name, "image", or "none". Only the page's main is read, so
// the owl in Earful's footer is not counted.
func thanksPictureOf(page string) string {
	main := mainRe.FindString(page)
	switch {
	case strings.Contains(main, "owl-happy"):
		return "owl"
	case thanksImageRe.MatchString(main):
		return "image"
	}
	if m := drawingRe.FindStringSubmatch(main); m != nil {
		return m[1]
	}
	return "none"
}

// thanksForm is the Style tab's form choosing a thanks picture, with the
// rest of the style left empty.
func thanksForm(picture string) url.Values {
	form := styleForm("")
	form.Set("thanks_picture", picture)
	form.Set("thanks_alt", "")
	return form
}

// ownThanksPicture is a picture no other test uploads, so its address is
// this test's alone.
func ownThanksPicture(t *testing.T) apptest.Upload {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 40))
	if _, err := rand.Read(img.Pix); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return apptest.Upload{Field: "thanks_image", Name: "team.png", Data: buf.Bytes()}
}

// thankedBy submits the survey once as a new respondent and returns the
// thanks page.
func thankedBy(t *testing.T, app *apptest.App, id string) string {
	t.Helper()
	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "an answer")
	resp, thanks := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit: status %d\n%s", resp.StatusCode, thanks)
	}
	return thanks
}

// previewThanks submits the preview and returns the page the creator is
// shown, which is drawn from the draft.
func previewThanks(t *testing.T, app *apptest.App, creator *http.Client, id string) string {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/preview", nil)
	defer resp.Body.Close()
	return apptest.ReadBody(t, resp)
}

// TestStyleThanks_EachChoice: each picture is shown on the preview's
// thanks page as soon as it is chosen, and to respondents only once a
// version is published with it.
func TestStyleThanks_EachChoice(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-choices"))

	// Each choice has a survey of its own: a survey takes only so many
	// submissions from one address in a row.
	for _, picture := range []string{"check", "envelope", "confetti", "none"} {
		id := styledSurvey(t, app, creator, "Thanked with "+picture)
		resp, saved := postStyle(t, app, creator, id, thanksForm(picture))
		if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
			t.Fatalf("choosing %s: status %d\n%s", picture, resp.StatusCode, saved)
		}
		if got := thanksPictureOf(previewThanks(t, app, creator, id)); got != picture {
			t.Errorf("after choosing %s the preview thanks with %q", picture, got)
		}
		if got := thanksPictureOf(thankedBy(t, app, id)); got != "owl" {
			t.Errorf("%s reached respondents before it was published: they are thanked with %q", picture, got)
		}
		app.Publish(t, creator, id)
		if got := thanksPictureOf(thankedBy(t, app, id)); got != picture {
			t.Errorf("after publishing %s respondents are thanked with %q", picture, got)
		}
	}

	// The owl is the default, and choosing it again is choosing nothing.
	id := styledSurvey(t, app, creator, "Thanked with the owl")
	postStyle(t, app, creator, id, thanksForm("check"))
	postStyle(t, app, creator, id, thanksForm("owl"))
	if got := thanksPictureOf(previewThanks(t, app, creator, id)); got != "owl" {
		t.Errorf("choosing the owl again thanks with %q", got)
	}
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Publish version 2") {
		t.Errorf("choosing the owl, which is live, offers a publish")
	}
}

// TestStyleThanks_DrawingsAreTheThemes: a drawing is coloured by the
// theme, so it carries no colour of its own and no Signal.
func TestStyleThanks_DrawingsAreTheThemes(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-theme"))
	id := styledSurvey(t, app, creator, "Drawn")
	form := thanksForm("confetti")
	form.Set("theme", "forest")
	postStyle(t, app, creator, id, form)
	app.Publish(t, creator, id)

	thanks := thankedBy(t, app, id)
	svg := regexp.MustCompile(`(?s)<svg[^>]*\bthanks-drawing\b.*?</svg>`).FindString(thanks)
	if svg == "" {
		t.Fatalf("no drawing on the thanks page:\n%s", mainRe.FindString(thanks))
	}
	if !strings.Contains(svg, `aria-hidden="true"`) {
		t.Errorf("the drawing is decoration and should be hidden from a screen reader: %s", svg)
	}
	for _, attr := range []string{"fill=", "stroke=", "style="} {
		if strings.Contains(svg, attr) {
			t.Errorf("the drawing sets its own colour with %s, so no theme can colour it", attr)
		}
	}
	if !strings.Contains(thanks, `class="theme-forest"`) {
		t.Errorf("the thanks page is not drawn in the survey's theme")
	}
}

// TestStyleThanks_FrozenPerVersion: a response is thanked with the
// picture of the version it answered, whatever was published since.
func TestStyleThanks_FrozenPerVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-frozen"))
	id := app.CreateSurvey(t, creator, "Frozen thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postStyle(t, app, creator, id, thanksForm("envelope"))
	app.Publish(t, creator, id)

	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "from version 1")

	postStyle(t, app, creator, id, thanksForm("confetti"))
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("a changed thanks picture alone could not be published:\n%s", body)
	}
	_, thanks := submitAfterReading(t, app, respondent, id, form)
	if got := thanksPictureOf(thanks); got != "envelope" {
		t.Errorf("a response to version 1 was thanked with %q, want version 1's envelope", got)
	}
	if got := thanksPictureOf(thankedBy(t, app, id)); got != "confetti" {
		t.Errorf("a response to version 2 was thanked with %q", got)
	}
}

// TestStyleThanks_OwnPicture: a picture the creator uploads is shown on
// the white plate with its description, from the creator's address in
// the preview and the public one once published. Choosing a file picks
// it, whichever choice was left ticked.
func TestStyleThanks_OwnPicture(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-own"))
	id := styledSurvey(t, app, creator, "Our picture")

	form := thanksForm("owl")
	form.Set("thanks_alt", "Our team waving")
	resp, saved := postPictures(t, app, creator, id, form, ownThanksPicture(t))
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("uploading a thanks picture: status %d\n%s", resp.StatusCode, saved)
	}
	if !strings.Contains(saved, `name="thanks_picture" value="image" checked`) {
		t.Errorf("a file chosen did not make it the page's picture")
	}
	if !strings.Contains(saved, "js-style-image-thanks-image") || !strings.Contains(saved, "Remove the thank you picture") {
		t.Errorf("the Style tab does not show the thanks picture with its own remove box")
	}

	preview := previewThanks(t, app, creator, id)
	tag := thanksImageRe.FindString(preview)
	if !strings.Contains(tag, `src="/surveys/`+id+`/style-image/`) || !strings.Contains(tag, `alt="Our team waving"`) {
		t.Fatalf("the preview's thanks picture: %s", tag)
	}
	if !strings.Contains(tag, `width="64"`) || !strings.Contains(tag, `height="40"`) {
		t.Errorf("the picture's size is not written out, or it was cropped: %s", tag)
	}
	if strings.Contains(mainRe.FindString(preview), "owl-happy") {
		t.Errorf("the owl is drawn beside the creator's picture; a page has one picture")
	}

	app.Publish(t, creator, id)
	thanks := thankedBy(t, app, id)
	tag = thanksImageRe.FindString(thanks)
	src := srcRe.FindStringSubmatch(tag)
	if src == nil || !strings.HasPrefix(src[1], "/style-image/") {
		t.Fatalf("the published thanks picture: %s", tag)
	}
	got, body := fetch(t, &http.Client{}, app.Server.URL+src[1])
	if got.StatusCode != http.StatusOK {
		t.Fatalf("the public thanks picture: status %d", got.StatusCode)
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("the served picture does not decode: %v", err)
	}
	for _, external := range findExternalURLs(thanks) {
		t.Errorf("a thanks page with a picture references a third-party origin: %s", external)
	}

	// Choosing a drawing lets the picture go: the draft refers only to
	// what its pages show.
	if resp, page := postStyle(t, app, creator, id, thanksForm("check")); resp.StatusCode != http.StatusOK {
		t.Fatalf("choosing a drawing: status %d\n%s", resp.StatusCode, page)
	}
	if tab := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style"); strings.Contains(tab, "js-style-image-current") {
		t.Errorf("the creator's picture is kept after another picture was chosen")
	}
}

// TestStyleThanks_RefusesWhatItShouldNot: a picture with no description,
// "your own picture" with no picture, a picture of a kind that is not
// one, and a choice that is not offered are each refused with a message,
// and the draft is left as it was.
func TestStyleThanks_RefusesWhatItShouldNot(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-refused"))
	id := styledSurvey(t, app, creator, "Refused thanks")
	postStyle(t, app, creator, id, thanksForm("envelope"))

	undescribed := thanksForm("image")
	withoutPicture := thanksForm("image")
	withoutPicture.Set("thanks_alt", "Our team")
	gif := thanksForm("image")
	gif.Set("thanks_alt", "A moving picture")
	cases := []struct {
		name  string
		form  url.Values
		files []apptest.Upload
		want  string
	}{
		{"no description", undescribed, []apptest.Upload{ownThanksPicture(t)}, "say what the picture shows"},
		{"no picture", withoutPicture, nil, "choose an image to upload, or another picture"},
		{"a GIF", gif, []apptest.Upload{{Field: "thanks_image", Name: "moving.gif", Data: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")}}, "use a PNG, JPEG or WebP image"},
		{"an unknown choice", thanksForm("fireworks"), nil, "choose one of the pictures offered"},
	}
	for _, c := range cases {
		resp, page := postPictures(t, app, creator, id, c.form, c.files...)
		if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(page, c.want) {
			t.Errorf("%s: status %d, want 422 saying %q\n%s", c.name, resp.StatusCode, c.want, page)
		}
	}
	if got := thanksPictureOf(previewThanks(t, app, creator, id)); got != "envelope" {
		t.Errorf("a refused save changed the draft's thanks picture to %q", got)
	}
}

// TestStyleThanks_Localized: the description of the creator's picture is
// translated with the questions; a Dutch reader is given it in Dutch.
func TestStyleThanks_Localized(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-lang"))
	id := app.CreateSurvey(t, creator, "Described thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	form := thanksForm("image")
	form.Set("thanks_alt", "Our team waving")
	if resp, page := postPictures(t, app, creator, id, form, ownThanksPicture(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the picture: status %d\n%s", resp.StatusCode, page)
	}
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	languages := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(languages, "Thank you picture description in Dutch") {
		t.Fatalf("the languages page does not ask for the picture's description:\n%s", languages)
	}
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity:    {"Iets?"},
		"style_thanks_alt": {"Ons team zwaait"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publishing was refused:\n%s", body)
	}

	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id+"?lang=nl")
	answers := respondForm(t, page)
	answers.Set("q_"+extractAnswerFields(t, page)[0], "Ja")
	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+id+"?lang=nl", answers)
	if tag := thanksImageRe.FindString(thanks); !strings.Contains(tag, `alt="Ons team zwaait"`) {
		t.Errorf("a Dutch reader's thanks picture is described as: %s", tag)
	}
}

// TestStyleThanks_Exported: a version's thanks picture is in the archive,
// named in its style; a drawing is named, and the owl leaves no trace.
func TestStyleThanks_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-export"))
	id := app.CreateSurvey(t, creator, "Exported thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)
	postStyle(t, app, creator, id, thanksForm("envelope"))
	app.Publish(t, creator, id)
	form := thanksForm("image")
	form.Set("thanks_alt", "Our team waving")
	postPictures(t, app, creator, id, form, ownThanksPicture(t))
	app.Publish(t, creator, id)

	link := waitForExport(t, app, creator)
	_, raw := fetch(t, creator, app.Server.URL+link)
	files := openArchive(t, raw)
	var archive export.Archive
	if err := json.Unmarshal(files["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	for _, survey := range archive.Surveys {
		if survey.ID != id {
			continue
		}
		if len(survey.Versions) != 3 {
			t.Fatalf("versions = %d, want 3", len(survey.Versions))
		}
		styles := map[int]*export.Style{}
		for _, version := range survey.Versions {
			styles[version.Number] = version.Style
		}
		if style := styles[1]; style != nil {
			t.Errorf("version 1 thanked with the owl, and has a style: %+v", style)
		}
		if style := styles[2]; style == nil || style.Thanks == nil || style.Thanks.Picture != "envelope" || style.Thanks.Image != nil {
			t.Fatalf("version 2's style = %+v, want the envelope", style)
		}
		if styles[3] == nil {
			t.Fatalf("version 3 has no style")
		}
		thanks := styles[3].Thanks
		if thanks == nil || thanks.Picture != "image" || thanks.Image == nil {
			t.Fatalf("version 3's thanks picture = %+v", thanks)
		}
		if thanks.Image.Alt != "Our team waving" || thanks.Image.Width != 64 || thanks.Image.Height != 40 {
			t.Errorf("version 3's picture = %+v", thanks.Image)
		}
		if _, err := png.Decode(bytes.NewReader(files[thanks.Image.File])); err != nil {
			t.Errorf("%s is not in the archive as a PNG: %v", thanks.Image.File, err)
		}
		return
	}
	t.Fatalf("the survey is missing from the export")
}

// TestStyleThanks_EditorPointsToStyle: the editor's thank you panel says
// where the picture above the message is chosen.
func TestStyleThanks_EditorPointsToStyle(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-thanks-editor"))
	id := app.CreateSurvey(t, creator, "Pointed", true)
	editor := app.SurveyPage(t, creator, id)
	link := regexp.MustCompile(`<a class="js-thanks-picture-link" href="([^"]+)"`).FindStringSubmatch(editor)
	if link == nil || link[1] != "/surveys/"+id+"/style#style-thanks-heading" {
		t.Errorf("the thank you panel does not point to the Style tab: %v", link)
	}
}

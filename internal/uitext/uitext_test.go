package uitext_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/TryEarful/earful/internal/uitext"
)

const english = `
[respond.submit]
label = "Submit answers"

[respond.question]
position = "Question {{.Current}} of {{.Total}}"

[surveys.invites.send]
one = "Send 1 invite"
other = "Send {{.Count}} invites"

[respond.disclosure]
identified_html = "You're answering as <strong>{{.Email}}</strong>."

[voice.stop.label]
description = "Button. Ends a recording."
other = "Stop"

[only.in]
english = "Only in English"
`

const spanish = `
[respond.submit.label]
hash = "sha1-0"
other = "Enviar respuestas"

[surveys.invites.send]
hash = "sha1-0"
one = "Enviar 1 invitación"
many = "Enviar {{.Count}} de invitaciones"
other = "Enviar {{.Count}} invitaciones"
`

func files(en, es string) fstest.MapFS {
	fsys := fstest.MapFS{"active.en.toml": {Data: []byte(en)}}
	if es != "" {
		fsys["active.es.toml"] = &fstest.MapFile{Data: []byte(es)}
	}
	return fsys
}

func load(t *testing.T, opts uitext.Options) *uitext.Catalog {
	t.Helper()
	if opts.Languages == nil {
		opts.Languages = []string{"en", "es"}
	}
	c, err := uitext.Load(files(english, spanish), opts)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return c
}

func TestMessagesAreNamedByTheirPlaceInTheFile(t *testing.T) {
	en := load(t, uitext.Options{}).Localizer()
	for id, want := range map[uitext.ID]string{
		"respond.submit.label": "Submit answers",
		"voice.stop.label":     "Stop",
	} {
		if got := en.T(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	got := en.T("respond.question.position", uitext.Args{"Current": 2, "Total": 5})
	if got != "Question 2 of 5" {
		t.Errorf("placeholders = %q", got)
	}
}

func TestCountsChooseTheForm(t *testing.T) {
	c := load(t, uitext.Options{})
	en, es := c.Localizer("en"), c.Localizer("es")
	for _, tc := range []struct {
		l     uitext.Localizer
		count int
		want  string
	}{
		{en, 1, "Send 1 invite"},
		{en, 2, "Send 2 invites"},
		{en, 0, "Send 0 invites"},
		{es, 1, "Enviar 1 invitación"},
		{es, 2, "Enviar 2 invitaciones"},
		// Spanish counts exact millions differently: "un millón de".
		{es, 1_000_000, "Enviar 1000000 de invitaciones"},
	} {
		if got := tc.l.N("surveys.invites.send", tc.count); got != tc.want {
			t.Errorf("%s, %d = %q, want %q", tc.l.Lang(), tc.count, got, tc.want)
		}
	}
}

// A value put into markup is whatever a person typed: an email address,
// a workspace's name. It is text, however it is spelled.
func TestValuesInMarkupAreEscaped(t *testing.T) {
	en := load(t, uitext.Options{}).Localizer()
	got := en.HTML("respond.disclosure.identified_html", uitext.Args{"Email": `<script>alert(1)</script>&co`})
	want := "You're answering as <strong>&lt;script&gt;alert(1)&lt;/script&gt;&amp;co</strong>."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTheCallersArgsAreLeftAlone(t *testing.T) {
	en := load(t, uitext.Options{}).Localizer()
	args := uitext.Args{"Email": "a&b@example.test"}
	en.HTML("respond.disclosure.identified_html", args)
	en.N("surveys.invites.send", 3, args)
	if len(args) != 1 || args["Email"] != "a&b@example.test" {
		t.Errorf("the caller's values were changed: %v", args)
	}
}

func TestAnUntranslatedMessageIsShownInTheSource(t *testing.T) {
	es := load(t, uitext.Options{}).Localizer("es")
	if got := es.T("only.in.english"); got != "Only in English" {
		t.Errorf("got %q", got)
	}
}

func TestAMissingMessageShowsItsName(t *testing.T) {
	en := load(t, uitext.Options{}).Localizer()
	if got := en.T("nowhere.to.be.found"); got != "nowhere.to.be.found" {
		t.Errorf("got %q", got)
	}
}

func TestStrictPanics(t *testing.T) {
	en := load(t, uitext.Options{Strict: true}).Localizer()
	for name, render := range map[string]func(){
		"a message that does not exist": func() { en.T("nowhere.to.be.found") },
		"a placeholder given no value":  func() { en.T("respond.question.position", uitext.Args{"Current": 1}) },
		"markup rendered as text":       func() { en.T("respond.disclosure.identified_html", uitext.Args{"Email": "x"}) },
		"text rendered as markup":       func() { en.HTML("respond.submit.label") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("rendered without complaint")
				}
			}()
			render()
		})
	}
}

func TestTheLanguageIsTheBestOneServed(t *testing.T) {
	c := load(t, uitext.Options{})
	for _, tc := range []struct {
		prefs []string
		want  string
	}{
		{nil, "en"},
		{[]string{""}, "en"},
		{[]string{"es"}, "es"},
		{[]string{"es-MX"}, "es"},
		{[]string{"es-419"}, "es"},
		{[]string{"en-GB"}, "en"},
		{[]string{"pt"}, "en"},
		{[]string{"nl"}, "en"},
		// A header, with its weights: Dutch is not served, Spanish is.
		{[]string{"nl-BE,nl;q=0.9,es;q=0.8,en;q=0.7"}, "es"},
		// Earlier preferences win: a chosen language before the browser's.
		{[]string{"es", "en-US,en;q=0.9"}, "es"},
		{[]string{"", "es-AR,es;q=0.9"}, "es"},
		{[]string{"not a language", "es"}, "es"},
	} {
		if got := c.Localizer(tc.prefs...).Lang(); got != tc.want {
			t.Errorf("%q → %s, want %s", tc.prefs, got, tc.want)
		}
	}
}

// A language with a file and no place in the list is checked and
// offered to nobody.
func TestALanguageNotServedIsNotOffered(t *testing.T) {
	c, err := uitext.Load(files(english, spanish), uitext.Options{Languages: []string{"en"}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.Localizer("es").T("respond.submit.label"); got != "Submit answers" {
		t.Errorf("got %q", got)
	}
	if len(c.Written("es")) == 0 {
		t.Error("the unserved language was not read")
	}
	if _, err := uitext.Load(files(english, "[broken"), uitext.Options{Languages: []string{"en"}}); err == nil {
		t.Error("a broken file for an unserved language was accepted")
	}
}

func TestLoadRefusesWhatCouldNotBeShown(t *testing.T) {
	for name, tc := range map[string]struct{ en, want string }{
		"a name ending in a field": {
			"[surveys.form]\ndescription = \"Description\"\n",
			"surveys.form",
		},
		"a field beside a message": {
			"[surveys.form]\ndescription = \"Description\"\nlabel = \"Title\"\n",
			"",
		},
		"the word translation": {
			"[results.translation]\nlabel = \"Translate\"\n",
			"translation",
		},
		"a placeholder left open": {
			"[respond.question]\nposition = \"Question {{.Current\"\n",
			"respond.question.position",
		},
		"a name of one word": {
			"submit = \"Submit\"\n",
			"submit",
		},
		"a name in capitals": {
			"[Respond.submit]\nlabel = \"Submit\"\n",
			"Respond",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := uitext.Load(files(tc.en, ""), uitext.Options{Languages: []string{"en"}})
			if err == nil {
				t.Fatal("loaded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not name the message: %v", err)
			}
		})
	}
	if _, err := uitext.Load(fstest.MapFS{"active.es.toml": {Data: []byte(spanish)}}, uitext.Options{}); err == nil {
		t.Error("loaded without the source language")
	}
}

// The files in the binary are the ones that matter.
func TestTheEmbeddedTextLoads(t *testing.T) {
	c := uitext.Embedded()
	if got := c.Languages(); len(got) == 0 || got[0] != uitext.Source {
		t.Errorf("languages = %v", got)
	}
	if got := uitext.From(context.Background()).Lang(); got != uitext.Source {
		t.Errorf("a context with no language renders %s", got)
	}
}

// A script is given messages as they are written, to fill in for
// itself: one wording as it is, and the forms of one that has several.
func TestScriptsAreGivenMessagesAsWritten(t *testing.T) {
	c := load(t, uitext.Options{})

	en := c.Localizer("en").ForScripts("respond.question", "surveys.invites.send", "voice")
	if got := en["respond.question.position"]; got != "Question {{.Current}} of {{.Total}}" {
		t.Errorf("a message is filled in before the script has it: %v", got)
	}
	forms, ok := en["surveys.invites.send"].(map[string]string)
	if !ok || forms["one"] != "Send 1 invite" || forms["other"] != "Send {{.Count}} invites" {
		t.Errorf("forms = %v", en["surveys.invites.send"])
	}
	if got := en["voice.stop.label"]; got != "Stop" {
		t.Errorf("a name that begins the names of several gave %v", got)
	}
	if _, ok := en["respond.submit.label"]; ok || len(en) != 3 {
		t.Errorf("a script was given more than was named: %v", en)
	}

	// In the language of the page, and in the source where a message
	// has not been translated.
	es := c.Localizer("es").ForScripts("respond.submit", "surveys.invites.send", "only.in")
	if got := es["respond.submit.label"]; got != "Enviar respuestas" {
		t.Errorf("got %v", got)
	}
	if forms, _ := es["surveys.invites.send"].(map[string]string); forms["many"] == "" {
		t.Errorf("Spanish is given without its form for millions: %v", forms)
	}
	if got := es["only.in.english"]; got != "Only in English" {
		t.Errorf("an untranslated message is given as %v", got)
	}
}

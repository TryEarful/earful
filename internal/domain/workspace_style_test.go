package domain

import (
	"errors"
	"testing"
)

func TestWorkspaceStyleTranslations(t *testing.T) {
	var w WorkspaceStyle
	if err := w.SetStyle(Style{
		Header: StyleHeader{Name: "Acme", Tagline: "We ask, you tell", Links: []StyleLink{{Label: "Home", URL: "https://acme.example"}}},
		Footer: StyleFooter{Text: "Acme Ltd"},
	}); err != nil {
		t.Fatal(err)
	}
	if !w.HasWords() {
		t.Fatal("a style with a tagline has words to translate")
	}
	if !w.Pending("es") {
		t.Fatal("a language never translated is pending")
	}

	// Drafted, not read: still pending, and the label count follows the
	// style's links whatever the form sent.
	if err := w.SetTranslation("ES", StyleWords{Tagline: "Preguntamos", HeaderLinks: []string{"Inicio", "extra"}, FooterText: "Acme SL"}, false); err != nil {
		t.Fatal(err)
	}
	if got := w.Localizations["es"].HeaderLinks; len(got) != 1 || got[0] != "Inicio" {
		t.Errorf("labels should fit the style's links, got %v", got)
	}
	if !w.Pending("es") {
		t.Error("an unreviewed translation is pending")
	}
	if err := w.SetTranslation("es", StyleWords{Tagline: "Preguntamos", HeaderLinks: []string{"Inicio"}, FooterText: "Acme SL"}, true); err != nil {
		t.Fatal(err)
	}
	if w.Pending("es") || w.Stale("es") {
		t.Error("a reviewed translation of the current wording is done")
	}

	// The footer changes: the footer's words wait, the header's do not.
	if err := w.SetStyle(Style{Header: w.Style.Header, Footer: StyleFooter{Text: "Acme Group"}}); err != nil {
		t.Fatal(err)
	}
	if !w.Stale("es") || !w.Pending("es") {
		t.Error("a translation of an earlier wording is stale and pending")
	}
	if !w.PendingFor("es", StyleParts{Footer: true}) {
		t.Error("the changed footer is pending")
	}
	if w.PendingFor("es", StyleParts{Header: true}) {
		t.Error("the header did not change and should not be pending")
	}
	// A part with no words is never waited on.
	if w.PendingFor("fr", StyleParts{Theme: true, Thanks: true}) {
		t.Error("parts without words have nothing to translate")
	}

	w.RemoveLanguage("es")
	if len(w.Languages()) != 0 || w.Localizations != nil {
		t.Errorf("removing the only language should leave none, got %v", w.Languages())
	}
	if err := w.SetTranslation("not a language!", StyleWords{}, true); !errors.Is(err, ErrLanguageInvalid) {
		t.Errorf("an invalid language code should be refused, got %v", err)
	}
}

// TestWorkspaceStyle_ATranslationOfAnEarlierWordingIsRefused: a
// translation made against wording that has changed since is not stored;
// one made against the wording as it stands, or saying nothing of it, is.
func TestWorkspaceStyle_ATranslationOfAnEarlierWordingIsRefused(t *testing.T) {
	w := WorkspaceStyle{Style: Style{Header: StyleHeader{Name: "Corner Workshop", Tagline: "Evening classes."}}}
	earlier := w.Style.Words().Fingerprint()
	w.Style.Header.Tagline = "Weekend classes."
	words := StyleWords{Tagline: "Clases de tarde."}
	if err := w.SetTranslationOf(earlier, "es", words, true); !errors.Is(err, ErrAccountWordsChanged) {
		t.Fatalf("a translation of an earlier wording: got %v, want ErrAccountWordsChanged", err)
	}
	if _, stored := w.Localizations["es"]; stored {
		t.Fatal("a refused translation was stored")
	}
	if err := w.SetTranslationOf(w.Style.Words().Fingerprint(), "es", words, true); err != nil {
		t.Fatalf("a translation of the current wording: %v", err)
	}
	if err := w.SetTranslationOf("", "fr", StyleWords{Tagline: "Cours du week-end."}, true); err != nil {
		t.Fatalf("a translation that names no wording: %v", err)
	}
	if earlier == w.Style.Words().Fingerprint() {
		t.Error("two wordings have the same fingerprint")
	}
}

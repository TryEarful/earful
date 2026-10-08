package domain

import (
	"errors"
	"testing"
)

// The account's style a survey follows in these tests: Ocean, a header
// with a logo and a link, and a footer.
func accountForTest() WorkspaceStyle {
	return WorkspaceStyle{Style: Style{
		Theme: ThemeOcean,
		Header: StyleHeader{
			Name: "Corner Workshop", Tagline: "Evening classes.",
			Links:   []StyleLink{{Label: "Courses", URL: "https://example.com/courses"}},
			Logo:    StyleImage{SHA256: hexHash('a'), Width: 4, Height: 4},
			LogoAlt: "Corner Workshop",
		},
		Footer: StyleFooter{Text: "Corner Workshop Ltd"},
	}}
}

func hexHash(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func shown(b bool) *bool { return &b }

// TestStyleChoice_ALegacyDraftKeepsItsLook: a draft saved before an
// account could have a style keeps every part it set as its own, and
// takes from the account only what it left empty.
func TestStyleChoice_ALegacyDraftKeepsItsLook(t *testing.T) {
	d, err := ParseDraft([]byte(`{"questions":[],"style":{"header":{"name":"Old name"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if own := d.OwnParts(); !own.Header || own.Theme || own.Footer || own.Thanks {
		t.Fatalf("own parts = %+v, want only the header", own)
	}
	got := d.ResolvedStyle(accountForTest().Style)
	if got.Header.Name != "Old name" || !got.Header.Logo.IsZero() {
		t.Errorf("the legacy header was not kept whole: %+v", got.Header)
	}
	if got.Theme != ThemeOcean || got.Footer.Text != "Corner Workshop Ltd" {
		t.Errorf("the parts the draft left empty were not the account's: %+v", got)
	}
	// With no account style, a legacy draft is drawn exactly as before.
	if !d.ResolvedStyle(Style{}).Equal(d.Style) {
		t.Errorf("with no account style the draft is drawn as %+v, want %+v", d.ResolvedStyle(Style{}), d.Style)
	}
}

// TestStyleChoice_SavingWhatTheAccountHasKeepsFollowing: the Style tab
// shows the account's parts, and saving them unchanged leaves the
// survey following, so a later change to the account reaches it.
func TestStyleChoice_SavingWhatTheAccountHasKeepsFollowing(t *testing.T) {
	account := accountForTest()
	var d Draft
	form := d.FormStyle(account.Style)
	// The form posts the theme by name and the logo's text with it.
	form.Theme = ThemeOcean
	if err := d.SetStyleFromForm(account, form, AllStyleParts, shown(true), shown(true)); err != nil {
		t.Fatal(err)
	}
	if !d.OwnParts().IsZero() || !d.Style.IsZero() {
		t.Fatalf("saving the account's style unchanged made parts the survey's own: %+v %+v", d.OwnParts(), d.Style)
	}
	account.Style.Footer.Text = "Corner Workshop plc"
	if got := d.ResolvedStyle(account.Style).Footer.Text; got != "Corner Workshop plc" {
		t.Errorf("the account's later change did not reach the survey: %q", got)
	}
}

// TestStyleChoice_ChangingAWordMakesTheWholePartOwn: a header is one
// statement of who is asking, so changing its tagline makes the whole
// header the survey's, and a later account logo does not reach it.
func TestStyleChoice_ChangingAWordMakesTheWholePartOwn(t *testing.T) {
	account := accountForTest()
	var d Draft
	form := d.FormStyle(account.Style)
	form.Header.Tagline = "Weekend classes."
	if err := d.SetStyleFromForm(account, form, StyleParts{Header: true}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if own := d.OwnParts(); !own.Header || own.Theme || own.Footer {
		t.Fatalf("own parts = %+v, want the header alone", own)
	}
	if d.Style.Header.Logo != account.Style.Header.Logo {
		t.Errorf("the header made its own lost the account's logo it kept")
	}
	account.Style.Header.Logo = StyleImage{SHA256: hexHash('b'), Width: 4, Height: 4}
	if got := d.ResolvedStyle(account.Style).Header; got.Logo.SHA256 != hexHash('a') || got.Tagline != "Weekend classes." {
		t.Errorf("the survey's own header followed the account: %+v", got)
	}
}

// TestStyleChoice_EarfulsThemeUnderAnOceanAccount: Earful's theme is the
// empty string, so a survey that chooses it under an account drawn in
// Ocean has to say it is its own.
func TestStyleChoice_EarfulsThemeUnderAnOceanAccount(t *testing.T) {
	account := accountForTest()
	var d Draft
	form := d.FormStyle(account.Style)
	form.Theme = ThemeEarful
	if err := d.SetStyleFromForm(account, form, StyleParts{Theme: true}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !d.StyleChoice.Own.Theme || d.Style.Theme != "" {
		t.Fatalf("Earful's theme was not recorded as the survey's own: %+v", d.StyleChoice)
	}
	if got := d.ResolvedStyle(account.Style).ThemeName(); got != ThemeEarful {
		t.Errorf("resolved theme = %q, want earful", got)
	}
	// Choosing Ocean again, which is the account's, follows it.
	form.Theme = ThemeOcean
	if err := d.SetStyleFromForm(account, form, StyleParts{Theme: true}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d.OwnParts().Theme {
		t.Errorf("choosing the account's theme left the theme the survey's own")
	}
}

// TestStyleChoice_EqualityTraps: what a page would draw the same is the
// same part, whatever the form left behind.
func TestStyleChoice_EqualityTraps(t *testing.T) {
	account := accountForTest()
	account.Style.Header.Logo, account.Style.Header.LogoAlt = StyleImage{}, ""
	account.Style.Thanks = StyleThanks{Picture: ThanksCheck}
	var d Draft
	form := d.FormStyle(account.Style)
	// Text left in the logo's field with no logo is shown to nobody.
	form.Header.LogoAlt = "left over"
	// A picture the thanks choice would not show is set aside.
	form.Thanks.Image, form.Thanks.Alt = StyleImage{SHA256: hexHash('c'), Width: 4, Height: 4}, "left over"
	if err := d.SetStyleFromForm(account, form, AllStyleParts, nil, nil); err != nil {
		t.Fatal(err)
	}
	if own := d.OwnParts(); own.Header || own.Thanks {
		t.Errorf("parts the page would draw the same became the survey's own: %+v", own)
	}
}

// TestStyleChoice_TurningOffTheHeaderAndFooter: a survey with no header
// carries no logo, so Earful's owl comes back; its footer goes too when
// that is off. Turning them on again finds what was there.
func TestStyleChoice_TurningOffTheHeaderAndFooter(t *testing.T) {
	account := accountForTest()
	var d Draft
	if err := d.SetStyleFromForm(account, d.FormStyle(account.Style), StyleParts{}, shown(false), shown(false)); err != nil {
		t.Fatal(err)
	}
	got := d.ResolvedStyle(account.Style)
	if !got.Header.IsZero() || !got.Footer.IsZero() || got.Branded() {
		t.Errorf("a survey with its header and footer off still shows them: %+v", got)
	}
	if d.FormStyle(account.Style).Header.Name != "Corner Workshop" {
		t.Errorf("the form does not show the header it would turn back on")
	}
	if f := d.FollowedParts(); f.Header || f.Footer || !f.Theme || !f.Thanks {
		t.Errorf("followed parts = %+v, want the theme and the thanks picture", f)
	}
	if err := d.SetStyleFromForm(account, d.FormStyle(account.Style), StyleParts{}, shown(true), nil); err != nil {
		t.Fatal(err)
	}
	if d.ResolvedStyle(account.Style).Header.Name != "Corner Workshop" {
		t.Errorf("turning the header on again did not bring it back")
	}
}

// TestStyleChoice_Reset: every part follows the account again.
func TestStyleChoice_Reset(t *testing.T) {
	account := accountForTest()
	d := Draft{Style: Style{Theme: ThemeForest, Footer: StyleFooter{Text: "Own"}}, StyleChoice: StyleChoice{NoHeader: true}}
	d.ResetStyle()
	if !d.ResolvedStyle(account.Style).Equal(account.Style) {
		t.Errorf("a reset survey is drawn as %+v, want the account's", d.ResolvedStyle(account.Style))
	}
}

// translatedDraft is a draft with a Spanish localization.
func translatedDraft(s Style, choice StyleChoice) Draft {
	return Draft{
		Style:         s,
		StyleChoice:   choice,
		Localizations: map[string]Localization{"es": {Lang: "es", Questions: map[string]LocalizedQuestion{}}},
	}
}

// TestStyleChoice_TranslationsArePartByPart: the survey translates its
// own parts' words, and handing one part back to the account leaves the
// rest of a read translation read.
func TestStyleChoice_TranslationsArePartByPart(t *testing.T) {
	d := translatedDraft(Style{
		Header: StyleHeader{Tagline: "Evening classes."},
		Footer: StyleFooter{Text: "Corner Workshop Ltd"},
	}, StyleChoice{})
	if err := d.SetStyleTranslation("es", StyleWords{Tagline: "Clases de tarde.", FooterText: "Corner Workshop SL"}, true); err != nil {
		t.Fatal(err)
	}
	if d.StylePending("es") {
		t.Fatal("a read translation of every own word is pending")
	}
	// The header goes back to the account; the footer stays read.
	d.Style.Header = StyleHeader{}
	if d.StylePending("es") || d.StyleStale("es") {
		t.Errorf("handing the header back made the footer's translation pending")
	}
	if got := d.OwnStyleWords(); got.Tagline != "" || got.FooterText != "Corner Workshop Ltd" {
		t.Errorf("own words = %+v, want the footer's alone", got)
	}
}

// TestStyleChoice_AccountTranslationGatesPublishing: a survey that shows
// the account's words in Spanish waits for the account to be read in
// Spanish, and is told which language; one that shows only words of its
// own does not wait.
func TestStyleChoice_AccountTranslationGatesPublishing(t *testing.T) {
	account := accountForTest()
	d := translatedDraft(Style{}, StyleChoice{})
	err := d.ReadyToPublish(account)
	if lang, ok := IsAccountStyleTranslationError(err); !ok || lang != "es" {
		t.Fatalf("ReadyToPublish = %v, want the account's Spanish named", err)
	}
	if err := account.SetTranslation("es", StyleWords{
		Tagline: "Clases de tarde.", LogoAlt: "Corner Workshop", HeaderLinks: []string{"Cursos"}, FooterText: "Corner Workshop SL",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := d.ReadyToPublish(account); err != nil {
		t.Fatalf("a read account translation still holds the survey back: %v", err)
	}
	words, ok := d.ResolvedStyleWords(account, "es")
	if !ok || words.Tagline != "Clases de tarde." || words.FooterText != "Corner Workshop SL" {
		t.Errorf("resolved Spanish words = %+v, %v", words, ok)
	}
	// The account's wording changes: the survey waits again.
	account.Style.Header.Tagline = "Weekend classes."
	if _, ok := IsAccountStyleTranslationError(d.ReadyToPublish(account)); !ok {
		t.Errorf("a stale account translation does not hold the survey back")
	}
	// A survey with no header or footer of the account's does not wait.
	own := translatedDraft(Style{}, StyleChoice{NoHeader: true, NoFooter: true})
	if err := own.ReadyToPublish(account); err != nil {
		t.Errorf("a survey showing none of the account's words waits on its translation: %v", err)
	}
	// A survey's own unread words are its own error, not the account's.
	mine := translatedDraft(Style{Footer: StyleFooter{Text: "Mine"}}, StyleChoice{NoHeader: true})
	if err := mine.ReadyToPublish(account); !errors.Is(err, ErrUnreviewedTrans) {
		t.Errorf("ReadyToPublish = %v, want the survey's own translation unread", err)
	}
}

// TestStyleChoice_AdoptedTranslationsStayRead: a header made the
// survey's own only to change its name keeps the account's read
// translation of its words.
func TestStyleChoice_AdoptedTranslationsStayRead(t *testing.T) {
	account := accountForTest()
	if err := account.SetTranslation("es", StyleWords{
		Tagline: "Clases de tarde.", LogoAlt: "Corner Workshop", HeaderLinks: []string{"Cursos"}, FooterText: "Corner Workshop SL",
	}, true); err != nil {
		t.Fatal(err)
	}
	d := translatedDraft(Style{}, StyleChoice{})
	form := d.FormStyle(account.Style)
	form.Header.Name = "Corner Workshop North"
	if err := d.SetStyleFromForm(account, form, StyleParts{Header: true}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !d.OwnParts().Header {
		t.Fatal("changing the name did not make the header the survey's own")
	}
	if d.StylePending("es") {
		t.Errorf("the header's words, still the account's, are pending after taking it over: %+v", d.Localizations["es"].Style)
	}
	if err := d.ReadyToPublish(account); err != nil {
		t.Errorf("publishing waits after taking over the header: %v", err)
	}
	words, ok := d.ResolvedStyleWords(account, "es")
	if !ok || words.Tagline != "Clases de tarde." || words.FooterText != "Corner Workshop SL" {
		t.Errorf("resolved Spanish words = %+v, %v", words, ok)
	}
}

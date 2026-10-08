package domain

import (
	"testing"
)

// publishedForTest is d as a version publishing it against account
// would freeze it: what the store writes, read back.
func publishedForTest(t *testing.T, d Draft, account WorkspaceStyle) PublishedVersion {
	t.Helper()
	if err := d.ReadyToPublish(account); err != nil {
		t.Fatalf("the draft cannot be published: %v", err)
	}
	v := PublishedVersion{
		Questions: d.Questions,
		Thanks:    d.Thanks,
		Style:     d.ResolvedStyle(account.Style),
		Follows:   d.FollowedParts(),
	}
	for _, lang := range d.Languages() {
		for _, q := range d.Questions {
			translated, ok := d.Localizations[lang].Questions[q.IdentityID]
			if !ok || translated.Text == "" {
				continue
			}
			if v.QuestionTranslations == nil {
				v.QuestionTranslations = map[string]map[string]LocalizedQuestion{}
			}
			if v.QuestionTranslations[lang] == nil {
				v.QuestionTranslations[lang] = map[string]LocalizedQuestion{}
			}
			v.QuestionTranslations[lang][q.IdentityID] = LocalizedQuestion{Text: translated.Text, Options: translated.Options}
		}
		if thanks, ok := d.LocalizedThanks(lang); ok {
			if v.ThanksTranslations == nil {
				v.ThanksTranslations = map[string]ThankYou{}
			}
			v.ThanksTranslations[lang] = thanks
		}
		if words, ok := frozenWords(d, account, lang); ok {
			if v.StyleTranslations == nil {
				v.StyleTranslations = map[string]StyleWords{}
			}
			v.StyleTranslations[lang] = words
		}
	}
	return v
}

// refreshScenario is a survey in English and Spanish that draws Earful's
// theme and a footer of its own, and takes its header and thanks picture
// from an Ocean account that is read in Spanish.
func refreshScenario(t *testing.T) (Draft, WorkspaceStyle) {
	t.Helper()
	account := accountForTest()
	if err := account.SetTranslation("es", StyleWords{
		Tagline: "Clases de tarde.", LogoAlt: "Corner Workshop", HeaderLinks: []string{"Cursos"}, FooterText: "Corner Workshop SL",
	}, true); err != nil {
		t.Fatal(err)
	}
	const id = "6f1e2a90-3c51-4b8e-9a43-2a1d5f7c9e10"
	d := Draft{
		Questions: []Question{{IdentityID: id, Type: LongText, Text: "How was the class?"}},
		Thanks:    ThankYou{Message: "Thanks for coming."},
		Style:     Style{Footer: StyleFooter{Text: "Our own footer"}},
		StyleChoice: StyleChoice{
			Own: StyleParts{Theme: true, Footer: true},
		},
	}
	if err := d.AddLanguage("es"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTranslation("es", id, "¿Qué tal la clase?", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetThanksTranslation("es", "Gracias por venir.", "", true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetStyleTranslation("es", StyleWords{FooterText: "Nuestro pie"}, true); err != nil {
		t.Fatal(err)
	}
	return d, account
}

// TestPublishedVersion_RoundTrips: a version read back as a draft and
// published again against the same account is the same version: the
// same style, the same parts followed, the same words in every language,
// and nothing to review.
func TestPublishedVersion_RoundTrips(t *testing.T) {
	d, account := refreshScenario(t)
	v := publishedForTest(t, d, account)
	again := v.Draft()
	if err := again.ReadyToPublish(account); err != nil {
		t.Fatalf("the version read back cannot be published again: %v", err)
	}
	if again.FollowedParts() != v.Follows {
		t.Errorf("followed parts = %+v, want %+v", again.FollowedParts(), v.Follows)
	}
	republished := publishedForTest(t, again, account)
	if !StyleDiffers(republished.Style, v.Style).IsZero() {
		t.Errorf("style published again = %+v, want %+v", republished.Style, v.Style)
	}
	if !republished.StyleTranslations["es"].Equal(v.StyleTranslations["es"]) {
		t.Errorf("Spanish words published again = %+v, want %+v", republished.StyleTranslations["es"], v.StyleTranslations["es"])
	}
	if republished.ThanksTranslations["es"] != v.ThanksTranslations["es"] {
		t.Errorf("Spanish thanks published again = %+v", republished.ThanksTranslations["es"])
	}
	if got := republished.QuestionTranslations["es"][d.Questions[0].IdentityID].Text; got != "¿Qué tal la clase?" {
		t.Errorf("Spanish question published again = %q", got)
	}
	if r := v.RefreshAgainst(account); r.Changes || r.BlockedIn != "" {
		t.Errorf("RefreshAgainst the same account = %+v, want nothing to do", r)
	}
}

// TestPublishedVersion_ARefreshTakesTheAccountsNewParts: published again
// after the account's footer and header change, a version takes the
// header it follows and keeps the footer of its own.
func TestPublishedVersion_ARefreshTakesTheAccountsNewParts(t *testing.T) {
	d, account := refreshScenario(t)
	v := publishedForTest(t, d, account)
	before := account
	after := account
	after.Style.Header.Name = "Corner Workshop Cooperative"
	after.Style.Footer.Text = "12 Mill Lane"

	if !v.ChangedBetween(before, after) {
		t.Fatal("a change to the header the version follows does not reach it")
	}
	r := v.RefreshAgainst(after)
	if !r.Changes || r.BlockedIn != "" {
		t.Fatalf("RefreshAgainst = %+v, want a change with nothing blocking", r)
	}
	got := v.Draft().ResolvedStyle(after.Style)
	if got.Header.Name != "Corner Workshop Cooperative" {
		t.Errorf("header = %q, want the account's new name", got.Header.Name)
	}
	if got.Footer.Text != "Our own footer" || got.Theme != "" {
		t.Errorf("own parts changed: footer %q, theme %q", got.Footer.Text, got.Theme)
	}

	// A change to a part the version does not follow does not reach it.
	footerOnly := account
	footerOnly.Style.Footer.Text = "12 Mill Lane"
	if v.ChangedBetween(before, footerOnly) || v.RefreshAgainst(footerOnly).Changes {
		t.Errorf("a change to the account's footer reached a version with a footer of its own")
	}
}

// TestPublishedVersion_ATranslationAloneIsAChange: rewording the
// account's Spanish reaches a version that went out in Spanish.
func TestPublishedVersion_ATranslationAloneIsAChange(t *testing.T) {
	d, account := refreshScenario(t)
	v := publishedForTest(t, d, account)
	after := account
	after.Localizations = map[string]LocalizedStyle{}
	for lang, l := range account.Localizations {
		after.Localizations[lang] = l
	}
	if err := after.SetTranslation("es", StyleWords{
		Tagline: "Clases por la tarde.", LogoAlt: "Corner Workshop", HeaderLinks: []string{"Cursos"}, FooterText: "Corner Workshop SL",
	}, true); err != nil {
		t.Fatal(err)
	}
	if !v.ChangedBetween(account, after) || !v.RefreshAgainst(after).Changes {
		t.Fatal("a new Spanish tagline does not reach a version that went out in Spanish")
	}
}

// TestPublishedVersion_AnUnreadAccountTranslationBlocks: a version that
// shows the account's words in Spanish cannot go out again until the
// account's new wording is read in Spanish, and the language is named.
func TestPublishedVersion_AnUnreadAccountTranslationBlocks(t *testing.T) {
	d, account := refreshScenario(t)
	v := publishedForTest(t, d, account)
	after := account
	after.Style.Header.Tagline = "Weekend classes."
	r := v.RefreshAgainst(after)
	if !r.Changes || r.BlockedIn != "es" {
		t.Errorf("RefreshAgainst = %+v, want a change blocked in Spanish", r)
	}
}

// TestPublishedVersion_AHeaderTurnedOffStaysOff: a version that showed no
// header and took none from its account shows none when published again,
// whatever header the account has.
func TestPublishedVersion_AHeaderTurnedOffStaysOff(t *testing.T) {
	account := accountForTest()
	d := Draft{
		Questions:   []Question{{IdentityID: "0b7d4c1e-8f2a-4e6b-9d3c-5a1f2e7b8c90", Type: LongText, Text: "Anything else?"}},
		StyleChoice: StyleChoice{NoHeader: true},
	}
	v := publishedForTest(t, d, account)
	if v.Follows.Header || !v.Style.Header.IsZero() {
		t.Fatalf("the version shows or follows a header: %+v", v)
	}
	after := account
	after.Style.Header.Name = "A new name"
	if v.ChangedBetween(account, after) {
		t.Errorf("a change to the account's header reached a version that shows none")
	}
	if got := v.Draft().ResolvedStyle(after.Style); !got.Header.IsZero() {
		t.Errorf("the header came back: %+v", got.Header)
	}
	after.Style.Footer.Text = "A new footer"
	if !v.RefreshAgainst(after).Changes {
		t.Errorf("a change to the footer the version follows does not reach it")
	}
}

// TestPublishedVersion_ALegacyVersionFollowsNothing: a version published
// before a survey could follow an account names no part, and nothing the
// account does changes it.
func TestPublishedVersion_ALegacyVersionFollowsNothing(t *testing.T) {
	v := PublishedVersion{Style: Style{Footer: StyleFooter{Text: "Old footer"}}}
	after := accountForTest()
	if v.ChangedBetween(WorkspaceStyle{}, after) || v.RefreshAgainst(after).Changes {
		t.Errorf("a version that follows nothing was reached by the account")
	}
}

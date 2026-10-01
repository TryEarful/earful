package domain_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
)

// TestThankYou_LinkAddresses: only an absolute web address is a link a
// respondent can be sent to. Anything a browser would run, or resolve
// against Earful itself, is refused.
func TestThankYou_LinkAddresses(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://example.com",
		"http://example.com/book?table=2#now",
		"HTTPS://Example.com/path",
	} {
		if _, err := domain.NewThankYou("Thanks", "Book", raw); err != nil {
			t.Errorf("%q was refused: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"/surveys",
		"//example.com/no-scheme",
		"example.com",
		"ftp://example.com/file",
		"mailto:someone@example.com",
		"https://user:secret@example.com",
		"https://example.com@evil.example",
		"https://",
		"https:///path",
		"https://exa mple.com",
		"https://example.com/\nnext",
		"https://example.com/" + strings.Repeat("a", 2000),
	} {
		_, err := domain.NewThankYou("Thanks", "Book", raw)
		if !errors.Is(err, domain.ErrThanksLinkURL) {
			t.Errorf("%q: err = %v, want ErrThanksLinkURL", raw, err)
		}
	}
}

// TestThankYou_LinkNeedsBothParts: a link is a label and an address
// together, or neither.
func TestThankYou_LinkNeedsBothParts(t *testing.T) {
	t.Parallel()
	if _, err := domain.NewThankYou("", "", "https://example.com"); !errors.Is(err, domain.ErrThanksLinkLabel) {
		t.Errorf("an address without a label: err = %v", err)
	}
	if _, err := domain.NewThankYou("", "Book", "  "); !errors.Is(err, domain.ErrThanksLinkAddress) {
		t.Errorf("a label without an address: err = %v", err)
	}
	thanks, err := domain.NewThankYou("", "Book", "https://example.com")
	if err != nil || !thanks.HasLink() || thanks.Message != "" {
		t.Errorf("a link without a message is allowed: %+v, %v", thanks, err)
	}
	empty, err := domain.NewThankYou("  \n ", " ", " ")
	if err != nil || !empty.IsZero() {
		t.Errorf("blank fields are the default page: %+v, %v", empty, err)
	}
}

func TestThankYou_Limits(t *testing.T) {
	t.Parallel()
	// Counted in characters, not bytes: a message in Greek is as long as
	// it looks.
	if _, err := domain.NewThankYou(strings.Repeat("λ", 1000), "", ""); err != nil {
		t.Errorf("a message at the limit was refused: %v", err)
	}
	var limit domain.LimitError
	_, err := domain.NewThankYou(strings.Repeat("λ", 1001), "", "")
	if !errors.As(err, &limit) || limit.Kind != domain.LimitThanksMessage {
		t.Errorf("an overlong message: err = %v", err)
	}
	_, err = domain.NewThankYou("", strings.Repeat("a", 61), "https://example.com")
	if !errors.As(err, &limit) || limit.Kind != domain.LimitThanksLabel {
		t.Errorf("an overlong label: err = %v", err)
	}
}

// TestThankYou_Paragraphs: the writer's layout is kept, as text.
func TestThankYou_Paragraphs(t *testing.T) {
	t.Parallel()
	thanks, err := domain.NewThankYou("\r\nThank you!\r\nSee you soon.\r\n\r\n\r\n<b>Bye</b>  \n", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if thanks.Message != "Thank you!\nSee you soon.\n\n\n<b>Bye</b>" {
		t.Errorf("message = %q", thanks.Message)
	}
	want := [][]string{{"Thank you!", "See you soon."}, {"<b>Bye</b>"}}
	if got := thanks.Paragraphs(); !reflect.DeepEqual(got, want) {
		t.Errorf("paragraphs = %q, want %q", got, want)
	}
}

// TestDraft_ThanksIsCheckedAtPublish: a rule added after a draft was
// written still gates the version it becomes.
func TestDraft_ThanksIsCheckedAtPublish(t *testing.T) {
	t.Parallel()
	d := domain.Draft{
		Questions: []domain.Question{{IdentityID: "a", Type: domain.ShortText, Text: "Why?"}},
		Thanks:    domain.ThankYou{Message: "Thanks", LinkLabel: "Go", LinkURL: "javascript:alert(1)"},
	}
	if err := d.ValidateForPublish(); !errors.Is(err, domain.ErrThanksLinkURL) {
		t.Errorf("err = %v, want ErrThanksLinkURL", err)
	}
	if err := d.SetThanks(d.Thanks); !errors.Is(err, domain.ErrThanksLinkURL) {
		t.Errorf("SetThanks: err = %v, want ErrThanksLinkURL", err)
	}
}

// TestDraft_ThanksTranslationGatesPublishing: a language is complete
// only once the thank you page is translated and read, and a change to
// the source sends it back for review.
func TestDraft_ThanksTranslationGatesPublishing(t *testing.T) {
	t.Parallel()
	d := domain.Draft{Questions: []domain.Question{{IdentityID: "a", Type: domain.ShortText, Text: "Why?"}}}
	if err := d.AddLanguage("nl"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTranslation("nl", "a", "Waarom?", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := d.ReadyToPublish(); err != nil {
		t.Fatalf("no thank you message, nothing more to translate: %v", err)
	}

	if err := d.SetThanks(domain.ThankYou{Message: "Thanks", LinkLabel: "Book", LinkURL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
	if !d.ThanksPending("nl") || !errors.Is(d.ReadyToPublish(), domain.ErrUnreviewedTrans) {
		t.Fatal("an untranslated thank you message must block publishing")
	}

	// Drafted but not read.
	if err := d.SetThanksTranslation("nl", "Bedankt", "Reserveer", false); err != nil {
		t.Fatal(err)
	}
	if !d.ThanksPending("nl") {
		t.Error("an unreviewed translation is still pending")
	}
	// Read, but missing the label the source has.
	if err := d.SetThanksTranslation("nl", "Bedankt", "", true); err != nil {
		t.Fatal(err)
	}
	if !d.ThanksPending("nl") {
		t.Error("a translation missing the link label is still pending")
	}
	if err := d.SetThanksTranslation("nl", "Bedankt", "Reserveer", true); err != nil {
		t.Fatal(err)
	}
	if err := d.ReadyToPublish(); err != nil {
		t.Fatalf("a reviewed translation should publish: %v", err)
	}
	got, ok := d.LocalizedThanks("nl")
	want := domain.ThankYou{Message: "Bedankt", LinkLabel: "Reserveer", LinkURL: "https://example.com"}
	if !ok || got != want {
		t.Errorf("LocalizedThanks = %+v, %v; want %+v", got, ok, want)
	}

	// The creator rewords the message: the translation is stale.
	d.Thanks.Message = "Thank you very much"
	if !d.ThanksPending("nl") || !d.ThanksStale("nl") {
		t.Error("a translation of an old wording must be reviewed again")
	}
	if _, ok := d.LocalizedThanks("nl"); ok {
		t.Error("a stale translation must not be published")
	}
}

// TestDraft_ThanksSurvivesEncoding: the thank you page is part of the
// stored draft, and a draft without one encodes as it always has.
func TestDraft_ThanksSurvivesEncoding(t *testing.T) {
	t.Parallel()
	plain, err := domain.Draft{}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "thanks") {
		t.Errorf("an empty thank you page was stored: %s", plain)
	}

	d := domain.Draft{Thanks: domain.ThankYou{Message: "Thanks\nagain", LinkLabel: "Book", LinkURL: "https://example.com"}}
	if err := d.AddLanguage("nl"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetThanksTranslation("nl", "Bedankt", "Reserveer", true); err != nil {
		t.Fatal(err)
	}
	raw, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := domain.ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Thanks != d.Thanks || !reflect.DeepEqual(back.Localizations["nl"].Thanks, d.Localizations["nl"].Thanks) {
		t.Errorf("round trip lost the thank you page: %+v", back)
	}
}

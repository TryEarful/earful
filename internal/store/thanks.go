package store

import (
	"encoding/json"
	"fmt"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// localizedThanksColumn is one language's entry in
// survey_versions.thanks_localizations (migration 00022). The link's
// address is not repeated per language: it is the version's own.
type localizedThanksColumn struct {
	Message   string `json:"message,omitempty"`
	LinkLabel string `json:"link_label,omitempty"`
}

// thanksColumns turns a draft's thank you page into the version's
// columns. A part left empty is NULL, which is what "show the default"
// has always meant for a version.
func thanksColumns(draft domain.Draft) (message, label, link *string, localized []byte, err error) {
	message = nullIfEmpty(draft.Thanks.Message)
	label = nullIfEmpty(draft.Thanks.LinkLabel)
	link = nullIfEmpty(draft.Thanks.LinkURL)

	byLang := map[string]localizedThanksColumn{}
	for _, lang := range draft.Languages() {
		translated, ok := draft.LocalizedThanks(lang)
		if !ok {
			continue
		}
		byLang[lang] = localizedThanksColumn{Message: translated.Message, LinkLabel: translated.LinkLabel}
	}
	if len(byLang) > 0 {
		localized, err = json.Marshal(byLang)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("store: encode localized thanks: %w", err)
		}
	}
	return message, label, link, localized, nil
}

// thanksFromVersion reads a version's thank you page, and the same page in
// each language it was published with.
func thanksFromVersion(v db.SurveyVersion) (domain.ThankYou, map[string]domain.ThankYou, error) {
	thanks := domain.ThankYou{
		Message:   deref(v.ThanksMessage),
		LinkLabel: deref(v.ThanksLinkLabel),
		LinkURL:   deref(v.ThanksLinkUrl),
	}
	if len(v.ThanksLocalizations) == 0 {
		return thanks, nil, nil
	}
	var columns map[string]localizedThanksColumn
	if err := json.Unmarshal(v.ThanksLocalizations, &columns); err != nil {
		return domain.ThankYou{}, nil, fmt.Errorf("store: decode localized thanks: %w", err)
	}
	byLang := make(map[string]domain.ThankYou, len(columns))
	for lang, c := range columns {
		byLang[lang] = domain.ThankYou{Message: c.Message, LinkLabel: c.LinkLabel, LinkURL: thanks.LinkURL}
	}
	return thanks, byLang, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

package store

import (
	"encoding/json"
	"fmt"

	"github.com/TryEarful/earful/internal/domain"
)

// frozenStyle is survey_versions.style (migration 00024): the style a
// version was published with and, under localizations, its words in each
// language the version carries a reviewed translation of them for. The
// name, the theme and the links' addresses are not repeated per
// language: they are the version's own.
type frozenStyle struct {
	domain.Style
	Localizations map[string]domain.StyleWords `json:"localizations,omitempty"`
}

// styleColumn turns a draft's style into the version's style column. No
// style is NULL, which is what a version published before a survey
// could have one holds. A language is frozen only once its translation
// has been reviewed against the current wording, as the thank you page's
// is.
func styleColumn(draft domain.Draft) ([]byte, error) {
	if draft.Style.IsZero() {
		return nil, nil
	}
	frozen := frozenStyle{Style: draft.Style}
	for _, lang := range draft.Languages() {
		if _, ok := draft.LocalizedStyle(lang); !ok {
			continue
		}
		if frozen.Localizations == nil {
			frozen.Localizations = map[string]domain.StyleWords{}
		}
		frozen.Localizations[lang] = draft.Localizations[lang].Style.StyleWords
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return nil, fmt.Errorf("store: encode style: %w", err)
	}
	return encoded, nil
}

// styleFromColumn reads a version's style back, and the same style
// worded in each language it was published with.
func styleFromColumn(raw []byte) (domain.Style, map[string]domain.Style, error) {
	if len(raw) == 0 {
		return domain.Style{}, nil, nil
	}
	var frozen frozenStyle
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return domain.Style{}, nil, fmt.Errorf("store: decode style: %w", err)
	}
	if len(frozen.Localizations) == 0 {
		return frozen.Style, nil, nil
	}
	byLang := make(map[string]domain.Style, len(frozen.Localizations))
	for lang, words := range frozen.Localizations {
		byLang[lang] = frozen.Style.WithWords(words)
	}
	return frozen.Style, byLang, nil
}

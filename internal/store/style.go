package store

import (
	"encoding/json"
	"fmt"

	"github.com/TryEarful/earful/internal/domain"
)

// styleColumn turns a draft's style into the version's style column
// (migration 00024). No style is NULL, which is what a version published
// before a survey could have one holds.
func styleColumn(draft domain.Draft) ([]byte, error) {
	if draft.Style.IsZero() {
		return nil, nil
	}
	encoded, err := json.Marshal(draft.Style)
	if err != nil {
		return nil, fmt.Errorf("store: encode style: %w", err)
	}
	return encoded, nil
}

// styleFromColumn reads a version's style back.
func styleFromColumn(raw []byte) (domain.Style, error) {
	if len(raw) == 0 {
		return domain.Style{}, nil
	}
	var style domain.Style
	if err := json.Unmarshal(raw, &style); err != nil {
		return domain.Style{}, fmt.Errorf("store: decode style: %w", err)
	}
	return style, nil
}

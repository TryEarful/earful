// Package domain holds the entities and invariants that are true
// regardless of storage or transport: what a question may look like, what
// a draft may contain, when a survey is open. No SQL, no HTTP.
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// QuestionType is one of the eight MVP question types (SPEC.md story 8).
// The set is closed: the database CHECK constraint, the renderer and this
// list must agree, so adding a type is a deliberate three-place change.
type QuestionType string

const (
	LongText       QuestionType = "long_text"
	ShortText      QuestionType = "short_text"
	SingleChoice   QuestionType = "single_choice"
	MultipleChoice QuestionType = "multiple_choice"
	RatingScale    QuestionType = "rating_scale"
	NPS            QuestionType = "nps"
	YesNo          QuestionType = "yes_no"
	Dropdown       QuestionType = "dropdown"
)

// QuestionTypes lists every supported type in the order the editor offers
// them: the two text types first (the ones voice answering serves), then
// choice, then scales.
var QuestionTypes = []QuestionType{
	LongText, ShortText, SingleChoice, MultipleChoice,
	RatingScale, NPS, YesNo, Dropdown,
}

// Label is the human name shown in the editor.
func (t QuestionType) Label() string {
	switch t {
	case LongText:
		return "Long text"
	case ShortText:
		return "Short text"
	case SingleChoice:
		return "Single choice"
	case MultipleChoice:
		return "Multiple choice"
	case RatingScale:
		return "Rating scale"
	case NPS:
		return "Net Promoter Score"
	case YesNo:
		return "Yes / No"
	case Dropdown:
		return "Dropdown"
	default:
		return string(t)
	}
}

// Hint explains when to reach for a type; shown beside the picker.
func (t QuestionType) Hint() string {
	switch t {
	case LongText:
		return "An open answer. Respondents can speak it instead of typing."
	case ShortText:
		return "A word or a line — a name, a role, a city."
	case SingleChoice:
		return "Pick exactly one option."
	case MultipleChoice:
		return "Pick any number of options."
	case RatingScale:
		return "A numeric scale, e.g. 1–5."
	case NPS:
		return "The standard 0–10 recommendation question."
	case YesNo:
		return "A straight yes or no."
	case Dropdown:
		return "One option from a long list, in a compact control."
	}
	return ""
}

// NeedsOptions reports whether a type requires an author-supplied option
// list. Rating scales and NPS have bounds instead; text and yes/no have
// neither.
func (t QuestionType) NeedsOptions() bool {
	switch t {
	case SingleChoice, MultipleChoice, Dropdown:
		return true
	}
	return false
}

// NeedsScale reports whether a type carries numeric bounds the author can
// set. NPS is fixed at 0–10 by definition, so it does not.
func (t QuestionType) NeedsScale() bool { return t == RatingScale }

// AcceptsVoice reports whether a respondent can answer by speaking
// (ADR-0004: voice is for open text).
func (t QuestionType) AcceptsVoice() bool {
	return t == LongText || t == ShortText
}

func (t QuestionType) valid() bool {
	for _, known := range QuestionTypes {
		if known == t {
			return true
		}
	}
	return false
}

// NPS bounds are fixed by the metric's definition, not by the author.
const (
	NPSMin = 0
	NPSMax = 10
)

// Rating-scale bounds the editor accepts. The ceiling is a usability
// judgement, not a storage limit: past ~10 points a scale stops being
// readable on a phone.
const (
	ratingScaleMinLow  = 0
	ratingScaleMinHigh = 1
	ratingScaleMaxCap  = 10
)

// Question is one question as it exists in a Draft. Once published it is
// frozen into a `questions` row; the shape is deliberately the same so
// the respondent renderer serves drafts (preview) and published versions
// through identical code.
type Question struct {
	// IdentityID is the Question Identity (ADR-0001): minted when the
	// question first appears in a draft and preserved through every
	// rewording, so results aggregate across versions.
	IdentityID string `json:"identity_id"`
	Type       QuestionType `json:"type"`
	Text       string       `json:"text"`
	Options    []string     `json:"options,omitempty"`
	Required   bool         `json:"required"`
	ScaleMin   int          `json:"scale_min,omitempty"`
	ScaleMax   int          `json:"scale_max,omitempty"`
}

// DefaultRatingScaleMin/Max are the bounds assumed for a rating scale
// that carries none. Only rows published before migration 00009 can be in
// that state — publish did not persist the bounds then — and these are
// the values the editor itself offers by default, so an old survey reads
// back as the scale it almost certainly was.
const (
	DefaultRatingScaleMin = 1
	DefaultRatingScaleMax = 5
)

// Scale returns the effective bounds for scale-shaped types. NPS is fixed
// by its definition; a rating scale with no usable bounds falls back to
// the defaults rather than degenerating to a single point.
func (q Question) Scale() (min, max int) {
	if q.Type == NPS {
		return NPSMin, NPSMax
	}
	if q.ScaleMax <= q.ScaleMin {
		return DefaultRatingScaleMin, DefaultRatingScaleMax
	}
	return q.ScaleMin, q.ScaleMax
}

// ScalePoints returns every selectable value of a scale question.
func (q Question) ScalePoints() []int {
	min, max := q.Scale()
	if max < min {
		return nil
	}
	points := make([]int, 0, max-min+1)
	for v := min; v <= max; v++ {
		points = append(points, v)
	}
	return points
}

// ErrEmptyQuestionText and friends are validation errors surfaced to the
// editor as inline messages, so they read as guidance, not diagnostics.
var (
	ErrEmptyQuestionText = errors.New("give the question some text")
	ErrUnknownType       = errors.New("choose a question type")
	ErrTooFewOptions     = errors.New("list at least two options")
	ErrEmptyOption       = errors.New("remove the blank option")
	ErrDuplicateOption   = errors.New("two options are identical")
	ErrBadScale          = fmt.Errorf("the scale must start at 0 or 1 and end no higher than %d", ratingScaleMaxCap)
)

// maxQuestionTextLen keeps a question readable and bounds the row; a
// question longer than this is a paragraph, not a question.
const maxQuestionTextLen = 500

// Validate enforces every invariant a question must satisfy before it can
// enter a draft. Publishing re-runs it, so a draft that predates a rule
// cannot be published in violation of it.
func (q Question) Validate() error {
	if !q.Type.valid() {
		return ErrUnknownType
	}
	if strings.TrimSpace(q.Text) == "" {
		return ErrEmptyQuestionText
	}
	if len([]rune(q.Text)) > maxQuestionTextLen {
		return fmt.Errorf("keep the question under %d characters", maxQuestionTextLen)
	}
	if q.Type.NeedsOptions() {
		if len(q.Options) < 2 {
			return ErrTooFewOptions
		}
		seen := make(map[string]bool, len(q.Options))
		for _, opt := range q.Options {
			trimmed := strings.TrimSpace(opt)
			if trimmed == "" {
				return ErrEmptyOption
			}
			if seen[strings.ToLower(trimmed)] {
				return ErrDuplicateOption
			}
			seen[strings.ToLower(trimmed)] = true
		}
	}
	if q.Type.NeedsScale() {
		if q.ScaleMin != ratingScaleMinLow && q.ScaleMin != ratingScaleMinHigh {
			return ErrBadScale
		}
		if q.ScaleMax > ratingScaleMaxCap || q.ScaleMax <= q.ScaleMin {
			return ErrBadScale
		}
	}
	return nil
}

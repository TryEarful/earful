package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
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

// --- the pictures of a style (migration 00025) -----------------------------

// MaxSurveyImages is how many pictures are stored for one survey: the
// ones its versions show, the draft's, and what was replaced and is not
// yet purged. It bounds what one survey can put in the database
// (ADR-0018).
const MaxSurveyImages = 10

// ErrTooManyImages refuses a picture for a survey that already stores
// MaxSurveyImages, every one of them shown by a version or the draft.
var ErrTooManyImages = error(domain.LimitError{Kind: domain.LimitStyleImages, Limit: MaxSurveyImages})

// Image is a stored picture as it is served.
type Image struct {
	// SHA256 is the hash of Bytes in hex: the picture's address.
	SHA256      string
	ContentType string
	Bytes       []byte
}

// NewImage is a picture to store, as internal/styleimage prepared it.
type NewImage struct {
	SHA256      string
	ContentType string
	Width       int
	Height      int
	Bytes       []byte
}

// ImageLimitError refuses a picture for a survey that already stores
// MaxSurveyImages, every one of them shown by a version, the draft, or
// the style being saved, or for an account that already stores
// MaxWorkspaceImages. SHA256 names the picture that did not fit; Limit
// is which limit it met, a survey's where it is nil.
type ImageLimitError struct {
	SHA256 string
	Limit  error
}

func (e ImageLimitError) Error() string { return e.Unwrap().Error() }

func (e ImageLimitError) Unwrap() error {
	if e.Limit == nil {
		return ErrTooManyImages
	}
	return e.Limit
}

// SaveStyle stores the pictures a style brings and the draft that refers
// to them, together: a draft never refers to a picture that is not
// stored. Storing a picture already stored does nothing. A survey at its
// limit first loses the pictures that nothing shows, never one the draft
// being saved refers to, and is refused only if that makes no room. The
// survey is held while this happens, so two saves of its style take turns
// rather than counting, or removing, each other's pictures.
func (s *Surveys) SaveStyle(ctx context.Context, surveyID, userID uuid.UUID, draft domain.Draft, images []NewImage, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin save style: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	if _, err := q.LockSurveyForStyle(ctx, surveyID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("store: hold survey: %w", err)
	}
	keep := styleHashes(draft.Style)
	for _, img := range images {
		if err := saveImage(ctx, q, surveyID, img, keep, now); err != nil {
			return err
		}
	}
	if err := saveDraft(ctx, q, surveyID, userID, draft, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveImage stores one picture through q, inside the caller's
// transaction, keeping the pictures in keep whatever the limit.
func saveImage(ctx context.Context, q *db.Queries, surveyID uuid.UUID, img NewImage, keep []string, now time.Time) error {
	hash, err := hex.DecodeString(img.SHA256)
	if err != nil {
		return fmt.Errorf("store: image hash: %w", err)
	}
	exists, err := q.SurveyImageExists(ctx, db.SurveyImageExistsParams{SurveyID: surveyID, Sha256: hash})
	if err != nil {
		return fmt.Errorf("store: find image: %w", err)
	}
	if exists {
		return nil
	}
	count, err := q.CountSurveyImages(ctx, surveyID)
	if err != nil {
		return fmt.Errorf("store: count images: %w", err)
	}
	if count >= MaxSurveyImages {
		removed, err := q.DeleteUnusedSurveyImages(ctx, db.DeleteUnusedSurveyImagesParams{SurveyID: surveyID, Keep: keep})
		if err != nil {
			return fmt.Errorf("store: remove unused images: %w", err)
		}
		if count-removed >= MaxSurveyImages {
			return ImageLimitError{SHA256: img.SHA256}
		}
	}
	if err := q.CreateSurveyImage(ctx, db.CreateSurveyImageParams{
		SurveyID:    surveyID,
		Sha256:      hash,
		ContentType: img.ContentType,
		Width:       int32(img.Width),
		Height:      int32(img.Height),
		SizeBytes:   int32(len(img.Bytes)),
		Bytes:       img.Bytes,
		CreatedAt:   now,
	}); err != nil {
		return fmt.Errorf("store: create image: %w", err)
	}
	return nil
}

// ErrStyleImageMissing refuses to publish a draft whose style refers to a
// picture the survey does not store. A version cannot be changed once it
// is published, so it would show a broken picture for good.
var ErrStyleImageMissing = errors.New("a picture of the survey's style is missing, so choose it again on the Style tab")

// checkStyleImages refuses a draft whose style refers to a picture the
// survey does not store, through q, inside the caller's transaction. The
// caller holds the survey (LockSurveyForStyle), so nothing removes a
// picture between this check and the version that shows it.
func checkStyleImages(ctx context.Context, q *db.Queries, surveyID uuid.UUID, style domain.Style) error {
	hashes := styleHashes(style)
	if len(hashes) == 0 {
		return nil
	}
	stored, err := q.CountSurveyImagesOf(ctx, db.CountSurveyImagesOfParams{SurveyID: surveyID, Hashes: hashes})
	if err != nil {
		return fmt.Errorf("store: count style images: %w", err)
	}
	if int(stored) != len(hashes) {
		return ErrStyleImageMissing
	}
	return nil
}

// styleHashes are the addresses of the pictures a style shows, each once.
// Never nil: the queries read it as an array, and NULL is not one.
func styleHashes(style domain.Style) []string {
	hashes := []string{}
	for _, img := range style.Images() {
		if !slices.Contains(hashes, img.SHA256) {
			hashes = append(hashes, img.SHA256)
		}
	}
	return hashes
}

// DraftImage is a picture stored for a survey, as its creator may see
// it: on the Style tab and in the preview, before any version shows it.
// A survey in another workspace has no pictures as far as the caller can
// tell.
func (s *Surveys) DraftImage(ctx context.Context, workspaceID, surveyID uuid.UUID, sha string) (Image, error) {
	hash, ok := imageHash(sha)
	if !ok {
		return Image{}, ErrNotFound
	}
	row, err := s.q.GetSurveyImageForWorkspace(ctx, db.GetSurveyImageForWorkspaceParams{
		SurveyID: surveyID, Sha256: hash, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, fmt.Errorf("store: get draft image: %w", err)
	}
	return Image{SHA256: sha, ContentType: row.ContentType, Bytes: row.Bytes}, nil
}

// PublishedImage is a picture anybody may fetch: one that a published
// version of a survey that has not been deleted shows.
func (s *Surveys) PublishedImage(ctx context.Context, sha string) (Image, error) {
	hash, ok := imageHash(sha)
	if !ok {
		return Image{}, ErrNotFound
	}
	row, err := s.q.GetPublishedSurveyImage(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, fmt.Errorf("store: get published image: %w", err)
	}
	return Image{SHA256: sha, ContentType: row.ContentType, Bytes: row.Bytes}, nil
}

// ImageMeta is what is said of a stored picture without its bytes.
type ImageMeta struct {
	ContentType string
	Size        int
}

// PublishedImageMeta is PublishedImage without the bytes: whether anybody
// may fetch the picture, its type and its size.
func (s *Surveys) PublishedImageMeta(ctx context.Context, sha string) (ImageMeta, error) {
	hash, ok := imageHash(sha)
	if !ok {
		return ImageMeta{}, ErrNotFound
	}
	row, err := s.q.GetPublishedSurveyImageMeta(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImageMeta{}, ErrNotFound
	}
	if err != nil {
		return ImageMeta{}, fmt.Errorf("store: get published image: %w", err)
	}
	return ImageMeta{ContentType: row.ContentType, Size: int(row.SizeBytes)}, nil
}

// PublishedImages are the pictures a survey's versions show, for the
// workspace export.
func (s *Surveys) PublishedImages(ctx context.Context, surveyID uuid.UUID) ([]Image, error) {
	rows, err := s.q.ListPublishedImagesForSurvey(ctx, surveyID)
	if err != nil {
		return nil, fmt.Errorf("store: list published images: %w", err)
	}
	out := make([]Image, 0, len(rows))
	for _, row := range rows {
		out = append(out, Image{SHA256: hex.EncodeToString(row.Sha256), ContentType: row.ContentType, Bytes: row.Bytes})
	}
	return out, nil
}

// imageHash reads a picture's address. Only a SHA-256 in lower case hex
// is one, so nothing else reaches the database.
func imageHash(sha string) ([]byte, bool) {
	if len(sha) != 64 || strings.ToLower(sha) != sha {
		return nil, false
	}
	hash, err := hex.DecodeString(sha)
	return hash, err == nil
}

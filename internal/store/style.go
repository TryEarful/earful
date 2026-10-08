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
	// Follows names the parts of the style the version took from its
	// account's style (ADR-0023), so that a later change to the account
	// can tell which live versions show what changed. A version published
	// before a survey could follow an account names none.
	Follows domain.StyleParts `json:"follows,omitzero"`
}

// styleColumn turns a draft's style, resolved against its account's, into
// the version's style column: the style the version's pages are drawn in,
// complete, so that nothing a respondent sees changes with the account
// afterwards. A version with no style and no part taken from an account
// is NULL, which is what a version published before a survey could have
// one holds. A language is frozen only once both its translations, the
// survey's own and the account's, have been reviewed against the current
// wording, as the thank you page's is.
func styleColumn(draft domain.Draft, account domain.WorkspaceStyle) ([]byte, error) {
	frozen := frozenStyle{Style: draft.ResolvedStyle(account.Style), Follows: draft.FollowedParts()}
	if frozen.Style.IsZero() && frozen.Follows.IsZero() {
		return nil, nil
	}
	for _, lang := range draft.Languages() {
		words, ok := draft.ResolvedStyleWords(account, lang)
		if !ok || words.IsZero() {
			continue
		}
		if frozen.Localizations == nil {
			frozen.Localizations = map[string]domain.StyleWords{}
		}
		frozen.Localizations[lang] = words
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
//
// A part the survey made its own while keeping one of its account's
// pictures gets its own copy of the picture here (ADR-0023), so the
// draft refers only to pictures the survey stores.
func (s *Surveys) SaveStyle(ctx context.Context, surveyID, userID uuid.UUID, draft domain.Draft, images []NewImage, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin save style: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	workspaceID, _, err := holdForStyle(ctx, q, surveyID)
	if err != nil {
		return err
	}
	keep := styleHashes(draft.Style)
	for _, img := range images {
		if err := saveImage(ctx, q, surveyID, img, keep, now); err != nil {
			return err
		}
	}
	if err := ensureSurveyImages(ctx, q, workspaceID, surveyID, keep, now); err != nil {
		return err
	}
	if err := saveDraft(ctx, q, surveyID, userID, draft, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// holdForStyle holds a survey and reads its account's style, through q,
// inside the caller's transaction: the survey first, then the account's
// style, which is held against change until the transaction ends. Every
// path that holds both holds them in this order. A workspace with no
// account style has the zero one.
func holdForStyle(ctx context.Context, q *db.Queries, surveyID uuid.UUID) (uuid.UUID, domain.WorkspaceStyle, error) {
	workspaceID, err := q.LockSurveyForStyle(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.WorkspaceStyle{}, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, domain.WorkspaceStyle{}, fmt.Errorf("store: hold survey: %w", err)
	}
	row, err := q.ShareWorkspaceStyle(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspaceID, domain.WorkspaceStyle{}, nil
	}
	if err != nil {
		return uuid.Nil, domain.WorkspaceStyle{}, fmt.Errorf("store: read workspace style: %w", err)
	}
	account, err := workspaceStyleFrom(row.Style, row.Localizations, row.UpdatedAt)
	if err != nil {
		return uuid.Nil, domain.WorkspaceStyle{}, err
	}
	return workspaceID, account, nil
}

// ensureSurveyImages copies into the survey, through q, inside the
// caller's transaction, each picture of hashes it does not store yet
// from its account's pictures (ADR-0023). A survey at its limit first
// loses the pictures nothing shows, never one in hashes, and is refused
// only if that makes no room. A picture neither the survey nor its
// account stores is left for checkStyleImages to refuse.
func ensureSurveyImages(ctx context.Context, q *db.Queries, workspaceID, surveyID uuid.UUID, hashes []string, now time.Time) error {
	if len(hashes) == 0 {
		return nil
	}
	copies, count, err := countCopies(ctx, q, workspaceID, surveyID, hashes)
	if err != nil {
		return err
	}
	if copies == 0 {
		return nil
	}
	if count+copies > MaxSurveyImages {
		removed, err := q.DeleteUnusedSurveyImages(ctx, db.DeleteUnusedSurveyImagesParams{SurveyID: surveyID, Keep: hashes})
		if err != nil {
			return fmt.Errorf("store: remove unused images: %w", err)
		}
		if count-removed+copies > MaxSurveyImages {
			return ImageLimitError{}
		}
	}
	if _, err := q.CopyWorkspaceImagesToSurvey(ctx, db.CopyWorkspaceImagesToSurveyParams{
		SurveyID: surveyID, CreatedAt: now, WorkspaceID: workspaceID, Hashes: hashes,
	}); err != nil {
		return fmt.Errorf("store: copy account images: %w", err)
	}
	return nil
}

// countCopies is, through q, how many of the account's pictures among
// hashes the survey would be given a copy of, and how many pictures the
// survey stores now. A picture the account does not store either is not
// counted: there is nothing to copy, and checkStyleImages refuses it.
func countCopies(ctx context.Context, q *db.Queries, workspaceID, surveyID uuid.UUID, hashes []string) (copies, count int64, err error) {
	copies, err = q.CountCopyableWorkspaceImages(ctx, db.CountCopyableWorkspaceImagesParams{
		WorkspaceID: workspaceID, Hashes: hashes, SurveyID: surveyID,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("store: count account images to copy: %w", err)
	}
	if copies == 0 {
		return 0, 0, nil
	}
	count, err = q.CountSurveyImages(ctx, surveyID)
	if err != nil {
		return 0, 0, fmt.Errorf("store: count images: %w", err)
	}
	return copies, count, nil
}

// roomForCopies reports, through q, whether the survey has room for its
// copies of the account's pictures among hashes, counting out the
// pictures nothing shows as ensureSurveyImages would remove them. It
// removes nothing.
func roomForCopies(ctx context.Context, q *db.Queries, workspaceID, surveyID uuid.UUID, hashes []string) (bool, error) {
	if len(hashes) == 0 {
		return true, nil
	}
	copies, count, err := countCopies(ctx, q, workspaceID, surveyID, hashes)
	if err != nil {
		return false, err
	}
	if count+copies <= MaxSurveyImages {
		return true, nil
	}
	unused, err := q.CountUnusedSurveyImages(ctx, db.CountUnusedSurveyImagesParams{SurveyID: surveyID, Keep: hashes})
	if err != nil {
		return false, fmt.Errorf("store: count unused images: %w", err)
	}
	return count-unused+copies <= MaxSurveyImages, nil
}

// picturesMissing reports, through q, whether any picture among hashes is
// stored neither for the survey nor by its account: one ensureSurveyImages
// could not copy, which publishing would refuse as missing.
func picturesMissing(ctx context.Context, q *db.Queries, workspaceID, surveyID uuid.UUID, hashes []string) (bool, error) {
	if len(hashes) == 0 {
		return false, nil
	}
	stored, err := q.CountSurveyImagesOf(ctx, db.CountSurveyImagesOfParams{SurveyID: surveyID, Hashes: hashes})
	if err != nil {
		return false, fmt.Errorf("store: count style images: %w", err)
	}
	copies, err := q.CountCopyableWorkspaceImages(ctx, db.CountCopyableWorkspaceImagesParams{
		WorkspaceID: workspaceID, Hashes: hashes, SurveyID: surveyID,
	})
	if err != nil {
		return false, fmt.Errorf("store: count account images to copy: %w", err)
	}
	return int(stored+copies) < len(hashes), nil
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

// DraftImage is a picture stored for a survey, or for its account's
// style, as its creator may see it: on the Style tab and in the preview,
// before any version shows it. A survey in another workspace has no
// pictures as far as the caller can tell.
func (s *Surveys) DraftImage(ctx context.Context, workspaceID, surveyID uuid.UUID, sha string) (Image, error) {
	hash, ok := imageHash(sha)
	if !ok {
		return Image{}, ErrNotFound
	}
	row, err := s.q.GetSurveyImageForWorkspace(ctx, db.GetSurveyImageForWorkspaceParams{
		SurveyID: surveyID, Sha256: hash, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A part the survey follows shows its account's picture, which the
		// survey copies only when it is published (ADR-0023). It is the
		// caller's own workspace's picture, so it is theirs to see.
		return s.WorkspaceImage(ctx, workspaceID, sha)
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

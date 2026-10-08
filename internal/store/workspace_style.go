package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// MaxWorkspaceImages is how many pictures are stored for one account's
// style: the ones it shows and what was replaced and is not yet purged.
// It bounds what one account can put in the database, as
// MaxSurveyImages does for a survey (ADR-0023).
const MaxWorkspaceImages = 10

// ErrTooManyWorkspaceImages refuses a picture for an account that already
// stores MaxWorkspaceImages, every one of them shown by its style.
var ErrTooManyWorkspaceImages = error(domain.LimitError{Kind: domain.LimitAccountStyleImages, Limit: MaxWorkspaceImages})

// WorkspaceStyle is the account's style (ADR-0023), or the zero value
// for a workspace that has none. A deleted workspace's is read too, so
// the export a closing account sends itself can carry it.
func (s *Surveys) WorkspaceStyle(ctx context.Context, workspaceID uuid.UUID) (domain.WorkspaceStyle, error) {
	row, err := s.q.GetWorkspaceStyle(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkspaceStyle{}, nil
	}
	if err != nil {
		return domain.WorkspaceStyle{}, fmt.Errorf("store: get workspace style: %w", err)
	}
	return workspaceStyleFrom(row.Style, row.Localizations, row.UpdatedAt)
}

// workspaceStyleFrom reads an account's style from its row's columns.
func workspaceStyleFrom(style, localizations []byte, updatedAt time.Time) (domain.WorkspaceStyle, error) {
	out := domain.WorkspaceStyle{UpdatedAt: updatedAt}
	if err := json.Unmarshal(style, &out.Style); err != nil {
		return domain.WorkspaceStyle{}, fmt.Errorf("store: decode workspace style: %w", err)
	}
	if len(localizations) > 0 {
		if err := json.Unmarshal(localizations, &out.Localizations); err != nil {
			return domain.WorkspaceStyle{}, fmt.Errorf("store: decode workspace style languages: %w", err)
		}
	}
	if len(out.Localizations) == 0 {
		out.Localizations = nil
	}
	return out, nil
}

// SaveWorkspaceStyle stores the pictures an account's style brings and
// the style that refers to them, together, as SaveStyle does for a
// survey: the style never refers to a picture that is not stored. An
// account at its limit first loses the pictures its style no longer
// shows, never one the style being saved refers to, and is refused only
// if that makes no room. The account's row is held while this happens,
// so two saves take turns.
func (s *Surveys) SaveWorkspaceStyle(ctx context.Context, workspaceID, userID uuid.UUID, style domain.Style, images []NewImage, now time.Time) error {
	encoded, err := json.Marshal(style)
	if err != nil {
		return fmt.Errorf("store: encode workspace style: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin save workspace style: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	if err := q.EnsureWorkspaceStyle(ctx, db.EnsureWorkspaceStyleParams{WorkspaceID: workspaceID, UpdatedAt: now}); err != nil {
		return fmt.Errorf("store: make workspace style: %w", err)
	}
	if _, err := q.LockWorkspaceStyle(ctx, workspaceID); err != nil {
		return fmt.Errorf("store: hold workspace style: %w", err)
	}
	keep := styleHashes(style)
	for _, img := range images {
		if err := saveWorkspaceImage(ctx, q, workspaceID, img, keep, now); err != nil {
			return err
		}
	}
	user := uuid.NullUUID{UUID: userID, Valid: userID != uuid.Nil}
	if err := q.UpdateWorkspaceStyle(ctx, db.UpdateWorkspaceStyleParams{
		WorkspaceID: workspaceID, Style: encoded, UpdatedBy: user, UpdatedAt: now,
	}); err != nil {
		return fmt.Errorf("store: update workspace style: %w", err)
	}
	return tx.Commit(ctx)
}

// SaveWorkspaceStyleTranslation changes the account style's words in one
// language, reading the style afresh under its row's hold so that the
// translation is checked against the wording it is saved beside: change
// is called with the account's style as it stands and changes it in
// place. A workspace with no account style has nothing to translate and
// is refused with ErrNotFound.
func (s *Surveys) SaveWorkspaceStyleTranslation(ctx context.Context, workspaceID, userID uuid.UUID, now time.Time, change func(*domain.WorkspaceStyle) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin save workspace style languages: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	if _, err := q.LockWorkspaceStyle(ctx, workspaceID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("store: hold workspace style: %w", err)
	}
	row, err := q.GetWorkspaceStyle(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("store: get workspace style: %w", err)
	}
	current, err := workspaceStyleFrom(row.Style, row.Localizations, row.UpdatedAt)
	if err != nil {
		return err
	}
	if err := change(&current); err != nil {
		return err
	}
	encoded := []byte("{}")
	if len(current.Localizations) > 0 {
		if encoded, err = json.Marshal(current.Localizations); err != nil {
			return fmt.Errorf("store: encode workspace style languages: %w", err)
		}
	}
	user := uuid.NullUUID{UUID: userID, Valid: userID != uuid.Nil}
	if err := q.UpdateWorkspaceStyleLocalizations(ctx, db.UpdateWorkspaceStyleLocalizationsParams{
		WorkspaceID: workspaceID, Localizations: encoded, UpdatedBy: user, UpdatedAt: now,
	}); err != nil {
		return fmt.Errorf("store: update workspace style languages: %w", err)
	}
	return tx.Commit(ctx)
}

// WorkspaceDraftLanguages are the languages the workspace's surveys are
// being translated into, as their drafts hold them, in order.
func (s *Surveys) WorkspaceDraftLanguages(ctx context.Context, workspaceID uuid.UUID) ([]string, error) {
	langs, err := s.q.WorkspaceDraftLanguages(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("store: list workspace draft languages: %w", err)
	}
	return langs, nil
}

// saveWorkspaceImage stores one of an account's pictures through q,
// inside the caller's transaction, keeping the pictures in keep whatever
// the limit.
func saveWorkspaceImage(ctx context.Context, q *db.Queries, workspaceID uuid.UUID, img NewImage, keep []string, now time.Time) error {
	hash, err := hex.DecodeString(img.SHA256)
	if err != nil {
		return fmt.Errorf("store: image hash: %w", err)
	}
	exists, err := q.WorkspaceImageExists(ctx, db.WorkspaceImageExistsParams{WorkspaceID: workspaceID, Sha256: hash})
	if err != nil {
		return fmt.Errorf("store: find workspace image: %w", err)
	}
	if exists {
		return nil
	}
	count, err := q.CountWorkspaceImages(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("store: count workspace images: %w", err)
	}
	if count >= MaxWorkspaceImages {
		removed, err := q.DeleteUnusedWorkspaceImages(ctx, db.DeleteUnusedWorkspaceImagesParams{WorkspaceID: workspaceID, Keep: keep})
		if err != nil {
			return fmt.Errorf("store: remove unused workspace images: %w", err)
		}
		if count-removed >= MaxWorkspaceImages {
			return ImageLimitError{SHA256: img.SHA256, Limit: ErrTooManyWorkspaceImages}
		}
	}
	if err := q.CreateWorkspaceImage(ctx, db.CreateWorkspaceImageParams{
		WorkspaceID: workspaceID,
		Sha256:      hash,
		ContentType: img.ContentType,
		Width:       int32(img.Width),
		Height:      int32(img.Height),
		SizeBytes:   int32(len(img.Bytes)),
		Bytes:       img.Bytes,
		CreatedAt:   now,
	}); err != nil {
		return fmt.Errorf("store: create workspace image: %w", err)
	}
	return nil
}

// WorkspaceImage is one of an account's pictures, for its creator's own
// pages. Another workspace's pictures are not found.
func (s *Surveys) WorkspaceImage(ctx context.Context, workspaceID uuid.UUID, sha string) (Image, error) {
	hash, ok := imageHash(sha)
	if !ok {
		return Image{}, ErrNotFound
	}
	row, err := s.q.GetWorkspaceImage(ctx, db.GetWorkspaceImageParams{WorkspaceID: workspaceID, Sha256: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, fmt.Errorf("store: get workspace image: %w", err)
	}
	return Image{SHA256: sha, ContentType: row.ContentType, Bytes: row.Bytes}, nil
}

// WorkspaceStyleImages are the pictures an account's style shows, for
// the workspace export.
func (s *Surveys) WorkspaceStyleImages(ctx context.Context, workspaceID uuid.UUID) ([]Image, error) {
	rows, err := s.q.ListWorkspaceStyleImages(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("store: list workspace style images: %w", err)
	}
	out := make([]Image, 0, len(rows))
	for _, row := range rows {
		out = append(out, Image{SHA256: hex.EncodeToString(row.Sha256), ContentType: row.ContentType, Bytes: row.Bytes})
	}
	return out, nil
}

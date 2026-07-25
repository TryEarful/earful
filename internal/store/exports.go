package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/store/db"
)

// ExportJob is one workspace export: requested, built in the background,
// then downloadable until it expires (M7-T3).
type ExportJob struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Status      string
	SizeBytes   int64
	Error       string
	CreatedAt   time.Time
	FinishedAt  *time.Time
	ExpiresAt   *time.Time
}

// Export job states.
const (
	ExportPending = "pending"
	ExportRunning = "running"
	ExportReady   = "ready"
	ExportFailed  = "failed"
)

// InProgress reports whether the job is still being built.
func (j ExportJob) InProgress() bool { return j.Status == ExportPending || j.Status == ExportRunning }

// Downloadable reports whether the archive is ready and unexpired.
func (j ExportJob) Downloadable(now time.Time) bool {
	return j.Status == ExportReady && j.ExpiresAt != nil && now.Before(*j.ExpiresAt)
}

// CreateExportJob queues a build.
func (s *Surveys) CreateExportJob(ctx context.Context, workspaceID, userID uuid.UUID, now time.Time) (ExportJob, error) {
	row, err := s.q.CreateExportJob(ctx, db.CreateExportJobParams{
		WorkspaceID: workspaceID,
		RequestedBy: uuid.NullUUID{UUID: userID, Valid: true},
		CreatedAt:   now,
	})
	if err != nil {
		return ExportJob{}, fmt.Errorf("store: create export job: %w", err)
	}
	return exportFromRow(row.ID, row.WorkspaceID, row.Status, row.SizeBytes, row.Error,
		row.CreatedAt, row.FinishedAt, row.ExpiresAt), nil
}

// ClaimExportJob marks a job as being built, and reports false if
// another instance got there first.
func (s *Surveys) ClaimExportJob(ctx context.Context, jobID uuid.UUID) (bool, error) {
	_, err := s.q.ClaimExportJob(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: claim export job: %w", err)
	}
	return true, nil
}

// FinishExportJob stores the archive and sets its expiry.
func (s *Surveys) FinishExportJob(ctx context.Context, jobID uuid.UUID, archive []byte, now, expiresAt time.Time) error {
	err := s.q.FinishExportJob(ctx, db.FinishExportJobParams{
		ID:         jobID,
		Archive:    archive,
		SizeBytes:  int64(len(archive)),
		FinishedAt: &now,
		ExpiresAt:  &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("store: finish export job: %w", err)
	}
	return nil
}

// FailExportJob records why a build did not produce an archive. The
// message is shown to the person who asked, so it has to be readable.
func (s *Surveys) FailExportJob(ctx context.Context, jobID uuid.UUID, message string, now time.Time) error {
	if err := s.q.FailExportJob(ctx, db.FailExportJobParams{
		ID: jobID, Error: &message, FinishedAt: &now,
	}); err != nil {
		return fmt.Errorf("store: fail export job: %w", err)
	}
	return nil
}

// LatestExportJob is what the account page shows: the most recent export
// and its state.
func (s *Surveys) LatestExportJob(ctx context.Context, workspaceID uuid.UUID) (ExportJob, error) {
	row, err := s.q.LatestExportJob(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExportJob{}, ErrNotFound
	}
	if err != nil {
		return ExportJob{}, fmt.Errorf("store: latest export job: %w", err)
	}
	return exportFromRow(row.ID, row.WorkspaceID, row.Status, row.SizeBytes, row.Error,
		row.CreatedAt, row.FinishedAt, row.ExpiresAt), nil
}

// ExportArchive reads an archive back for download, for this workspace
// and only while it is unexpired.
func (s *Surveys) ExportArchive(ctx context.Context, jobID, workspaceID uuid.UUID, now time.Time) ([]byte, error) {
	row, err := s.q.GetExportArchive(ctx, db.GetExportArchiveParams{
		ID: jobID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get export archive: %w", err)
	}
	if row.ExpiresAt == nil || !now.Before(*row.ExpiresAt) {
		return nil, ErrNotFound
	}
	return row.Archive, nil
}

func exportFromRow(id, workspaceID uuid.UUID, status string, size int64, errMsg *string,
	createdAt time.Time, finishedAt, expiresAt *time.Time) ExportJob {
	job := ExportJob{
		ID: id, WorkspaceID: workspaceID, Status: status, SizeBytes: size,
		CreatedAt: createdAt, FinishedAt: finishedAt, ExpiresAt: expiresAt,
	}
	if errMsg != nil {
		job.Error = *errMsg
	}
	return job
}

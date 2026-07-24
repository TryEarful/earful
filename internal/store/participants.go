package store

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/store/db"
)

// Participant is one invited person as the creator sees them.
type Participant struct {
	ID          uuid.UUID
	Email       string
	InvitedAt   *time.Time
	BouncedAt   *time.Time
	SubmittedAt *time.Time
	Suppressed  bool
}

// Status is the single word the participant list shows.
func (p Participant) Status() string {
	switch {
	case p.SubmittedAt != nil:
		return "Submitted"
	case p.BouncedAt != nil:
		return "Bounced"
	case p.Suppressed:
		return "Suppressed"
	case p.InvitedAt != nil:
		return "Invited"
	default:
		return "Pending"
	}
}

// maxImportBatch bounds one paste/upload. Bigger audiences import in
// batches; the cap keeps the request small and the dedupe cheap.
const maxImportBatch = 1000

var ErrImportTooLarge = fmt.Errorf("store: import at most %d addresses at a time", maxImportBatch)

// ImportParticipants parses pasted text or CSV content into addresses and
// inserts the new ones. Duplicates — within the paste or against earlier
// imports — are silently deduplicated (M4-T3). Returns how many were
// added and how many entries were skipped as unparseable.
func (s *Surveys) ImportParticipants(ctx context.Context, surveyID uuid.UUID, raw string) (added, invalid int, err error) {
	addresses := splitAddresses(raw)
	if len(addresses) > maxImportBatch {
		return 0, 0, ErrImportTooLarge
	}

	for _, candidate := range addresses {
		parsed, err := mail.ParseAddress(candidate)
		if err != nil || parsed.Name != "" {
			invalid++
			continue
		}
		address := strings.ToLower(parsed.Address)
		// The stored token is a placeholder: the real one is minted at
		// send time (only hashes are ever stored, and the emailed link
		// needs the raw token).
		rows, err := s.q.ImportParticipant(ctx, db.ImportParticipantParams{
			SurveyID:  surveyID,
			Email:     address,
			TokenHash: auth.HashToken(auth.NewToken()),
		})
		if err != nil {
			return added, invalid, fmt.Errorf("store: import participant: %w", err)
		}
		added += int(rows)
	}
	return added, invalid, nil
}

// splitAddresses accepts newline-, comma- and semicolon-separated input —
// a pasted column, a CSV line, or a whole CSV file all reduce to this.
func splitAddresses(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Participants lists a survey's participants with their derived status.
func (s *Surveys) Participants(ctx context.Context, surveyID uuid.UUID) ([]Participant, error) {
	rows, err := s.q.ListParticipants(ctx, surveyID)
	if err != nil {
		return nil, fmt.Errorf("store: list participants: %w", err)
	}
	out := make([]Participant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Participant{
			ID: r.ID, Email: r.Email,
			InvitedAt: r.InvitedAt, BouncedAt: r.BouncedAt, SubmittedAt: r.SubmittedAt,
			Suppressed: r.Suppressed,
		})
	}
	return out, nil
}

// PendingInvites returns up to limit participants still awaiting their
// invite, with suppressed and bounced addresses already excluded.
func (s *Surveys) PendingInvites(ctx context.Context, surveyID uuid.UUID, limit int) ([]Participant, error) {
	rows, err := s.q.PendingInvites(ctx, db.PendingInvitesParams{SurveyID: surveyID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: pending invites: %w", err)
	}
	out := make([]Participant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Participant{ID: r.ID, Email: r.Email})
	}
	return out, nil
}

// IssueInviteToken mints the participant's real token and marks them
// invited, returning the raw token for the email. Re-issuing invalidates
// any earlier link — only the most recent invite works.
func (s *Surveys) IssueInviteToken(ctx context.Context, participantID uuid.UUID, now time.Time) (string, error) {
	raw := auth.NewToken()
	if err := s.q.SetParticipantTokenAndInvited(ctx, db.SetParticipantTokenAndInvitedParams{
		ID: participantID, TokenHash: auth.HashToken(raw), InvitedAt: &now,
	}); err != nil {
		return "", fmt.Errorf("store: issue invite token: %w", err)
	}
	return raw, nil
}

// ClearInvited rolls a participant back to pending after a failed send.
func (s *Surveys) ClearInvited(ctx context.Context, participantID uuid.UUID) error {
	if err := s.q.ClearParticipantInvited(ctx, participantID); err != nil {
		return fmt.Errorf("store: clear invited: %w", err)
	}
	return nil
}

// ResolvedParticipant is an invite link resolved to its person and
// survey. The token is the credential; the survey comes with it.
type ResolvedParticipant struct {
	ID          uuid.UUID
	SurveyID    uuid.UUID
	Email       string
	SubmittedAt *time.Time
}

func (s *Surveys) ParticipantByToken(ctx context.Context, rawToken string) (ResolvedParticipant, error) {
	row, err := s.q.GetParticipantByTokenHash(ctx, auth.HashToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedParticipant{}, ErrNotFound
	}
	if err != nil {
		return ResolvedParticipant{}, fmt.Errorf("store: participant by token: %w", err)
	}
	return ResolvedParticipant{
		ID: row.ID, SurveyID: row.SurveyID, Email: row.Email, SubmittedAt: row.SubmittedAt,
	}, nil
}

// Suppress adds an address to the never-mail list and marks its
// participant rows bounced, everywhere.
func (s *Surveys) Suppress(ctx context.Context, address, reason string, now time.Time) error {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return nil
	}
	if err := s.q.AddSuppression(ctx, db.AddSuppressionParams{Email: address, Reason: reason}); err != nil {
		return fmt.Errorf("store: add suppression: %w", err)
	}
	if err := s.q.MarkParticipantEmailBounced(ctx, db.MarkParticipantEmailBouncedParams{
		Email: address, BouncedAt: &now,
	}); err != nil {
		return fmt.Errorf("store: mark bounced: %w", err)
	}
	return nil
}

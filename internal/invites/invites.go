// Package invites sends participant invitations under the per-workspace
// drip cap (M4-T4). Sending is synchronous and cap-bounded: pressing
// "Send invites" delivers what this hour's allowance permits and reports
// what remains, and pressing it again later drains the rest. Nothing
// sends on a timer: every send is a creator's press of the button, which
// is also where a suspended workspace is refused. Deliverability is
// why the cap exists: 10,000 invites in one burst from a fresh domain is
// how sending reputations die (ADR-0005).
package invites

import (
	"context"
	"github.com/TryEarful/earful/internal/uitext"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/store"
)

// perWorkspacePerHour is the drip cap from M4-T4's ticket text.
const perWorkspacePerHour = 200

// Service owns the send loop.
type Service struct {
	surveys *store.Surveys
	sender  email.Sender
	clock   clock.Clock
	baseURL string
	cap     *antibot.Limiter
}

func NewService(surveys *store.Surveys, sender email.Sender, c clock.Clock, baseURL string) *Service {
	return &Service{
		surveys: surveys,
		sender:  sender,
		clock:   c,
		baseURL: baseURL,
		cap:     antibot.NewLimiter(perWorkspacePerHour, time.Hour, c),
	}
}

// Result reports one send run in creator terms.
type Result struct {
	Sent      int
	Remaining int
	// Failed counts sender errors (kept pending; retried next run).
	Failed int
}

// pendingFetchBound caps one run's working set; far above the hourly cap,
// so Remaining reports the true backlog for any realistic audience.
const pendingFetchBound = 10_000

// SendPending emails invites to pending participants of one survey until
// the pending list, or the workspace's hourly allowance, runs out.
// Suppressed and bounced addresses never even enter the list (the query
// excludes them) — a bounced address is never re-mailed (M4-T4 AC).
func (s *Service) SendPending(ctx context.Context, workspaceID, surveyID uuid.UUID, surveyTitle, workspaceName string) (Result, error) {
	pending, err := s.surveys.PendingInvites(ctx, surveyID, pendingFetchBound)
	if err != nil {
		return Result{}, err
	}

	var result Result
	for _, participant := range pending {
		if !s.cap.Allow(workspaceID.String()) {
			break
		}
		raw, err := s.surveys.IssueInviteToken(ctx, participant.ID, s.clock.Now())
		if err != nil {
			return result, err
		}
		link := s.baseURL + "/p/" + raw
		// An invitation is written in the language of whoever sends it.
		// What language the person invited reads is not known, and is
		// not something to record about them in order to find out.
		l := uitext.From(ctx)
		// The title is quoted as Go quotes it, so that one with a quote
		// of its own in it cannot be read as ending early. The message
		// supplies the marks, which differ from language to language.
		quoted := strconv.Quote(surveyTitle)
		err = s.sender.Send(ctx, email.Message{
			To:      participant.Email,
			Subject: l.T("email.invite.subject", uitext.Args{"Survey": surveyTitle}),
			Text: l.T("email.invite.body", uitext.Args{
				"Workspace": workspaceName, "Survey": quoted[1 : len(quoted)-1], "Link": link,
			}),
		})
		if err != nil {
			// A failed send rolls back to pending so the next run retries
			// it; the rotated token is harmless (nothing was emailed).
			if clearErr := s.surveys.ClearInvited(ctx, participant.ID); clearErr != nil {
				return result, clearErr
			}
			result.Failed++
			continue
		}
		result.Sent++
	}
	// Whatever was neither sent nor failed was never attempted this run.
	result.Remaining = len(pending) - result.Sent - result.Failed
	return result, nil
}

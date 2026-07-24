// Package auth implements sessions, magic-link login, Google OIDC login,
// the first-login personal-workspace transaction (ADR-0002), and account
// deletion (M2). It is the only package that resolves credentials to a
// user; everything downstream trusts the AuthInfo it produces.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/store/db"
)

const (
	// SessionTTL is fixed (not sliding) for the MVP: after 30 days you
	// log in again.
	SessionTTL = 30 * 24 * time.Hour
	// MagicLinkTTL per PLAN.md M2-T3.
	MagicLinkTTL = 15 * time.Minute
	// magicPerEmailPerHour is enforced against the database so it
	// survives restarts; the per-IP limit is in-memory (antibot.Limiter).
	magicPerEmailPerHour = 5
)

var (
	ErrRateLimited  = errors.New("auth: rate limited")
	ErrInvalidToken = errors.New("auth: invalid or unknown token")
	ErrExpiredToken = errors.New("auth: token expired")
	ErrUsedToken    = errors.New("auth: token already used")
	ErrInvalidEmail = errors.New("auth: invalid email address")
	ErrNoSession    = errors.New("auth: no valid session")
)

// Service wires the auth flows to their dependencies. All fields are
// required except IPLimiter, which callers construct with NewService.
type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	clock     clock.Clock
	sender    email.Sender
	baseURL   string
	ipLimiter *antibot.Limiter

	// M12 private beta: password-login and invite-code-signup limiters
	// (both axes) and the beta-mode flag that closes the account-creation
	// side doors.
	pwIPLimiter        *antibot.Limiter
	pwEmailLimiter     *antibot.Limiter
	signupIPLimiter    *antibot.Limiter
	signupEmailLimiter *antibot.Limiter
	betaMode           bool
}

func NewService(pool *pgxpool.Pool, c clock.Clock, sender email.Sender, baseURL string) *Service {
	return &Service{
		pool:               pool,
		q:                  db.New(pool),
		clock:              c,
		sender:             sender,
		baseURL:            strings.TrimSuffix(baseURL, "/"),
		ipLimiter:          antibot.NewLimiter(10, time.Hour, c),
		pwIPLimiter:        antibot.NewLimiter(passwordPerIPPerHour, time.Hour, c),
		pwEmailLimiter:     antibot.NewLimiter(passwordPerEmailPerHour, time.Hour, c),
		signupIPLimiter:    antibot.NewLimiter(signupPerIPPerHour, time.Hour, c),
		signupEmailLimiter: antibot.NewLimiter(signupPerEmailPerHour, time.Hour, c),
	}
}

// AuthInfo is the resolved identity attached to authenticated requests.
// MVP is sole-membership (ADR-0002), so one workspace is the workspace.
type AuthInfo struct {
	SessionID     uuid.UUID
	UserID        uuid.UUID
	Email         string
	WorkspaceID   uuid.UUID
	WorkspaceName string
	CSRFToken     string
	// IsSuperAdmin gates the /admin surface (M12); granted only via the
	// earful admin CLI, never through the web.
	IsSuperAdmin bool
}

// RequestMagicLink validates and rate-limits a login request, stores the
// hashed single-use token, and emails the link. It succeeds for any
// well-formed address — signup happens at verification time, so there is
// no account-existence signal to leak.
func (s *Service) RequestMagicLink(ctx context.Context, address, ip string) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil || addr.Name != "" {
		return ErrInvalidEmail
	}
	addressNorm := strings.ToLower(addr.Address)

	if !s.ipLimiter.Allow(ip) {
		return ErrRateLimited
	}
	n, err := s.q.CountRecentMagicLinksForEmail(ctx, db.CountRecentMagicLinksForEmailParams{
		Email:     addressNorm,
		CreatedAt: s.clock.Now().Add(-time.Hour),
	})
	if err != nil {
		return fmt.Errorf("auth: count recent magic links: %w", err)
	}
	if n >= magicPerEmailPerHour {
		return ErrRateLimited
	}

	raw := NewToken()
	if err := s.q.CreateMagicLinkToken(ctx, db.CreateMagicLinkTokenParams{
		TokenHash: HashToken(raw),
		Email:     addressNorm,
		ExpiresAt: s.clock.Now().Add(MagicLinkTTL),
	}); err != nil {
		return fmt.Errorf("auth: create magic link: %w", err)
	}

	link := s.baseURL + "/auth/magic/verify?token=" + raw
	return s.sender.Send(ctx, email.Message{
		To:      addressNorm,
		Subject: "Your Earful sign-in link",
		Text: "Click to sign in to Earful:\n\n" + link +
			"\n\nThe link is valid for 15 minutes and can be used once. " +
			"If you didn't request it, ignore this email.",
	})
}

// PeekMagicToken checks a token without consuming it — the GET
// confirmation page uses it so email-scanner prefetch cannot burn the
// single-use token; only the explicit POST consumes.
func (s *Service) PeekMagicToken(ctx context.Context, raw string) (string, error) {
	row, err := s.q.GetMagicLinkToken(ctx, HashToken(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("auth: get magic link: %w", err)
	}
	if row.UsedAt != nil {
		return "", ErrUsedToken
	}
	if !s.clock.Now().Before(row.ExpiresAt) {
		return "", ErrExpiredToken
	}
	return row.Email, nil
}

// ConsumeMagicToken atomically marks the token used and returns its email.
// The UPDATE ... WHERE used_at IS NULL guard makes replay a database-level
// impossibility, not a code-path hope.
func (s *Service) ConsumeMagicToken(ctx context.Context, raw string) (string, error) {
	row, err := s.q.ConsumeMagicLinkToken(ctx, db.ConsumeMagicLinkTokenParams{
		TokenHash: HashToken(raw),
		UsedAt:    ptr(s.clock.Now()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown token or already used; don't distinguish for callers.
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("auth: consume magic link: %w", err)
	}
	if !s.clock.Now().Before(row.ExpiresAt) {
		return "", ErrExpiredToken
	}
	return row.Email, nil
}

// LoginByEmail finds or creates the user for a verified email address and
// guarantees their personal workspace exists (ADR-0002).
func (s *Service) LoginByEmail(ctx context.Context, address string) (db.User, db.Workspace, error) {
	return s.login(ctx, strings.ToLower(address), nil)
}

// LoginByGoogle resolves a verified Google identity: by google_sub first,
// then by email (backfilling google_sub onto an existing magic-link
// account), else creates a fresh user.
func (s *Service) LoginByGoogle(ctx context.Context, sub, address string) (db.User, db.Workspace, error) {
	return s.login(ctx, strings.ToLower(address), &sub)
}

func (s *Service) login(ctx context.Context, address string, googleSub *string) (db.User, db.Workspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: begin login tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	qtx := s.q.WithTx(tx)

	var user db.User
	found := false
	if googleSub != nil {
		user, err = qtx.GetUserByGoogleSub(ctx, googleSub)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, db.Workspace{}, fmt.Errorf("auth: get user by sub: %w", err)
		}
		found = err == nil
	}
	if !found {
		user, err = qtx.GetUserByEmail(ctx, address)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Beta mode closes the side doors: magic-link and Google
			// sign-in still work for EXISTING accounts, but neither may
			// create one — otherwise Google login would stroll past the
			// invite-code gate (M12).
			if s.betaMode {
				return db.User{}, db.Workspace{}, ErrBetaRequired
			}
			user, err = qtx.CreateUser(ctx, db.CreateUserParams{Email: address, GoogleSub: googleSub})
			if err != nil {
				return db.User{}, db.Workspace{}, fmt.Errorf("auth: create user: %w", err)
			}
		case err != nil:
			return db.User{}, db.Workspace{}, fmt.Errorf("auth: get user by email: %w", err)
		default:
			if googleSub != nil && user.GoogleSub == nil {
				if err := qtx.SetUserGoogleSub(ctx, db.SetUserGoogleSubParams{ID: user.ID, GoogleSub: googleSub}); err != nil {
					return db.User{}, db.Workspace{}, fmt.Errorf("auth: backfill google sub: %w", err)
				}
				user.GoogleSub = googleSub
			}
		}
	}

	ws, err := qtx.GetWorkspaceForUser(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		ws, err = qtx.CreateWorkspace(ctx, workspaceNameFor(address))
		if err != nil {
			return db.User{}, db.Workspace{}, fmt.Errorf("auth: create workspace: %w", err)
		}
		if err := qtx.CreateWorkspaceMember(ctx, db.CreateWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: user.ID}); err != nil {
			return db.User{}, db.Workspace{}, fmt.Errorf("auth: create membership: %w", err)
		}
	} else if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: get workspace: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: commit login tx: %w", err)
	}
	return user, ws, nil
}

// CreateSession issues a fresh session for the user and returns the raw
// cookie token (stored only as a hash). Callers must always set a NEW
// cookie from this — never reuse a pre-login value — which is what makes
// session fixation structurally impossible.
func (s *Service) CreateSession(ctx context.Context, userID uuid.UUID) (raw string, sess db.Session, err error) {
	raw = NewToken()
	sess, err = s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: HashToken(raw),
		UserID:    userID,
		CsrfToken: NewToken(),
		ExpiresAt: s.clock.Now().Add(SessionTTL),
	})
	if err != nil {
		return "", db.Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return raw, sess, nil
}

// Authenticate resolves a raw cookie token to the requesting identity.
func (s *Service) Authenticate(ctx context.Context, raw string) (AuthInfo, error) {
	row, err := s.q.AuthenticateSession(ctx, HashToken(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthInfo{}, ErrNoSession
	}
	if err != nil {
		return AuthInfo{}, fmt.Errorf("auth: authenticate: %w", err)
	}
	if !s.clock.Now().Before(row.ExpiresAt) {
		return AuthInfo{}, ErrNoSession
	}
	return AuthInfo{
		SessionID:     row.SessionID,
		UserID:        row.UserID,
		Email:         row.Email,
		WorkspaceID:   row.WorkspaceID,
		WorkspaceName: row.WorkspaceName,
		CSRFToken:     row.CsrfToken,
		IsSuperAdmin:  row.IsSuperAdmin,
	}, nil
}

// Logout destroys the session server-side.
func (s *Service) Logout(ctx context.Context, raw string) error {
	if err := s.q.DeleteSessionByTokenHash(ctx, HashToken(raw)); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// DeleteAccount soft-deletes the user and their workspaces and revokes
// every session (M2-T5). Hard deletion happens in the purge subcommand
// after the 30-day window (M8-T2). The same email can sign up again
// immediately — the partial unique index only covers live rows.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin delete tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	qtx := s.q.WithTx(tx)
	now := ptr(s.clock.Now())
	if err := qtx.SoftDeleteWorkspacesForUser(ctx, db.SoftDeleteWorkspacesForUserParams{UserID: userID, DeletedAt: now}); err != nil {
		return fmt.Errorf("auth: soft-delete workspaces: %w", err)
	}
	if err := qtx.SoftDeleteUser(ctx, db.SoftDeleteUserParams{ID: userID, DeletedAt: now}); err != nil {
		return fmt.Errorf("auth: soft-delete user: %w", err)
	}
	if err := qtx.DeleteSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit delete tx: %w", err)
	}
	return nil
}

// workspaceNameFor derives the personal-workspace name from the email's
// local part: "sam@example.com" → "sam's workspace".
func workspaceNameFor(address string) string {
	local, _, ok := strings.Cut(address, "@")
	if !ok || local == "" {
		return "Personal workspace"
	}
	return local + "'s workspace"
}

func ptr[T any](v T) *T { return &v }

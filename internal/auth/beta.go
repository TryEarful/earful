package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/TryEarful/earful/internal/store/db"
)

// M12: the private-beta gate. Accounts are created with a one-shot
// invite code and thereafter authenticate with email+password — no email
// is ever sent, because there is deliberately no email infrastructure
// until Brevo lands. Codes gate CREATION only; they are marked used
// (used_at/used_by) in the same transaction that creates the account.

const (
	// PasswordMinLen: NIST's floor. No composition rules — length is the
	// only requirement that measurably helps.
	PasswordMinLen = 8

	passwordPerIPPerHour    = 20
	passwordPerEmailPerHour = 10

	// signup is throttled like login: every attempt runs a bcrypt hash and
	// writes rows, so an unthrottled endpoint is both a CPU-exhaustion and
	// an enumeration lever on the single Cloud Run instance.
	signupPerIPPerHour    = 20
	signupPerEmailPerHour = 10
)

var (
	// ErrBadCredentials is the single answer to every failed password
	// login — unknown email, Google-only account, wrong password — so the
	// endpoint leaks nothing about which it was.
	ErrBadCredentials = errors.New("auth: invalid email or password")
	// ErrInvalidCode is the single answer for unknown, used, and revoked
	// invite codes alike.
	ErrInvalidCode = errors.New("auth: invalid invite code")
	// ErrWeakPassword rejects passwords under PasswordMinLen.
	ErrWeakPassword = errors.New("auth: password too short")
	// ErrEmailTaken surfaces on signup/email-change collisions. On signup
	// it is only reachable AFTER a valid one-shot invite code has been
	// validated (SignupWithCode checks the code before touching the users
	// table), so the friendlier error leaks nothing to an un-invited
	// enumerator — only to someone who already holds a live code.
	ErrEmailTaken = errors.New("auth: email already in use")
	// ErrNoPassword marks Google-only accounts attempting password-gated
	// actions; until Brevo, a super admin sets their password.
	ErrNoPassword = errors.New("auth: account has no password")
	// ErrBetaRequired is returned by account-CREATING paths while beta
	// mode is on: without it, signing in with Google would mint an
	// account and stroll past the invite-code gate.
	ErrBetaRequired = errors.New("auth: private beta requires an invite code")
	// ErrUserNotFound is for admin actions that name a missing account.
	ErrUserNotFound = errors.New("auth: no such user")
)

// dummyHash keeps failed logins for unknown emails as slow as failed
// logins for known ones (uniform timing, no existence oracle).
var dummyHash = mustHash("earful-timing-equalizer-not-a-real-password")

func mustHash(s string) []byte {
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: bcrypt self-test failed: " + err.Error())
	}
	return h
}

// SetBetaMode flips the private-beta gate (config BETA_MODE). While on,
// login() refuses to CREATE accounts (existing users keep every sign-in
// method they have).
func (s *Service) SetBetaMode(on bool) { s.betaMode = on }

// betaCodeAlphabet omits lookalikes (0/o, 1/l/i) — these codes get read
// aloud and typed.
const betaCodeAlphabet = "abcdefghjkmnpqrstvwxyz23456789"

// NewBetaCode mints a human-typeable one-shot invite code,
// "earful-xxxx-xxxx-xxxx" (~59 bits from a 30-char alphabet — plenty
// behind a rate-limited endpoint). Shown once; only the hash is stored.
func NewBetaCode() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	out := make([]byte, 0, 21)
	out = append(out, "earful"...)
	for i, c := range b {
		if i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, betaCodeAlphabet[int(c)%len(betaCodeAlphabet)])
	}
	return string(out)
}

// canonicalBetaCode makes pasted codes forgiving: case, spaces, and
// dash placement don't matter; the hash covers only the alphanumerics.
func canonicalBetaCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SignupWithCode creates the account, its personal workspace, and marks
// the invite code used — one transaction, zero emails.
//
// Ordering is a security property, not an implementation detail: the
// invite code is validated and row-locked FIRST, before the users table
// is ever touched and before the (deliberately expensive) bcrypt hash. A
// request without a valid code returns ErrInvalidCode and never learns
// whether the email exists, so the endpoint is not a beta-membership
// enumeration oracle, and a codeless flood cannot burn bcrypt CPU on the
// single instance. The code is locked (FOR UPDATE) and only marked used
// once the account row exists, so used_by is still recorded and two
// racing signups still resolve to exactly one account.
func (s *Service) SignupWithCode(ctx context.Context, address, password, code, ip string) (db.User, db.Workspace, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil || addr.Name != "" {
		return db.User{}, db.Workspace{}, ErrInvalidEmail
	}
	addressNorm := strings.ToLower(addr.Address)
	if len(password) < PasswordMinLen {
		return db.User{}, db.Workspace{}, ErrWeakPassword
	}
	if !s.signupIPLimiter.Allow(ip) || !s.signupEmailLimiter.Allow(addressNorm) {
		return db.User{}, db.Workspace{}, ErrRateLimited
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: begin signup tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	qtx := s.q.WithTx(tx)

	// Gate on code possession first: validate and lock the code. Nothing
	// below — the existence-revealing user insert, the bcrypt cost — is
	// reachable without a live invite code.
	if _, err := qtx.GetActiveBetaCodeForUpdate(ctx, HashToken(canonicalBetaCode(code))); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, db.Workspace{}, ErrInvalidCode
		}
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: lock beta code: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: hash password: %w", err)
	}

	user, err := qtx.CreateUserWithPassword(ctx, db.CreateUserWithPasswordParams{
		Email:        addressNorm,
		PasswordHash: ptr(string(hash)),
	})
	if isUniqueViolation(err) {
		return db.User{}, db.Workspace{}, ErrEmailTaken
	}
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: create user: %w", err)
	}

	// Mark the locked code used, recording who. The used_at IS NULL guard
	// still holds (we own the row lock), so a concurrent signup with the
	// same code blocked at GetActiveBetaCodeForUpdate above and will find
	// it spent once we commit.
	if _, err := qtx.ConsumeBetaCode(ctx, db.ConsumeBetaCodeParams{
		CodeHash: HashToken(canonicalBetaCode(code)),
		UsedAt:   ptr(s.clock.Now()),
		UsedBy:   uuid.NullUUID{UUID: user.ID, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, db.Workspace{}, ErrInvalidCode
		}
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: consume beta code: %w", err)
	}

	ws, err := qtx.CreateWorkspace(ctx, workspaceNameFor(addressNorm))
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: create workspace: %w", err)
	}
	if err := qtx.CreateWorkspaceMember(ctx, db.CreateWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: user.ID}); err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: create membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: commit signup tx: %w", err)
	}
	return user, ws, nil
}

// LoginWithPassword authenticates email+password. Every failure mode —
// unknown email, passwordless (Google-only) account, wrong password —
// costs one bcrypt comparison and returns the same error.
func (s *Service) LoginWithPassword(ctx context.Context, address, password, ip string) (db.User, db.Workspace, error) {
	addressNorm := strings.ToLower(strings.TrimSpace(address))
	if !s.pwIPLimiter.Allow(ip) || !s.pwEmailLimiter.Allow(addressNorm) {
		return db.User{}, db.Workspace{}, ErrRateLimited
	}

	user, err := s.q.GetUserByEmail(ctx, addressNorm)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return db.User{}, db.Workspace{}, ErrBadCredentials
	}
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return db.User{}, db.Workspace{}, ErrBadCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)) != nil {
		return db.User{}, db.Workspace{}, ErrBadCredentials
	}

	ws, err := s.q.GetWorkspaceForUser(ctx, user.ID)
	if err != nil {
		return db.User{}, db.Workspace{}, fmt.Errorf("auth: get workspace: %w", err)
	}
	return user, ws, nil
}

// ChangeEmail applies immediately after re-proving the current password
// (there is no ESP to verify the new address with — the verification
// step upgrades this flow when Brevo lands). google_sub stays linked.
func (s *Service) ChangeEmail(ctx context.Context, userID uuid.UUID, newEmail, currentPassword string) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(newEmail))
	if err != nil || addr.Name != "" {
		return ErrInvalidEmail
	}
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
		return ErrNoPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrBadCredentials
	}
	err = s.q.UpdateUserEmail(ctx, db.UpdateUserEmailParams{ID: userID, Email: strings.ToLower(addr.Address)})
	if isUniqueViolation(err) {
		return ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("auth: update email: %w", err)
	}
	return nil
}

// AdminResetPassword generates a temporary password for the account,
// revokes every session, and returns the plaintext exactly once — the
// beta's out-of-band answer to "I forgot my password" until self-serve
// reset exists (needs email).
func (s *Service) AdminResetPassword(ctx context.Context, address string) (string, error) {
	user, err := s.q.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(address)))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("auth: get user: %w", err)
	}
	temp := NewBetaCode() // same generator: typeable, ~59 bits, single display
	hash, err := bcrypt.GenerateFromPassword([]byte(temp), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash temp password: %w", err)
	}
	if err := s.q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: user.ID, PasswordHash: ptr(string(hash))}); err != nil {
		return "", fmt.Errorf("auth: set password: %w", err)
	}
	if err := s.q.DeleteSessionsForUser(ctx, user.ID); err != nil {
		return "", fmt.Errorf("auth: revoke sessions: %w", err)
	}
	return temp, nil
}

// MintBetaCodes creates n codes and returns their plaintexts — the only
// time they ever exist outside a hash.
func (s *Service) MintBetaCodes(ctx context.Context, n int, label string) ([]string, error) {
	codes := make([]string, 0, n)
	for range n {
		code := NewBetaCode()
		if _, err := s.q.CreateBetaCode(ctx, db.CreateBetaCodeParams{
			CodeHash: HashToken(canonicalBetaCode(code)),
			Label:    label,
		}); err != nil {
			return nil, fmt.Errorf("auth: create beta code: %w", err)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// ListBetaCodes returns code metadata (labels, used/revoked state, who
// used each) — never code material.
func (s *Service) ListBetaCodes(ctx context.Context) ([]db.ListBetaCodesRow, error) {
	rows, err := s.q.ListBetaCodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list beta codes: %w", err)
	}
	return rows, nil
}

// RevokeBetaCode withdraws an unused code; used codes are history and
// stay untouched.
func (s *Service) RevokeBetaCode(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.RevokeBetaCode(ctx, db.RevokeBetaCodeParams{ID: id, RevokedAt: ptr(s.clock.Now())})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidCode
	}
	if err != nil {
		return fmt.Errorf("auth: revoke beta code: %w", err)
	}
	return nil
}

// SetSuperAdmin grants or revokes the instance-level admin flag — only
// the CLI (with direct database access) calls this; no web path can.
func (s *Service) SetSuperAdmin(ctx context.Context, address string, on bool) error {
	_, err := s.q.SetUserSuperAdmin(ctx, db.SetUserSuperAdminParams{
		Email:        strings.ToLower(strings.TrimSpace(address)),
		IsSuperAdmin: on,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: set super admin: %w", err)
	}
	return nil
}

// isUniqueViolation matches Postgres error 23505 (duplicate key).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

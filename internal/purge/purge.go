// Package purge implements the retention promises (M8-T2): it
// hard-deletes what has been soft-deleted for long enough, expires
// tokens, and trims the logs that are supposed to be short-lived.
//
// The rule this package exists to keep is simple and load-bearing:
// "deleted in 30 days" has to be a thing that happens, not a thing the
// privacy notice says. `earful purge` is the same binary as the server,
// run by hand locally and by Cloud Scheduler in production.
//
// Two design points worth knowing before reading the SQL:
//
//   - Published versions, questions, their localizations, draft
//     revisions, answers and insight runs are immutable by database
//     trigger (ADR-0001). Purging is the one
//     legitimate exception, and it is granted narrowly: the transaction
//     sets `earful.purging = 'on'`, which the triggers accept for DELETE
//     and for nothing else.
//   - A dry run executes exactly the same statements and then rolls back.
//     Counting separately would mean two implementations of "what would
//     be deleted", and the reported numbers would eventually stop
//     matching what a real run does.
//
// The SQL is written out rather than generated through sqlc: this is a
// one-off ordered sequence over most of the schema, and twenty
// single-use queries in the shared querier would obscure both.
package purge

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Retention windows. Each is a promise made somewhere user-facing, so
// changing one means changing the privacy notice too.
const (
	// SoftDeleteWindow: deleting is undoable by support for 30 days
	// (story 60), then irreversible.
	SoftDeleteWindow = 30 * 24 * time.Hour
	// AbuseLogWindow: the only table that ever holds an IP, kept as
	// briefly as it can be while still being useful (ADR-0003).
	AbuseLogWindow = 30 * 24 * time.Hour
	// DraftRevisionWindow: editing history, which is useful for a while
	// and not forever. The newest revision of every draft is always kept.
	DraftRevisionWindow = 90 * 24 * time.Hour
	// UnusedImageWindow: how long a picture of a survey's style is kept
	// once nothing shows it, neither a published version nor the draft.
	// Long enough that a creator who replaced a logo by mistake can put
	// it back by uploading it again and lose nothing, short enough that
	// replaced uploads do not pile up (ADR-0018).
	UnusedImageWindow = 7 * 24 * time.Hour
	// ExpiredTokenGrace: how long a spent or expired sign-in token stays
	// before it is deleted. Short: it is useless the moment it expires.
	ExpiredTokenGrace = 24 * time.Hour
)

// Report is what one run did, in counts only. Never subjects: a purge
// log that named the people it erased would be its own retention
// problem.
type Report struct {
	DryRun bool
	Counts map[string]int64
	// Order is the order the steps ran in, so the log reads as a
	// sequence rather than a map.
	Order []string
}

func (r *Report) add(step string, n int64) {
	if r.Counts == nil {
		r.Counts = map[string]int64{}
	}
	if _, seen := r.Counts[step]; !seen {
		r.Order = append(r.Order, step)
	}
	r.Counts[step] += n
}

// Total is the number of rows the run removed (or would have removed).
func (r *Report) Total() int64 {
	var total int64
	for _, n := range r.Counts {
		total += n
	}
	return total
}

// Run purges everything past its retention window. Everything happens in
// one transaction: a partial purge that deleted responses but left their
// survey would be worse than no purge at all.
func Run(ctx context.Context, pool *pgxpool.Pool, now time.Time, dryRun bool) (*Report, error) {
	report := &Report{DryRun: dryRun}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("purge: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// The immutability triggers accept DELETE only while this is set, and
	// it is transaction-local, so it cannot leak into anything else.
	if _, err := tx.Exec(ctx, `SET LOCAL earful.purging = 'on'`); err != nil {
		return nil, fmt.Errorf("purge: enable purge mode: %w", err)
	}

	cutoff := now.Add(-SoftDeleteWindow)
	for _, step := range steps(now, cutoff) {
		tag, err := tx.Exec(ctx, step.sql, step.args...)
		if err != nil {
			return nil, fmt.Errorf("purge: %s: %w", step.name, err)
		}
		report.add(step.name, tag.RowsAffected())
	}

	if dryRun {
		// Deliberate: the work is done and thrown away, so the numbers
		// are the numbers a real run would produce.
		return report, tx.Rollback(ctx)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("purge: commit: %w", err)
	}
	return report, nil
}

type step struct {
	name string
	sql  string
	args []any
}

// steps is the whole purge, in dependency order: children before
// parents, always.
//
// `doomed_surveys` is the set of surveys whose time is up — either
// deleted themselves, or belonging to a workspace that was. Every
// survey-scoped delete below refers to it, so a survey and its
// contents can never be half-erased.
func steps(now, cutoff time.Time) []step {
	const doomed = `
WITH doomed AS (
    SELECT s.id FROM surveys s
    WHERE (s.deleted_at IS NOT NULL AND s.deleted_at < $1)
       OR s.workspace_id IN (SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1)
)`

	return []step{
		// --- responses deleted on their own (M8-T1) ---
		// A translation of an answer says what the answer said, so it
		// goes when the answer does, and before it: it refers to it.
		{name: "answer_translations_of_deleted_responses", args: []any{cutoff}, sql: `
DELETE FROM answer_translations WHERE answer_id IN (
    SELECT a.id FROM answers a
    JOIN responses r ON r.id = a.response_id
    WHERE r.deleted_at IS NOT NULL AND r.deleted_at < $1
)`},
		{name: "answers_of_deleted_responses", args: []any{cutoff}, sql: `
DELETE FROM answers WHERE response_id IN (
    SELECT id FROM responses WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "deleted_responses", args: []any{cutoff}, sql: `
DELETE FROM responses WHERE deleted_at IS NOT NULL AND deleted_at < $1`},

		// --- surveys, and everything under them ---
		{name: "answer_translations_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM answer_translations WHERE answer_id IN (
    SELECT a.id FROM answers a
    JOIN responses r ON r.id = a.response_id
    WHERE r.survey_id IN (SELECT id FROM doomed)
)`},
		{name: "answers_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM answers WHERE response_id IN (
    SELECT id FROM responses WHERE survey_id IN (SELECT id FROM doomed)
)`},
		{name: "responses_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM responses WHERE survey_id IN (SELECT id FROM doomed)`},
		// An Insight Summary quotes the answers it read.
		{name: "insights_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM insight_runs WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "localizations_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM question_localizations WHERE version_id IN (
    SELECT id FROM survey_versions WHERE survey_id IN (SELECT id FROM doomed)
)`},
		{name: "questions_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM questions WHERE version_id IN (
    SELECT id FROM survey_versions WHERE survey_id IN (SELECT id FROM doomed)
)`},
		{name: "versions_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM survey_versions WHERE survey_id IN (SELECT id FROM doomed)`},
		// The pictures of a style go with the survey, after the versions
		// that showed them.
		{name: "images_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM survey_images WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "question_identities_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM question_identities WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "draft_revisions_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM draft_revisions WHERE draft_id IN (
    SELECT id FROM survey_drafts WHERE survey_id IN (SELECT id FROM doomed)
)`},
		{name: "drafts_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM survey_drafts WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "participants_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM participants WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "stats_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM survey_stats WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "daily_stats_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM survey_stats_daily WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "ai_usage_of_doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM ai_usage WHERE survey_id IN (SELECT id FROM doomed)`},
		{name: "doomed_surveys", args: []any{cutoff}, sql: doomed + `
DELETE FROM surveys WHERE id IN (SELECT id FROM doomed)`},

		// --- workspaces and their owners ---
		{name: "export_jobs_of_deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM export_jobs WHERE workspace_id IN (
    SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "ai_usage_of_deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM ai_usage WHERE workspace_id IN (
    SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		// The account's style and its pictures (ADR-0023) go with the
		// workspace. No version refers to an account's picture: a survey
		// that showed one has its own copy, erased with the survey.
		{name: "workspace_images_of_deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM workspace_images WHERE workspace_id IN (
    SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "workspace_styles_of_deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM workspace_styles WHERE workspace_id IN (
    SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "memberships_of_deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM workspace_members WHERE workspace_id IN (
    SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "deleted_workspaces", args: []any{cutoff}, sql: `
DELETE FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < $1`},

		// A purged author leaves their work attributed to nobody rather
		// than blocking the erasure: the audit log renders that as "a
		// deleted account" (M3-T4).
		{name: "detach_purged_authors_from_versions", args: []any{cutoff}, sql: `
UPDATE survey_versions SET published_by = NULL WHERE published_by IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "detach_purged_authors_from_revisions", args: []any{cutoff}, sql: `
UPDATE draft_revisions SET saved_by = NULL WHERE saved_by IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "detach_purged_authors_from_drafts", args: []any{cutoff}, sql: `
UPDATE survey_drafts SET updated_by = NULL WHERE updated_by IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "detach_purged_users_from_beta_codes", args: []any{cutoff}, sql: `
UPDATE beta_codes SET used_by = NULL WHERE used_by IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "sessions_of_deleted_users", args: []any{cutoff}, sql: `
DELETE FROM sessions WHERE user_id IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "memberships_of_deleted_users", args: []any{cutoff}, sql: `
DELETE FROM workspace_members WHERE user_id IN (
    SELECT id FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1
)`},
		{name: "deleted_users", args: []any{cutoff}, sql: `
DELETE FROM users WHERE deleted_at IS NOT NULL AND deleted_at < $1`},

		// --- short-lived data ---
		{name: "expired_magic_links", args: []any{now.Add(-ExpiredTokenGrace)}, sql: `
DELETE FROM magic_link_tokens WHERE expires_at < $1`},
		{name: "expired_sessions", args: []any{now}, sql: `
DELETE FROM sessions WHERE expires_at < $1`},
		{name: "old_abuse_log", args: []any{now.Add(-AbuseLogWindow)}, sql: `
DELETE FROM abuse_log WHERE at < $1`},
		// Every draft keeps its most recent revision whatever its age:
		// the trail may be trimmed, but a draft with no history at all
		// would be a worse answer than an old one.
		{name: "old_draft_revisions", args: []any{now.Add(-DraftRevisionWindow)}, sql: `
DELETE FROM draft_revisions dr
WHERE dr.saved_at < $1
  AND dr.id <> (
      SELECT id FROM draft_revisions newest
      WHERE newest.draft_id = dr.draft_id
      ORDER BY saved_at DESC
      LIMIT 1
  )`},
		// A picture nothing shows: no published version of its survey
		// and not the survey's draft. A style refers to a picture by its
		// hash, wherever in the style it sits, so the reference is looked
		// for at any depth. The draft's revisions are not asked: nothing
		// restores one, so a picture only they mention can never be
		// shown again. A survey whose style is being saved or published
		// at this moment is held (LockSurveyForStyle) and is skipped
		// until the next run: the draft or version about to refer to a
		// picture is not yet visible to this statement. The hold taken
		// here is the same NO KEY UPDATE, so it never keeps a response
		// from being recorded while the purge runs.
		{name: "unused_style_images", args: []any{now.Add(-UnusedImageWindow)}, sql: `
DELETE FROM survey_images i
WHERE i.created_at < $1
  AND EXISTS (SELECT 1 FROM surveys s WHERE s.id = i.survey_id FOR NO KEY UPDATE SKIP LOCKED)
  AND NOT EXISTS (
      SELECT 1 FROM survey_versions v
      WHERE v.survey_id = i.survey_id
        AND v.style IS NOT NULL
        AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )
  AND NOT EXISTS (
      SELECT 1 FROM survey_drafts d
      WHERE d.survey_id = i.survey_id
        AND jsonb_path_exists(d.structure, '$.style.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )`},
		// An account's picture its style no longer shows (ADR-0023). No
		// version refers to one, since a survey that showed it holds its
		// own copy. A picture is stored only with its account's style row
		// (SaveWorkspaceStyle), so the row is there to hold: an account
		// whose style is being saved at this moment is held
		// (LockWorkspaceStyle) and is skipped until the next run.
		{name: "unused_workspace_images", args: []any{now.Add(-UnusedImageWindow)}, sql: `
DELETE FROM workspace_images i
WHERE i.created_at < $1
  AND EXISTS (
      SELECT 1 FROM workspace_styles w
      WHERE w.workspace_id = i.workspace_id
      FOR NO KEY UPDATE SKIP LOCKED
  )
  AND NOT EXISTS (
      SELECT 1 FROM workspace_styles w
      WHERE w.workspace_id = i.workspace_id
        AND jsonb_path_exists(w.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )`},
		// An expired export loses its archive immediately; the row stays
		// briefly so the account page can say what happened.
		{name: "expired_export_archives", args: []any{now}, sql: `
UPDATE export_jobs SET archive = NULL, size_bytes = 0
WHERE archive IS NOT NULL AND expires_at IS NOT NULL AND expires_at < $1`},
		{name: "old_export_jobs", args: []any{cutoff}, sql: `
DELETE FROM export_jobs WHERE created_at < $1`},
	}
}

// EraseSubject is the GDPR fast-path (M8-T3): erase one person now,
// skipping the 30-day wait. It marks everything of theirs deleted with a
// timestamp far enough in the past that the ordinary purge — running in
// the same transaction — takes it all.
//
// It handles both kinds of subject: an account holder, and a participant
// in someone else's invited survey.
func EraseSubject(ctx context.Context, pool *pgxpool.Pool, email string, now time.Time) (*Report, error) {
	report := &Report{}
	past := now.Add(-SoftDeleteWindow - time.Hour)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("purge: begin erasure: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `SET LOCAL earful.purging = 'on'`); err != nil {
		return nil, fmt.Errorf("purge: enable purge mode: %w", err)
	}

	// The account, its workspaces and its surveys, dated into the past so
	// the sweep below collects them.
	for _, s := range []step{
		{name: "erase_user", args: []any{email, past}, sql: `
UPDATE users SET deleted_at = $2 WHERE lower(email) = lower($1) AND deleted_at IS NULL`},
		{name: "erase_workspaces", args: []any{email, past}, sql: `
UPDATE workspaces SET deleted_at = $2 WHERE id IN (
    SELECT wm.workspace_id FROM workspace_members wm
    JOIN users u ON u.id = wm.user_id
    WHERE lower(u.email) = lower($1)
) AND deleted_at IS NULL`},
		{name: "erase_surveys", args: []any{email, past}, sql: `
UPDATE surveys SET deleted_at = $2 WHERE workspace_id IN (
    SELECT wm.workspace_id FROM workspace_members wm
    JOIN users u ON u.id = wm.user_id
    WHERE lower(u.email) = lower($1)
) AND deleted_at IS NULL`},
		// A participant in someone else's survey: their responses are
		// personal data, so they go, and the participant row with them.
		{name: "erase_participant_answer_translations", args: []any{email}, sql: `
DELETE FROM answer_translations WHERE answer_id IN (
    SELECT a.id FROM answers a
    JOIN responses r ON r.id = a.response_id
    JOIN participants p ON p.id = r.participant_id
    WHERE lower(p.email) = lower($1)
)`},
		{name: "erase_participant_answers", args: []any{email}, sql: `
DELETE FROM answers WHERE response_id IN (
    SELECT r.id FROM responses r
    JOIN participants p ON p.id = r.participant_id
    WHERE lower(p.email) = lower($1)
)`},
		{name: "erase_participant_responses", args: []any{email}, sql: `
DELETE FROM responses WHERE participant_id IN (
    SELECT id FROM participants WHERE lower(email) = lower($1)
)`},
		{name: "erase_participants", args: []any{email}, sql: `
DELETE FROM participants WHERE lower(email) = lower($1)`},
		{name: "erase_suppressions", args: []any{email}, sql: `
DELETE FROM suppressions WHERE lower(email) = lower($1)`},
		{name: "erase_magic_links", args: []any{email}, sql: `
DELETE FROM magic_link_tokens WHERE lower(email) = lower($1)`},
	} {
		tag, err := tx.Exec(ctx, s.sql, s.args...)
		if err != nil {
			return nil, fmt.Errorf("purge: %s: %w", s.name, err)
		}
		report.add(s.name, tag.RowsAffected())
	}

	// Then the ordinary sweep, in this same transaction, which is what
	// actually removes the rows just dated into the past.
	cutoff := now.Add(-SoftDeleteWindow)
	for _, s := range steps(now, cutoff) {
		tag, err := tx.Exec(ctx, s.sql, s.args...)
		if err != nil {
			return nil, fmt.Errorf("purge: erasure sweep %s: %w", s.name, err)
		}
		report.add(s.name, tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("purge: commit erasure: %w", err)
	}
	return report, nil
}

// Anonymous responses are deliberately absent from EraseSubject: they
// contain no personal data to erase, and no way to find "yours" among
// them exists by design (ADR-0003). That is a feature of the anonymity
// promise, and the runbook says so to anyone handling a request.

// Subject is what is known about one erasure subject before anything is
// erased. Support sees this and confirms — an erasure is irreversible,
// so it is never a single click.
type Subject struct {
	Email string
	// HasAccount is true when the address belongs to a user.
	HasAccount bool
	Workspaces int
	Surveys    int
	// ParticipantIn counts invited surveys the address was invited to,
	// and Responses their submitted answers there.
	ParticipantIn int
	Responses     int
	// Suppressed is true when the address is on the bounce/complaint
	// suppression list, which is itself personal data.
	Suppressed bool
}

// Found reports whether there is anything at all to erase.
func (s Subject) Found() bool {
	return s.HasAccount || s.ParticipantIn > 0 || s.Suppressed
}

// PreviewSubject reports what EraseSubject would remove. It is
// deliberately a separate read: preview, then confirm, is the only
// safe shape for an irreversible action taken on someone else's behalf.
func PreviewSubject(ctx context.Context, pool *pgxpool.Pool, email string) (Subject, error) {
	subject := Subject{Email: email}
	err := pool.QueryRow(ctx, `
SELECT
    EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL),
    (SELECT count(*) FROM workspace_members wm
        JOIN users u ON u.id = wm.user_id
        WHERE lower(u.email) = lower($1)),
    (SELECT count(*) FROM surveys s
        WHERE s.workspace_id IN (
            SELECT wm.workspace_id FROM workspace_members wm
            JOIN users u ON u.id = wm.user_id
            WHERE lower(u.email) = lower($1))),
    (SELECT count(*) FROM participants WHERE lower(email) = lower($1)),
    (SELECT count(*) FROM responses r
        JOIN participants p ON p.id = r.participant_id
        WHERE lower(p.email) = lower($1)),
    EXISTS (SELECT 1 FROM suppressions WHERE lower(email) = lower($1))`,
		email).Scan(&subject.HasAccount, &subject.Workspaces, &subject.Surveys,
		&subject.ParticipantIn, &subject.Responses, &subject.Suppressed)
	if err != nil {
		return Subject{}, fmt.Errorf("purge: preview subject: %w", err)
	}
	return subject, nil
}

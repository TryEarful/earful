// Package store_test verifies the guarantees that live in the database
// itself rather than in Go.
//
// These are the one deliberate exception to SPEC.md's "drive it like a
// browser" rule, and the reason is the point of the tests: ADR-0001's
// immutability and ADR-0003's fixed anonymity must hold against paths the
// application does not offer — a future admin script, a migration, a psql
// session. A test that only used the HTTP surface could not distinguish
// "the database refuses this" from "no handler happens to do it", which
// is precisely the distinction being asserted.
package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
)

func newStore(t *testing.T) (*store.Surveys, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	dsn := apptest.NewDB(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// A workspace and user of this test's own, per the isolation model in
	// docs/testing.md.
	ctx := context.Background()
	var workspaceID, userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (name) VALUES ($1) RETURNING id`,
		"immutability-test-"+uuid.NewString()[:8]).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING id`,
		apptest.UniqueEmail("immutability")).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return store.NewSurveys(pool), pool, workspaceID, userID
}

// publishedSurvey creates and publishes a one-question survey, returning
// the survey and version ids.
func publishedSurvey(t *testing.T, s *store.Surveys, workspaceID, userID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	survey, err := s.Create(ctx, workspaceID, userID, "Immutability", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	draft := domain.Draft{Questions: []domain.Question{{
		IdentityID: uuid.NewString(),
		Type:       domain.LongText,
		Text:       "What happened?",
	}}}
	if err := s.SaveDraft(ctx, survey.ID, userID, draft, time.Now()); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	version, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return survey.ID, version.ID
}

// TestPublishedVersionsRejectUpdateAndDelete is ADR-0001's core promise:
// what a respondent was shown can never be altered afterwards.
func TestPublishedVersionsRejectUpdateAndDelete(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	_, versionID := publishedSurvey(t, s, workspaceID, userID)
	ctx := context.Background()

	t.Run("version row cannot be updated", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE survey_versions SET number = number + 100 WHERE id = $1`, versionID)
		assertImmutable(t, err)
	})
	t.Run("version row cannot be deleted", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM survey_versions WHERE id = $1`, versionID)
		assertImmutable(t, err)
	})
	t.Run("question text cannot be rewritten", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`UPDATE questions SET text = 'a question nobody was ever asked' WHERE version_id = $1`, versionID)
		assertImmutable(t, err)
	})
	t.Run("question cannot be deleted", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM questions WHERE version_id = $1`, versionID)
		assertImmutable(t, err)
	})

	// And the data really is still there and unchanged.
	questions, err := s.QuestionsForVersion(ctx, versionID)
	if err != nil {
		t.Fatalf("read questions: %v", err)
	}
	if len(questions) != 1 || questions[0].Text != "What happened?" {
		t.Errorf("published question changed: %+v", questions)
	}
}

// TestDraftRevisionsAreAppendOnly: the audit log is only trustworthy if
// nobody can rewrite history after the fact.
func TestDraftRevisionsAreAppendOnly(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()

	survey, err := s.Create(ctx, workspaceID, userID, "Revisions", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	draft := domain.Draft{Questions: []domain.Question{{
		IdentityID: uuid.NewString(), Type: domain.ShortText, Text: "Name?",
	}}}
	if err := s.SaveDraft(ctx, survey.ID, userID, draft, time.Now()); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	_, draftID, err := s.Draft(ctx, survey.ID)
	if err != nil {
		t.Fatalf("load draft: %v", err)
	}

	_, err = pool.Exec(ctx, `UPDATE draft_revisions SET structure = '{"questions":[]}'::jsonb WHERE draft_id = $1`, draftID)
	assertImmutable(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM draft_revisions WHERE draft_id = $1`, draftID)
	assertImmutable(t, err)

	revisions, err := s.Revisions(ctx, draftID)
	if err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	if len(revisions) != 1 || revisions[0].QuestionCount != 1 {
		t.Errorf("revision history was altered: %+v", revisions)
	}
}

// TestAnonymityCannotBeChangedInTheDatabase is ADR-0003 enforced where it
// counts: even a direct UPDATE is refused, so the promise survives code
// that never went through the application.
func TestAnonymityCannotBeChangedInTheDatabase(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()

	for _, anonymous := range []bool{true, false} {
		survey, err := s.Create(ctx, workspaceID, userID, "Anonymity", anonymous, nil)
		if err != nil {
			t.Fatalf("create survey: %v", err)
		}
		_, err = pool.Exec(ctx, `UPDATE surveys SET is_anonymous = $2 WHERE id = $1`, survey.ID, !anonymous)
		if err == nil {
			t.Fatalf("is_anonymous=%v was changed by a direct UPDATE", anonymous)
		}
		if !strings.Contains(err.Error(), "immutable") {
			t.Errorf("unexpected error changing is_anonymous: %v", err)
		}

		// Other columns still update fine — the guard is targeted, not a
		// blanket freeze on the row.
		if _, err := pool.Exec(ctx, `UPDATE surveys SET title = 'renamed' WHERE id = $1`, survey.ID); err != nil {
			t.Errorf("unrelated column update was blocked: %v", err)
		}
	}
}

// TestSurveyIsAnonymousSurvivesSettingsUpdate proves the store's own
// update path cannot touch anonymity either, independent of the trigger.
func TestSurveyIsAnonymousSurvivesSettingsUpdate(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()

	survey, err := s.Create(ctx, workspaceID, userID, "Settings", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	if err := s.UpdateSettings(ctx, workspaceID, survey.ID, "Renamed", nil); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	reloaded, err := s.Get(ctx, workspaceID, survey.ID)
	if err != nil {
		t.Fatalf("get survey: %v", err)
	}
	if !reloaded.IsAnonymous {
		t.Error("anonymity was lost through a settings update")
	}
	if reloaded.Title != "Renamed" {
		t.Errorf("title = %q, want Renamed", reloaded.Title)
	}
}

// TestPublishAcrossVersionsKeepsIdentityAndFreezesWording is the
// ADR-0001 scenario in miniature: version 1's wording stays readable
// forever, while the identity carries into version 2.
func TestPublishAcrossVersionsKeepsIdentityAndFreezesWording(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()

	survey, err := s.Create(ctx, workspaceID, userID, "Two versions", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	identity := uuid.NewString()
	v1Draft := domain.Draft{Questions: []domain.Question{{
		IdentityID: identity, Type: domain.LongText, Text: "How was it?",
	}}}
	if err := s.SaveDraft(ctx, survey.ID, userID, v1Draft, time.Now()); err != nil {
		t.Fatalf("save v1 draft: %v", err)
	}
	v1, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now())
	if err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	v2Draft := domain.Draft{Questions: []domain.Question{{
		IdentityID: identity, Type: domain.LongText, Text: "Looking back, how did it go?",
	}}}
	if err := s.SaveDraft(ctx, survey.ID, userID, v2Draft, time.Now()); err != nil {
		t.Fatalf("save v2 draft: %v", err)
	}
	v2, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now())
	if err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	v1Questions, err := s.QuestionsForVersion(ctx, v1.ID)
	if err != nil {
		t.Fatalf("read v1: %v", err)
	}
	v2Questions, err := s.QuestionsForVersion(ctx, v2.ID)
	if err != nil {
		t.Fatalf("read v2: %v", err)
	}

	if v1Questions[0].Text != "How was it?" {
		t.Errorf("version 1 wording changed: %q", v1Questions[0].Text)
	}
	if v2Questions[0].Text != "Looking back, how did it go?" {
		t.Errorf("version 2 wording wrong: %q", v2Questions[0].Text)
	}
	if v1Questions[0].IdentityID != v2Questions[0].IdentityID {
		t.Errorf("Question Identity differs across versions: %s vs %s",
			v1Questions[0].IdentityID, v2Questions[0].IdentityID)
	}
}

func assertImmutable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the database allowed a mutation that must be impossible")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected an immutability error, got: %v", err)
	}
}

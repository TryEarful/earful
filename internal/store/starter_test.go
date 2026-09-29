package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
)

// starterDraft is a survey of one question, which is all the store needs
// to be asked to publish: what the Starter Survey says is tested where
// it is written.
func starterDraft(t *testing.T) domain.Draft {
	t.Helper()
	var draft domain.Draft
	if err := draft.Add(domain.Question{
		IdentityID: uuid.NewString(),
		Type:       domain.LongText,
		Text:       "How is it going?",
	}); err != nil {
		t.Fatalf("draft: %v", err)
	}
	return draft
}

// TestAddStarterSurvey_IsGivenToAWorkspaceOnce: a workspace holds one
// live Starter Survey. A second is refused in words, and by the database
// for a caller that does not ask first; a deleted one is no obstacle to
// another.
func TestAddStarterSurvey_IsGivenToAWorkspaceOnce(t *testing.T) {
	t.Parallel()
	surveys, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()
	now := time.Now()

	var owner string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id) VALUES ($1, $2)`, workspaceID, userID); err != nil {
		t.Fatalf("create membership: %v", err)
	}

	first, err := surveys.AddStarterSurvey(ctx, owner, "Starter", starterDraft(t), now)
	if err != nil {
		t.Fatalf("add starter survey: %v", err)
	}
	if first.Origin != store.OriginStarter || !first.IsAnonymous || first.LatestVersion != 1 {
		t.Errorf("the starter survey is %+v, want an anonymous survey of origin %q at version 1", first, store.OriginStarter)
	}
	listed, err := surveys.Get(ctx, workspaceID, first.ID)
	if err != nil {
		t.Fatalf("get starter survey: %v", err)
	}
	if listed.Origin != store.OriginStarter || listed.LatestVersion != 1 || listed.QuestionCount != 1 {
		t.Errorf("the starter survey reads back as %+v", listed)
	}

	if _, err := surveys.AddStarterSurvey(ctx, owner, "Starter", starterDraft(t), now); !errors.Is(err, store.ErrStarterExists) {
		t.Errorf("a second starter survey: err = %v, want ErrStarterExists", err)
	}
	// The index, for a writer that did not ask.
	_, err = pool.Exec(ctx,
		`INSERT INTO surveys (workspace_id, title, is_anonymous, created_by, origin) VALUES ($1, 'Second', true, $2, 'starter')`,
		workspaceID, userID)
	if err == nil {
		t.Error("the database accepted a second live starter survey in one workspace")
	}

	if err := surveys.SoftDelete(ctx, workspaceID, first.ID, now); err != nil {
		t.Fatalf("delete starter survey: %v", err)
	}
	second, err := surveys.AddStarterSurvey(ctx, owner, "Starter", starterDraft(t), now)
	if err != nil {
		t.Fatalf("add a starter survey after deleting the first: %v", err)
	}
	if second.ID == first.ID {
		t.Error("the deleted starter survey came back in place of a new one")
	}
}

// TestAddStarterSurvey_NeedsAnOwner: an address nobody signed up with
// owns no workspace to put a survey in.
func TestAddStarterSurvey_NeedsAnOwner(t *testing.T) {
	t.Parallel()
	surveys, _, _, _ := newStore(t)
	_, err := surveys.AddStarterSurvey(context.Background(),
		"nobody-"+uuid.NewString()[:8]+"@example.test", "Starter", starterDraft(t), time.Now())
	if !errors.Is(err, store.ErrOwnerUnknown) {
		t.Errorf("err = %v, want ErrOwnerUnknown", err)
	}
}

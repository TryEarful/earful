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

// TestRefreshFromAccount_ASurveyWithNoRoomIsSkippedAndTheRestGoOut: a
// survey that keeps its limit of pictures, every one shown by a version,
// cannot take the account's new logo. It is named with that, before and
// after the creator asks, and the other surveys still go out.
func TestRefreshFromAccount_ASurveyWithNoRoomIsSkippedAndTheRestGoOut(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()
	now := time.Now()

	logo := func(img store.NewImage) domain.Style {
		return domain.Style{Header: domain.StyleHeader{Name: "Corner Workshop", Logo: shows(img), LogoAlt: "The workshop's logo"}}
	}
	first, second := stylePicture(100), stylePicture(101)
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, logo(first), []store.NewImage{first}, now); err != nil {
		t.Fatalf("save account style: %v", err)
	}

	// Full follows the account's header and keeps a thanks picture of its
	// own in each of nine versions: with the account's logo, ten pictures,
	// every one shown.
	full, err := s.Create(ctx, workspaceID, userID, "Full of pictures", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	for n := 0; n < store.MaxSurveyImages-1; n++ {
		draft := stylePicturesDraft()
		picture := stylePicture(n)
		draft.Style.Thanks = domain.StyleThanks{Picture: domain.ThanksImage, Image: shows(picture), Alt: "A thank you"}
		if err := s.SaveStyle(ctx, full.ID, userID, draft, []store.NewImage{picture}, now); err != nil {
			t.Fatalf("save thanks picture %d: %v", n, err)
		}
		if _, err := s.Publish(ctx, workspaceID, full.ID, userID, now); err != nil {
			t.Fatalf("publish %d: %v", n, err)
		}
	}
	// Room follows everything and has pictures to spare.
	room, err := s.Create(ctx, workspaceID, userID, "Room to spare", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	if err := s.SaveDraft(ctx, room.ID, userID, stylePicturesDraft(), now); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := s.Publish(ctx, workspaceID, room.ID, userID, now); err != nil {
		t.Fatalf("publish: %v", err)
	}

	later := now.Add(time.Minute)
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, logo(second), []store.NewImage{second}, later); err != nil {
		t.Fatalf("change the account's logo: %v", err)
	}
	account, err := s.WorkspaceStyle(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	stale, err := s.StaleSurveys(ctx, workspaceID, account, later)
	if err != nil {
		t.Fatalf("stale surveys: %v", err)
	}
	holds := map[uuid.UUID]store.Hold{}
	for _, survey := range stale {
		holds[survey.ID] = survey.Hold
	}
	if hold, listed := holds[full.ID]; !listed || hold != store.HoldPictures {
		t.Errorf("the full survey is listed %v with hold %q, want it named for its pictures", listed, hold)
	}
	if hold, listed := holds[room.ID]; !listed || hold != "" {
		t.Errorf("the survey with room is listed %v with hold %q, want it ready", listed, hold)
	}

	// Asked to update both, as a stale page might ask: the full one is
	// skipped and named, and the other goes out.
	result, err := s.RefreshFromAccount(ctx, workspaceID, []uuid.UUID{full.ID, room.ID}, account.UpdatedAt.UnixMicro(), userID, later)
	if err != nil {
		t.Fatalf("one survey with no room stopped the rest: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "Room to spare" {
		t.Errorf("updated %q, want the survey with room", result.Updated)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].ID != full.ID || result.Skipped[0].Hold != store.HoldPictures {
		t.Errorf("skipped %+v, want the full survey, for its pictures", result.Skipped)
	}
	versions, err := s.Versions(ctx, full.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != store.MaxSurveyImages-1 {
		t.Errorf("the full survey has %d versions, want it left as it was", len(versions))
	}
}

// TestRefreshFromAccount_ASurveyDeletedSinceIsLeftAlone: a survey the
// creator deleted after the question listed it is left alone, and the
// rest go out; a survey of another workspace refuses the request.
func TestRefreshFromAccount_ASurveyDeletedSinceIsLeftAlone(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()
	now := time.Now()

	footer := func(text string) domain.Style {
		return domain.Style{Footer: domain.StyleFooter{Text: text}}
	}
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, footer("12 Mill Lane"), nil, now); err != nil {
		t.Fatalf("save account style: %v", err)
	}
	publish := func(title string) uuid.UUID {
		survey, err := s.Create(ctx, workspaceID, userID, title, true, nil)
		if err != nil {
			t.Fatalf("create survey: %v", err)
		}
		if err := s.SaveDraft(ctx, survey.ID, userID, stylePicturesDraft(), now); err != nil {
			t.Fatalf("save draft: %v", err)
		}
		if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, now); err != nil {
			t.Fatalf("publish: %v", err)
		}
		return survey.ID
	}
	gone, kept := publish("Deleted since"), publish("Kept")

	later := now.Add(time.Minute)
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, footer("14 Mill Lane"), nil, later); err != nil {
		t.Fatalf("change the account's footer: %v", err)
	}
	account, err := s.WorkspaceStyle(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, workspaceID, gone, later); err != nil {
		t.Fatalf("delete survey: %v", err)
	}

	result, err := s.RefreshFromAccount(ctx, workspaceID, []uuid.UUID{gone, kept}, account.UpdatedAt.UnixMicro(), userID, later)
	if err != nil {
		t.Fatalf("a survey deleted since stopped the rest: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "Kept" || len(result.Skipped) != 0 {
		t.Errorf("updated %q and skipped %+v, want only the kept survey updated", result.Updated, result.Skipped)
	}

	_, _, otherWorkspace, _ := newStore(t)
	if _, err := s.RefreshFromAccount(ctx, otherWorkspace, []uuid.UUID{kept}, account.UpdatedAt.UnixMicro(), userID, later); err != store.ErrNotFound {
		t.Errorf("another workspace's survey: got %v, want ErrNotFound", err)
	}
}

// TestPublish_APictureStoredNowhereIsMissingNotTooMany: a survey at its
// limit whose draft refers to a picture neither it nor its account
// stores is refused for the missing picture. There is nothing to copy,
// so the limit is not what stands in the way.
func TestPublish_APictureStoredNowhereIsMissingNotTooMany(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()
	now := time.Now()
	survey, err := s.Create(ctx, workspaceID, userID, "At its limit", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	for n := 0; n < store.MaxSurveyImages; n++ {
		draft := stylePicturesDraft()
		draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(stylePicture(n)), "A logo"
		if err := s.SaveStyle(ctx, survey.ID, userID, draft, []store.NewImage{stylePicture(n)}, now); err != nil {
			t.Fatalf("save logo %d: %v", n, err)
		}
		if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, now); err != nil {
			t.Fatalf("publish %d: %v", n, err)
		}
	}
	draft := stylePicturesDraft()
	draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(stylePicture(77)), "A logo"
	if err := s.SaveDraft(ctx, survey.ID, userID, draft, now); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, now); !errors.Is(err, store.ErrStyleImageMissing) {
		t.Fatalf("publishing a picture stored nowhere: got %v, want ErrStyleImageMissing", err)
	}
}

// TestStaleSurveys_APictureTheAccountNoLongerStoresIsNamed: a survey whose
// followed header would show an account picture that is stored nowhere is
// named for the missing picture, not offered an update that publishing
// would refuse; asked anyway, it is skipped with the same reason.
func TestStaleSurveys_APictureTheAccountNoLongerStoresIsNamed(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()
	now := time.Now()

	logo := func(img store.NewImage) domain.Style {
		return domain.Style{Header: domain.StyleHeader{Name: "Corner Workshop", Logo: shows(img), LogoAlt: "The workshop's logo"}}
	}
	first, second := stylePicture(200), stylePicture(201)
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, logo(first), []store.NewImage{first}, now); err != nil {
		t.Fatalf("save account style: %v", err)
	}
	survey, err := s.Create(ctx, workspaceID, userID, "Follows the header", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	if err := s.SaveDraft(ctx, survey.ID, userID, stylePicturesDraft(), now); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, now); err != nil {
		t.Fatalf("publish: %v", err)
	}

	later := now.Add(time.Minute)
	if err := s.SaveWorkspaceStyle(ctx, workspaceID, userID, logo(second), []store.NewImage{second}, later); err != nil {
		t.Fatalf("change the account's logo: %v", err)
	}
	// The account's new logo is gone from its pictures while its style
	// still shows it.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace_images WHERE workspace_id = $1 AND encode(sha256, 'hex') = $2`, workspaceID, second.SHA256); err != nil {
		t.Fatalf("remove the account's picture: %v", err)
	}
	account, err := s.WorkspaceStyle(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	stale, err := s.StaleSurveys(ctx, workspaceID, account, later)
	if err != nil {
		t.Fatalf("stale surveys: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != survey.ID || stale[0].Hold != store.HoldPictureMissing {
		t.Fatalf("stale surveys %+v, want the survey named for its missing picture", stale)
	}

	result, err := s.RefreshFromAccount(ctx, workspaceID, []uuid.UUID{survey.ID}, account.UpdatedAt.UnixMicro(), userID, later)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(result.Updated) != 0 || len(result.Skipped) != 1 || result.Skipped[0].Hold != store.HoldPictureMissing {
		t.Errorf("refresh %+v, want the survey skipped for its missing picture", result)
	}
}

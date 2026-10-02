package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/store/db"
)

// stylePicture is a stored picture's stand in: n names it.
func stylePicture(n int) store.NewImage {
	return store.NewImage{
		SHA256:      fmt.Sprintf("%064x", n+1),
		ContentType: "image/png", Width: 4, Height: 4, Bytes: []byte{byte(n)},
	}
}

func shows(img store.NewImage) domain.StyleImage {
	return domain.StyleImage{SHA256: img.SHA256, Width: img.Width, Height: img.Height}
}

func stylePicturesDraft() domain.Draft {
	return domain.Draft{Questions: []domain.Question{{
		IdentityID: uuid.NewString(), Type: domain.LongText, Text: "What happened?",
	}}}
}

// TestSaveStyle_RespondentsAreNotHeldUpBySavingAStyle: while a survey's
// style is being saved or published, the survey is held, and a response
// to it can still be recorded at once. A response refers to the survey
// row, and the hold is one that checking such a reference does not wait
// for.
func TestSaveStyle_RespondentsAreNotHeldUpBySavingAStyle(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()
	survey, err := s.Create(ctx, workspaceID, userID, "Answered while styled", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	if err := s.SaveDraft(ctx, survey.ID, userID, stylePicturesDraft(), time.Now()); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	version, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	saving, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = saving.Rollback(ctx) }()
	if _, err := db.New(saving).LockSurveyForStyle(ctx, survey.ID); err != nil {
		t.Fatalf("hold the survey: %v", err)
	}

	answering, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = answering.Rollback(ctx) }()
	if _, err := answering.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(answering).CreateResponse(ctx, db.CreateResponseParams{
		SurveyID: survey.ID, VersionID: version.ID, SubmittedAt: time.Now(),
	}); err != nil {
		t.Errorf("a response waited for the survey's style to be saved: %v", err)
	}
}

// TestSaveStyle_ASaveAtTheLimitKeepsEveryPictureItBrings: a survey near its
// limit of stored pictures, with pictures it no longer uses, is saved
// with a new banner and a new logo at once. Making room removes the
// unused pictures, never the ones the same save brings, and the style is
// published with both.
func TestSaveStyle_ASaveAtTheLimitKeepsEveryPictureItBrings(t *testing.T) {
	t.Parallel()
	s, pool, workspaceID, userID := newStore(t)
	ctx := context.Background()
	survey, err := s.Create(ctx, workspaceID, userID, "Pictures", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}

	// Nine logos tried and replaced: stored, and shown by nothing.
	for n := 0; n < store.MaxSurveyImages-1; n++ {
		draft := stylePicturesDraft()
		draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(stylePicture(n)), "A logo"
		if err := s.SaveStyle(ctx, survey.ID, userID, draft, []store.NewImage{stylePicture(n)}, time.Now()); err != nil {
			t.Fatalf("save logo %d: %v", n, err)
		}
	}
	draft := stylePicturesDraft()
	banner, logo := stylePicture(100), stylePicture(101)
	draft.Style.Header.Banner = shows(banner)
	draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(logo), "A logo"
	if err := s.SaveStyle(ctx, survey.ID, userID, draft, []store.NewImage{banner, logo}, time.Now()); err != nil {
		t.Fatalf("save banner and logo: %v", err)
	}
	for name, img := range map[string]store.NewImage{"banner": banner, "logo": logo} {
		if _, err := s.DraftImage(ctx, workspaceID, survey.ID, img.SHA256); err != nil {
			t.Errorf("the %s the save brought is not stored: %v", name, err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM survey_images WHERE survey_id = $1`, survey.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > store.MaxSurveyImages {
		t.Errorf("%d pictures stored, over the limit of %d", count, store.MaxSurveyImages)
	}
	if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for name, img := range map[string]store.NewImage{"banner": banner, "logo": logo} {
		if _, err := s.PublishedImage(ctx, img.SHA256); err != nil {
			t.Errorf("the published %s is not served: %v", name, err)
		}
	}
}

// TestSaveStyle_TenPicturesInUseRefuseAnEleventh: when every stored
// picture is shown, the save is refused, naming the picture that did not
// fit, and the draft is left as it was.
func TestSaveStyle_TenPicturesInUseRefuseAnEleventh(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()
	survey, err := s.Create(ctx, workspaceID, userID, "Pictures", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	for n := 0; n < store.MaxSurveyImages; n++ {
		draft := stylePicturesDraft()
		draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(stylePicture(n)), "A logo"
		if err := s.SaveStyle(ctx, survey.ID, userID, draft, []store.NewImage{stylePicture(n)}, time.Now()); err != nil {
			t.Fatalf("save logo %d: %v", n, err)
		}
		if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now()); err != nil {
			t.Fatalf("publish %d: %v", n, err)
		}
	}
	before, _, err := s.Draft(ctx, survey.ID)
	if err != nil {
		t.Fatal(err)
	}
	draft := stylePicturesDraft()
	eleventh := stylePicture(50)
	draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(eleventh), "A logo"
	err = s.SaveStyle(ctx, survey.ID, userID, draft, []store.NewImage{eleventh}, time.Now())
	var full store.ImageLimitError
	if !errors.As(err, &full) || full.SHA256 != eleventh.SHA256 || !errors.Is(err, store.ErrTooManyImages) {
		t.Fatalf("an eleventh picture: got %v, want the limit naming it", err)
	}
	after, _, err := s.Draft(ctx, survey.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Style.Equal(before.Style) {
		t.Error("a refused save changed the draft's style")
	}
}

// TestPublish_AStyleWhosePictureIsNotStoredIsRefused: a version cannot be
// changed once published, so a draft that refers to a picture the survey
// does not store is not published with it.
func TestPublish_AStyleWhosePictureIsNotStoredIsRefused(t *testing.T) {
	t.Parallel()
	s, _, workspaceID, userID := newStore(t)
	ctx := context.Background()
	survey, err := s.Create(ctx, workspaceID, userID, "Pictures", true, nil)
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	draft := stylePicturesDraft()
	draft.Style.Header.Logo, draft.Style.Header.LogoAlt = shows(stylePicture(7)), "A logo"
	if err := s.SaveDraft(ctx, survey.ID, userID, draft, time.Now()); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := s.Publish(ctx, workspaceID, survey.ID, userID, time.Now()); !errors.Is(err, store.ErrStyleImageMissing) {
		t.Fatalf("publish: got %v, want %v", err, store.ErrStyleImageMissing)
	}
}

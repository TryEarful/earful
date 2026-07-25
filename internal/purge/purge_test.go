package purge_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/purge"
)

// Purge is global by nature, which makes it the one exception to the
// isolation model: a global DELETE cannot share a database with tests
// that are still using it, and two purges running concurrently see each
// other's half-finished work. So these tests share their own database
// (apptest.NewIsolatedDB) and run serially — no t.Parallel() below, on
// purpose. Within that, they behave normally: seed through the real
// application, time-travel the fake clock, assert on what survives.

// purgeApp boots an instance on the isolated purge database.
func purgeApp(t *testing.T) *apptest.App {
	t.Helper()
	return apptest.New(t, apptest.Options{DSN: apptest.NewIsolatedDB(t, "purge")})
}

func poolFor(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// seedAnsweredSurvey builds a published survey with one response,
// through the real endpoints, and returns its id.
func seedAnsweredSurvey(t *testing.T, app *apptest.App, creator *http.Client, title string) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, title, true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	resp, err := respondent.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("open survey: %v", err)
	}
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()

	form := respondFields(t, page)
	form.Set("q_"+answerField(t, page), "An answer that should eventually be erased")
	app.Clock.Advance(5 * time.Second)
	resp, err = respondent.PostForm(app.Server.URL+"/s/"+id, form)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	resp.Body.Close()
	return id
}

// TestPurge_ErasesSoftDeletedSurveysAfterThirtyDays is story 60 and 61
// together: deleting is undoable for thirty days, and then it is not.
func TestPurge_ErasesSoftDeletedSurveysAfterThirtyDays(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-survey"))

	id := seedAnsweredSurvey(t, app, creator, "Doomed survey")
	keep := seedAnsweredSurvey(t, app, creator, "Surviving survey")

	app.PostForm(t, creator, "/surveys/"+id+"/delete", nil).Body.Close()

	// Twenty-nine days later: hidden from the creator, but still there
	// for support to restore.
	app.Clock.Advance(29 * 24 * time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge at 29 days: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 1 {
		t.Fatalf("a survey deleted 29 days ago is already gone; support could not restore it")
	}

	// Day thirty-one: erased, along with everything under it.
	app.Clock.Advance(2 * 24 * time.Hour)
	report, err := purge.Run(context.Background(), pool, app.Clock.Now(), false)
	if err != nil {
		t.Fatalf("purge at 31 days: %v", err)
	}
	if report.Total() == 0 {
		t.Fatal("purge reported nothing removed")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 0 {
		t.Error("the soft-deleted survey survived its retention window")
	}
	for _, check := range []struct {
		what  string
		query string
	}{
		{"responses", `SELECT count(*) FROM responses WHERE survey_id = $1`},
		{"answers", `SELECT count(*) FROM answers a JOIN responses r ON r.id = a.response_id WHERE r.survey_id = $1`},
		{"versions", `SELECT count(*) FROM survey_versions WHERE survey_id = $1`},
		{"questions", `SELECT count(*) FROM questions q JOIN survey_versions v ON v.id = q.version_id WHERE v.survey_id = $1`},
		{"drafts", `SELECT count(*) FROM survey_drafts WHERE survey_id = $1`},
		{"identities", `SELECT count(*) FROM question_identities WHERE survey_id = $1`},
		{"stats", `SELECT count(*) FROM survey_stats WHERE survey_id = $1`},
	} {
		if n := countRows(t, pool, check.query, id); n != 0 {
			t.Errorf("%s survived the purge (%d rows) — the survey is only half erased", check.what, n)
		}
	}

	// The other survey is untouched: purge deletes what is doomed, not
	// what is nearby.
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, keep); n != 1 {
		t.Error("purge removed a survey that was never deleted")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM responses WHERE survey_id = $1`, keep); n != 1 {
		t.Error("purge removed a live survey's responses")
	}
}

// TestPurge_DryRunChangesNothing: the flag exists so an operator can look
// before leaping, and the numbers must be the real ones.
func TestPurge_DryRunChangesNothing(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-dry"))

	id := seedAnsweredSurvey(t, app, creator, "Dry run survey")
	app.PostForm(t, creator, "/surveys/"+id+"/delete", nil).Body.Close()
	app.Clock.Advance(31 * 24 * time.Hour)

	report, err := purge.Run(context.Background(), pool, app.Clock.Now(), true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !report.DryRun {
		t.Error("report does not say it was a dry run")
	}
	if report.Counts["doomed_surveys"] == 0 {
		t.Error("a dry run reported no survey to delete, but one was due")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 1 {
		t.Fatal("a dry run deleted a survey")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM responses WHERE survey_id = $1`, id); n != 1 {
		t.Fatal("a dry run deleted responses")
	}

	// And the real run afterwards removes exactly what the dry run said.
	real, err := purge.Run(context.Background(), pool, app.Clock.Now(), false)
	if err != nil {
		t.Fatalf("real run: %v", err)
	}
	if real.Counts["doomed_surveys"] != report.Counts["doomed_surveys"] {
		t.Errorf("dry run promised %d surveys, real run removed %d",
			report.Counts["doomed_surveys"], real.Counts["doomed_surveys"])
	}
}

// TestPurge_IsIdempotent: a scheduled job runs every day, most days with
// nothing to do, and must be safe either way.
func TestPurge_IsIdempotent(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-twice"))

	id := seedAnsweredSurvey(t, app, creator, "Twice-purged survey")
	app.PostForm(t, creator, "/surveys/"+id+"/delete", nil).Body.Close()
	app.Clock.Advance(31 * 24 * time.Hour)

	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := purge.Run(context.Background(), pool, app.Clock.Now(), false)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Counts["doomed_surveys"] != 0 {
		t.Errorf("the second run found %d surveys to delete", second.Counts["doomed_surveys"])
	}
}

// TestPurge_TrimsShortRetentionData: the abuse log is the only table that
// ever holds an IP, and thirty days is what the privacy notice promises.
func TestPurge_TrimsShortRetentionData(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	ctx := context.Background()

	// A row that is ours to assert on: a unique path nobody else writes.
	path := "/s/purge-test-" + apptest.UniqueEmail("abuse")
	_, err := pool.Exec(ctx,
		`INSERT INTO abuse_log (ip, path, kind, at) VALUES ($1, $2, $3, $4)`,
		"203.0.113.7", path, "honeypot", app.Clock.Now().Add(-31*24*time.Hour))
	if err != nil {
		t.Fatalf("seed abuse log: %v", err)
	}
	fresh := path + "-fresh"
	if _, err := pool.Exec(ctx,
		`INSERT INTO abuse_log (ip, path, kind, at) VALUES ($1, $2, $3, $4)`,
		"203.0.113.8", fresh, "too_fast", app.Clock.Now()); err != nil {
		t.Fatalf("seed fresh abuse log: %v", err)
	}

	if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM abuse_log WHERE path = $1`, path); n != 0 {
		t.Error("an abuse-log row older than 30 days survived")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM abuse_log WHERE path = $1`, fresh); n != 1 {
		t.Error("purge removed a recent abuse-log row")
	}
}

// TestPurge_KeepsTheNewestDraftRevision: trimming history is fine;
// leaving a draft with no history at all is not.
func TestPurge_KeepsTheNewestDraftRevision(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-revisions"))

	id := app.CreateSurvey(t, creator, "Old draft", true)
	app.AddQuestion(t, creator, id, "short_text", "First", nil)
	app.AddQuestion(t, creator, id, "short_text", "Second", nil)
	app.AddQuestion(t, creator, id, "short_text", "Third", nil)

	before := countRows(t, pool,
		`SELECT count(*) FROM draft_revisions dr JOIN survey_drafts d ON d.id = dr.draft_id WHERE d.survey_id = $1`, id)
	if before < 3 {
		t.Fatalf("expected a revision per save, got %d", before)
	}

	app.Clock.Advance(91 * 24 * time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}

	after := countRows(t, pool,
		`SELECT count(*) FROM draft_revisions dr JOIN survey_drafts d ON d.id = dr.draft_id WHERE d.survey_id = $1`, id)
	if after != 1 {
		t.Errorf("draft has %d revisions after the trim, want exactly the newest one", after)
	}
	// The draft itself is untouched — the survey was never deleted.
	// Checked in the database rather than through the editor because the
	// clock has moved 91 days and the creator's session expired with it,
	// which is the session behaving correctly.
	var structure string
	err := pool.QueryRow(context.Background(),
		`SELECT structure::text FROM survey_drafts WHERE survey_id = $1`, id).Scan(&structure)
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	if !strings.Contains(structure, "Third") {
		t.Errorf("trimming revisions changed the draft: %s", structure)
	}
}

// TestErase_RemovesASubjectImmediately is M8-T3: a GDPR erasure request
// completes now, not in thirty days.
func TestErase_RemovesASubjectImmediately(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	ctx := context.Background()

	subject := apptest.UniqueEmail("erase-me")
	creator := app.Login(t, subject)
	id := seedAnsweredSurvey(t, app, creator, "Survey of an erased account")

	// A bystander, to prove erasure is targeted.
	bystander := apptest.UniqueEmail("bystander")
	other := app.Login(t, bystander)
	otherID := seedAnsweredSurvey(t, app, other, "Someone else's survey")

	report, err := purge.EraseSubject(ctx, pool, subject, app.Clock.Now())
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if report.Total() == 0 {
		t.Fatal("erasure reported nothing removed")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE lower(email) = lower($1)`, subject); n != 0 {
		t.Error("the erased account still exists")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 0 {
		t.Error("the erased account's survey still exists")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM responses WHERE survey_id = $1`, id); n != 0 {
		t.Error("responses to the erased account's survey still exist")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM users WHERE lower(email) = lower($1)`, bystander); n != 1 {
		t.Error("erasure removed somebody else's account")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, otherID); n != 1 {
		t.Error("erasure removed somebody else's survey")
	}
}

// TestErase_RemovesAParticipantsAnswers: a subject need not have an
// account. Someone invited to a survey can ask too, and their answers
// are personal data.
func TestErase_RemovesAParticipantsAnswers(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	ctx := context.Background()
	creator := app.Login(t, apptest.UniqueEmail("invited-erase"))

	id := app.CreateSurvey(t, creator, "Invited survey", false)
	app.AddQuestion(t, creator, id, "short_text", "Your take?", nil)
	app.Publish(t, creator, id)

	guest := apptest.UniqueEmail("guest-erase")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {guest}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil).Body.Close()

	if n := countRows(t, pool, `SELECT count(*) FROM participants WHERE lower(email) = lower($1)`, guest); n != 1 {
		t.Fatal("the participant was not created")
	}

	if _, err := purge.EraseSubject(ctx, pool, guest, app.Clock.Now()); err != nil {
		t.Fatalf("erase participant: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM participants WHERE lower(email) = lower($1)`, guest); n != 0 {
		t.Error("the participant survived erasure")
	}
	// The survey itself belongs to someone else and stays.
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 1 {
		t.Error("erasing a participant removed the creator's survey")
	}
}

// respondFields and answerField read a rendered respondent page the way
// a browser does: the form's own hidden fields, and the field name for
// the first question.
var (
	versionRe = regexp.MustCompile(`name="version_id" value="([0-9a-f-]{36})"`)
	formTsRe  = regexp.MustCompile(`name="form_ts" value="([^"]+)"`)
	nonceRe   = regexp.MustCompile(`name="form_nonce" value="([^"]+)"`)
	answerRe  = regexp.MustCompile(`name="q_([0-9a-f-]{36})"`)
)

func respondFields(t *testing.T, page string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, field := range []struct {
		name string
		re   *regexp.Regexp
	}{{"version_id", versionRe}, {"form_ts", formTsRe}, {"form_nonce", nonceRe}} {
		if m := field.re.FindStringSubmatch(page); m != nil {
			form.Set(field.name, m[1])
		}
	}
	if form.Get("version_id") == "" {
		t.Fatalf("no version_id on the respondent page:\n%s", page)
	}
	return form
}

func answerField(t *testing.T, page string) string {
	t.Helper()
	m := answerRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no answer field on the respondent page:\n%s", page)
	}
	return m[1]
}

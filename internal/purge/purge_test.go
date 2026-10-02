package purge_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
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
		{"daily stats", `SELECT count(*) FROM survey_stats_daily WHERE survey_id = $1`},
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

// TestPurge_ErasesWhatRefersToASurveysRows covers the rows that point
// at a question, an answer or a survey rather than hang beneath one in
// the schema's main line: a Localization, a translation of an answer,
// an Insight Summary. Each must go before the row it refers to, or the
// database refuses the purge and nothing at all is erased.
func TestPurge_ErasesWhatRefersToASurveysRows(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	ctx := context.Background()
	creator := app.Login(t, apptest.UniqueEmail("purge-references"))

	id := seedAnsweredSurvey(t, app, creator, "Doomed, with translations")

	// Written directly: what is tested is that the purge removes these
	// rows, and how the application comes to write them is tested where
	// it does.
	for _, insert := range []string{
		`INSERT INTO question_localizations (version_id, question_id, lang, text)
		 SELECT q.version_id, q.id, 'es', '¿Qué pasó?'
		 FROM questions q JOIN survey_versions v ON v.id = q.version_id
		 WHERE v.survey_id = $1`,
		`INSERT INTO answer_translations (answer_id, lang, text, model)
		 SELECT a.id, 'es', 'Una respuesta', 'test-model'
		 FROM answers a JOIN responses r ON r.id = a.response_id
		 WHERE r.survey_id = $1`,
		`INSERT INTO insight_runs (survey_id, response_count, model, output)
		 VALUES ($1, 1, 'test-model', 'A reading of the answers')`,
	} {
		tag, err := pool.Exec(ctx, insert, id)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if tag.RowsAffected() != 1 {
			t.Fatalf("seed wrote %d rows, want 1:\n%s", tag.RowsAffected(), insert)
		}
	}

	app.PostForm(t, creator, "/surveys/"+id+"/delete", nil).Body.Close()
	app.Clock.Advance(31 * 24 * time.Hour)
	if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}

	for _, check := range []struct {
		what  string
		query string
	}{
		{"the survey", `SELECT count(*) FROM surveys WHERE id = $1`},
		{"localizations", `SELECT count(*) FROM question_localizations l JOIN survey_versions v ON v.id = l.version_id WHERE v.survey_id = $1`},
		{"insights", `SELECT count(*) FROM insight_runs WHERE survey_id = $1`},
	} {
		if n := countRows(t, pool, check.query, id); n != 0 {
			t.Errorf("%s survived the purge (%d rows)", check.what, n)
		}
	}
	// The answers are gone, so a translation can only be counted as one
	// that refers to no answer.
	if n := countRows(t, pool, `SELECT count(*) FROM answer_translations t WHERE NOT EXISTS (SELECT 1 FROM answers a WHERE a.id = t.answer_id)`); n != 0 {
		t.Errorf("%d answer translations outlived their answers", n)
	}
}

// TestPurge_ReachesEveryTableUnderASurvey reads the schema for the
// tables that refer to a survey or to anything a survey is made of, and
// the purge for a DELETE from each. A table added later and not given
// one makes the purge fail on the first survey that uses it, which is
// found out thirty days after the survey was deleted.
func TestPurge_ReachesEveryTableUnderASurvey(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)

	source, err := os.ReadFile("purge.go")
	if err != nil {
		t.Fatalf("read purge.go: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
SELECT DISTINCT child.relname
FROM pg_constraint c
JOIN pg_class child ON child.oid = c.conrelid
JOIN pg_class parent ON parent.oid = c.confrelid
WHERE c.contype = 'f'
  AND parent.relname IN ('surveys', 'survey_versions', 'survey_drafts',
                         'questions', 'responses', 'answers')
ORDER BY 1`)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	defer rows.Close()

	var tables int
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables++
		if !strings.Contains(string(source), "DELETE FROM "+table+" ") {
			t.Errorf("%s refers to a survey's rows and the purge never deletes from it", table)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if tables == 0 {
		t.Fatal("the schema lists no table under a survey: the query no longer finds them")
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

// TestPurge_KeepsAClosureCopyForSevenDays: the copy emailed when an
// account closes promises 7 days. The nightly purge must not drop it the
// night after the account closes, and must drop it once the 7 days are
// over, well before the closed workspace itself is erased.
func TestPurge_KeepsAClosureCopyForSevenDays(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	ctx := context.Background()
	addr := apptest.UniqueEmail("purge-closure")
	creator := app.Login(t, addr)
	seedAnsweredSurvey(t, app, creator, "Sent on closure")

	app.PostForm(t, creator, "/account/delete", url.Values{"send_copy": {"yes"}}).Body.Close()

	const job = `FROM export_jobs e JOIN users u ON u.id = e.requested_by
WHERE u.email = $1 AND e.kind = 'closure'`
	deadline := time.Now().Add(20 * time.Second)
	for countRows(t, pool, `SELECT count(*) `+job+` AND e.status = 'ready'`, addr) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the closure copy was never built")
		}
		time.Sleep(50 * time.Millisecond)
	}
	archived := `SELECT count(*) ` + job + ` AND e.archive IS NOT NULL`

	// The first night, then the sixth day.
	for _, step := range []struct {
		day     int
		advance time.Duration
	}{{1, 25 * time.Hour}, {6, 5 * 24 * time.Hour}} {
		app.Clock.Advance(step.advance)
		if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
			t.Fatalf("purge on day %d: %v", step.day, err)
		}
		if n := countRows(t, pool, archived, addr); n != 1 {
			t.Fatalf("the closure copy was purged on day %d, inside its 7 days", step.day)
		}
	}

	app.Clock.Advance(24*time.Hour + time.Minute)
	if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge after day 7: %v", err)
	}
	if n := countRows(t, pool, archived, addr); n != 0 {
		t.Error("the closure copy outlived its 7 days")
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

// styleWithLogo saves a style with a logo of the test's own on a survey,
// and returns the logo's hash as the database holds it.
func styleWithLogo(t *testing.T, app *apptest.App, pool *pgxpool.Pool, creator *http.Client, id string, shade uint8) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 24, 24))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	resp := app.PostMultipart(t, creator, "/surveys/"+id+"/style",
		url.Values{"theme": {"ocean"}, "logo_alt": {"A logo"}},
		apptest.Upload{Field: "logo", Name: "logo.png", Data: buf.Bytes()})
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving a logo: status %d\n%s", resp.StatusCode, page)
	}
	var sha string
	if err := pool.QueryRow(context.Background(),
		`SELECT structure->'style'->'header'->'logo'->>'sha256' FROM survey_drafts WHERE survey_id = $1`, id).Scan(&sha); err != nil {
		t.Fatalf("read the stored logo: %v", err)
	}
	return sha
}

// TestPurge_ErasesASurveysPictures: the pictures of a style go with the
// survey, the published ones too, which nothing else may delete.
func TestPurge_ErasesASurveysPictures(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-pictures"))

	id := app.CreateSurvey(t, creator, "Doomed, with a logo", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	styleWithLogo(t, app, pool, creator, id, 40)
	app.Publish(t, creator, id)
	keep := app.CreateSurvey(t, creator, "Surviving, with a logo", true)
	app.AddQuestion(t, creator, keep, "long_text", "What happened?", nil)
	styleWithLogo(t, app, pool, creator, keep, 80)
	app.Publish(t, creator, keep)

	app.PostForm(t, creator, "/surveys/"+id+"/delete", nil).Body.Close()
	app.Clock.Advance(purge.SoftDeleteWindow + 24*time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM survey_images WHERE survey_id = $1`, id); n != 0 {
		t.Errorf("%d pictures outlived their survey", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM surveys WHERE id = $1`, id); n != 0 {
		t.Error("the survey outlived its retention window")
	}
	if n := countRows(t, pool, `SELECT count(*) FROM survey_images WHERE survey_id = $1`, keep); n != 1 {
		t.Errorf("a live survey has %d pictures after the purge, want its logo", n)
	}
}

// TestPurge_RemovesPicturesNothingShows: a picture that was replaced, so
// that no published version and not the draft shows it, is removed once
// it is a week old, and not before. One a version shows, or the draft
// has, stays whatever its age.
func TestPurge_RemovesPicturesNothingShows(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-unused-pictures"))

	id := app.CreateSurvey(t, creator, "Three logos", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	published := styleWithLogo(t, app, pool, creator, id, 40)
	app.Publish(t, creator, id)
	replaced := styleWithLogo(t, app, pool, creator, id, 80)
	drafted := styleWithLogo(t, app, pool, creator, id, 120)

	has := func(sha string) bool {
		return countRows(t, pool,
			`SELECT count(*) FROM survey_images WHERE survey_id = $1 AND encode(sha256, 'hex') = $2`, id, sha) == 1
	}
	app.Clock.Advance(purge.UnusedImageWindow - time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge before the week is out: %v", err)
	}
	if !has(replaced) {
		t.Fatal("a replaced picture was removed before it was a week old")
	}

	app.Clock.Advance(2 * time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge after a week: %v", err)
	}
	if has(replaced) {
		t.Error("a picture nothing shows outlived its week")
	}
	if !has(published) {
		t.Error("the purge removed a picture a published version shows")
	}
	if !has(drafted) {
		t.Error("the purge removed a picture the draft has")
	}
}

// styleWithThanksPicture uploads a thanks page picture of one shade and
// returns its address.
func styleWithThanksPicture(t *testing.T, app *apptest.App, pool *pgxpool.Pool, creator *http.Client, id string, shade uint8) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 32, 20))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	resp := app.PostMultipart(t, creator, "/surveys/"+id+"/style",
		url.Values{"thanks_picture": {"image"}, "thanks_alt": {"A picture"}},
		apptest.Upload{Field: "thanks_image", Name: "thanks.png", Data: buf.Bytes()})
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving a thanks picture: status %d\n%s", resp.StatusCode, page)
	}
	var sha string
	if err := pool.QueryRow(context.Background(),
		`SELECT structure->'style'->'thanks'->'image'->>'sha256' FROM survey_drafts WHERE survey_id = $1`, id).Scan(&sha); err != nil {
		t.Fatalf("read the stored thanks picture: %v", err)
	}
	return sha
}

// TestPurge_TheThanksPictureIsAPicture: the thanks page's picture is kept
// and let go by the rule every picture of a style follows: kept while a
// version or the draft shows it, removed a week after nothing does.
func TestPurge_TheThanksPictureIsAPicture(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-thanks-pictures"))

	id := app.CreateSurvey(t, creator, "Three thanks pictures", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	published := styleWithThanksPicture(t, app, pool, creator, id, 40)
	app.Publish(t, creator, id)
	replaced := styleWithThanksPicture(t, app, pool, creator, id, 80)
	drafted := styleWithThanksPicture(t, app, pool, creator, id, 120)

	has := func(sha string) bool {
		return countRows(t, pool,
			`SELECT count(*) FROM survey_images WHERE survey_id = $1 AND encode(sha256, 'hex') = $2`, id, sha) == 1
	}
	app.Clock.Advance(purge.UnusedImageWindow + time.Hour)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if has(replaced) {
		t.Error("a thanks picture nothing shows outlived its week")
	}
	if !has(published) {
		t.Error("the purge removed the thanks picture a published version shows")
	}
	if !has(drafted) {
		t.Error("the purge removed the thanks picture the draft has")
	}
}

// TestPurge_ASurveyBeingSavedKeepsItsPictures: a survey whose style is
// being saved or published at that moment is held, and the purge leaves
// its pictures for the next run, since the draft or version about to
// refer to one is not yet visible to it.
func TestPurge_ASurveyBeingSavedKeepsItsPictures(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-held-survey"))

	id := app.CreateSurvey(t, creator, "Held while saving", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	replaced := styleWithThanksPicture(t, app, pool, creator, id, 40)
	styleWithThanksPicture(t, app, pool, creator, id, 80)
	has := func() bool {
		return countRows(t, pool,
			`SELECT count(*) FROM survey_images WHERE survey_id = $1 AND encode(sha256, 'hex') = $2`, id, replaced) == 1
	}
	app.Clock.Advance(purge.UnusedImageWindow + time.Hour)

	ctx := context.Background()
	saving, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saving.Exec(ctx, `SELECT id FROM surveys WHERE id = $1 FOR NO KEY UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge while the survey is held: %v", err)
	}
	if !has() {
		t.Error("the purge removed a picture of a survey being saved")
	}
	if err := saving.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := purge.Run(ctx, pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if has() {
		t.Error("once the survey was let go, the unused picture outlived its week")
	}
}

// TestPurge_ASuspensionIsNotADeletion: a workspace an operator has
// suspended keeps everything, however long the suspension lasts. The
// purge erases what was deleted, and a suspension deletes nothing, so
// lifting it can put everything back as it was (ADR-0018).
func TestPurge_ASuspensionIsNotADeletion(t *testing.T) {
	app := purgeApp(t)
	pool := poolFor(t, app.DSN)
	creator := app.Login(t, apptest.UniqueEmail("purge-suspended"))

	id := seedAnsweredSurvey(t, app, creator, "Suspended, not deleted")
	styleWithLogo(t, app, pool, creator, id, 120)
	app.Publish(t, creator, id)
	if _, err := pool.Exec(context.Background(), `
		UPDATE workspaces SET suspended_at = $2, suspended_reason = 'A test suspension.'
		WHERE id = (SELECT workspace_id FROM surveys WHERE id = $1)`, id, app.Clock.Now()); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	app.Clock.Advance(4 * purge.SoftDeleteWindow)
	if _, err := purge.Run(context.Background(), pool, app.Clock.Now(), false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	for _, check := range []struct {
		what  string
		query string
		want  int
	}{
		{"the survey", `SELECT count(*) FROM surveys WHERE id = $1`, 1},
		{"its response", `SELECT count(*) FROM responses WHERE survey_id = $1`, 1},
		{"its versions", `SELECT count(*) FROM survey_versions WHERE survey_id = $1`, 2},
		{"its logo", `SELECT count(*) FROM survey_images WHERE survey_id = $1`, 1},
	} {
		if n := countRows(t, pool, check.query, id); n != check.want {
			t.Errorf("%s: %d rows after a long suspension, want %d", check.what, n, check.want)
		}
	}
}

package http_test

import (
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestResults_XLSXMatchesTheCSV is issue #22: the workbook opens in a
// spreadsheet library, carries the CSV's header and rows, types numbers
// as numbers, and stores a formula a respondent typed as defused text.
func TestResults_XLSXMatchesTheCSV(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("xlsx"))
	id := app.CreateSurvey(t, creator, "Workbook me", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	app.AddQuestion(t, creator, id, "rating_scale", "How likely?",
		url.Values{"scale_min": {"1"}, "scale_max": {"7"}})
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: `=cmd|'/c calc'!A1`, 1: "6"})

	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/results.csv")
	if err != nil {
		t.Fatalf("GET csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(apptest.ReadBody(t, resp))).ReadAll()
	resp.Body.Close()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}

	resp, err = creator.Get(app.Server.URL + "/surveys/" + id + "/results.xlsx")
	if err != nil {
		t.Fatalf("GET xlsx: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("xlsx status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, `filename="workbook-me-responses.xlsx"`) {
		t.Errorf("Content-Disposition = %q", got)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read xlsx: %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the workbook does not open: %v", err)
	}
	defer book.Close()
	sheet := book.GetSheetName(0)
	rows, err := book.GetRows(sheet)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}

	if len(rows) != len(records) {
		t.Fatalf("xlsx has %d rows, csv has %d", len(rows), len(records))
	}
	if !slices.Equal(rows[0], records[0]) {
		t.Errorf("header row differs:\n xlsx %q\n csv  %q", rows[0], records[0])
	}
	if !slices.Equal(rows[1], records[1]) {
		t.Errorf("response row differs:\n xlsx %q\n csv  %q", rows[1], records[1])
	}

	column := func(heading string) string {
		index := slices.Index(records[0], heading)
		if index < 0 {
			t.Fatalf("no %q column in %q", heading, records[0])
		}
		name, err := excelize.CoordinatesToCellName(index+1, 2)
		if err != nil {
			t.Fatal(err)
		}
		return name
	}
	isNumber := func(cell string) bool {
		kind, err := book.GetCellType(sheet, cell)
		if err != nil {
			t.Fatal(err)
		}
		// A numeric cell may omit its type: number is the default.
		return kind == excelize.CellTypeNumber || kind == excelize.CellTypeUnset
	}
	for _, heading := range []string{"version", "How likely?"} {
		if cell := column(heading); !isNumber(cell) {
			t.Errorf("%s (%s) is not a numeric cell", heading, cell)
		}
	}

	injected := column("What happened?")
	if isNumber(injected) {
		t.Errorf("the free-text answer was written as a number")
	}
	if formula, _ := book.GetCellFormula(sheet, injected); formula != "" {
		t.Errorf("the free-text answer became a formula: %q", formula)
	}
	if value, _ := book.GetCellValue(sheet, injected); value != `'=cmd|'/c calc'!A1` {
		t.Errorf("the free-text answer lost its defusing apostrophe: %q", value)
	}
}

// TestResults_XLSXIsWorkspaceScoped: the workbook answers to the same
// authorization as the CSV.
func TestResults_XLSXIsWorkspaceScoped(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("xlsxowner"))
	stranger := app.Login(t, apptest.UniqueEmail("xlsxstranger"))
	id := app.CreateSurvey(t, owner, "Private workbook", true)
	app.AddQuestion(t, owner, id, "long_text", "Secret question", nil)
	app.Publish(t, owner, id)

	resp, err := stranger.Get(app.Server.URL + "/surveys/" + id + "/results.xlsx")
	if err != nil {
		t.Fatalf("GET xlsx: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if bodyContains(body, "Secret question") {
		t.Errorf("the workbook leaked another workspace's survey")
	}
}

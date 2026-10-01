package http

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/TryEarful/earful/internal/store"
)

// Spreadsheet export (issue #22): the CSV's columns and rows as an .xlsx
// workbook, for people whose spreadsheet does not import CSV cleanly.
// Both read resultsTable, so the two downloads always agree.

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// xlsxSheet names the one worksheet. It is not translated: the column
// headings are the CSV's, which are not translated either.
const xlsxSheet = "Responses"

func writeResultsXLSX(buf *bytes.Buffer, survey store.Survey, results store.Results) error {
	file := excelize.NewFile()
	defer file.Close()
	if err := file.SetSheetName("Sheet1", xlsxSheet); err != nil {
		return err
	}
	stream, err := file.NewStreamWriter(xlsxSheet)
	if err != nil {
		return err
	}

	header, rows := resultsTable(survey, results)
	values := make([]any, len(header))
	for i, heading := range header {
		values[i] = heading
	}
	if err := stream.SetRow("A1", values); err != nil {
		return err
	}
	for r, row := range rows {
		values := make([]any, len(row))
		for i, cell := range row {
			// A string written this way is stored as text and never
			// evaluated; the csvSafe prefix stays so a value copied out of
			// the sheet into another tool is as defused as the CSV's.
			if cell.Number != nil {
				values[i] = *cell.Number
			} else {
				values[i] = cell.Text
			}
		}
		axis, err := excelize.CoordinatesToCellName(1, r+2)
		if err != nil {
			return err
		}
		if err := stream.SetRow(axis, values); err != nil {
			return err
		}
	}
	if err := stream.Flush(); err != nil {
		return err
	}
	_, err = file.WriteTo(buf)
	return err
}

// resultsXLSX serves the same table as resultsCSV, as a workbook. It is
// built in memory before anything is sent, so a failure is a proper
// error page rather than a truncated file.
func (s *server) resultsXLSX(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results", err)
		return
	}
	var buf bytes.Buffer
	if err := writeResultsXLSX(&buf, survey, results); err != nil {
		s.internalError(w, r, "write results xlsx", err)
		return
	}

	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+strings.TrimSuffix(csvFilename(survey.Title), ".csv")+`.xlsx"`)
	_, _ = w.Write(buf.Bytes())
}

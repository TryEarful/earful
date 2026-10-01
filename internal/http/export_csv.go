package http

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/TryEarful/earful/internal/store"
)

// CSV export (M7-T2). One row per response; one column per Question
// Identity, so a survey that reworded a question still has one column for
// it and the version column says which wording that row saw.

// resultsCell is one cell of a results export. Every format writes Text;
// a format with typed cells writes Number instead when it is set, so a
// spreadsheet can sum the column. Text has already been through csvSafe
// wherever a respondent or a creator wrote it.
type resultsCell struct {
	Text   string
	Number *int
}

func textCell(value string) resultsCell { return resultsCell{Text: value} }

func numberCell(value int) resultsCell {
	return resultsCell{Text: strconv.Itoa(value), Number: &value}
}

// resultsTable builds the header and rows every results export writes.
// The CSV and the spreadsheet both read it, so their columns cannot
// drift apart.
func resultsTable(survey store.Survey, results store.Results) (header []string, rows [][]resultsCell) {
	header = []string{"response_id", "version", "submitted_at", "duration_secs"}
	if !survey.IsAnonymous {
		// Anonymous surveys have no such column to export — the schema
		// itself has nowhere to put one (ADR-0003).
		header = append(header, "participant_email")
	}
	for _, question := range results.Questions {
		header = append(header, csvSafe(question.Text))
	}

	for _, response := range results.Responses {
		row := []resultsCell{
			textCell(response.ID.String()),
			numberCell(response.VersionNumber),
			textCell(response.SubmittedAt.UTC().Format("2006-01-02T15:04:05Z")),
			textCell(""),
		}
		if response.DurationSecs != nil {
			row[3] = numberCell(*response.DurationSecs)
		}
		if !survey.IsAnonymous {
			email := ""
			if response.ParticipantEmail != nil {
				email = *response.ParticipantEmail
			}
			row = append(row, textCell(csvSafe(email)))
		}
		for _, question := range results.Questions {
			// A missing entry means the question was skipped, or did not
			// exist in that version: an empty cell either way, and the
			// version column tells them apart.
			answer := response.Answers[question.IdentityID]
			display := answer.Display()
			cell := textCell(csvSafe(display))
			if answer.Number != nil && display == strconv.Itoa(*answer.Number) {
				// The CSV keeps the defused text: a negative scale value
				// leads with a minus sign, which the CSV has always
				// prefixed. A numeric cell is never evaluated.
				cell.Number = answer.Number
			}
			row = append(row, cell)
		}
		rows = append(rows, row)
	}
	return header, rows
}

func writeResultsCSV(out io.Writer, survey store.Survey, results store.Results) error {
	writer := csv.NewWriter(out)
	defer writer.Flush()

	header, rows := resultsTable(survey, results)
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		record := make([]string, len(row))
		for i, cell := range row {
			record[i] = cell.Text
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	return writer.Error()
}

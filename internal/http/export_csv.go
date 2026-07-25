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

func writeResultsCSV(out io.Writer, survey store.Survey, results store.Results) error {
	writer := csv.NewWriter(out)
	defer writer.Flush()

	header := []string{"response_id", "version", "submitted_at", "duration_secs"}
	if !survey.IsAnonymous {
		// Anonymous surveys have no such column to export — the schema
		// itself has nowhere to put one (ADR-0003).
		header = append(header, "participant_email")
	}
	for _, question := range results.Questions {
		header = append(header, csvSafe(question.Text))
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	for _, response := range results.Responses {
		row := []string{
			response.ID.String(),
			strconv.Itoa(response.VersionNumber),
			response.SubmittedAt.UTC().Format("2006-01-02T15:04:05Z"),
			"",
		}
		if response.DurationSecs != nil {
			row[3] = strconv.Itoa(*response.DurationSecs)
		}
		if !survey.IsAnonymous {
			email := ""
			if response.ParticipantEmail != nil {
				email = *response.ParticipantEmail
			}
			row = append(row, csvSafe(email))
		}
		for _, question := range results.Questions {
			// A missing entry means the question was skipped, or did not
			// exist in that version: an empty cell either way, and the
			// version column tells them apart.
			row = append(row, csvSafe(response.Answers[question.IdentityID].Display()))
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

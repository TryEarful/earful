package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/export"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// Workspace export (M7-T3). One button, an archive built in the
// background, a link that expires. The archive is everything the
// workspace holds, in a documented format (docs/export-format.md) that
// an importer can be written against — which is what makes "you can
// leave" a fact rather than a slogan.

const (
	// exportTTL is how long a built archive stays downloadable. Long
	// enough to notice the email-less "it's ready" on the account page,
	// short enough that a copy of a whole workspace does not sit around.
	exportTTL = 24 * time.Hour
	// exportMaxBytes caps one archive. Past this the job fails with an
	// explanation rather than trying to push a hundred megabytes through
	// a database row (ADR-0010's stated trade-off).
	exportMaxBytes = 64 << 20
	// exportStaleAfter is when an unfinished job is assumed dead, so a
	// crashed build does not block every later request.
	exportStaleAfter = 15 * time.Minute
)

// accountExport queues a build unless one is already running.
func (s *server) accountExport(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	now := s.clock.Now()

	if latest, err := s.surveys.LatestExportJob(r.Context(), info.WorkspaceID); err == nil {
		if latest.InProgress() && now.Sub(latest.CreatedAt) < exportStaleAfter {
			http.Redirect(w, r, "/account?notice=export_running", http.StatusSeeOther)
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, "read latest export", err)
		return
	}

	job, err := s.surveys.CreateExportJob(r.Context(), info.WorkspaceID, info.UserID, now)
	if err != nil {
		s.internalError(w, r, "create export job", err)
		return
	}
	s.startExport(job, info.WorkspaceID, info.WorkspaceName)
	http.Redirect(w, r, "/account?notice=export_started", http.StatusSeeOther)
}

// startExport builds the archive off the request. The work is bounded
// and the context is its own: a creator closing the tab must not cancel
// an export halfway through.
func (s *server) startExport(job store.ExportJob, workspaceID uuid.UUID, workspaceName string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		claimed, err := s.surveys.ClaimExportJob(ctx, job.ID)
		if err != nil || !claimed {
			// Another instance is building it, which is the point of
			// claiming rather than assuming.
			return
		}
		archive, err := s.buildWorkspaceArchive(ctx, workspaceID, workspaceName)
		now := s.clock.Now()
		if err != nil {
			s.logger.Error("workspace export failed", "error", err)
			message := "Something went wrong building the export. Try again, or ask support."
			if errors.Is(err, errExportTooLarge) {
				message = "This workspace is too large to export in one archive. " +
					"Export individual surveys as CSV, or ask support for a copy."
			}
			if failErr := s.surveys.FailExportJob(ctx, job.ID, message, now); failErr != nil {
				s.logger.Error("marking export failed did not work", "error", failErr)
			}
			return
		}
		if err := s.surveys.FinishExportJob(ctx, job.ID, archive, now, now.Add(exportTTL)); err != nil {
			s.logger.Error("storing export archive failed", "error", err)
		}
	}()
}

var errExportTooLarge = errors.New("export: archive exceeds the size limit")

// buildWorkspaceArchive reads the whole workspace and zips it.
func (s *server) buildWorkspaceArchive(ctx context.Context, workspaceID uuid.UUID, workspaceName string) ([]byte, error) {
	surveys, err := s.surveys.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	archive := export.Archive{
		FormatVersion: export.FormatVersion,
		ExportedAt:    s.clock.Now().UTC(),
		Workspace:     export.Workspace{ID: workspaceID.String(), Name: workspaceName},
	}
	var csvs []export.CSVFile

	for _, survey := range surveys {
		exported := export.Survey{
			ID:          survey.ID.String(),
			Title:       survey.Title,
			IsAnonymous: survey.IsAnonymous,
			Status:      string(survey.StatusAt(s.clock.Now())),
			CreatedAt:   survey.CreatedAt.UTC(),
			CloseAt:     survey.CloseAt,
			ClosedAt:    survey.ClosedAt,
		}

		versions, err := s.surveys.Versions(ctx, survey.ID)
		if err != nil {
			return nil, err
		}
		for _, version := range versions {
			questions, err := s.surveys.QuestionsForVersion(ctx, version.ID)
			if err != nil {
				return nil, err
			}
			exportedVersion := export.Version{
				Number:      version.Number,
				PublishedAt: version.PublishedAt.UTC(),
			}
			for i, question := range questions {
				min, max := question.Scale()
				exportedQuestion := export.Question{
					IdentityID: question.IdentityID,
					Position:   i + 1,
					Type:       string(question.Type),
					Text:       question.Text,
					Options:    question.Options,
					Required:   question.Required,
				}
				if question.Type.NeedsScale() || question.Type == domain.NPS {
					exportedQuestion.ScaleMin, exportedQuestion.ScaleMax = min, max
				}
				exportedVersion.Questions = append(exportedVersion.Questions, exportedQuestion)
			}
			exported.Versions = append(exported.Versions, exportedVersion)
		}

		if !survey.IsAnonymous {
			participants, err := s.surveys.Participants(ctx, survey.ID)
			if err != nil {
				return nil, err
			}
			for _, participant := range participants {
				exported.Participants = append(exported.Participants, export.Participant{
					Email:       participant.Email,
					InvitedAt:   participant.InvitedAt,
					SubmittedAt: participant.SubmittedAt,
					BouncedAt:   participant.BouncedAt,
				})
			}
		}

		results, err := s.surveys.SurveyResults(ctx, survey.ID)
		if err != nil {
			return nil, err
		}
		for _, response := range results.Responses {
			exportedResponse := export.Response{
				ID:               response.ID.String(),
				Version:          response.VersionNumber,
				SubmittedAt:      response.SubmittedAt.UTC(),
				DurationSecs:     response.DurationSecs,
				ParticipantEmail: response.ParticipantEmail,
				Answers:          map[string]export.Answer{},
			}
			for identity, value := range response.Answers {
				exportedResponse.Answers[identity] = export.Answer{
					Text: value.Text, Choice: value.Choice, Choices: value.Choices,
					Number: value.Number, Bool: value.Bool,
				}
			}
			exported.Responses = append(exported.Responses, exportedResponse)
		}

		stats, err := s.surveys.SurveyStats(ctx, survey.ID)
		if err != nil {
			return nil, err
		}
		for _, stat := range stats {
			exported.Stats = append(exported.Stats, export.Stat{
				Metric: stat.Metric, Bucket: stat.Bucket, Count: stat.Count,
			})
		}

		// The stored Insight Summary, if there is one, travels with its
		// label attached (story 53 and M10-T2).
		if run, err := s.surveys.LatestInsightRun(ctx, survey.ID); err == nil && run.Output != "" {
			exported.Insights = append(exported.Insights, export.Insight{
				Model:         run.Model,
				GeneratedAt:   run.CreatedAt.UTC(),
				ResponseCount: run.ResponseCount,
				Output:        run.Output,
				Note:          export.InsightNote,
			})
			csvs = append(csvs, export.CSVFile{
				Name: strings.TrimSuffix(exportCSVName(survey), ".csv") + ".insight.txt",
				Content: []byte(fmt.Sprintf("%s\nModel: %s\nGenerated: %s\nResponses read: %d\n\n%s\n",
					export.InsightNote, run.Model, run.CreatedAt.UTC().Format(time.RFC3339),
					run.ResponseCount, run.Output)),
			})
		}

		// The same CSV the survey's own download produces: one format,
		// not two.
		var csv bytes.Buffer
		if err := writeResultsCSV(&csv, survey, results); err != nil {
			return nil, err
		}
		csvs = append(csvs, export.CSVFile{
			Name:    exportCSVName(survey),
			Content: csv.Bytes(),
		})

		archive.Surveys = append(archive.Surveys, exported)
	}

	built, err := export.Build(archive, csvs)
	if err != nil {
		return nil, err
	}
	if len(built) > exportMaxBytes {
		return nil, errExportTooLarge
	}
	return built, nil
}

// exportCSVName keeps files recognisable without letting a survey title
// choose a path: the id is what guarantees uniqueness.
func exportCSVName(survey store.Survey) string {
	slug := strings.TrimSuffix(csvFilename(survey.Title), "-responses.csv")
	if slug == "survey" {
		slug = "untitled"
	}
	return fmt.Sprintf("%s-%s.csv", slug, survey.ID.String()[:8])
}

// exportDownload serves a built archive. A session in the owning
// workspace is required, so the link is not a bearer capability — and it
// expires regardless.
func (s *server) exportDownload(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	jobID, err := uuid.Parse(r.PathValue("jobID"))
	if err != nil {
		render(w, r, http.StatusNotFound, templates.ErrorPage("Export not found",
			"This download link has expired or doesn't belong to your workspace. Start a new export from your account page."))
		return
	}
	archive, err := s.surveys.ExportArchive(r.Context(), jobID, info.WorkspaceID, s.clock.Now())
	if errors.Is(err, store.ErrNotFound) {
		render(w, r, http.StatusNotFound, templates.ErrorPage("Export not found",
			"This download link has expired or doesn't belong to your workspace. Start a new export from your account page."))
		return
	}
	if err != nil {
		s.internalError(w, r, "read export archive", err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="earful-workspace-export.zip"`)
	if _, err := w.Write(archive); err != nil {
		s.logger.Debug("export download interrupted", "error", err)
	}
}

// viewExportJob is what the account page shows about the latest export.
func viewExportJob(job store.ExportJob, now time.Time) templates.ExportView {
	view := templates.ExportView{
		Status:    job.Status,
		Building:  job.InProgress(),
		Failed:    job.Status == store.ExportFailed,
		Error:     job.Error,
		SizeLabel: humanBytes(job.SizeBytes),
	}
	if job.FinishedAt != nil {
		view.FinishedAt = job.FinishedAt.Format(dateTimeLayout)
	}
	if job.Downloadable(now) {
		view.Ready = true
		view.ExpiresAt = job.ExpiresAt.Format(dateTimeLayout)
		view.DownloadPath = "/exports/" + job.ID.String()
	}
	return view
}

func humanBytes(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1<<20:
		return fmt.Sprintf("%d KB", size/1024+1)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	}
}

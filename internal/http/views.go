package http

import (
	"github.com/TryEarful/earful/internal/uitext"
	"sort"
	"time"

	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// Formatting for creator-facing pages. Dates are rendered in one place so
// every screen agrees.
const (
	dayLayout      = "2 January 2006"
	dateTimeLayout = "2 Jan 2006, 15:04"
)

func viewSurvey(s store.Survey, now time.Time) templates.SurveyView {
	v := templates.SurveyView{
		ID:             s.ID.String(),
		Title:          s.Title,
		Status:         s.StatusAt(now),
		IsAnonymous:    s.IsAnonymous,
		ManuallyClosed: s.ClosedAt != nil,
		LatestVersion:  s.LatestVersion,
		QuestionCount:  s.QuestionCount,
		CreatedAt:      s.CreatedAt.Format(dayLayout),
	}
	if s.CloseAt != nil {
		// Stored as the exclusive end of the closing day; show the day
		// itself, which is what the creator entered.
		day := s.CloseAt.Add(-time.Second)
		v.CloseAtInput = day.Format(closeDateLayout)
		v.CloseAtLabel = day.Format(dayLayout)
	}
	return v
}

func viewSurveys(surveys []store.Survey, now time.Time) []templates.SurveyView {
	out := make([]templates.SurveyView, 0, len(surveys))
	for _, s := range surveys {
		out = append(out, viewSurvey(s, now))
	}
	return out
}

func viewVersions(versions []store.Version) []templates.VersionView {
	out := make([]templates.VersionView, 0, len(versions))
	for _, v := range versions {
		out = append(out, templates.VersionView{
			Number:      v.Number,
			PublishedAt: v.PublishedAt.Format(dateTimeLayout),
			PublishedBy: v.PublishedBy,
		})
	}
	return out
}

// auditEntries derives the Audit Log (M3-T4) by merging draft saves and
// publishes into one reverse-chronological trail — the two halves of "who
// changed what".
func auditEntries(l uitext.Localizer, revisions []store.Revision, versions []store.Version) []templates.AuditEntry {
	type dated struct {
		at    time.Time
		entry templates.AuditEntry
	}
	all := make([]dated, 0, len(revisions)+len(versions))

	for _, r := range revisions {
		all = append(all, dated{at: r.SavedAt, entry: templates.AuditEntry{
			When: r.SavedAt.Format(dateTimeLayout),
			Who:  orUnknown(l, r.SavedBy),
			What: l.N("audit.entry.saved", r.QuestionCount),
		}})
	}
	for _, v := range versions {
		all = append(all, dated{at: v.PublishedAt, entry: templates.AuditEntry{
			When:    v.PublishedAt.Format(dateTimeLayout),
			Who:     orUnknown(l, v.PublishedBy),
			What:    l.T("audit.entry.published", uitext.Args{"Version": v.Number}),
			Publish: true,
		}})
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	out := make([]templates.AuditEntry, 0, len(all))
	for _, d := range all {
		out = append(out, d.entry)
	}
	return out
}

// orUnknown covers rows whose author was purged (M8): the trail keeps the
// event even when the person is gone.
func orUnknown(l uitext.Localizer, who string) string {
	if who == "" {
		return l.T("audit.entry.nobody")
	}
	return who
}

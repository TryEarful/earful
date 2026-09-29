package http

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/TryEarful/earful/internal/audience"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// The words for stored values that only a handler shows. The ones a
// template shows too are in web/templates/names.go.

// languageNames are the messages for the languages the domain has a
// name for. domain.LanguageName stays English: it is what a model is
// told to translate into, and a model is addressed in English.
var languageNames = map[string]uitext.ID{
	"ar":    "language.ar.name",
	"cs":    "language.cs.name",
	"da":    "language.da.name",
	"de":    "language.de.name",
	"el":    "language.el.name",
	"en":    "language.en.name",
	"es":    "language.es.name",
	"fi":    "language.fi.name",
	"fr":    "language.fr.name",
	"he":    "language.he.name",
	"hi":    "language.hi.name",
	"hu":    "language.hu.name",
	"id":    "language.id.name",
	"it":    "language.it.name",
	"ja":    "language.ja.name",
	"ko":    "language.ko.name",
	"nb":    "language.nb.name",
	"nl":    "language.nl.name",
	"pl":    "language.pl.name",
	"pt":    "language.pt.name",
	"pt-br": "language.pt_br.name",
	"ro":    "language.ro.name",
	"ru":    "language.ru.name",
	"sv":    "language.sv.name",
	"tr":    "language.tr.name",
	"uk":    "language.uk.name",
	"vi":    "language.vi.name",
	"zh":    "language.zh.name",
}

// languageName is the name of a language as a sentence has it: "Dutch",
// "neerlandés". A language with no message is called what the domain
// calls it, which for one it has no name for is its code.
func languageName(l uitext.Localizer, code string) string {
	code = domain.NormalizeLang(code)
	if id, ok := languageNames[strings.ToLower(code)]; ok {
		return l.T(id)
	}
	return domain.LanguageName(code)
}

// languageLabel is the name of a language standing on its own, as a
// heading or an entry in a list, where it begins with a capital in
// languages that write their names without one.
func languageLabel(l uitext.Localizer, code string) string {
	return capital(languageName(l, code))
}

func capital(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(first)) + s[size:]
}

// participantStatus is the word for where a participant stands.
func participantStatus(l uitext.Localizer, status string) string {
	return templates.Named(l, participantStatusName(status), status)
}

func participantStatusName(status string) uitext.ID {
	switch status {
	case store.ParticipantSubmitted:
		return "participant.status.submitted"
	case store.ParticipantBounced:
		return "participant.status.bounced"
	case store.ParticipantSuppressed:
		return "participant.status.suppressed"
	case store.ParticipantInvited:
		return "participant.status.invited"
	case store.ParticipantPending:
		return "participant.status.pending"
	}
	return ""
}

// audienceGroup is the word for a group of the audience. A browser is
// called by its name in every language; what is worded is the kind of
// device, and the group for whatever was not recognised.
func audienceGroup(l uitext.Localizer, metric, bucket string) string {
	return templates.Named(l, audienceGroupName(metric, bucket), bucket)
}

func audienceGroupName(metric, bucket string) uitext.ID {
	switch {
	case bucket == audience.BrowserOther && (metric == store.MetricBrowser || metric == store.MetricDevice):
		return "audience.group.unknown"
	case metric == store.MetricDevice && bucket == audience.DevicePhone:
		return "audience.device.phone"
	case metric == store.MetricDevice && bucket == audience.DeviceTablet:
		return "audience.device.tablet"
	case metric == store.MetricDevice && bucket == audience.DeviceDesktop:
		return "audience.device.desktop"
	}
	return ""
}

// displayAnswer is an answer as a reader of the results sees it. An
// answer of yes or no is stored as true or false, and is a word only
// when it is shown; every other answer is what the respondent gave.
func displayAnswer(l uitext.Localizer, value domain.AnswerValue) string {
	if value.Bool != nil && value.Text == "" && value.Choice == "" && len(value.Choices) == 0 && value.Number == nil {
		if *value.Bool {
			return l.T("answer.yes")
		}
		return l.T("answer.no")
	}
	return value.Display()
}

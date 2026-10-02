// Package export builds a workspace archive: the "leave anytime"
// promise in SPEC.md's solution statement, and the thing that makes the
// AGPL self-hosting story real rather than theoretical.
//
// The format is documented in docs/export-format.md and versioned, and
// that document is the contract an importer will be written against
// (story 76). Two rules follow from that:
//
//   - Nothing here is lossy on purpose. Every version's questions, every
//     response pinned to the version it answered, participants for
//     invited surveys, and the survey-level counters all travel.
//   - Nothing here invents structure the product does not have. The JSON
//     mirrors the domain, so a reader who understands Earful understands
//     the file.
package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// FormatVersion is the contract. Bump it when the shape changes in a way
// an importer would notice, and say what changed in docs/export-format.md.
const FormatVersion = 7

// Archive is the whole export, as it appears in workspace.json.
type Archive struct {
	FormatVersion int       `json:"format_version"`
	ExportedAt    time.Time `json:"exported_at"`
	Workspace     Workspace `json:"workspace"`
	Surveys       []Survey  `json:"surveys"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Survey struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	IsAnonymous  bool          `json:"is_anonymous"`
	Status       string        `json:"status"`
	CreatedAt    time.Time     `json:"created_at"`
	CloseAt      *time.Time    `json:"close_at,omitempty"`
	ClosedAt     *time.Time    `json:"closed_at,omitempty"`
	Versions     []Version     `json:"versions"`
	Participants []Participant `json:"participants,omitempty"`
	Responses    []Response    `json:"responses"`
	Stats        []Stat        `json:"stats,omitempty"`
	// StatsDaily are the dated flow counters (ADR-0012): the same kind
	// of thing as Stats, with a day. Format version 2.
	StatsDaily []DailyStat `json:"stats_daily,omitempty"`
	// Insights are AI readings of the answers, exported with the label
	// they carry in the product: a model and a time, never presented as
	// data (story 53).
	Insights []Insight `json:"insights,omitempty"`
}

// Insight is one stored Insight Summary.
type Insight struct {
	Model         string    `json:"model"`
	GeneratedAt   time.Time `json:"generated_at"`
	ResponseCount int       `json:"response_count"`
	Output        string    `json:"output"`
	// Note is spelled out in the file itself so a reader who opens only
	// this object still knows what they are reading.
	Note string `json:"note"`
}

// InsightNote is the label every exported summary carries.
const InsightNote = "AI-generated summary of the responses, not the responses themselves."

type Version struct {
	Number      int        `json:"number"`
	PublishedAt time.Time  `json:"published_at"`
	Questions   []Question `json:"questions"`
	// Thanks is the creator's thank you page as this version was
	// published with it, absent where the default was shown. Format
	// version 4.
	Thanks *Thanks `json:"thanks,omitempty"`
	// Style is how this version's pages looked to the people answering
	// it, absent where it had Earful's own look. Format version 7.
	Style *Style `json:"style,omitempty"`
}

// Style is a version's style (ADR-0018). Theme names the theme its
// pages were drawn in; Header and Footer are what stood above and below
// the survey, in the creator's wording; Thanks is the thanks page's
// picture. Each part is absent when it was
// not set.
type Style struct {
	Theme  string       `json:"theme,omitempty"`
	Header *StyleHeader `json:"header,omitempty"`
	Footer *StyleFooter `json:"footer,omitempty"`
	Thanks *StyleThanks `json:"thanks,omitempty"`
}

// StyleThanks is the picture the thanks page showed above its heading,
// absent where it showed Earful's owl. Picture names it: "check",
// "envelope" or "confetti" for one of the drawings Earful offers, "none"
// for no picture, or "image" for the creator's own, which Image then
// names under images/ with its alternative text.
type StyleThanks struct {
	Picture string      `json:"picture"`
	Image   *StyleImage `json:"image,omitempty"`
}

// StyleHeader is the head of a respondent's page: whose survey it was.
type StyleHeader struct {
	Name    string      `json:"name,omitempty"`
	Tagline string      `json:"tagline,omitempty"`
	Links   []StyleLink `json:"links,omitempty"`
	// Banner is the strip across the head of the page, and Logo the
	// survey's own mark. Each names a file under images/ in the archive.
	Banner *StyleImage `json:"banner,omitempty"`
	Logo   *StyleImage `json:"logo,omitempty"`
}

// StyleImage is a picture of a style: the file in the archive that
// holds it, its size in pixels, and for a logo the alternative text a
// respondent who could not see it was given.
type StyleImage struct {
	File   string `json:"file"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Alt    string `json:"alt,omitempty"`
}

// ImagePath is where a picture sits in the archive: under images/, named
// by the hash of its bytes, which is what a style refers to it by. A
// picture several versions show is one file.
func ImagePath(sha256, ext string) string { return "images/" + sha256 + ext }

// StyleFooter is the creator's own footer.
type StyleFooter struct {
	Text  string      `json:"text,omitempty"`
	Links []StyleLink `json:"links,omitempty"`
}

// StyleLink is a link in a header or a footer: always a label and an
// address together.
type StyleLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Thanks is what a respondent read after sending their answers, in the
// creator's wording. Each part is absent when it was not set.
type Thanks struct {
	Message   string `json:"message,omitempty"`
	LinkLabel string `json:"link_label,omitempty"`
	LinkURL   string `json:"link_url,omitempty"`
}

type Question struct {
	// IdentityID is the Question Identity: the key that makes results
	// comparable across versions, and the key an importer must preserve.
	IdentityID string   `json:"identity_id"`
	Position   int      `json:"position"`
	Type       string   `json:"type"`
	Text       string   `json:"text"`
	Options    []string `json:"options,omitempty"`
	Required   bool     `json:"required"`
	ScaleMin   int      `json:"scale_min,omitempty"`
	ScaleMax   int      `json:"scale_max,omitempty"`
	// AllowOther is whether the question offered Other with a box to
	// write in, absent where it did not. Format version 6.
	AllowOther bool `json:"allow_other,omitempty"`
}

type Participant struct {
	Email       string     `json:"email"`
	InvitedAt   *time.Time `json:"invited_at,omitempty"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	BouncedAt   *time.Time `json:"bounced_at,omitempty"`
}

type Response struct {
	ID          string    `json:"id"`
	Version     int       `json:"version"`
	SubmittedAt time.Time `json:"submitted_at"`
	// DurationSecs is the only per-response metadata that exists
	// (ADR-0009). There is no IP and no user agent to export, in any
	// survey, because no such column exists.
	DurationSecs *int `json:"duration_secs,omitempty"`
	// ParticipantEmail appears for invited surveys only.
	ParticipantEmail *string `json:"participant_email,omitempty"`
	// Answers are keyed by Question Identity.
	Answers map[string]Answer `json:"answers"`
}

// Answer mirrors domain.AnswerValue: exactly one of these is set,
// according to the question's type.
type Answer struct {
	Text    string   `json:"text,omitempty"`
	Choice  string   `json:"choice,omitempty"`
	Choices []string `json:"choices,omitempty"`
	Number  *int     `json:"number,omitempty"`
	Bool    *bool    `json:"bool,omitempty"`
	// Date is a calendar day, yyyy-mm-dd, with no time or zone.
	Date string `json:"date,omitempty"`
	// Other is what the respondent wrote beside Other, with Choice set
	// to, or Choices holding, the marker domain.OtherChoice. Format
	// version 6.
	Other string `json:"other,omitempty"`
}

// Stat is one unlinked survey-level counter (ADR-0009). It travels so an
// export is complete, and it carries no more meaning here than it does
// in the product: a count, never a person.
type Stat struct {
	Metric string `json:"metric"`
	Bucket string `json:"bucket,omitempty"`
	Count  int    `json:"count"`
}

// DailyStat is one dated survey-level counter (ADR-0012). Only the flow
// metrics carry a day; the audience metrics in Stats never do.
type DailyStat struct {
	Metric string `json:"metric"`
	Bucket string `json:"bucket,omitempty"`
	Day    string `json:"day"`
	Count  int    `json:"count"`
}

// CSVFile is a per-survey responses table, generated by the caller with
// the same writer the single-survey export uses — one CSV format, not
// two.
type CSVFile struct {
	Name    string
	Content []byte
}

// ErrTooLarge stops an archive that has grown past its limit.
var ErrTooLarge = errors.New("export: archive exceeds the size limit")

// Writer writes the zip as the workspace is read. The pictures the
// surveys' styles show go in as each survey's are read, so a workspace's
// pictures are never all held at once, and the archive stops as soon as
// it passes its limit rather than once it has all been built.
type Writer struct {
	buf    bytes.Buffer
	zw     *zip.Writer
	limit  int
	images map[string]bool
}

// NewWriter starts an archive of at most limit bytes.
func NewWriter(limit int) *Writer {
	w := &Writer{limit: limit, images: map[string]bool{}}
	w.zw = zip.NewWriter(&w.buf)
	return w
}

// Image adds a picture at its ImagePath, once however many versions show
// it.
func (w *Writer) Image(path string, content []byte) error {
	if w.images[path] {
		return nil
	}
	w.images[path] = true
	// A PNG or a JPEG is compressed already: deflating it again costs the
	// time and saves next to nothing, so it is stored as it is.
	return w.write(path, content, zip.Store)
}

// Finish writes workspace.json, a CSV per survey and a README that tells
// a human what they are holding, and returns the archive.
func (w *Writer) Finish(archive Archive, csvs []CSVFile) ([]byte, error) {
	document, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export: encode workspace.json: %w", err)
	}
	if err := w.write("workspace.json", document, zip.Deflate); err != nil {
		return nil, err
	}
	for _, csv := range csvs {
		if err := w.write("surveys/"+csv.Name, csv.Content, zip.Deflate); err != nil {
			return nil, err
		}
	}
	if err := w.write("README.txt", []byte(readme(archive)), zip.Deflate); err != nil {
		return nil, err
	}
	if err := w.zw.Close(); err != nil {
		return nil, fmt.Errorf("export: close archive: %w", err)
	}
	if w.buf.Len() > w.limit {
		return nil, ErrTooLarge
	}
	return w.buf.Bytes(), nil
}

func (w *Writer) write(name string, content []byte, method uint16) error {
	if err := writeFile(w.zw, name, content, method); err != nil {
		return err
	}
	if err := w.zw.Flush(); err != nil {
		return fmt.Errorf("export: write %s: %w", name, err)
	}
	if w.buf.Len() > w.limit {
		return ErrTooLarge
	}
	return nil
}

func writeFile(zw *zip.Writer, name string, content []byte, method uint16) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		return fmt.Errorf("export: create %s: %w", name, err)
	}
	if _, err := w.Write(content); err != nil {
		return fmt.Errorf("export: write %s: %w", name, err)
	}
	return nil
}

func readme(archive Archive) string {
	return fmt.Sprintf(`Earful workspace export
=======================

Workspace: %s
Exported:  %s
Format:    version %d

What's here
-----------

workspace.json   Everything, in one documented JSON document: every
                 survey, every published version with its questions,
                 every response pinned to the version it answered, and
                 participants for invited surveys.

surveys/         One CSV per survey — the same file the survey's own
                 "Download CSV" button produces. Convenient for
                 spreadsheets; workspace.json is the complete record.
                 Where a survey has an AI Insight Summary, it sits
                 beside its CSV as a .insight.txt file, labelled with
                 the model that wrote it. It is analysis, not data.

images/          The logos and banners that surveys showed, as they were
                 served. workspace.json names the file of each beside the
                 version that showed it. Absent where no survey had one.

What's deliberately absent
--------------------------

Anonymous responses carry no email address, no IP address and no device
details, because no such column exists anywhere near a response. There
is nothing here that was withheld: this is everything Earful holds.

Moving this somewhere else
--------------------------

The format is a stable, versioned contract, documented at
docs/export-format.md in the Earful repository. Question identities are
the key to preserve: they are what keeps results comparable when a
question is reworded, and an importer that discards them will silently
split one question's history in two.

Earful is AGPL-3.0. You can run it yourself: https://github.com/TryEarful/earful
`, archive.Workspace.Name, archive.ExportedAt.UTC().Format(time.RFC3339), archive.FormatVersion)
}

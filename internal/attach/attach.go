// Package attach turns files a creator uploads with an AI prompt into
// attachments a model can read (issue #5).
//
// A file is identified by its content, never by its name alone: the
// name must be on the allowlist and the content must be what the name
// says. Each format is either forwarded as it is, because the models
// read it natively, or converted to text here, because they do not:
//
//	pdf, png, jpg, jpeg   forwarded as uploaded
//	txt, md, csv          forwarded as text
//	html, svg             reduced to their text; an SVG is never
//	                      forwarded as an image, since it is a document
//	                      that can carry script and links
//	docx, pptx, xlsx      text read from the Office Open XML parts
//	odt, odp              text read from content.xml
//	zip                   unpacked one level, each member treated as
//	                      its own file under the same allowlist
//	doc, ppt, xls         refused: no extractor in pure Go reads the
//	                      legacy binary formats well enough to be
//	                      worth the dependency, and every version of
//	                      Office and LibreOffice saves the modern ones
//
// Everything happens in memory. Nothing here writes a file, and nothing
// here logs a file's name or content: what a creator uploads lives for
// one request and is forwarded to the model only.
package attach

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/TryEarful/earful/internal/ai"
)

// Limits bound what one request may carry. Sizes are of the content as
// read, after decompression, so an archive cannot hide its size.
type Limits struct {
	// MaxFiles is how many attachments one request may produce, counting
	// each member of an archive.
	MaxFiles int
	// MaxFileBytes caps one file, one archive member and the text read
	// out of one document.
	MaxFileBytes int64
	// MaxTotalBytes caps everything one request carries, together.
	MaxTotalBytes int64
	// MaxRatio is the most an archive member may expand over its
	// compressed size. Ordinary text compresses well under 20 to 1;
	// a member past this is a decompression bomb.
	MaxRatio int64
	// MaxDocumentXMLBytes caps the XML read out of one Office or
	// OpenDocument file. Those formats are verbose, so the budget is
	// larger than MaxFileBytes; it still bounds the work one upload
	// can cause.
	MaxDocumentXMLBytes int64
}

// Defaults are the limits the application runs with.
var Defaults = Limits{
	MaxFiles:            20,
	MaxFileBytes:        5 << 20,
	MaxTotalBytes:       10 << 20,
	MaxRatio:            100,
	MaxDocumentXMLBytes: 64 << 20,
}

// File is one upload as it arrived: the name the browser sent and the
// bytes, in memory.
type File struct {
	Name string
	Data []byte
}

// Reason says why a file was refused, in terms a creator can act on.
type Reason string

const (
	// ReasonType: not on the allowlist, or the content is not what the
	// name says.
	ReasonType Reason = "type"
	// ReasonLegacy: doc, ppt or xls. Modern names the format to save as
	// instead.
	ReasonLegacy Reason = "legacy"
	// ReasonTooLarge: one file, member or document's text is over
	// MaxFileBytes.
	ReasonTooLarge Reason = "too_large"
	// ReasonTotal: everything together is over MaxTotalBytes.
	ReasonTotal Reason = "total"
	// ReasonCount: more than MaxFiles attachments.
	ReasonCount Reason = "count"
	// ReasonNested: an archive inside an archive.
	ReasonNested Reason = "nested"
	// ReasonBomb: an archive that expands far past its size.
	ReasonBomb Reason = "bomb"
	// ReasonUnreadable: damaged, encrypted or otherwise unreadable.
	ReasonUnreadable Reason = "unreadable"
	// ReasonEmpty: no content, or no text in a format read for its text.
	ReasonEmpty Reason = "empty"
	// ReasonMalicious: the hash lookup reported the file as malware.
	ReasonMalicious Reason = "malicious"
)

// Error is a refusal of one file. Name is the file as the creator
// knows it, so the message can say which one.
type Error struct {
	Reason Reason
	Name   string
	// Modern is the format to save a legacy file as ("docx").
	Modern string
}

func (e *Error) Error() string {
	return fmt.Sprintf("attach: %s refused: %s", e.Name, e.Reason)
}

// Scanner looks a file up by its SHA-256 and reports whether it is
// known to be malicious. Only the hash leaves the process.
type Scanner interface {
	Malicious(ctx context.Context, sum [sha256.Size]byte) (bool, error)
}

// Options configure Prepare. The zero value uses Defaults and no
// scanner.
type Options struct {
	Limits  Limits
	Scanner Scanner
	// Logger receives a lookup failure, without the file's name.
	Logger *slog.Logger
}

// Prepare checks every file and returns what a model is sent, in the
// order the files came. The first refusal stops it: a creator who sent
// three files and sees questions drafted from two would not know the
// third was ignored.
func Prepare(ctx context.Context, files []File, opts Options) ([]ai.Attachment, error) {
	lim := opts.Limits
	if lim == (Limits{}) {
		lim = Defaults
	}
	p := &preparer{lim: lim}
	for _, f := range files {
		name := cleanName(f.Name)
		if err := p.scan(ctx, opts, name, f.Data); err != nil {
			return nil, err
		}
		if err := p.add(name, f.Data, true); err != nil {
			return nil, err
		}
	}
	return p.out, nil
}

// scan asks the scanner about one upload. A failed lookup does not
// refuse the file: a hash lookup only recognises files already known,
// so it cannot vouch for a new one either way, and refusing uploads
// whenever the lookup service is slow or rate limited would trade the
// feature for little protection. The file is never stored or opened by
// anything but the extractor and the model.
func (p *preparer) scan(ctx context.Context, opts Options, name string, data []byte) error {
	if opts.Scanner == nil {
		return nil
	}
	malicious, err := opts.Scanner.Malicious(ctx, sha256.Sum256(data))
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Warn("attachment lookup failed; the file is used unchecked", "error", err)
		}
		return nil
	}
	if malicious {
		return &Error{Reason: ReasonMalicious, Name: name}
	}
	return nil
}

type preparer struct {
	lim   Limits
	out   []ai.Attachment
	total int64
}

// add identifies one file and appends what it yields. topLevel is false
// for an archive member, which may not be an archive itself.
func (p *preparer) add(name string, data []byte, topLevel bool) error {
	if len(data) == 0 {
		return &Error{Reason: ReasonEmpty, Name: name}
	}
	if int64(len(data)) > p.lim.MaxFileBytes {
		return &Error{Reason: ReasonTooLarge, Name: name}
	}
	k, err := identify(name, data)
	if err != nil {
		return err
	}
	if k == kindZip {
		if !topLevel {
			return &Error{Reason: ReasonNested, Name: name}
		}
		return p.unpack(name, data)
	}

	if len(p.out) >= p.lim.MaxFiles {
		return &Error{Reason: ReasonCount, Name: name}
	}
	p.total += int64(len(data))
	if p.total > p.lim.MaxTotalBytes {
		return &Error{Reason: ReasonTotal, Name: name}
	}

	attachment, err := convert(name, k, data, p.lim)
	if err != nil {
		return err
	}
	p.out = append(p.out, attachment)
	return nil
}

// convert produces what the model is sent for a file of kind k.
func convert(name string, k kind, data []byte, lim Limits) (ai.Attachment, error) {
	switch k {
	case kindPDF:
		return ai.Attachment{Name: name, MIME: ai.MIMEPDF, Data: data}, nil
	case kindPNG:
		return ai.Attachment{Name: name, MIME: ai.MIMEPNG, Data: data}, nil
	case kindJPEG:
		return ai.Attachment{Name: name, MIME: ai.MIMEJPEG, Data: data}, nil
	}

	var text string
	var err error
	switch k {
	case kindText:
		text = string(data)
	case kindHTML:
		text, err = htmlText(data, lim)
	case kindSVG:
		text, err = svgText(data, lim)
	default:
		text, err = documentText(k, data, lim)
	}
	if err != nil {
		return ai.Attachment{}, withName(err, name)
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, byteOrderMark))
	if text == "" {
		return ai.Attachment{}, &Error{Reason: ReasonEmpty, Name: name}
	}
	return ai.Attachment{Name: name, MIME: ai.MIMEText, Data: []byte(text)}, nil
}

// byteOrderMark is what some editors put at the start of UTF-8 text; it
// is not part of what the file says.
var byteOrderMark = string(rune(0xFEFF))

// withName attaches the file's name to a refusal raised while reading
// it, and turns any other failure into an unreadable file.
func withName(err error, name string) error {
	var refusal *Error
	if errors.As(err, &refusal) {
		if refusal.Name == "" {
			refusal.Name = name
		}
		return refusal
	}
	return &Error{Reason: ReasonUnreadable, Name: name}
}

type kind int

const (
	kindText kind = iota
	kindHTML
	kindSVG
	kindPDF
	kindPNG
	kindJPEG
	kindDOCX
	kindPPTX
	kindXLSX
	kindODT
	kindODP
	kindZip
)

// byExtension is the allowlist, and what each name claims to be.
var byExtension = map[string]kind{
	"txt": kindText, "md": kindText, "csv": kindText,
	"html": kindHTML, "htm": kindHTML,
	"svg": kindSVG,
	"pdf": kindPDF,
	"png": kindPNG, "jpg": kindJPEG, "jpeg": kindJPEG,
	"docx": kindDOCX, "pptx": kindPPTX, "xlsx": kindXLSX,
	"odt": kindODT, "odp": kindODP,
	"zip": kindZip,
}

// legacy maps each refused binary Office format to the one to save as.
var legacy = map[string]string{"doc": "docx", "ppt": "pptx", "xls": "xlsx"}

// Accept is the file picker's filter: every name on the allowlist, and
// the legacy formats too, so that choosing one gets the message saying
// what to save it as rather than a picker that hides the file.
func Accept() string {
	var exts []string
	for ext := range byExtension {
		exts = append(exts, "."+ext)
	}
	for ext := range legacy {
		exts = append(exts, "."+ext)
	}
	sort.Strings(exts)
	return strings.Join(exts, ",")
}

// identify decides what a file is from its content, and refuses it
// unless that agrees with its name. The name alone is never trusted: a
// renamed executable is still an executable.
func identify(name string, data []byte) (kind, error) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if modern, ok := legacy[ext]; ok {
		return 0, &Error{Reason: ReasonLegacy, Name: name, Modern: modern}
	}
	claimed, ok := byExtension[ext]
	if !ok {
		return 0, &Error{Reason: ReasonType, Name: name}
	}
	sniffed := http.DetectContentType(data)
	refuse := &Error{Reason: ReasonType, Name: name}

	switch claimed {
	case kindPDF:
		if sniffed != "application/pdf" {
			return 0, refuse
		}
	case kindPNG:
		if sniffed != "image/png" {
			return 0, refuse
		}
	case kindJPEG:
		if sniffed != "image/jpeg" {
			return 0, refuse
		}
	case kindText, kindHTML, kindSVG:
		if !isText(sniffed, data) {
			return 0, refuse
		}
		if claimed == kindSVG && !bytes.Contains(data, []byte("<svg")) {
			return 0, refuse
		}
	case kindZip:
		if sniffed != "application/zip" {
			return 0, refuse
		}
	default: // the Office and OpenDocument containers
		if sniffed != "application/zip" {
			return 0, refuse
		}
		inside, err := containerKind(data)
		if err != nil {
			return 0, withName(err, name)
		}
		if inside != claimed {
			return 0, refuse
		}
	}
	return claimed, nil
}

// isText accepts UTF-8 text without control characters other than
// whitespace: what a text editor saves, and nothing a binary file looks
// like.
func isText(sniffed string, data []byte) bool {
	if !strings.HasPrefix(sniffed, "text/") || !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' && r != '\f' {
			return false
		}
	}
	return true
}

// cleanName keeps the last element of a path and drops control
// characters, so a name read back to the creator or shown to the model
// is just a file name.
func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	if strings.TrimSpace(name) == "" {
		return "file"
	}
	return name
}

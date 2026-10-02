package http

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/attach"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// Files attached to an AI prompt (issue #5). The editor's drafting panel
// and the new-survey form post them as multipart forms; the streaming
// socket never carries them, since a file is one more thing to frame,
// cap and resume over a connection built for text, and the plain post
// already does the job.
//
// A file lives in this process's memory for one request and is
// forwarded to the model only: never written, never stored, never
// logged.

// attachField is the form field that carries the files.
const attachField = "files"

// readUploads parses a multipart body before anything else reads it.
// The memory allowance is the whole request cap for the route, so the standard
// library's parser keeps every part in memory: with a smaller one it
// would write the remainder of a large file to a temporary file, and an
// upload must never reach the disk. A body over the cap is refused here
// with its own page, since what was typed cannot be read back from a
// body that was cut off.
func (s *server) readUploads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			next.ServeHTTP(w, r)
			return
		}
		limit := bodyLimit(r)
		if err := r.ParseMultipartForm(limit); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				render(w, r, http.StatusRequestEntityTooLarge, templates.ErrorPage(
					say(r, "error.upload.title"),
					say(r, "error.upload.body", uitext.Args{"Size": megabytes(limit)})))
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		next.ServeHTTP(w, r)
	})
}

// uploaded is the files posted with a form, read whole into memory. A
// file input left empty still posts a part with no name and no bytes;
// that is not a file.
func uploaded(r *http.Request) ([]attach.File, error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	var files []attach.File
	for _, header := range r.MultipartForm.File[attachField] {
		if header.Filename == "" && header.Size == 0 {
			continue
		}
		data, err := readUpload(header)
		if err != nil {
			return nil, err
		}
		files = append(files, attach.File{Name: header.Filename, Data: data})
	}
	return files, nil
}

func readUpload(header *multipart.FileHeader) ([]byte, error) {
	f, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// hasUploads reports whether a form carries any file, before anything
// is read from it.
func hasUploads(r *http.Request) bool {
	if r.MultipartForm == nil {
		return false
	}
	for _, header := range r.MultipartForm.File[attachField] {
		if header.Filename != "" || header.Size > 0 {
			return true
		}
	}
	return false
}

// promptAttachments reads, checks and converts the files sent with a
// prompt. The string is a refusal to show the creator, in place of the
// attachments, when a file cannot be used.
func (s *server) promptAttachments(r *http.Request) ([]ai.Attachment, string) {
	files, err := uploaded(r)
	if err != nil {
		return nil, say(r, "attach.error.unreadable_upload")
	}
	if len(files) == 0 {
		return nil, ""
	}
	attachments, err := attach.Prepare(r.Context(), files, attach.Options{
		Scanner: s.attachScanner,
		Logger:  s.logger,
	})
	if err != nil {
		return nil, attachRefusal(text(r), err)
	}
	return attachments, ""
}

// attachRefusals word each reason a file is refused.
var attachRefusals = map[attach.Reason]uitext.ID{
	attach.ReasonType:       "attach.error.type",
	attach.ReasonLegacy:     "attach.error.legacy",
	attach.ReasonTooLarge:   "attach.error.too_large",
	attach.ReasonTotal:      "attach.error.total",
	attach.ReasonCount:      "attach.error.count",
	attach.ReasonNested:     "attach.error.nested",
	attach.ReasonBomb:       "attach.error.bomb",
	attach.ReasonUnreadable: "attach.error.unreadable",
	attach.ReasonEmpty:      "attach.error.empty",
	attach.ReasonMalicious:  "attach.error.malicious",
}

// attachRefusal says which file was refused and what to do about it.
func attachRefusal(l uitext.Localizer, err error) string {
	var refusal *attach.Error
	if !errors.As(err, &refusal) {
		return l.T("attach.error.unreadable_upload")
	}
	id, ok := attachRefusals[refusal.Reason]
	if !ok {
		id = "attach.error.unreadable"
	}
	return l.T(id, uitext.Args{
		"Name":   refusal.Name,
		"Modern": refusal.Modern,
		"Size":   megabytes(attach.Defaults.MaxFileBytes),
		"Total":  megabytes(attach.Defaults.MaxTotalBytes),
		"Limit":  attach.Defaults.MaxFiles,
	})
}

// megabytes is a byte cap as a reader is told it.
func megabytes(n int64) int64 { return n >> 20 }

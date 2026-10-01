// Package ai is the single seam to language models (SPEC.md "AI
// surface"): four streaming operations behind one Provider interface.
// Production (stg/pro) speaks to Vertex AI; local dev and self-hosters
// speak to anything OpenAI-compatible (ollama, llamafile) plus
// whisper-cli for transcription; tests use the scripted Fake. Model names
// are configuration, never code (PLAN.md architecture overview).
package ai

import (
	"context"
	"errors"
	"io"
)

// Provider is the seam. Every method streams: the value of AI output
// arriving token by token is the product experience (M6-T3's "watch
// questions stream in"), so nothing here buffers whole responses.
type Provider interface {
	// Generate produces survey-building output from a creator prompt.
	Generate(ctx context.Context, req GenerateRequest) (Stream, error)
	// Transcribe turns spoken audio into text (ADR-0004: the audio is
	// consumed from the reader and never stored anywhere).
	Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error)
	// Translate renders text into a target language (M11).
	Translate(ctx context.Context, req TranslateRequest) (Stream, error)
	// Analyze produces cross-respondent insight output (M10).
	Analyze(ctx context.Context, req AnalyzeRequest) (Stream, error)
}

// Stream delivers text fragments until io.EOF. Close is idempotent and
// must be called; it releases the underlying connection or process.
type Stream interface {
	// Recv returns the next fragment, or io.EOF when the output is
	// complete.
	Recv() (string, error)
	Close() error
}

type GenerateRequest struct {
	// System frames the task; Prompt is the creator's input.
	System string
	Prompt string
	// Attachments are files the creator sent with the prompt, already
	// checked and, where a model cannot read the original format,
	// converted to text (internal/attach). They live in memory for one
	// request and are never stored.
	Attachments []Attachment
}

// Attachment is one file forwarded to a model. MIME is one of the kinds
// a provider can be asked to read: MIMEText for anything turned into
// text, MIMEPDF, MIMEPNG or MIMEJPEG for what goes to the model as it
// was uploaded. Name is the file's name as the creator gave it, which
// the model is told, so a prompt can refer to "the attached
// spreadsheet".
type Attachment struct {
	Name string
	MIME string
	Data []byte
}

// The attachment kinds a provider may be handed.
const (
	MIMEText = "text/plain"
	MIMEPDF  = "application/pdf"
	MIMEPNG  = "image/png"
	MIMEJPEG = "image/jpeg"
)

// IsText reports whether the attachment is text a text-only model can
// read inline.
func (a Attachment) IsText() bool { return a.MIME == MIMEText }

// ErrAttachmentUnsupported marks an attachment a provider cannot read,
// such as an image sent to a text-only model. Callers show it as a
// refusal of that file, not as the AI being down.
var ErrAttachmentUnsupported = errors.New("ai: this provider cannot read the attachment")

// attachmentNames lists the attachments' names, for the providers that
// acknowledge what they were sent instead of reading it.
func attachmentNames(attachments []Attachment) []string {
	names := make([]string, 0, len(attachments))
	for _, a := range attachments {
		names = append(names, a.Name)
	}
	return names
}

type TranscribeRequest struct {
	// Audio is consumed exactly once and never persisted (ADR-0004).
	Audio    io.Reader
	MIMEType string
	// Language is the respondent's chosen language as a BCP-47-ish code
	// ("en", "nl"); empty means auto-detect. M11-T3 verifies this hint
	// passes through.
	Language string
}

type TranslateRequest struct {
	Text       string
	SourceLang string
	TargetLang string
}

type AnalyzeRequest struct {
	System string
	Prompt string
}

// ErrUnsupported marks an operation a provider cannot perform (e.g.
// whisper-cli asked to Generate); the composite provider routes around
// it, and callers treat it as "AI not configured" (Appendix D: features
// degrade gracefully).
var ErrUnsupported = errors.New("ai: operation not supported by this provider")

// Collect drains a stream into one string — for callers that need the
// whole output (transcription review, cached translations) rather than
// the live feed.
func Collect(s Stream) (string, error) {
	defer s.Close()
	var out []byte
	for {
		fragment, err := s.Recv()
		out = append(out, fragment...)
		if errors.Is(err, io.EOF) {
			return string(out), nil
		}
		if err != nil {
			return string(out), err
		}
	}
}

// readerStream adapts a sequence of already-available fragments.
type sliceStream struct {
	fragments []string
	pos       int
}

func newSliceStream(fragments ...string) *sliceStream {
	return &sliceStream{fragments: fragments}
}

func (s *sliceStream) Recv() (string, error) {
	if s.pos >= len(s.fragments) {
		return "", io.EOF
	}
	fragment := s.fragments[s.pos]
	s.pos++
	return fragment, nil
}

func (s *sliceStream) Close() error { return nil }

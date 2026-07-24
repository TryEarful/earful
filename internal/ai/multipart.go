package ai

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
)

// audioForm builds the multipart body for whisper-style transcription
// endpoints, kept out of the request method for readability.
type audioForm struct {
	writer *multipart.Writer
}

func newAudioForm(buf *bytes.Buffer) *audioForm {
	return &audioForm{writer: multipart.NewWriter(buf)}
}

func (f *audioForm) write(req TranscribeRequest, model string) error {
	filename := "audio" + extensionFor(req.MIMEType)
	part, err := f.writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("ai: build audio form: %w", err)
	}
	if _, err := io.Copy(part, req.Audio); err != nil {
		return fmt.Errorf("ai: copy audio: %w", err)
	}
	if err := f.writer.WriteField("model", model); err != nil {
		return fmt.Errorf("ai: write model field: %w", err)
	}
	if req.Language != "" {
		if err := f.writer.WriteField("language", req.Language); err != nil {
			return fmt.Errorf("ai: write language field: %w", err)
		}
	}
	return f.writer.Close()
}

func (f *audioForm) contentType() string { return f.writer.FormDataContentType() }

func extensionFor(mimeType string) string {
	switch {
	case strings.Contains(mimeType, "webm"):
		return ".webm"
	case strings.Contains(mimeType, "ogg"):
		return ".ogg"
	case strings.Contains(mimeType, "mp4"):
		return ".mp4"
	case strings.Contains(mimeType, "mpeg"):
		return ".mp3"
	default:
		return ".wav"
	}
}

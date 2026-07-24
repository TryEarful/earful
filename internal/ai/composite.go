package ai

import "context"

// Composite routes transcription to one provider and text operations to
// another — the local-dev shape (whisper-cli for voice, ollama/llamafile
// for text) and the graceful-degradation shape (text AI configured, voice
// not, or vice versa). A nil member means that capability is absent and
// callers get ErrUnsupported, which the product treats as "feature not
// available" rather than an error page (Appendix D).
type Composite struct {
	Text        Provider // Generate, Translate, Analyze
	Transcriber Provider // Transcribe
}

func (c *Composite) Generate(ctx context.Context, req GenerateRequest) (Stream, error) {
	if c.Text == nil {
		return nil, ErrUnsupported
	}
	return c.Text.Generate(ctx, req)
}

func (c *Composite) Translate(ctx context.Context, req TranslateRequest) (Stream, error) {
	if c.Text == nil {
		return nil, ErrUnsupported
	}
	return c.Text.Translate(ctx, req)
}

func (c *Composite) Analyze(ctx context.Context, req AnalyzeRequest) (Stream, error) {
	if c.Text == nil {
		return nil, ErrUnsupported
	}
	return c.Text.Analyze(ctx, req)
}

func (c *Composite) Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error) {
	if c.Transcriber == nil {
		return nil, ErrUnsupported
	}
	return c.Transcriber.Transcribe(ctx, req)
}

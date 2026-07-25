package ai

// Op names one provider operation. The names double as the `kind`
// recorded in ai_usage, so metering rows, quota reports and model
// configuration all speak the same vocabulary.
type Op string

const (
	OpGenerate   Op = "generate"
	OpTranscribe Op = "transcribe"
	OpTranslate  Op = "translate"
	OpAnalyze    Op = "analyze"
)

// ModelSet is which model serves which operation. SPEC.md's AI surface
// asks for insights on a stronger tier than the rest; that is this
// struct, not a branch in code — the product never names a model.
// Anything left empty falls back to Default.
type ModelSet struct {
	Default    string
	Generate   string
	Transcribe string
	Translate  string
	Analyze    string
}

// For returns the model configured for op.
func (m ModelSet) For(op Op) string {
	var model string
	switch op {
	case OpGenerate:
		model = m.Generate
	case OpTranscribe:
		model = m.Transcribe
	case OpTranslate:
		model = m.Translate
	case OpAnalyze:
		model = m.Analyze
	}
	if model == "" {
		return m.Default
	}
	return model
}

// Empty reports whether no model is configured for any operation.
func (m ModelSet) Empty() bool {
	return m.Default == "" && m.Generate == "" && m.Transcribe == "" &&
		m.Translate == "" && m.Analyze == ""
}

// translationSystemPrompt is shared by every text backend so a
// translation means the same thing whichever provider produced it (M11's
// creator-reviewed Localizations compare across runs).
func translationSystemPrompt(sourceLang, targetLang string) string {
	source := sourceLang
	if source == "" {
		source = "the source language (detect it)"
	}
	return "You are a translator. Translate the user's text from " + source + " to " + targetLang +
		". Output only the translation — no preamble, no notes."
}

// transcriptionPrompt instructs a general-purpose multimodal model to act
// as a transcriber. Whisper-shaped backends need no prompt; Gemini-shaped
// ones do (ADR-0004's server path).
func transcriptionPrompt(language string) string {
	prompt := "Transcribe the spoken audio verbatim. Output only the transcript, with no commentary, " +
		"no speaker labels and no timestamps. If the audio contains no speech, output nothing."
	if language != "" {
		prompt += " The speaker is speaking " + language + "."
	}
	return prompt
}

// Capable is implemented by providers that know in advance which
// operations they can perform.
type Capable interface {
	Supports(op Op) bool
}

// Supports reports whether p can perform op. A provider that does not
// implement Capable is assumed able: the honest failure is then an
// ErrUnsupported at call time, which callers already handle, and
// assuming otherwise would hide a working feature.
func Supports(p Provider, op Op) bool {
	if p == nil {
		return false
	}
	if c, ok := p.(Capable); ok {
		return c.Supports(op)
	}
	return true
}

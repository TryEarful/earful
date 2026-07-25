package ai

import (
	"time"

	"github.com/TryEarful/earful/internal/config"
)

// FromConfig assembles the configured Provider (M6-T1's swap-via-env-var
// AC). The result is always non-nil: unconfigured capabilities answer
// ErrUnsupported, and the product renders them as absent features rather
// than failures (Appendix D).
//
// Text and voice are selected separately because they routinely come from
// different places — a laptop runs llamafile for text and whisper-cli for
// voice; production runs one Vertex project for both. Adding a backend
// means adding a case here and a value to config.validate, nothing else:
// no handler, template or test names a provider.
func FromConfig(cfg config.Config) Provider {
	models := ModelSet{
		Default:    cfg.AIModel,
		Generate:   cfg.AIModelGenerate,
		Analyze:    cfg.AIModelAnalyze,
		Translate:  cfg.AIModelTranslate,
		Transcribe: cfg.AIModelTranscribe,
	}

	composite := &Composite{}
	text := newTextProvider(cfg, models)
	if text != nil {
		composite.Text = text
	}
	if transcriber := newTranscriber(cfg, models, text); transcriber != nil {
		composite.Transcriber = transcriber
	}
	return composite
}

// newTextProvider returns nil — not a typed nil — when text AI is off, so
// the composite's nil check works.
func newTextProvider(cfg config.Config, models ModelSet) Provider {
	switch cfg.AIProvider {
	case "openai":
		return &OpenAICompat{BaseURL: cfg.AIBaseURL, Models: models, APIKey: cfg.AIAPIKey}
	case "vertex":
		return &Vertex{Project: cfg.VertexProject, Location: cfg.VertexLocation, Models: models}
	case "scripted":
		return &Scripted{Delay: scriptedDelay}
	default:
		return nil
	}
}

// newTranscriber picks the voice backend, reusing the text provider when
// both speak to the same service — one client, one set of credentials.
func newTranscriber(cfg config.Config, models ModelSet, text Provider) Provider {
	switch cfg.TranscribeProvider {
	case "whisper-cli":
		return &WhisperCLI{Bin: cfg.WhisperBin, Model: cfg.WhisperModel}
	case "openai":
		// The audio endpoint lives on the same server as chat, so this is
		// only available when that server is configured.
		if o, ok := text.(*OpenAICompat); ok {
			o.SupportsAudio = true
			return o
		}
		return nil
	case "vertex":
		if v, ok := text.(*Vertex); ok {
			return v
		}
		return &Vertex{Project: cfg.VertexProject, Location: cfg.VertexLocation, Models: models}
	case "scripted":
		if s, ok := text.(*Scripted); ok {
			return s
		}
		return &Scripted{Delay: scriptedDelay}
	default:
		return nil
	}
}

// scriptedDelay paces the development provider so a browser shows text
// arriving rather than appearing. Small enough not to slow the e2e suite.
const scriptedDelay = 25 * time.Millisecond

package ai

import (
	"github.com/TryEarful/earful/internal/config"
)

// FromConfig assembles the configured Provider (M6-T1's swap-via-env-var
// AC). The result is always non-nil: unconfigured capabilities answer
// ErrUnsupported, and the product renders them as absent features rather
// than failures (Appendix D).
func FromConfig(cfg config.Config) Provider {
	composite := &Composite{}

	var openai *OpenAICompat
	if cfg.AIProvider == "openai" {
		openai = &OpenAICompat{
			BaseURL: cfg.AIBaseURL,
			Model:   cfg.AIModel,
			APIKey:  cfg.AIAPIKey,
		}
		composite.Text = openai
	}

	switch cfg.TranscribeProvider {
	case "whisper-cli":
		composite.Transcriber = &WhisperCLI{Bin: cfg.WhisperBin, Model: cfg.WhisperModel}
	case "openai":
		if openai != nil {
			openai.SupportsAudio = true
			composite.Transcriber = openai
		}
	}
	return composite
}

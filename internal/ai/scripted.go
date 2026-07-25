package ai

import (
	"context"
	"strings"
	"time"
)

// Scripted is a deterministic provider for local development and the
// browser suite: it produces well-formed output for every operation
// without a model, a GPU or a network. It is how the streaming surfaces
// (M5's transcript, M6-T3's questions, M10's insights) can be driven in a
// real browser on a laptop that has no llamafile running.
//
// It is refused outside APP_ENV=development at boot (config.validate), so
// no deployed environment can ever serve invented content to a user.
type Scripted struct {
	// Delay paces fragments so streaming looks like streaming; zero is
	// fine for tests, ~40ms is pleasant in a browser.
	Delay time.Duration
}

func (s *Scripted) Generate(_ context.Context, req GenerateRequest) (Stream, error) {
	topic := strings.TrimSpace(req.Prompt)
	if topic == "" {
		topic = "your topic"
	}
	if len(topic) > 60 {
		topic = topic[:60]
	}
	// NDJSON, exactly the shape the real generation prompt asks for: one
	// question per line, so the reader can surface each as it completes.
	lines := []string{
		`{"type":"long_text","text":"In your own words, what stands out about ` + jsonEscape(topic) + `?","required":true}`,
		`{"type":"single_choice","text":"How often does it come up?","options":["Daily","Weekly","Rarely"]}`,
		`{"type":"rating_scale","text":"How would you rate it overall?","scale_min":1,"scale_max":5}`,
		`{"type":"nps","text":"How likely are you to recommend us?"}`,
		`{"type":"short_text","text":"Anything we should change first?"}`,
	}
	return s.fragments(joinLines(lines)), nil
}

func (s *Scripted) Analyze(_ context.Context, _ AnalyzeRequest) (Stream, error) {
	return s.fragments(
		"Three themes run through these answers.\n\n" +
			"1. Speed is the most common praise — respondents describe getting started quickly.\n" +
			"2. Onboarding is the most common friction, mentioned in roughly a third of answers.\n" +
			"3. A smaller group asks for exports.\n\n" +
			"Representative quotes:\n" +
			"- \"It took me two minutes to send my first survey.\"\n" +
			"- \"I got stuck the first time I tried to invite people.\"\n",
	), nil
}

func (s *Scripted) Translate(_ context.Context, req TranslateRequest) (Stream, error) {
	// Marked, not invented: a reviewer must be able to see at a glance
	// that this text came from the scripted provider, not a translator.
	return s.fragments("[" + req.TargetLang + "] " + req.Text), nil
}

func (s *Scripted) Transcribe(_ context.Context, req TranscribeRequest) (Stream, error) {
	// Drain the reader: real providers consume it, and a caller that
	// depends on that must behave the same here (ADR-0004 keeps the audio
	// nowhere else, so dropping it on the floor is the correct handling).
	if req.Audio != nil {
		buf := make([]byte, 32*1024)
		for {
			if _, err := req.Audio.Read(buf); err != nil {
				break
			}
		}
	}
	return s.fragments("This is a scripted transcript. Voice input reached the server " +
		"and came back as editable text."), nil
}

// fragments splits text into word-sized pieces so consumers see a real
// stream rather than one lump.
func (s *Scripted) fragments(text string) Stream {
	words := strings.SplitAfter(text, " ")
	return &fakeStream{fragments: words, delay: s.Delay}
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

// jsonEscape makes an arbitrary creator prompt safe to embed in the
// scripted JSON line above.
func jsonEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", " ", "\t", " ")
	return r.Replace(s)
}

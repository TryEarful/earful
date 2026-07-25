package ai

import (
	"context"
	"io"
	"sync"
	"time"
)

// Fake is the scripted streaming provider from SPEC.md's Testing
// Decisions. Tests script each operation's output and inspect what was
// asked — including the transcription language hint, whose passthrough
// M11-T3 makes a tested contract.
type Fake struct {
	mu sync.Mutex

	// Scripted outputs, one slice of fragments per call, consumed in
	// order. When a script runs out its last entry repeats.
	GenerateScript   [][]string
	TranscribeScript [][]string
	TranslateScript  [][]string
	AnalyzeScript    [][]string

	// Err, when set, fails every call — the "provider is down" script.
	Err error

	// FragmentDelay pauses before each fragment. Streaming surfaces
	// (M5's transcript, M6-T3's questions, M10's insights) are only
	// observably streaming if fragments arrive apart in time.
	FragmentDelay time.Duration

	// StreamErr ends a stream with this error after StreamErrAfter
	// fragments instead of io.EOF — the "provider died mid-stream"
	// script, which every consumer has to survive without losing the
	// fragments it already delivered.
	StreamErr      error
	StreamErrAfter int

	// Recorded calls, for assertions.
	GenerateCalls   []GenerateRequest
	TranscribeCalls []TranscribeRecord
	TranslateCalls  []TranslateRequest
	AnalyzeCalls    []AnalyzeRequest
}

// TranscribeRecord captures what Transcribe was asked, with the audio
// drained to bytes (the reader is single-use).
type TranscribeRecord struct {
	Audio    []byte
	MIMEType string
	Language string
}

func (f *Fake) Generate(_ context.Context, req GenerateRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.GenerateCalls = append(f.GenerateCalls, req)
	return f.stream(takeScript(&f.GenerateScript, len(f.GenerateCalls))), nil
}

func (f *Fake) Transcribe(_ context.Context, req TranscribeRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	audio, err := io.ReadAll(req.Audio)
	if err != nil {
		return nil, err
	}
	f.TranscribeCalls = append(f.TranscribeCalls, TranscribeRecord{
		Audio: audio, MIMEType: req.MIMEType, Language: req.Language,
	})
	return f.stream(takeScript(&f.TranscribeScript, len(f.TranscribeCalls))), nil
}

func (f *Fake) Translate(_ context.Context, req TranslateRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.TranslateCalls = append(f.TranslateCalls, req)
	return f.stream(takeScript(&f.TranslateScript, len(f.TranslateCalls))), nil
}

func (f *Fake) Analyze(_ context.Context, req AnalyzeRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.AnalyzeCalls = append(f.AnalyzeCalls, req)
	return f.stream(takeScript(&f.AnalyzeScript, len(f.AnalyzeCalls))), nil
}

// stream wraps scripted fragments in the timing and failure behaviour the
// Fake is currently configured for. Called with f.mu held.
func (f *Fake) stream(fragments []string) Stream {
	return &fakeStream{
		fragments: fragments,
		delay:     f.FragmentDelay,
		err:       f.StreamErr,
		errAfter:  f.StreamErrAfter,
	}
}

type fakeStream struct {
	fragments []string
	pos       int
	delay     time.Duration
	err       error
	errAfter  int
}

func (s *fakeStream) Recv() (string, error) {
	if s.err != nil && s.pos >= s.errAfter {
		return "", s.err
	}
	if s.pos >= len(s.fragments) {
		return "", io.EOF
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	fragment := s.fragments[s.pos]
	s.pos++
	return fragment, nil
}

func (s *fakeStream) Close() error { return nil }

// takeScript returns the nth (1-based) scripted output, repeating the
// final entry once the script runs out, and a placeholder if none exists.
func takeScript(script *[][]string, call int) []string {
	s := *script
	if len(s) == 0 {
		return []string{"scripted output"}
	}
	if call-1 < len(s) {
		return s[call-1]
	}
	return s[len(s)-1]
}

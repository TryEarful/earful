package ai

import (
	"context"
	"io"
	"sync"
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
	return newSliceStream(takeScript(&f.GenerateScript, len(f.GenerateCalls))...), nil
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
	return newSliceStream(takeScript(&f.TranscribeScript, len(f.TranscribeCalls))...), nil
}

func (f *Fake) Translate(_ context.Context, req TranslateRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.TranslateCalls = append(f.TranslateCalls, req)
	return newSliceStream(takeScript(&f.TranslateScript, len(f.TranslateCalls))...), nil
}

func (f *Fake) Analyze(_ context.Context, req AnalyzeRequest) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.AnalyzeCalls = append(f.AnalyzeCalls, req)
	return newSliceStream(takeScript(&f.AnalyzeScript, len(f.AnalyzeCalls))...), nil
}

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

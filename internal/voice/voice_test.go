package voice_test

import (
	"encoding/binary"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/voice"
)

func TestBuffer_CapsAudioAndStaysBounded(t *testing.T) {
	t.Parallel()
	// Two seconds of headroom, then 500 chunks of a tenth of a second
	// each: fifty seconds of speech arriving at a socket that must not
	// grow with it (M5-T2's bounded-memory AC).
	buf := voice.NewBuffer(2)
	chunk := make([]byte, voice.BytesPerSecond/10)

	var refused error
	for i := 0; i < 500; i++ {
		if err := buf.Append(chunk); err != nil {
			refused = err
			break
		}
	}
	if !errors.Is(refused, voice.ErrTooLong) {
		t.Fatalf("500 chunks were accepted into a 2-second buffer (err = %v)", refused)
	}
	if got, want := buf.Len(), 2*voice.BytesPerSecond; got != want {
		t.Errorf("buffered %d bytes, want exactly the cap %d", got, want)
	}
	if got := buf.Seconds(); got != 2 {
		t.Errorf("Seconds() = %d, want 2", got)
	}
}

func TestBuffer_SecondsRoundUp(t *testing.T) {
	t.Parallel()
	buf := voice.NewBuffer(10)
	if got := buf.Seconds(); got != 0 {
		t.Errorf("empty buffer = %d seconds, want 0", got)
	}
	// A fifth of a second of speech still costs a second of quota:
	// rounding down would make a rapid series of short takes free.
	if err := buf.Append(make([]byte, voice.BytesPerSecond/5)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := buf.Seconds(); got != 1 {
		t.Errorf("Seconds() = %d, want 1", got)
	}
}

func TestBuffer_WAVIsAWellFormedSixteenKilohertzMonoFile(t *testing.T) {
	t.Parallel()
	buf := voice.NewBuffer(5)
	pcm := make([]byte, 3200) // a tenth of a second
	if err := buf.Append(pcm); err != nil {
		t.Fatalf("Append: %v", err)
	}
	wav := buf.WAV()

	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file: %q", wav[:12])
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != 1 {
		t.Errorf("channels = %d, want mono", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != voice.SampleRate {
		t.Errorf("sample rate = %d, want %d", got, voice.SampleRate)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Errorf("bit depth = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); int(got) != len(pcm) {
		t.Errorf("data size = %d, want %d", got, len(pcm))
	}
	if len(wav) != 44+len(pcm) {
		t.Errorf("file length = %d, want header + payload", len(wav))
	}
}

// TestBuffer_ResetLeavesNothingBehind is ADR-0004 taken literally: once a
// transcript exists, the audio is gone.
func TestBuffer_ResetLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	buf := voice.NewBuffer(5)
	speech := []byte{0x11, 0x22, 0x33, 0x44}
	if err := buf.Append(speech); err != nil {
		t.Fatalf("Append: %v", err)
	}
	wav := buf.WAV()
	buf.Reset()

	if buf.Len() != 0 || buf.Seconds() != 0 {
		t.Errorf("buffer still holds %d bytes after Reset", buf.Len())
	}
	// The WAV the caller already holds is its own copy — Reset must not
	// corrupt an in-flight transcription request.
	if wav[44] != 0x11 {
		t.Error("Reset reached into an already-produced WAV")
	}
}

func TestBudget_LimitsOneResponseSessionAndExpires(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Now())
	budget := voice.NewBudget(60, time.Hour, clk)

	if got := budget.Remaining("nonce-a"); got != 60 {
		t.Errorf("fresh session has %d seconds, want 60", got)
	}
	budget.Spend("nonce-a", 45)
	if got := budget.Remaining("nonce-a"); got != 15 {
		t.Errorf("after 45s spent, %d remain, want 15", got)
	}
	// Another respondent is unaffected.
	if got := budget.Remaining("nonce-b"); got != 60 {
		t.Errorf("a different session has %d seconds, want 60", got)
	}
	budget.Spend("nonce-a", 30)
	if got := budget.Remaining("nonce-a"); got != 0 {
		t.Errorf("overspending leaves %d, want 0", got)
	}
	// Sessions are short-lived; the entry ages out rather than
	// accumulating forever.
	clk.Advance(2 * time.Hour)
	if got := budget.Remaining("nonce-a"); got != 60 {
		t.Errorf("an expired session has %d seconds, want a fresh 60", got)
	}
}

// TestVoicePackageTouchesNoStorage is the mechanical half of ADR-0004's
// promise (M5-T2's "grep gates" AC): the one package that holds audio
// bytes must contain no way to write them anywhere. A reviewer can check
// this by reading the package; this test checks it on every build.
func TestVoicePackageTouchesNoStorage(t *testing.T) {
	t.Parallel()
	forbidden := map[string][]string{
		"os":     {"Create", "CreateTemp", "WriteFile", "OpenFile", "Open", "MkdirTemp", "Mkdir", "MkdirAll"},
		"ioutil": {"WriteFile", "TempFile"},
		"exec":   {"Command", "CommandContext"},
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			for _, banned := range forbidden[pkg.Name] {
				if sel.Sel.Name == banned {
					t.Errorf("%s: %s.%s in the package that holds audio — ADR-0004 forbids audio "+
						"reaching disk, a process or anywhere else",
						fset.Position(call.Pos()), pkg.Name, sel.Sel.Name)
				}
			}
			return true
		})
	}
}

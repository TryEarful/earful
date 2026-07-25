// Package voice holds the server side of spoken answers: the in-memory
// audio buffer and the per-response second budget.
//
// ADR-0004 is absolute — audio is transcribed and immediately discarded,
// never written to disk, object storage, logs or the database, in any
// environment. This package is deliberately the only place audio bytes
// live, so that promise is auditable by reading one file: there is no
// filesystem access here, no store, no logger.
//
// Respondents send 16-bit little-endian PCM at 16 kHz, mono. That format
// is chosen so no transcoder is needed anywhere: whisper.cpp wants
// exactly this, Vertex accepts WAV, and a self-hoster does not have to
// install ffmpeg to let people speak.
package voice

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/TryEarful/earful/internal/clock"
)

const (
	// SampleRate and BytesPerSample define the wire format the browser
	// worklet produces.
	SampleRate     = 16000
	BytesPerSample = 2
	Channels       = 1

	// BytesPerSecond is what one second of that format costs in memory.
	BytesPerSecond = SampleRate * BytesPerSample * Channels
)

// ErrTooLong means the speaker passed the per-answer cap. The socket ends
// the take and offers the transcript so far; it is a limit, not a fault.
var ErrTooLong = errors.New("voice: audio exceeds the per-answer limit")

// Buffer accumulates one spoken answer in memory.
type Buffer struct {
	max int // bytes
	pcm []byte
}

// NewBuffer caps a take at maxSeconds of audio.
func NewBuffer(maxSeconds int) *Buffer {
	return &Buffer{max: maxSeconds * BytesPerSecond}
}

// Append adds a chunk, refusing to grow past the cap. The cap is why a
// long-running socket cannot become unbounded memory.
func (b *Buffer) Append(chunk []byte) error {
	if len(b.pcm)+len(chunk) > b.max {
		room := b.max - len(b.pcm)
		if room > 0 {
			b.pcm = append(b.pcm, chunk[:room]...)
		}
		return ErrTooLong
	}
	b.pcm = append(b.pcm, chunk...)
	return nil
}

// Len is the buffered audio in bytes.
func (b *Buffer) Len() int { return len(b.pcm) }

// Seconds is the buffered audio in whole seconds, rounded up: a
// two-second answer must cost two seconds of quota, not one.
func (b *Buffer) Seconds() int {
	if len(b.pcm) == 0 {
		return 0
	}
	return (len(b.pcm) + BytesPerSecond - 1) / BytesPerSecond
}

// WAV returns the buffered audio as a WAV file in memory. Nothing writes
// it anywhere; it exists for the duration of one transcription call.
func (b *Buffer) WAV() []byte {
	const headerSize = 44
	out := make([]byte, headerSize+len(b.pcm))
	byteRate := SampleRate * Channels * BytesPerSample

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(b.pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16) // PCM header size
	binary.LittleEndian.PutUint16(out[20:22], 1)  // PCM, uncompressed
	binary.LittleEndian.PutUint16(out[22:24], Channels)
	binary.LittleEndian.PutUint32(out[24:28], SampleRate)
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], Channels*BytesPerSample)
	binary.LittleEndian.PutUint16(out[34:36], BytesPerSample*8)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(b.pcm)))
	copy(out[headerSize:], b.pcm)
	return out
}

// Reset zeroes and releases the audio. Overwriting first is
// belt-and-braces — Go would reuse the memory anyway — but "the bytes are
// gone the moment the transcript exists" is the promise, and this is what
// makes it literally true.
func (b *Buffer) Reset() {
	for i := range b.pcm {
		b.pcm[i] = 0
	}
	b.pcm = nil
}

// Budget tracks how many seconds of transcription one response session
// has spent (M5-T4's per-response cap). It is in-memory and per-process
// on purpose: the key is a form nonce, which identifies a browser session
// and nothing else — persisting it would be storing a fact about a
// respondent, which anonymous surveys must not do (ADR-0003).
type Budget struct {
	mu      sync.Mutex
	clock   clock.Clock
	limit   int
	ttl     time.Duration
	entries map[string]budgetEntry
}

type budgetEntry struct {
	spent   int
	touched time.Time
}

// maxBudgetEntries bounds the map the way antibot.Limiter bounds its own:
// a flood of unique keys must not become unbounded memory.
const maxBudgetEntries = 50_000

func NewBudget(limitSeconds int, ttl time.Duration, clk clock.Clock) *Budget {
	return &Budget{clock: clk, limit: limitSeconds, ttl: ttl, entries: map[string]budgetEntry{}}
}

// Remaining is how many more seconds this session may transcribe. A
// limit of zero means uncapped — the sensible setting for a self-hoster
// whose transcription costs nothing per second.
func (b *Budget) Remaining(key string) int {
	if b.limit <= 0 {
		return math.MaxInt32
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.entries[key]
	if !ok || b.clock.Now().Sub(entry.touched) > b.ttl {
		return b.limit
	}
	if entry.spent >= b.limit {
		return 0
	}
	return b.limit - entry.spent
}

// Spend records seconds against a session.
func (b *Budget) Spend(key string, seconds int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	entry, ok := b.entries[key]
	if !ok || now.Sub(entry.touched) > b.ttl {
		entry = budgetEntry{}
	}
	entry.spent += seconds
	entry.touched = now
	if len(b.entries) >= maxBudgetEntries {
		b.sweep(now)
	}
	b.entries[key] = entry
}

func (b *Budget) sweep(now time.Time) {
	for key, entry := range b.entries {
		if now.Sub(entry.touched) > b.ttl {
			delete(b.entries, key)
		}
	}
}

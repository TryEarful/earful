// Package ws is the WebSocket transport for everything that streams:
// AI-drafted questions (M6-T3), voice transcripts (M5-T2) and insight
// summaries (M10-T2). It exists so those features share one set of
// answers to the awkward questions — origin checking, read limits,
// connection lifetime, frame vocabulary — rather than three.
//
// Three constraints shape it:
//
//   - A WebSocket handshake is a GET, so the stdlib CrossOriginProtection
//     that guards every mutation does not apply. Origin verification
//     happens here, and same-origin is the only thing allowed: respondent
//     pages load no third-party anything (ADR-0006), and neither do ours.
//   - Cloud Run terminates connections at 60 minutes (ADR-0007), so a
//     connection is closed deliberately at 55 with a frame that says so,
//     and the browser client reconnects.
//   - Audio arrives as binary frames and must never grow without bound,
//     so the read limit is explicit rather than inherited.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Frame types, server to client. The client renders `chunk` text as it
// arrives, treats `done` as completion, and shows `error` as a message —
// with `code` letting it distinguish a refusal it can explain (a quota)
// from one it cannot.
const (
	FrameChunk  = "chunk"
	FrameDone   = "done"
	FrameError  = "error"
	FrameStatus = "status"
)

// Frame is one server→client message. One shape for every stream keeps
// the browser client generic: it is the same code for questions,
// transcripts and insights.
type Frame struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// Message is one client→server message: either a JSON control frame or a
// binary payload (audio).
type Message struct {
	Binary bool
	Data   []byte
	// Control is the decoded JSON of a text frame; zero for binary.
	Control Control
}

// Control is the client's side of the vocabulary. Handlers interpret
// Action; the rest is per-feature payload.
type Control struct {
	Action string `json:"action"`
	Lang   string `json:"lang,omitempty"`
	Text   string `json:"text,omitempty"`
}

// Options tune one connection. Zero values mean the defaults below,
// which are the right answer for every current caller.
type Options struct {
	// MaxMessageBytes bounds a single incoming message.
	MaxMessageBytes int64
	// MaxLifetime closes the connection after this long, under Cloud
	// Run's own 60-minute ceiling.
	MaxLifetime time.Duration
	// PingInterval keeps intermediaries from dropping an idle stream
	// (a model can think for a while before its first token).
	PingInterval time.Duration
}

const (
	defaultMaxMessageBytes = 1 << 20 // 1 MiB: far above a control frame, ample for one audio chunk
	defaultMaxLifetime     = 55 * time.Minute
	defaultPingInterval    = 30 * time.Second
)

// Conn is an accepted connection with its lifetime context.
type Conn struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

// Accept upgrades the request. The returned Conn must be Closed; its
// Context is cancelled when the connection ends for any reason,
// including the lifetime cap, so a streaming AI call bound to it stops
// costing money the moment the reader goes away.
func Accept(w http.ResponseWriter, r *http.Request, opts Options) (*Conn, error) {
	if opts.MaxMessageBytes <= 0 {
		opts.MaxMessageBytes = defaultMaxMessageBytes
	}
	if opts.MaxLifetime <= 0 {
		opts.MaxLifetime = defaultMaxLifetime
	}
	if opts.PingInterval <= 0 {
		opts.PingInterval = defaultPingInterval
	}

	// No OriginPatterns: the library then requires the Origin host to
	// equal the request host. Same-origin only, deliberately.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("ws: accept: %w", err)
	}
	conn.SetReadLimit(opts.MaxMessageBytes)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), opts.MaxLifetime)
	c := &Conn{conn: conn, ctx: ctx, cancel: cancel}
	go c.keepalive(opts.PingInterval)
	return c, nil
}

// Context is cancelled when the connection closes or the lifetime cap
// expires. Pass it to every call made on the connection's behalf.
func (c *Conn) Context() context.Context { return c.ctx }

func (c *Conn) keepalive(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			// Say goodbye rather than going silent: the browser client
			// treats a clean close as "reconnect", a stall as "broken".
			c.conn.Close(websocket.StatusGoingAway, "connection lifetime reached")
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// Receive reads the next client message. It returns io.EOF-shaped errors
// as ErrClosed so handlers can end quietly on a normal disconnect —
// a respondent closing a tab is not an incident.
func (c *Conn) Receive() (Message, error) {
	typ, data, err := c.conn.Read(c.ctx)
	if err != nil {
		if isClosed(err) {
			return Message{}, ErrClosed
		}
		return Message{}, fmt.Errorf("ws: read: %w", err)
	}
	if typ == websocket.MessageBinary {
		return Message{Binary: true, Data: data}, nil
	}
	var control Control
	if err := json.Unmarshal(data, &control); err != nil {
		return Message{}, fmt.Errorf("ws: decode control frame: %w", err)
	}
	return Message{Control: control}, nil
}

// Send writes one frame.
func (c *Conn) Send(f Frame) error {
	// Checked explicitly: a write can otherwise succeed into a buffer
	// after the lifetime cap has fired, and a caller streaming model
	// output would keep spending on a reader that is gone.
	if c.ctx.Err() != nil {
		return ErrClosed
	}
	payload, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("ws: encode frame: %w", err)
	}
	if err := c.conn.Write(c.ctx, websocket.MessageText, payload); err != nil {
		if isClosed(err) {
			return ErrClosed
		}
		return fmt.Errorf("ws: write: %w", err)
	}
	return nil
}

// Chunk, Status, Done and Fail are the vocabulary handlers actually use.
func (c *Conn) Chunk(text string) error  { return c.Send(Frame{Type: FrameChunk, Text: text}) }
func (c *Conn) Status(text string) error { return c.Send(Frame{Type: FrameStatus, Text: text}) }
func (c *Conn) Done() error              { return c.Send(Frame{Type: FrameDone}) }

// Fail reports a refusal the user can understand. code is a short
// machine-readable tag (for example "quota") the client may special-case;
// message is what a person reads, so it must never carry internals.
func (c *Conn) Fail(code, message string) error {
	return c.Send(Frame{Type: FrameError, Code: code, Error: message})
}

// Close ends the connection. Calling it twice is harmless.
func (c *Conn) Close() {
	c.cancel()
	c.conn.Close(websocket.StatusNormalClosure, "")
}

// ErrClosed marks an ordinary disconnect: the peer went away, or the
// lifetime cap fired. Handlers stop; they do not log an error.
var ErrClosed = errors.New("ws: connection closed")

func isClosed(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return true
	}
	return websocket.CloseStatus(err) != -1
}

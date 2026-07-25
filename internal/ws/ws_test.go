package ws_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/TryEarful/earful/internal/ws"
)

// serve boots an httptest server whose only route upgrades and hands the
// connection to fn.
func serve(t *testing.T, opts ws.Options, fn func(*ws.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, opts)
		if err != nil {
			return // Accept already answered the request
		}
		defer conn.Close()
		fn(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server, header http.Header) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func TestConn_CarriesControlFramesBinaryAndStreamedText(t *testing.T) {
	t.Parallel()
	srv := serve(t, ws.Options{}, func(conn *ws.Conn) {
		// One control frame, then one binary payload, then a stream back.
		msg, err := conn.Receive()
		if err != nil || msg.Binary || msg.Control.Action != "start" || msg.Control.Lang != "nl" {
			t.Errorf("first message = %+v (%v), want a start control frame in nl", msg, err)
		}
		msg, err = conn.Receive()
		if err != nil || !msg.Binary || string(msg.Data) != "audio-bytes" {
			t.Errorf("second message = %+v (%v), want binary audio", msg, err)
		}
		_ = conn.Status("transcribing")
		_ = conn.Chunk("hallo ")
		_ = conn.Chunk("daar")
		_ = conn.Done()
	})

	client := dial(t, srv, nil)
	ctx := context.Background()
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"action":"start","lang":"nl"}`)); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := client.Write(ctx, websocket.MessageBinary, []byte("audio-bytes")); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	var text strings.Builder
	for {
		_, data, err := client.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		frame := string(data)
		switch {
		case strings.Contains(frame, `"type":"status"`):
			continue
		case strings.Contains(frame, `"type":"done"`):
			if got := text.String(); got != "hallo daar" {
				t.Errorf("streamed text = %q, want %q", got, "hallo daar")
			}
			return
		case strings.Contains(frame, `"type":"chunk"`):
			// Crude on purpose: the wire format is what the browser sees.
			start := strings.Index(frame, `"text":"`) + len(`"text":"`)
			text.WriteString(frame[start : start+strings.Index(frame[start:], `"`)])
		default:
			t.Fatalf("unexpected frame: %s", frame)
		}
	}
}

// TestAccept_RejectsCrossOriginHandshakes is the reason this package
// exists: a handshake is a GET, so the stdlib cross-origin protection
// that guards mutations never sees it.
func TestAccept_RejectsCrossOriginHandshakes(t *testing.T) {
	t.Parallel()
	srv := serve(t, ws.Options{}, func(conn *ws.Conn) { _ = conn.Done() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("a cross-origin handshake was accepted")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAccept_EnforcesTheReadLimit(t *testing.T) {
	t.Parallel()
	srv := serve(t, ws.Options{MaxMessageBytes: 1024}, func(conn *ws.Conn) {
		if _, err := conn.Receive(); err == nil {
			t.Error("an oversized message was accepted")
		}
	})

	client := dial(t, srv, nil)
	ctx := context.Background()
	if err := client.Write(ctx, websocket.MessageBinary, make([]byte, 4096)); err != nil {
		// Some stacks surface the refusal on write; either is fine.
		return
	}
	if _, _, err := client.Read(ctx); err == nil {
		t.Error("the connection survived an oversized message")
	}
}

// TestAccept_ClosesAtTheLifetimeCap: Cloud Run cuts connections at 60
// minutes (ADR-0007). Ending deliberately, and telling the client, is
// what lets it reconnect instead of appearing to hang.
func TestAccept_ClosesAtTheLifetimeCap(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	srv := serve(t, ws.Options{MaxLifetime: 50 * time.Millisecond}, func(conn *ws.Conn) {
		<-conn.Context().Done()
		done <- conn.Chunk("too late")
	})

	dial(t, srv, nil)
	select {
	case err := <-done:
		if !errors.Is(err, ws.ErrClosed) {
			t.Errorf("send after the lifetime cap = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection outlived its lifetime cap")
	}
}

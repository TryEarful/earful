package http

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/TryEarful/earful/internal/logging"
)

// RequestLogger logs one structured line per request. It scrubs the
// request URL (query strings will carry magic-link tokens and, in invited
// surveys, participant identifiers) and never logs request/response bodies,
// so transcripts and answer content never reach the log stream.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("request",
				"method", r.Method,
				"path", logging.ScrubURL(r.URL),
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the writer underneath, so capabilities this wrapper does
// not itself implement stay reachable through http.ResponseController —
// notably hijacking, which a WebSocket upgrade needs (M5's voice socket,
// M6-T3's streamed questions). Without it the upgrade fails with a
// puzzling 501, which is exactly how this was found.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack forwards to the underlying writer. http.ResponseController would
// find it through Unwrap, but libraries commonly type-assert
// http.Hijacker directly, so the interface is implemented outright.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	w.status = http.StatusSwitchingProtocols
	return hijacker.Hijack()
}

// Flush forwards to the underlying writer for the same reason.
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Recover converts a panic in any downstream handler into a 500 response
// instead of crashing the server.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "error", rec)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

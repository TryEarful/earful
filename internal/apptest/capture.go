package apptest

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/TryEarful/earful/internal/email"
)

// CaptureDirEnv names a directory. When it is set, every HTML page the
// instance serves and every email it sends is also written there, one
// folder per test, so two runs of the suite can be compared page for
// page. It exists to move the interface's wording out of the templates
// without changing a character of what is rendered: the assertions in
// the suite check phrases, and a move can keep every phrase while
// losing the space between two of them.
const CaptureDirEnv = "EARFUL_CAPTURE_DIR"

// capture writes what one test's instance produced under dir.
type capture struct {
	dir string
	seq atomic.Int64
}

// newCapture returns nil when capturing is off, which is the usual case.
func newCapture(t *testing.T) *capture {
	t.Helper()
	root := os.Getenv(CaptureDirEnv)
	if root == "" {
		return nil
	}
	dir := filepath.Join(root, strings.ReplaceAll(t.Name(), "/", "__"))
	// A test may boot more than one instance; each gets its own folder.
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		dir = filepath.Join(root, strings.ReplaceAll(t.Name(), "/", "__")+fmt.Sprintf("__%d", n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("apptest: capture directory: %v", err)
	}
	return &capture{dir: dir}
}

// wrap records HTML responses on their way out. A request that upgrades
// the connection is passed through untouched: a socket is not a page.
func (c *capture) wrap(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		n := c.seq.Add(1)
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			return
		}
		head := fmt.Sprintf("<!-- %s %s %d -->\n", r.Method, r.URL.RequestURI(), rec.status)
		name := filepath.Join(c.dir, fmt.Sprintf("%04d.html", n))
		_ = os.WriteFile(name, append([]byte(head), rec.body.Bytes()...), 0o644)
	})
}

// emails writes the outbox once the test is over.
func (c *capture) emails(outbox *email.Capture) {
	if c == nil {
		return
	}
	var b bytes.Buffer
	for _, msg := range outbox.All() {
		fmt.Fprintf(&b, "To: %s\nSubject: %s\n\n%s\n\n----\n", msg.To, msg.Subject, msg.Text)
	}
	if b.Len() > 0 {
		_ = os.WriteFile(filepath.Join(c.dir, "emails.txt"), b.Bytes(), 0o644)
	}
}

type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

// Unwrap keeps flushing and the rest reachable through
// http.ResponseController.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

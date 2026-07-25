package ai

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// sseStream turns a server-sent-event body into text fragments. Both
// backends that stream — the OpenAI-compatible one and Vertex — speak
// SSE and differ only in the JSON inside each `data:` line, so the
// framing lives here once and the decoder is supplied per backend.
type sseStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	// decode returns the text carried by one data payload. io.EOF ends
	// the stream; any other error fails it. An empty fragment with a nil
	// error means "nothing to deliver from this frame" (keep-alives,
	// role-only deltas, safety metadata).
	decode func(data []byte) (string, error)
}

// maxSSELine bounds one event payload. bufio.Scanner's 64 KB default is
// too small for a model that emits a long block in a single frame, which
// would otherwise surface as a truncated answer rather than an error.
const maxSSELine = 4 << 20

func newSSEStream(body io.ReadCloser, decode func(data []byte) (string, error)) *sseStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELine)
	return &sseStream{body: body, scanner: scanner, decode: decode}
}

func (s *sseStream) Recv() (string, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		fragment, err := s.decode([]byte(data))
		if err != nil {
			return "", err
		}
		if fragment != "" {
			return fragment, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return "", fmt.Errorf("ai: read stream: %w", err)
	}
	return "", io.EOF
}

func (s *sseStream) Close() error { return s.body.Close() }

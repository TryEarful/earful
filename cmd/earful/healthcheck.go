package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/TryEarful/earful/internal/config"
)

// runHealthcheck probes the local serve process's /healthz. It exists for
// container healthchecks: the runtime image is distroless (no shell, no
// curl), so the compose healthcheck execs the binary itself.
func runHealthcheck(ctx context.Context, _ []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthz returned %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

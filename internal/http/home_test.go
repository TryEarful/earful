package http_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

func TestHome_RendersPlaceholderPage(t *testing.T) {
	dsn := apptest.NewDB(t)
	srv := apptest.NewServer(t, dsn)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Earful") {
		t.Errorf("body does not contain %q:\n%s", "Earful", body)
	}
}

func TestStatic_ServesCSS(t *testing.T) {
	dsn := apptest.NewDB(t)
	srv := apptest.NewServer(t, dsn)

	resp, err := http.Get(srv.URL + "/static/css/app.css")
	if err != nil {
		t.Fatalf("GET /static/css/app.css: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

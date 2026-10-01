package attach

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VirusTotal looks a file up by its SHA-256 in VirusTotal's file
// reports (API v3). Only the hash is sent, never the file: a file it has
// not seen before is unknown to it, which this treats as not malicious.
// It is off unless VIRUSTOTAL_API_KEY is set, so an instance makes this
// third-party call only when its operator chose to.
type VirusTotal struct {
	APIKey string
	// BaseURL overrides the API origin (tests point it at a stub).
	BaseURL string
	// Client defaults to one with Timeout as its deadline.
	Client *http.Client
}

// virusTotalTimeout bounds one lookup. The creator is waiting on the
// upload, and a lookup that fails does not block it (see Prepare), so a
// slow answer is worth less than a prompt one.
const virusTotalTimeout = 5 * time.Second

// Malicious reports whether any engine in the file's last analysis
// flagged it as malicious.
func (v *VirusTotal) Malicious(ctx context.Context, sum [sha256.Size]byte) (bool, error) {
	base := v.BaseURL
	if base == "" {
		base = "https://www.virustotal.com"
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: virusTotalTimeout}
	}
	ctx, cancel := context.WithTimeout(ctx, virusTotalTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(base, "/")+"/api/v3/files/"+hex.EncodeToString(sum[:]), nil)
	if err != nil {
		return false, fmt.Errorf("attach: virustotal request: %w", err)
	}
	req.Header.Set("x-apikey", v.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("attach: virustotal lookup: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil // never seen: nothing known against it
	default:
		return false, fmt.Errorf("attach: virustotal lookup: status %d", resp.StatusCode)
	}
	var report struct {
		Data struct {
			Attributes struct {
				Stats struct {
					Malicious int `json:"malicious"`
				} `json:"last_analysis_stats"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&report); err != nil {
		return false, fmt.Errorf("attach: virustotal report: %w", err)
	}
	return report.Data.Attributes.Stats.Malicious > 0, nil
}

package http

import (
	"net/http"
	"net/url"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/geoip"
	"github.com/TryEarful/earful/web/templates"
)

// The trust page (M8-T4). Public, no session, and served by the
// application rather than only by the marketing site — a self-hoster
// gets it with the software, and a respondent following the link from a
// survey lands on the trust page of the instance that actually holds
// their answer.
//
// The processor list is PLAN.md Appendix B, and it is conditional on
// purpose: an instance that sends no email through Brevo does not list
// Brevo, because listing a processor you do not use is as misleading as
// omitting one you do.

func (s *server) trustPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, templates.Trust(templates.TrustData{
		InstanceName:      instanceName(s.cfg),
		Region:            s.hostingRegion(),
		ContactEmail:      s.contactEmail(),
		Processors:        s.processors(),
		GeoAttribution:    geoip.Attribution,
		GeoAttributionURL: geoip.AttributionURL,
	}))
}

// instanceName is what a reader should call this deployment: its own
// host, not a brand, because a self-hosted instance is not us.
func instanceName(cfg config.Config) string {
	if parsed, err := url.Parse(cfg.BaseURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return "this instance"
}

func (s *server) hostingRegion() string {
	if s.cfg.Env == config.EnvProduction || s.cfg.Env == config.EnvStaging {
		return "Google Cloud, europe-west4 (Netherlands)"
	}
	return "wherever this instance is running"
}

func (s *server) contactEmail() string {
	if s.cfg.EmailFrom != "" {
		return s.cfg.EmailFrom
	}
	return "support@tryearful.com"
}

// processors lists exactly the companies this deployment actually
// involves.
func (s *server) processors() []templates.ProcessorView {
	var out []templates.ProcessorView

	if s.cfg.Env == config.EnvProduction || s.cfg.Env == config.EnvStaging {
		out = append(out, templates.ProcessorView{
			Name:    "Google Cloud",
			Purpose: "Hosting: the application, the database, backups and logs",
			Data:    "Everything the service holds",
			Region:  "europe-west4",
		})
	}
	if s.cfg.EmailSender == "brevo" {
		out = append(out, templates.ProcessorView{
			Name:    "Brevo",
			Purpose: "Sending sign-in links and survey invitations",
			Data:    "Email addresses of account holders and invited participants",
			Region:  "EU (France)",
		})
	}
	switch s.cfg.AIProvider {
	case "vertex":
		out = append(out, templates.ProcessorView{
			Name:    "Google Vertex AI",
			Purpose: "Transcribing spoken answers, drafting questions, summaries and translations",
			Data:    "Audio in transit (never stored), question and answer text",
			Region:  s.cfg.VertexLocation,
		})
	case "openai":
		out = append(out, templates.ProcessorView{
			Name:    "Self-configured AI backend",
			Purpose: "Transcription, drafting, summaries and translations",
			Data:    "Audio in transit (never stored), question and answer text",
			Region:  "Wherever this instance's operator points it",
		})
	}
	if s.cfg.GoogleLoginEnabled() {
		out = append(out, templates.ProcessorView{
			Name:    "Google Identity",
			Purpose: "Signing in, only for people who choose Google",
			Data:    "Email address and Google account id",
			Region:  "Global",
		})
	}
	if len(out) == 0 {
		out = append(out, templates.ProcessorView{
			Name:    "Nobody",
			Purpose: "This instance runs entirely on its operator's own infrastructure",
			Data:    "—",
			Region:  "—",
		})
	}
	return out
}

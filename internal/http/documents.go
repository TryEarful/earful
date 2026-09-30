package http

import (
	"net/http"
	"net/url"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/geoip"
	"github.com/TryEarful/earful/internal/pages"
	"github.com/TryEarful/earful/web/templates"
)

// Documents: the trust page, the terms, the help pages. They are public,
// with no session, and served by the application rather than only by the
// marketing site — a self-hoster gets them with the software, and a
// respondent following a link from a survey lands on the trust page of
// the instance that actually holds their answer.
//
// What a document says about the instance comes from pageFacts, and is
// conditional on purpose: an instance that sends no email through Brevo
// does not list Brevo, because listing a processor you do not use is as
// misleading as omitting one you do.

// pageFacts is what this instance can truthfully say about itself. A
// fact that was not configured is empty, and the documents leave out
// the sentence that would have stated it.
func pageFacts(cfg config.Config) pages.Facts {
	hosted := cfg.Env == config.EnvProduction || cfg.Env == config.EnvStaging
	brevo := cfg.EmailSender == "brevo"
	ai := ""
	if cfg.AIProvider == "vertex" || cfg.AIProvider == "openai" {
		ai = cfg.AIProvider
	}
	google := cfg.GoogleLoginEnabled()
	return pages.Facts{
		"Instance":     instanceName(cfg),
		"Region":       cfg.HostingRegion,
		"ContactEmail": contactEmail(cfg),
		// The companies involved, each only if this instance uses it.
		"GoogleCloud": hosted,
		"Brevo":       brevo,
		"AI":          ai,
		// The configured location, not a claim: ADR-0011 and ADR-0013
		// keep every call pinned there, and the page would change if
		// that ever stopped being true.
		"VertexLocation":    cfg.VertexLocation,
		"GoogleLogin":       google,
		"NoProcessors":      !hosted && !brevo && ai == "" && !google,
		"GeoAttribution":    geoip.Attribution,
		"GeoAttributionURL": geoip.AttributionURL,
	}
}

// instanceName is what a reader should call this deployment: its own
// host, not a brand: a self-hosted instance is not the SaaS. It is empty
// when the instance has no host to be called by.
func instanceName(cfg config.Config) string {
	if parsed, err := url.Parse(cfg.BaseURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return ""
}

// contactEmail answers where data-subject requests go, or "" when no
// address is configured. Naming an unrelated address would route an
// erasure request to somebody with no relationship to the data and no
// power to act on it, which is worse for the person asking than an
// instance admitting it has published no contact.
func contactEmail(cfg config.Config) string {
	if cfg.ContactEmail != "" {
		return cfg.ContactEmail
	}
	// EMAIL_FROM's default is a placeholder rather than a monitored
	// address, so it does not qualify as a contact.
	if cfg.EmailFrom != "" && cfg.EmailFrom != "earful@localhost" {
		return cfg.EmailFrom
	}
	return ""
}

// registerDocuments gives every document its address, and the same
// address ending in .md for the document as Markdown. A draft has an
// address only in development, where its writer can read it.
func (s *server) registerDocuments(mux *http.ServeMux) {
	for _, address := range s.pages.Addresses() {
		if page, _ := s.pages.Page(address, pages.Source); page.Draft && s.cfg.Env != config.EnvDevelopment {
			continue
		}
		mux.HandleFunc("GET /"+address, s.document(address))
		mux.HandleFunc("GET /"+address+".md", s.documentMarkdown(address))
	}
}

func (s *server) document(address string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := s.pages.Page(address, interfaceLanguage(r))
		data := templates.DocumentData{
			Title:        page.Title,
			ShortTitle:   page.ShortTitle,
			Lang:         page.Lang,
			HTML:         page.HTML,
			Markdown:     page.Markdown,
			MarkdownPath: "/" + address + ".md",
		}
		// A reader who arrived from the signed in pages is offered the
		// way back. The cookie is only looked at, not checked: a stale one
		// leads to the sign in page, which is where its owner belongs.
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			data.Back = "/dashboard"
		}
		if !page.LastUpdate.IsZero() {
			data.Updated = text(r).Day(page.LastUpdate)
		}
		render(w, r, http.StatusOK, templates.Document(data))
	}
}

// documentMarkdown serves the document as its writer wrote it, with this
// instance's facts in it: what a reader keeps a copy of, or hands to
// another program to read.
func (s *server) documentMarkdown(address string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := s.pages.Page(address, interfaceLanguage(r))
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Language", page.Lang)
		_, _ = w.Write([]byte(page.Markdown))
	}
}

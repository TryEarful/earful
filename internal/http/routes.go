package http

import (
	"net/http"

	"github.com/TryEarful/earful/web/static"
)

func (s *server) registerRoutes(mux *http.ServeMux) {
	// Public.
	mux.Handle("GET /{$}", homeHandler())
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	mux.HandleFunc("GET /healthz", s.healthz)
	// Same check at /health: Google's front end intercepts /healthz on
	// *.run.app URLs (returns its own 404 before the app ever sees the
	// request), so anything probing through the public URL — the Cloud
	// Monitoring uptime check, the deploy smoke — uses /health instead.
	// Container-level probes bypass the GFE and keep using /healthz.
	mux.HandleFunc("GET /health", s.healthz)
	mux.HandleFunc("GET /goodbye", s.goodbye)
	mux.HandleFunc("GET /robots.txt", robotsTxt)
	// The trust page (M8-T4): public, and served by the instance that
	// actually holds the data it describes.
	mux.HandleFunc("GET /trust", s.trustPage)

	// Respondent path (M4). No session, no workspace: the share link is
	// the credential.
	mux.HandleFunc("GET /s/{surveyID}", s.respondPage)
	mux.HandleFunc("POST /s/{surveyID}", s.respondSubmit)
	mux.HandleFunc("GET /s/{surveyID}/challenge", s.respondChallenge)
	// Voice (M5-T2): a WebSocket carries audio up and the transcript back.
	// Audio is held in memory for one request and never stored (ADR-0004).
	mux.HandleFunc("GET /s/{surveyID}/voice", s.voiceSocket)
	// Personal invite links (M4-T3): the token is the credential.
	mux.HandleFunc("GET /p/{token}", s.participantRespondPage)
	mux.HandleFunc("POST /p/{token}", s.participantRespondSubmit)
	mux.HandleFunc("GET /p/{token}/voice", s.participantVoiceSocket)
	// ESP events (M4-T6); a wrong or absent secret is a plain 404.
	mux.HandleFunc("POST /webhooks/email/{secret}", s.emailWebhook)

	// Auth flows (pre-session; cross-origin mutations are rejected by the
	// stdlib CrossOriginProtection layer in NewHandler).
	mux.HandleFunc("GET /login", s.loginPage)
	// M12 private beta: password login (works whenever an account holds a
	// password) and invite-code signup (404 outside beta mode).
	mux.HandleFunc("POST /login", s.passwordLogin)
	mux.HandleFunc("GET /signup", s.signupPage)
	mux.HandleFunc("POST /signup", s.signupSubmit)
	mux.HandleFunc("POST /auth/magic/request", s.magicRequest)
	mux.HandleFunc("GET /auth/magic/verify", s.magicVerifyPage)
	mux.HandleFunc("POST /auth/magic/verify", s.magicVerifyConsume)
	mux.HandleFunc("GET /auth/google/start", s.googleStart)
	mux.HandleFunc("GET /auth/google/callback", s.googleCallback)

	// Authenticated, workspace-scoped. Mutations additionally require the
	// per-session CSRF token.
	get := func(pattern string, h http.HandlerFunc) {
		mux.Handle("GET "+pattern, s.requireAuth(h))
	}
	post := func(pattern string, h http.HandlerFunc) {
		mux.Handle("POST "+pattern, s.requireAuth(s.requireCSRF(h)))
	}

	get("/dashboard", s.surveyList)
	get("/account", s.accountPage)
	post("/account/delete", s.accountDelete)
	post("/account/email", s.accountEmail)
	// Workspace export (M7-T3): the "leave anytime" promise. The download
	// needs a session in the owning workspace, so the link is not a
	// bearer capability — and it expires anyway.
	post("/account/export", s.accountExport)
	get("/exports/{jobID}", s.exportDownload)
	post("/logout", s.logout)

	// Super-admin surface (M12): invite-code management and password
	// resets. Non-admins get a 404 from requireSuperAdmin — the pages
	// don't exist as far as they can tell.
	mux.Handle("GET /admin/beta-codes", s.requireAuth(s.requireSuperAdmin(http.HandlerFunc(s.adminBetaCodesPage))))
	mux.Handle("POST /admin/beta-codes", s.requireAuth(s.requireCSRF(s.requireSuperAdmin(http.HandlerFunc(s.adminBetaCodesMint)))))
	mux.Handle("POST /admin/beta-codes/revoke", s.requireAuth(s.requireCSRF(s.requireSuperAdmin(http.HandlerFunc(s.adminBetaCodesRevoke)))))
	// Founder metrics (M9-T7): our own numbers, super-admin only.
	mux.Handle("GET /admin/metrics", s.requireAuth(s.requireSuperAdmin(http.HandlerFunc(s.adminMetricsPage))))
	// Erasure fast-path (M8-T3): look up, then confirm.
	mux.Handle("GET /admin/erasure", s.requireAuth(s.requireSuperAdmin(http.HandlerFunc(s.adminErasurePage))))
	mux.Handle("POST /admin/erasure", s.requireAuth(s.requireCSRF(s.requireSuperAdmin(http.HandlerFunc(s.adminErasureRun)))))
	mux.Handle("POST /admin/reset-password", s.requireAuth(s.requireCSRF(s.requireSuperAdmin(http.HandlerFunc(s.adminResetPassword)))))

	// Survey building (M3). Every handler resolves the survey through the
	// session's workspace, so a survey from another workspace is
	// indistinguishable from one that does not exist.
	get("/surveys", s.surveyList)
	get("/surveys/new", s.newSurveyPage)
	post("/surveys", s.surveyCreate)
	get("/surveys/{surveyID}", s.surveyPage)
	get("/surveys/{surveyID}/audit", s.surveyAudit)
	// Results and exports (M7). Both read the same fold-by-identity view.
	get("/surveys/{surveyID}/results", s.surveyResults)
	get("/surveys/{surveyID}/results.csv", s.resultsCSV)
	post("/surveys/{surveyID}/responses/{responseID}/delete", s.responseDelete)
	// Insight Summaries (M10). The POST is the whole feature; the socket
	// streams the same run as it is written.
	post("/surveys/{surveyID}/answers/translate", s.answersTranslate)
	post("/surveys/{surveyID}/insights", s.surveyInsights)
	mux.Handle("GET /surveys/{surveyID}/insights/stream", s.requireAuth(http.HandlerFunc(s.surveyInsightsSocket)))
	// Preview renders the draft through the real respondent renderer
	// (M3-T6). Its POST writes nothing at all.
	get("/surveys/{surveyID}/preview", s.previewPage)
	post("/surveys/{surveyID}/preview", s.previewSubmit)
	post("/surveys/{surveyID}/settings", s.surveySettings)
	post("/surveys/{surveyID}/publish", s.surveyPublish)
	post("/surveys/{surveyID}/close", s.surveyClose)
	post("/surveys/{surveyID}/reopen", s.surveyReopen)
	post("/surveys/{surveyID}/delete", s.surveyDelete)
	post("/surveys/{surveyID}/participants", s.participantsImport)
	post("/surveys/{surveyID}/participants/send", s.participantsSend)
	// AI-drafted questions (M6-T3). The POST is the whole feature; the
	// socket is the same operation with the output visible as it arrives.
	post("/surveys/{surveyID}/generate", s.surveyGenerate)
	mux.Handle("GET /surveys/{surveyID}/generate/stream", s.requireAuth(http.HandlerFunc(s.surveyGenerateSocket)))
	// Localization (M11-T1): drafted by AI, reviewed by a person, frozen
	// into the version at publish.
	get("/surveys/{surveyID}/localizations", s.localizationsPage)
	post("/surveys/{surveyID}/localizations", s.localizationAdd)
	post("/surveys/{surveyID}/localizations/{lang}", s.localizationSave)
	post("/surveys/{surveyID}/localizations/{lang}/draft", s.localizationDraft)
	post("/surveys/{surveyID}/localizations/{lang}/delete", s.localizationRemove)
	post("/surveys/{surveyID}/questions", s.questionAdd)
	post("/surveys/{surveyID}/questions/{questionID}", s.questionUpdate)
	post("/surveys/{surveyID}/questions/{questionID}/delete", s.questionDelete)
	post("/surveys/{surveyID}/questions/{questionID}/move", s.questionMove)
}

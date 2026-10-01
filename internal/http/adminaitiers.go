package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// AI tiers (issue #3). A workspace's tier chooses which daily token cap
// the meter applies to it. Only a super admin changes it, here, by
// finding the workspace through a member's address as the other support
// tools find an account. The global € breaker applies to every tier, so
// no tier is a way past the instance's budget.

func (s *server) adminAITiersPage(w http.ResponseWriter, r *http.Request) {
	data := templates.AITiersData{}
	if r.URL.Query().Get("notice") == "saved" {
		data.Notice = say(r, "admin.tiers.saved")
	}
	s.renderAITiers(w, r, http.StatusOK, strings.TrimSpace(r.URL.Query().Get("email")), data)
}

func (s *server) adminAITiersSet(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	address := strings.TrimSpace(r.PostFormValue("email"))

	tier, ok := ai.ParseTier(r.PostFormValue("tier"))
	workspaceID, err := uuid.Parse(r.PostFormValue("workspace"))
	if !ok || err != nil {
		s.renderAITiers(w, r, http.StatusUnprocessableEntity, address,
			templates.AITiersData{Error: say(r, "admin.tiers.error.invalid")})
		return
	}

	before, err := s.surveys.WorkspaceAITier(r.Context(), workspaceID)
	if err == nil {
		err = s.surveys.SetWorkspaceAITier(r.Context(), workspaceID, string(tier))
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderAITiers(w, r, http.StatusUnprocessableEntity, address,
			templates.AITiersData{Error: say(r, "admin.tiers.error.workspace")})
		return
	case err != nil:
		s.internalError(w, r, "set workspace ai tier", err)
		return
	}
	// The record of who changed an allowance, and from what. Ids only:
	// the workspace's name and its members' addresses stay out of logs.
	s.logger.Info("workspace ai tier changed",
		"workspace", workspaceID, "from", before, "to", string(tier), "by", info.UserID)

	http.Redirect(w, r, "/admin/ai-tiers?"+url.Values{
		"email": {address}, "notice": {"saved"},
	}.Encode(), http.StatusSeeOther)
}

// renderAITiers looks up the account's workspaces, if an address was
// given, and renders the page around whatever data the caller set.
func (s *server) renderAITiers(w http.ResponseWriter, r *http.Request, status int, address string, data templates.AITiersData) {
	info, _ := authFrom(r.Context())
	loc := text(r)
	caps := s.aiMeter.DailyTokens
	for _, tier := range ai.Tiers {
		data.Options = append(data.Options, templates.AITierOption{
			Value: string(tier),
			Label: say(r, "admin.tiers.option", uitext.Args{
				"Tier": loc.T(tierMessage(tier)), "Cap": loc.Count(caps.For(tier)),
			}),
		})
	}

	data.Email = address
	if address != "" {
		workspaces, err := s.surveys.WorkspacesForAITier(r.Context(), strings.ToLower(address))
		if err != nil {
			s.internalError(w, r, "list workspaces for ai tier", err)
			return
		}
		data.Searched = true
		for _, ws := range workspaces {
			usage, err := s.aiMeter.Usage(r.Context(), ws.ID)
			if err != nil {
				s.internalError(w, r, "read workspace ai usage", err)
				return
			}
			data.Workspaces = append(data.Workspaces, templates.AITierWorkspace{
				ID:   ws.ID.String(),
				Name: ws.Name,
				Tier: string(usage.Tier),
				Usage: say(r, "admin.tiers.usage", uitext.Args{
					"Tokens": loc.Count(usage.Tokens), "Cap": loc.Count(usage.Cap),
				}),
			})
		}
	}
	render(w, r, status, templates.AdminAITiers(info.Email, info.CSRFToken, data))
}

// tierMessage names the message that says a tier's name.
func tierMessage(tier ai.Tier) uitext.ID {
	switch tier {
	case ai.TierLowNormal:
		return "ai.tier.low_normal"
	case ai.TierHigh:
		return "ai.tier.high"
	default:
		return "ai.tier.normal"
	}
}

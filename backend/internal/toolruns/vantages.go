package toolruns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// VantageView is one entry of GET /tools/vantages.
type VantageView struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`
}

// Why a vantage is not ready.
const (
	ReasonServerDisabled     = "server_disabled"
	ReasonAgentTooOld        = "agent_too_old"
	ReasonToolsOff           = "tools_off"
	ReasonNotEnabledOnServer = "not_enabled_on_server"
	ReasonOffline            = "offline"
)

// reasonText is the refusal message for a vantage that is not ready.
func reasonText(reason, name string) string {
	switch reason {
	case ReasonServerDisabled:
		return "runs from the Sentinel server are switched off"
	case ReasonAgentTooOld:
		return "This agent's version can't run tools — update it"
	case ReasonToolsOff:
		return fmt.Sprintf("network tools are switched off for %s in Sentinel", name)
	case ReasonNotEnabledOnServer:
		return fmt.Sprintf("%s doesn't have ENABLE_TOOLS=true in its config", name)
	case ReasonOffline:
		return fmt.Sprintf("%s hasn't asked for jobs in the last minute (offline?)", name)
	}
	return reason
}

// agentReason is why a cannot run tools now, or "" when it can. Order:
// too old (never reported tools_local), switched off in Sentinel, not
// enabled on the agent's host, no poll within ReadyWindow.
func (s *Service) agentReason(a *models.Agent, now time.Time) string {
	switch {
	case a.ToolsLocal == nil:
		return ReasonAgentTooOld
	case !a.ToolsEnabled:
		return ReasonToolsOff
	case !*a.ToolsLocal:
		return ReasonNotEnabledOnServer
	}
	if last, ok := s.polls.lastSeen(a.ID); !ok || now.Sub(last) > ReadyWindow {
		return ReasonOffline
	}
	return ""
}

// Vantages lists the Sentinel server first, then every agent by name, each
// with whether it can run tools now and, when not, why.
func (s *Service) Vantages(ctx context.Context) ([]VantageView, error) {
	settings := LoadSettings(ctx, s.settings)
	out := []VantageView{{Kind: models.VantageSentinel, Name: sentinelVantageName, Ready: settings.ServerEnabled}}
	if !settings.ServerEnabled {
		out[0].Reason = ReasonServerDisabled
	}
	var agents []models.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	sort.Slice(agents, func(i, j int) bool {
		a, b := strings.ToLower(agents[i].Name), strings.ToLower(agents[j].Name)
		if a != b {
			return a < b
		}
		return agents[i].AgentID < agents[j].AgentID
	})
	now := s.now()
	for i := range agents {
		reason := s.agentReason(&agents[i], now)
		out = append(out, VantageView{
			Kind: models.VantageAgent, AgentID: agents[i].AgentID, Name: agents[i].Name,
			Ready: reason == "", Reason: reason,
		})
	}
	return out, nil
}

// AllowlistEmpty reports whether no target is allowed yet, for the Tools
// page's banner (grant holders cannot read the admin settings).
func (s *Service) AllowlistEmpty(ctx context.Context) bool {
	allow, _ := nettools.ParseAllowlist(LoadSettings(ctx, s.settings).Allowlist)
	return allow.Empty()
}

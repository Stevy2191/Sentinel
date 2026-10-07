package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// netToolsAdmin serves what only admins may change about network tools: the
// settings (allowlist, the Sentinel vantage, retention), the per-user grant
// and the per-agent switch. Every change is audited.
type netToolsAdmin struct {
	settings *services.SettingsService
	auth     *services.AuthService
	agents   *services.AgentService
	audit    auditRecorder
}

// netToolsToggle is the body of the grant and agent-switch routes.
type netToolsToggle struct {
	Enabled *bool `json:"enabled"`
}

// RegisterNetToolsAdminRoutes mounts the admin routes on the admin group
// (RequireAdmin is the caller's).
func RegisterNetToolsAdminRoutes(admin *gin.RouterGroup, settings *services.SettingsService, auth *services.AuthService, agents *services.AgentService, audit auditRecorder) {
	h := &netToolsAdmin{settings: settings, auth: auth, agents: agents, audit: audit}
	admin.GET("/settings/net-tools", h.getSettings)
	admin.PUT("/settings/net-tools", h.putSettings)
	admin.PATCH("/users/:id/net-tools", h.setUserGrant)
	admin.PUT("/agents/:agent_id/tools", h.setAgentTools)
}

// getSettings handles GET /settings/net-tools.
func (h *netToolsAdmin) getSettings(c *gin.Context) {
	respondSuccess(c, http.StatusOK, toolruns.LoadSettings(c.Request.Context(), h.settings))
}

// putSettings handles PUT /settings/net-tools. Bad allowlist entries are
// each reported (422 invalid_allowlist with "entries") and nothing is saved.
// Saving never touches runs already in progress.
func (h *netToolsAdmin) putSettings(c *gin.Context) {
	var req toolruns.Settings
	if err := c.ShouldBindJSON(&req); err != nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return
	}
	ctx := c.Request.Context()
	before := toolruns.LoadSettings(ctx, h.settings)
	saved, err := toolruns.SaveSettings(ctx, h.settings, req)
	if err != nil {
		var bad *toolruns.SettingsError
		switch {
		case !errors.As(err, &bad):
			respondInternal(c, "saving network tools settings", err)
		case len(bad.Entries) > 0:
			c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
				"success": false,
				"error":   gin.H{"code": codeInvalidAllowlist, "message": bad.Message},
				"entries": bad.Entries,
			})
		default:
			respondToolError(c, http.StatusUnprocessableEntity, toolruns.CodeInvalidParams, bad.Message)
		}
		return
	}
	h.record(c, models.ActionNetToolsSettingsUpdated, models.ResourceSettings, nil, models.AuditChanges{
		Before: netToolsSettingsAudit(before),
		After:  netToolsSettingsAudit(saved),
	})
	respondSuccess(c, http.StatusOK, saved)
}

func netToolsSettingsAudit(s toolruns.Settings) map[string]any {
	return map[string]any{"allowlist": s.Allowlist, "server_enabled": s.ServerEnabled, "retention_days": s.RetentionDays}
}

// setUserGrant handles PATCH /users/:id/net-tools. The flag is stored for an
// admin too, though an admin may always use the tools: it then says what was
// chosen should the account be demoted.
func (h *netToolsAdmin) setUserGrant(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such user")
		return
	}
	enabled, ok := bindNetToolsToggle(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	before, err := h.auth.GetUserByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such user")
		return
	}
	if err != nil {
		respondInternal(c, "reading a user", err)
		return
	}
	updated, err := h.auth.SetNetTools(ctx, id, enabled)
	if err != nil {
		respondInternal(c, "changing a network tools grant", err)
		return
	}
	h.record(c, models.ActionUserNetToolsChanged, models.ResourceUser, &id, models.AuditChanges{
		Before:  map[string]any{"net_tools": before.NetTools},
		After:   map[string]any{"net_tools": updated.NetTools},
		Summary: map[string]any{"username": updated.Username},
	})
	respondSuccess(c, http.StatusOK, gin.H{"id": updated.ID, "net_tools": updated.NetTools})
}

// setAgentTools handles PUT /agents/:agent_id/tools: Sentinel's half of the
// agent opt-in. The agent comes back with its token hidden.
func (h *netToolsAdmin) setAgentTools(c *gin.Context) {
	enabled, ok := bindNetToolsToggle(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	before, err := h.agents.Get(ctx, c.Param("agent_id"))
	if err != nil {
		respondToolAgentError(c, err)
		return
	}
	updated, err := h.agents.SetToolsEnabled(ctx, before.AgentID, enabled)
	if err != nil {
		respondToolAgentError(c, err)
		return
	}
	h.record(c, models.ActionAgentToolsChanged, models.ResourceAgent, &updated.ID, models.AuditChanges{
		Before:  map[string]any{"tools_enabled": before.ToolsEnabled},
		After:   map[string]any{"tools_enabled": updated.ToolsEnabled},
		Summary: map[string]any{"agent_id": updated.AgentID, "name": updated.Name},
	})
	updated.HideToken()
	respondSuccess(c, http.StatusOK, updated)
}

// respondToolAgentError answers a missing agent as 404 not_found.
func respondToolAgentError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAgentNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such agent")
		return
	}
	respondInternal(c, "switching network tools on an agent", err)
}

// bindNetToolsToggle reads {"enabled": true|false}; enabled is required.
func bindNetToolsToggle(c *gin.Context) (bool, bool) {
	var req netToolsToggle
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "enabled (true or false) is required")
		return false, false
	}
	return *req.Enabled, true
}

// record writes one audit entry as the signed-in admin.
func (h *netToolsAdmin) record(c *gin.Context, action, resource string, id *uuid.UUID, changes models.AuditChanges) {
	if h.audit == nil {
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), action, resource, id, changes)
}

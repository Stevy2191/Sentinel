package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// netToolsAdminKey holds the admin flag RequireNetTools read from the
// database, so a tool handler judges "admin" by the account as it is now
// rather than by the token's claim, which can be a day old.
const netToolsAdminKey = "net_tools_is_admin"

// netToolsRefusedKey marks a request RequireNetTools turned away, for
// auditToolRefusal.
const netToolsRefusedKey = "net_tools_refused"

// Codes the tool endpoints answer beyond the toolruns ones.
const (
	codeInvalidAllowlist = "invalid_allowlist"
	codeToolsDisabled    = "tools_disabled"
)

const msgNetToolsRequired = "network tools access required"

// respondToolError writes the coded error envelope of the tool endpoints,
// {"success": false, "error": {"code", "message"}}, and aborts, so it is safe
// in middleware as well as in handlers.
func respondToolError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": false,
		"error":   gin.H{"code": code, "message": message},
	})
}

// RequireNetTools lets admins and users granted network tools through, and
// answers everyone else 403 forbidden. Mount it after AuthMiddleware.
//
// The account is re-read on every request, as RequireAdmin does: a token is
// valid for a day, and a grant an admin removes must stop working at once.
// The admin flag read here is stored under netToolsAdminKey.
func RequireNetTools(users adminChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			refuseNetTools(c)
			return
		}
		user, err := users.GetUserByID(c.Request.Context(), userID)
		if err != nil {
			// The grant cannot be confirmed, so the request does not go on:
			// failing open here would defeat the check.
			log.Printf("[tools] access check for %s failed: %v", userID, err)
			refuseNetTools(c)
			return
		}
		if !user.IsAdmin && !user.NetTools {
			refuseNetTools(c)
			return
		}
		c.Set(netToolsAdminKey, user.IsAdmin)
		c.Next()
	}
}

func refuseNetTools(c *gin.Context) {
	c.Set(netToolsRefusedKey, true)
	respondToolError(c, http.StatusForbidden, toolruns.CodeForbidden, msgNetToolsRequired)
}

// auditToolRefusal records a run request that RequireNetTools refused: the
// spec audits runs "refused for permission reasons". It is mounted ahead of
// the guard on POST /tools/runs only, so refused reads cannot flood the log.
func auditToolRefusal(audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if audit == nil || !c.GetBool(netToolsRefusedKey) {
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionToolRunRefused, models.ResourceToolRun, nil,
			models.AuditChanges{Summary: map[string]any{"reason": msgNetToolsRequired}})
	}
}

// requesterFrom is who is asking, for the tool-run service. IsAdmin is the
// flag RequireNetTools read from the database.
func requesterFrom(c *gin.Context) toolruns.Requester {
	userID, username, _, _ := GetUserFromContext(c)
	return toolruns.Requester{
		UserID:   userID,
		Username: username,
		IsAdmin:  c.GetBool(netToolsAdminKey),
		IP:       c.ClientIP(),
	}
}

// writeRefusal answers a *toolruns.Refusal in the coded shape and reports
// whether err was one.
func writeRefusal(c *gin.Context, err error) bool {
	var r *toolruns.Refusal
	if !errors.As(err, &r) {
		return false
	}
	respondToolError(c, r.Status, r.Code, r.Message)
	return true
}

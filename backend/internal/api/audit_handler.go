package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// ListAuditLogHandler handles GET /api/v1/audit-log.
//
// Admin-only. The log records who acted on what across every account, so
// exposing it to ordinary users would leak exactly the cross-tenant activity
// the rest of the API is careful to keep separate.
//
// Filters: action, resource_type, resource_id, user_id, limit, offset.
func ListAuditLogHandler(audit *services.AuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := services.AuditFilter{
			Action:       c.Query("action"),
			ResourceType: c.Query("resource_type"),
		}
		if v := c.Query("resource_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				respondError(c, http.StatusBadRequest, "invalid resource_id")
				return
			}
			f.ResourceID = &id
		}
		if v := c.Query("user_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				respondError(c, http.StatusBadRequest, "invalid user_id")
				return
			}
			f.UserID = &id
		}
		if v, err := strconv.Atoi(c.Query("limit")); err == nil {
			f.Limit = v
		}
		if v, err := strconv.Atoi(c.Query("offset")); err == nil && v > 0 {
			f.Offset = v
		}

		entries, total, err := audit.List(c.Request.Context(), f)
		if err != nil {
			respondInternal(c, "listing audit log", err)
			return
		}

		c.Header("X-Total-Count", strconv.FormatInt(total, 10))
		respondSuccess(c, http.StatusOK, entries)
	}
}

// RegisterAuditRoutes mounts the audit log on an admin-gated group.
func RegisterAuditRoutes(admin *gin.RouterGroup, audit *services.AuditService) {
	admin.GET("/audit-log", ListAuditLogHandler(audit))
}

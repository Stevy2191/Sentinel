package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// parseShareUserID reads and validates the :user_id URL parameter.
func parseShareUserID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid user_id: must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// requireMonitorOwner ensures the current user owns the monitor (admins bypass).
// It writes the response and returns (currentUserID, false) on denial.
func requireMonitorOwner(c *gin.Context, ms *services.MonitorService, monitorID uuid.UUID) (uuid.UUID, bool) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return uuid.Nil, false
	}
	if isAdmin {
		return userID, true
	}
	owns, err := ms.IsMonitorOwner(c.Request.Context(), userID, monitorID)
	if err != nil {
		respondError(c, classifyServiceError(err), err.Error())
		return uuid.Nil, false
	}
	if !owns {
		respondError(c, http.StatusForbidden, "you don't own this monitor")
		return uuid.Nil, false
	}
	return userID, true
}

type shareMonitorRequest struct {
	UserID     uuid.UUID `json:"user_id"`
	Permission string    `json:"permission"`
}

type updateShareRequest struct {
	Permission string `json:"permission"`
}

// ShareMonitorHandler handles POST /api/v1/monitors/:id/share.
func ShareMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		monitorID, ok := parseMonitorID(c)
		if !ok {
			return
		}
		currentUser, ok := requireMonitorOwner(c, monitorService, monitorID)
		if !ok {
			return
		}
		var req shareMonitorRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.UserID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "user_id is required")
			return
		}
		if req.Permission == "" {
			req.Permission = models.PermissionReadonly
		}
		if err := models.ValidateSharePermission(req.Permission); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		share, err := monitorService.ShareMonitor(c.Request.Context(), monitorID, req.UserID, currentUser, req.Permission)
		if err != nil {
			if errors.Is(err, services.ErrShareExists) {
				respondError(c, http.StatusConflict, err.Error())
				return
			}
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, share)
	}
}

// UpdateMonitorShareHandler handles PATCH /api/v1/monitors/:id/share/:user_id.
func UpdateMonitorShareHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		monitorID, ok := parseMonitorID(c)
		if !ok {
			return
		}
		targetUser, ok := parseShareUserID(c)
		if !ok {
			return
		}
		if _, ok := requireMonitorOwner(c, monitorService, monitorID); !ok {
			return
		}
		var req updateShareRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := models.ValidateSharePermission(req.Permission); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if err := monitorService.UpdateMonitorShare(c.Request.Context(), monitorID, targetUser, req.Permission); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "share updated"})
	}
}

// RevokeMonitorShareHandler handles DELETE /api/v1/monitors/:id/share/:user_id.
func RevokeMonitorShareHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		monitorID, ok := parseMonitorID(c)
		if !ok {
			return
		}
		targetUser, ok := parseShareUserID(c)
		if !ok {
			return
		}
		if _, ok := requireMonitorOwner(c, monitorService, monitorID); !ok {
			return
		}
		if err := monitorService.RevokeMonitorShare(c.Request.Context(), monitorID, targetUser); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "monitor unshared"})
	}
}

// GetMonitorSharesHandler handles GET /api/v1/monitors/:id/shares.
func GetMonitorSharesHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		monitorID, ok := parseMonitorID(c)
		if !ok {
			return
		}
		if _, ok := requireMonitorOwner(c, monitorService, monitorID); !ok {
			return
		}
		shares, err := monitorService.GetMonitorShares(c.Request.Context(), monitorID)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, shares)
	}
}

// ListUsersHandler handles GET /api/v1/users, returning id/username/email for
// the share picker (any authenticated user).
func ListUsersHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		users, err := authService.ListUsers(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]gin.H, 0, len(users))
		for _, u := range users {
			out = append(out, gin.H{
				"id": u.ID, "username": u.Username, "email": u.Email,
				"role": u.Role, "is_admin": u.IsAdmin, "created_at": u.CreatedAt,
			})
		}
		respondSuccess(c, http.StatusOK, out)
	}
}

// RegisterMonitorSharingRoutes mounts the sharing + users endpoints.
func RegisterMonitorSharingRoutes(rg *gin.RouterGroup, monitorService *services.MonitorService, authService *services.AuthService) {
	monitors := rg.Group("/monitors")
	monitors.GET("/:id/shares", GetMonitorSharesHandler(monitorService))
	monitors.POST("/:id/share", ShareMonitorHandler(monitorService))
	monitors.PATCH("/:id/share/:user_id", UpdateMonitorShareHandler(monitorService))
	monitors.DELETE("/:id/share/:user_id", RevokeMonitorShareHandler(monitorService))

	rg.GET("/users", ListUsersHandler(authService))
}

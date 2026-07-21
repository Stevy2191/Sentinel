package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type inviteUserRequest struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	SendEmail bool   `json:"send_email"`
}

// InviteUserHandler handles POST /api/v1/invitations (admin).
func InviteUserHandler(invitationService *services.InvitationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, username, _, _ := GetUserFromContext(c)
		var req inviteUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		inv, err := invitationService.CreateInvitation(c.Request.Context(), req.Email, req.Role, userID)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		resp := gin.H{
			"id": inv.ID, "email": inv.Email, "role": inv.Role,
			"accepted": inv.Accepted, "expires_at": inv.ExpiresAt,
		}
		if req.SendEmail {
			if err := invitationService.SendInvitationEmail(inv, username); err != nil {
				resp["email_warning"] = err.Error()
			} else {
				resp["email_sent"] = true
			}
		}
		respondSuccess(c, http.StatusCreated, resp)
	}
}

// ResendInvitationEmailHandler handles POST /api/v1/invitations/resend-email/:id (admin).
func ResendInvitationEmailHandler(invitationService *services.InvitationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, username, _, _ := GetUserFromContext(c)
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid invitation id: must be a UUID")
			return
		}
		// Resend only applies to still-pending invitations.
		invites, err := invitationService.ListPendingInvitations(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range invites {
			if invites[i].ID != id {
				continue
			}
			if err := invitationService.SendInvitationEmail(&invites[i], username); err != nil {
				if errors.Is(err, services.ErrEmailNotConfigured) {
					respondError(c, http.StatusBadRequest, err.Error())
					return
				}
				respondError(c, http.StatusInternalServerError, err.Error())
				return
			}
			respondSuccess(c, http.StatusOK, gin.H{"message": "invitation email resent"})
			return
		}
		respondError(c, http.StatusBadRequest, "invitation not found, already accepted, or expired")
	}
}

// ListPendingInvitationsHandler handles GET /api/v1/invitations/pending (admin).
func ListPendingInvitationsHandler(invitationService *services.InvitationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		invites, err := invitationService.ListPendingInvitations(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]gin.H, 0, len(invites))
		for _, inv := range invites {
			out = append(out, gin.H{
				"id": inv.ID, "email": inv.Email, "role": inv.Role,
				"invited_by_user_id": inv.InvitedByUserID,
				"expires_at":         inv.ExpiresAt, "created_at": inv.CreatedAt,
			})
		}
		respondSuccess(c, http.StatusOK, out)
	}
}

// GetInvitationDetailsHandler handles GET /api/v1/invitations/:token (public).
func GetInvitationDetailsHandler(invitationService *services.InvitationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		inv, err := invitationService.GetInvitationByToken(c.Request.Context(), c.Param("token"))
		if err != nil {
			respondError(c, http.StatusNotFound, "invitation not found")
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"email": inv.Email, "role": inv.Role,
			"expires_at": inv.ExpiresAt,
			"expired":    inv.IsExpired(),
			"accepted":   inv.Accepted,
		})
	}
}

type acceptInvitationRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AcceptInvitationHandler handles POST /api/v1/invitations/:token/accept (public).
func AcceptInvitationHandler(invitationService *services.InvitationService, authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req acceptInvitationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		user, err := invitationService.AcceptInvitation(c.Request.Context(), c.Param("token"), req.Username, req.Password)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		// Issue a token so the new user is logged in immediately.
		token, err := authService.GenerateJWT(user.ID, user.Username, user.IsAdmin)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "user created but could not issue token")
			return
		}
		respondSuccess(c, http.StatusCreated, gin.H{
			"token":    token,
			"user_id":  user.ID,
			"username": user.Username,
			"is_admin": user.IsAdmin,
			"role":     user.Role,
		})
	}
}

// RegisterInvitationRoutes mounts admin invitation routes on the admin group and
// public accept/details routes on the router.
func RegisterInvitationRoutes(admin *gin.RouterGroup, router *gin.Engine, invitationService *services.InvitationService, authService *services.AuthService) {
	inv := admin.Group("/invitations")
	inv.POST("", InviteUserHandler(invitationService))
	inv.GET("/pending", ListPendingInvitationsHandler(invitationService))
	inv.POST("/resend-email/:id", ResendInvitationEmailHandler(invitationService))

	public := router.Group("/api/v1/invitations")
	public.GET("/:token", GetInvitationDetailsHandler(invitationService))
	public.POST("/:token/accept", AcceptInvitationHandler(invitationService, authService))
}

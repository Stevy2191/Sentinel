package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// parseUserPathID reads and validates the :id URL parameter as a user UUID.
func parseUserPathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid user id: must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

type createUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type autoPasswordUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

// CreateUserHandler handles POST /api/v1/users (admin).
func CreateUserHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Role == "" {
			req.Role = "user"
		}
		user, err := authService.CreateManagedUser(c.Request.Context(), req.Username, req.Email, req.Password, req.Role)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusCreated, gin.H{
			"id": user.ID, "username": user.Username, "email": user.Email, "role": user.Role,
		})
	}
}

// CreateUserWithAutoPasswordHandler handles POST /api/v1/users/auto-password (admin).
func CreateUserWithAutoPasswordHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req autoPasswordUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Role == "" {
			req.Role = "user"
		}
		user, pw, err := authService.CreateManagedUserAutoPassword(c.Request.Context(), req.Username, req.Email, req.Role)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusCreated, gin.H{
			"user":               gin.H{"id": user.ID, "username": user.Username, "email": user.Email, "role": user.Role},
			"temporary_password": pw,
		})
	}
}

// DeleteUserHandler handles DELETE /api/v1/users/:id (admin, not self).
func DeleteUserHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUserPathID(c)
		if !ok {
			return
		}
		currentUser, _, _, _ := GetUserFromContext(c)
		if id == currentUser {
			respondError(c, http.StatusBadRequest, "you cannot delete your own account")
			return
		}
		if err := authService.DeleteUser(c.Request.Context(), id); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "user deleted"})
	}
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// ResetPasswordHandler handles POST /api/v1/users/:id/reset-password (admin).
func ResetPasswordHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUserPathID(c)
		if !ok {
			return
		}
		var req resetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := authService.ResetUserPassword(c.Request.Context(), id, req.NewPassword); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "password reset"})
	}
}

// ResetPasswordAutoHandler handles POST /api/v1/users/:id/reset-password-auto (admin).
func ResetPasswordAutoHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUserPathID(c)
		if !ok {
			return
		}
		pw, err := authService.ResetUserPasswordAuto(c.Request.Context(), id)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"temporary_password": pw})
	}
}

type changeRoleRequest struct {
	Role string `json:"role"`
}

// ChangeUserRoleHandler handles PATCH /api/v1/users/:id/role (admin, not self).
func ChangeUserRoleHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUserPathID(c)
		if !ok {
			return
		}
		currentUser, _, _, _ := GetUserFromContext(c)
		if id == currentUser {
			respondError(c, http.StatusBadRequest, "you cannot change your own role")
			return
		}
		var req changeRoleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := authService.ChangeUserRole(c.Request.Context(), id, req.Role); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "role": req.Role})
	}
}

type changeOwnPasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangeOwnPasswordHandler handles POST /api/v1/auth/change-password (any auth).
func ChangeOwnPasswordHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		var req changeOwnPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := authService.ChangePassword(c.Request.Context(), userID, req.OldPassword, req.NewPassword); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "password changed"})
	}
}

// RegisterUserManagementRoutes mounts admin user-management routes (the group is
// admin-gated by the caller) plus the self change-password route.
func RegisterUserManagementRoutes(admin *gin.RouterGroup, authService *services.AuthService) {
	users := admin.Group("/users")
	users.POST("", CreateUserHandler(authService))
	users.POST("/auto-password", CreateUserWithAutoPasswordHandler(authService))
	users.DELETE("/:id", DeleteUserHandler(authService))
	users.POST("/:id/reset-password", ResetPasswordHandler(authService))
	users.POST("/:id/reset-password-auto", ResetPasswordAutoHandler(authService))
	users.PATCH("/:id/role", ChangeUserRoleHandler(authService))
}

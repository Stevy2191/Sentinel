package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// respondAuthError writes the nested auth error envelope.
func respondAuthError(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{
		"success": false,
		"error":   gin.H{"code": code, "message": message},
	})
}

// ---- Request bodies ----

type registerRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type verifyMFARequest struct {
	MFAToken string `json:"mfa_token"`
	TOTPCode string `json:"totp_code"`
}

type totpRequest struct {
	TOTPCode string `json:"totp_code"`
}

// RegisterHandler handles POST /api/v1/auth/register. The first account created
// becomes an admin.
func RegisterHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAuthError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			respondAuthError(c, http.StatusBadRequest, "username and password are required")
			return
		}
		if req.Password != req.PasswordConfirm {
			respondAuthError(c, http.StatusBadRequest, "passwords do not match")
			return
		}

		ctx := c.Request.Context()
		hasUsers, err := authService.HasAnyUser(ctx)
		if err != nil {
			respondAuthError(c, http.StatusInternalServerError, err.Error())
			return
		}
		isAdmin := !hasUsers

		user, err := authService.CreateUser(ctx, req.Username, req.Password, isAdmin)
		if err != nil {
			respondAuthError(c, http.StatusBadRequest, err.Error())
			return
		}

		log.Printf("User registered: %s (admin=%t)", user.Username, isAdmin)
		respondSuccess(c, http.StatusCreated, gin.H{
			"user_id":  user.ID,
			"username": user.Username,
			"is_admin": user.IsAdmin,
			"message":  "User created successfully",
		})
	}
}

// LoginHandler handles POST /api/v1/auth/login.
func LoginHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAuthError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		log.Printf("User login attempt: %s", req.Username)

		ctx := c.Request.Context()
		user, err := authService.VerifyPassword(ctx, req.Username, req.Password)
		if err != nil {
			respondAuthError(c, http.StatusUnauthorized, "Invalid credentials")
			return
		}

		if user.MFAEnabled {
			mfaToken, err := authService.GenerateMFAToken(user.ID, user.Username)
			if err != nil {
				respondAuthError(c, http.StatusInternalServerError, "could not start MFA challenge")
				return
			}
			respondSuccess(c, http.StatusOK, gin.H{
				"mfa_required": true,
				"mfa_token":    mfaToken,
			})
			return
		}

		token, err := authService.GenerateJWT(user.ID, user.Username, user.IsAdmin)
		if err != nil {
			respondAuthError(c, http.StatusInternalServerError, "could not issue token")
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"token":       token,
			"user_id":     user.ID,
			"username":    user.Username,
			"is_admin":    user.IsAdmin,
			"mfa_enabled": false,
		})
	}
}

// VerifyMFAHandler handles POST /api/v1/auth/mfa/verify. It accepts either a
// TOTP code or a backup code.
func VerifyMFAHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req verifyMFARequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAuthError(c, http.StatusBadRequest, "invalid request body")
			return
		}

		ctx := c.Request.Context()
		userID, username, err := authService.VerifyMFAToken(req.MFAToken)
		if err != nil {
			respondAuthError(c, http.StatusUnauthorized, "MFA session expired")
			return
		}

		ok, _ := authService.VerifyMFA(ctx, userID, req.TOTPCode)
		if !ok {
			// Fall back to a one-time backup code.
			ok, _ = authService.VerifyMFABackupCode(ctx, userID, req.TOTPCode)
		}
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "Invalid TOTP code or backup code")
			return
		}

		user, err := authService.GetUserByID(ctx, userID)
		if err != nil {
			respondAuthError(c, http.StatusUnauthorized, "user not found")
			return
		}
		token, err := authService.GenerateJWT(userID, username, user.IsAdmin)
		if err != nil {
			respondAuthError(c, http.StatusInternalServerError, "could not issue token")
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"token":    token,
			"user_id":  userID,
			"username": username,
		})
	}
}

// SetupMFAHandler handles POST /api/v1/auth/mfa/setup (protected).
func SetupMFAHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := c.Request.Context()

		secret, qrURL, err := authService.SetupMFA(ctx, userID)
		if err != nil {
			respondAuthError(c, http.StatusInternalServerError, err.Error())
			return
		}
		// Backup codes are generated during setup; return them once for the user
		// to record.
		user, err := authService.GetUserByID(ctx, userID)
		if err != nil {
			respondAuthError(c, http.StatusInternalServerError, err.Error())
			return
		}

		log.Printf("MFA setup initiated for: %s", userID)
		respondSuccess(c, http.StatusOK, gin.H{
			"secret":       secret,
			"qr_code_url":  qrURL,
			"backup_codes": []string(user.MFABackupCodes),
		})
	}
}

// ConfirmMFAHandler handles POST /api/v1/auth/mfa/confirm (protected).
func ConfirmMFAHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		var req totpRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAuthError(c, http.StatusBadRequest, "invalid request body")
			return
		}

		valid, err := authService.VerifyMFA(c.Request.Context(), userID, req.TOTPCode)
		if err != nil {
			respondAuthError(c, http.StatusBadRequest, err.Error())
			return
		}
		if !valid {
			respondAuthError(c, http.StatusUnauthorized, "Invalid TOTP code")
			return
		}

		log.Printf("MFA confirmed for: %s", userID)
		respondSuccess(c, http.StatusOK, gin.H{"message": "MFA confirmed and enabled"})
	}
}

// DisableMFAHandler handles POST /api/v1/auth/mfa/disable (protected). TOTP
// verification is required to disable.
func DisableMFAHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		var req totpRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAuthError(c, http.StatusBadRequest, "invalid request body")
			return
		}

		ctx := c.Request.Context()
		valid, err := authService.VerifyMFA(ctx, userID, req.TOTPCode)
		if err != nil {
			respondAuthError(c, http.StatusBadRequest, err.Error())
			return
		}
		if !valid {
			respondAuthError(c, http.StatusUnauthorized, "Invalid TOTP code")
			return
		}
		if err := authService.DisableMFA(ctx, userID); err != nil {
			respondAuthError(c, http.StatusInternalServerError, err.Error())
			return
		}

		log.Printf("MFA disabled for: %s", userID)
		respondSuccess(c, http.StatusOK, gin.H{"message": "MFA disabled"})
	}
}

// LogoutHandler handles POST /api/v1/auth/logout (protected). JWTs are
// stateless, so this is a no-op kept for client convenience.
func LogoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if userID, _, _, ok := GetUserFromContext(c); ok {
			log.Printf("User logged out: %s", userID)
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "Logged out successfully"})
	}
}

// GetCurrentUserHandler handles GET /api/v1/auth/me (protected).
func GetCurrentUserHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		user, err := authService.GetUserByID(c.Request.Context(), userID)
		if err != nil {
			respondAuthError(c, http.StatusNotFound, "user not found")
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"user_id":     user.ID,
			"username":    user.Username,
			"is_admin":    user.IsAdmin,
			"mfa_enabled": user.MFAEnabled,
			"last_login":  user.LastLogin,
		})
	}
}

// AuthMiddleware validates the Bearer JWT and stores the user in the context.
func AuthMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": http.StatusUnauthorized, "message": "missing or malformed Authorization header"},
			})
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		claims, err := authService.VerifyJWT(tokenString)
		if err != nil {
			log.Printf("[auth] failed auth from %s: %v", c.ClientIP(), err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": http.StatusUnauthorized, "message": "invalid or expired token"},
			})
			return
		}

		userIDStr, _ := claims["user_id"].(string)
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": http.StatusUnauthorized, "message": "invalid token subject"},
			})
			return
		}

		c.Set("user_id", userID)
		c.Set("username", claims["username"])
		c.Set("is_admin", claims["is_admin"])
		c.Next()
	}
}

// GetUserFromContext returns the authenticated user's id, username, and admin
// flag from the gin context (set by AuthMiddleware).
func GetUserFromContext(c *gin.Context) (userID uuid.UUID, username string, isAdmin bool, ok bool) {
	idVal, ok1 := c.Get("user_id")
	nameVal, ok2 := c.Get("username")
	adminVal, ok3 := c.Get("is_admin")
	if !ok1 || !ok2 || !ok3 {
		return uuid.Nil, "", false, false
	}
	id, _ := idVal.(uuid.UUID)
	name, _ := nameVal.(string)
	admin, _ := adminVal.(bool)
	return id, name, admin, true
}

// RegisterAuthRoutes mounts the auth endpoints: register/login/mfa-verify are
// public; the rest require a valid JWT.
func RegisterAuthRoutes(router *gin.Engine, authService *services.AuthService) {
	public := router.Group("/api/v1/auth")
	public.POST("/register", RegisterHandler(authService))
	public.POST("/login", LoginHandler(authService))
	public.POST("/mfa/verify", VerifyMFAHandler(authService))

	protected := router.Group("/api/v1/auth")
	protected.Use(AuthMiddleware(authService))
	protected.POST("/mfa/setup", SetupMFAHandler(authService))
	protected.POST("/mfa/confirm", ConfirmMFAHandler(authService))
	protected.POST("/mfa/disable", DisableMFAHandler(authService))
	protected.POST("/logout", LogoutHandler())
	protected.GET("/me", GetCurrentUserHandler(authService))
}

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// sslIDParam parses and validates the :id path parameter.
func sslIDParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "certificate id must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// createSSLRequest is the body for POST /ssl-certificates.
type createSSLRequest struct {
	Domain string `json:"domain"`
	// Nil takes the default week's warning.
	ExpiryNotificationDays *int  `json:"expiry_notification_days"`
	Enabled                *bool `json:"enabled"`
	// Which channels expiry warnings go to. Omitted or null means every
	// enabled channel; an empty list means none.
	NotifyChannels *[]string `json:"notify_channels"`
}

// CreateSSLCertificateHandler handles POST /api/v1/ssl-certificates. It stores
// the domain and reads its certificate immediately, so the row never sits at
// "unknown" waiting for the next daily sweep.
func CreateSSLCertificateHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createSSLRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}

		domain := models.NormalizeDomain(req.Domain)
		if err := models.ValidateDomain(domain); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		notifyDays := models.DefaultExpiryNotificationDays
		if req.ExpiryNotificationDays != nil {
			notifyDays = *req.ExpiryNotificationDays
			if notifyDays < models.MinExpiryNotificationDays || notifyDays > models.MaxExpiryNotificationDays {
				respondError(c, http.StatusBadRequest, fmt.Sprintf(
					"expiry_notification_days must be between %d and %d",
					models.MinExpiryNotificationDays, models.MaxExpiryNotificationDays))
				return
			}
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		cert := &models.SSLCertificate{
			Domain:                 domain,
			ExpiryNotificationDays: notifyDays,
			Enabled:                enabled,
			NotifyChannels:         normalizeChannelIDs(req.NotifyChannels),
		}
		if err := svc.Create(c.Request.Context(), cert); err != nil {
			// A duplicate domain is the caller's mistake, not a server fault.
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusCreated, cert)
	}
}

// normalizeChannelIDs cleans a submitted channel selection.
//
// nil in, nil out, and the distinction matters: null means "every channel",
// while an empty list means "none". Collapsing the two would silently turn a
// deliberate opt-out into alerts everywhere.
func normalizeChannelIDs(raw *[]string) models.StringSlice {
	if raw == nil {
		return nil
	}
	out := make(models.StringSlice, 0, len(*raw))
	seen := make(map[string]bool, len(*raw))
	for _, id := range *raw {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ListSSLCertificatesHandler handles GET /api/v1/ssl-certificates.
func ListSSLCertificatesHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		certs, err := svc.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "ListSSLCertificatesHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, certs)
	}
}

// GetSSLCertificateHandler handles GET /api/v1/ssl-certificates/:id.
func GetSSLCertificateHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sslIDParam(c)
		if !ok {
			return
		}
		cert, err := svc.Get(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, services.ErrSSLCertNotFound) {
				respondError(c, http.StatusNotFound, "no such certificate")
				return
			}
			respondInternal(c, "GetSSLCertificateHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, cert)
	}
}

// updateSSLRequest is the body for PATCH /ssl-certificates/:id. Only these two
// fields are the operator's to change; domain and check_interval are fixed.
type updateSSLRequest struct {
	ExpiryNotificationDays *int  `json:"expiry_notification_days"`
	Enabled                *bool `json:"enabled"`
	// Present only so a client that echoes the whole row back gets told rather
	// than silently having its change ignored.
	Domain         *string   `json:"domain"`
	CheckInterval  *int      `json:"check_interval"`
	NotifyChannels *[]string `json:"notify_channels"`
}

// UpdateSSLCertificateHandler handles PATCH /api/v1/ssl-certificates/:id.
func UpdateSSLCertificateHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sslIDParam(c)
		if !ok {
			return
		}
		var req updateSSLRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		// Refusing loudly beats accepting and ignoring: a caller that thinks it
		// renamed a domain should find out here, not from stale data later.
		if req.Domain != nil {
			respondError(c, http.StatusBadRequest,
				"domain cannot be changed; delete the entry and add the new domain")
			return
		}
		if req.CheckInterval != nil && *req.CheckInterval != models.SSLCheckIntervalSeconds {
			respondError(c, http.StatusBadRequest, "check_interval is fixed at one day and cannot be changed")
			return
		}

		current, err := svc.Get(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, services.ErrSSLCertNotFound) {
				respondError(c, http.StatusNotFound, "no such certificate")
				return
			}
			respondInternal(c, "UpdateSSLCertificateHandler", err)
			return
		}

		notifyDays := current.ExpiryNotificationDays
		if req.ExpiryNotificationDays != nil {
			notifyDays = *req.ExpiryNotificationDays
			if notifyDays < models.MinExpiryNotificationDays || notifyDays > models.MaxExpiryNotificationDays {
				respondError(c, http.StatusBadRequest, fmt.Sprintf(
					"expiry_notification_days must be between %d and %d",
					models.MinExpiryNotificationDays, models.MaxExpiryNotificationDays))
				return
			}
		}
		enabled := current.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		channels := current.NotifyChannels
		if req.NotifyChannels != nil {
			channels = normalizeChannelIDs(req.NotifyChannels)
		}

		updated, err := svc.Update(c.Request.Context(), id, notifyDays, enabled, channels)
		if err != nil {
			respondInternal(c, "UpdateSSLCertificateHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, updated)
	}
}

// DeleteSSLCertificateHandler handles DELETE /api/v1/ssl-certificates/:id.
func DeleteSSLCertificateHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sslIDParam(c)
		if !ok {
			return
		}
		if err := svc.Delete(c.Request.Context(), id); err != nil {
			if errors.Is(err, services.ErrSSLCertNotFound) {
				respondError(c, http.StatusNotFound, "no such certificate")
				return
			}
			respondInternal(c, "DeleteSSLCertificateHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "Certificate monitoring removed"})
	}
}

// CheckSSLCertificateNowHandler handles POST /api/v1/ssl-certificates/:id/check-now.
func CheckSSLCertificateNowHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sslIDParam(c)
		if !ok {
			return
		}
		cert, err := svc.Get(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, services.ErrSSLCertNotFound) {
				respondError(c, http.StatusNotFound, "no such certificate")
				return
			}
			respondInternal(c, "CheckSSLCertificateNowHandler", err)
			return
		}

		// Both clocks, so "Check now" means the whole row and not just its
		// certificate half.
		//
		// A failed read is a result, not a request failure: the row records why,
		// and the caller gets the updated certificate either way.
		checkErr, regErr := svc.Refresh(c.Request.Context(), cert)
		respondSuccess(c, http.StatusOK, gin.H{
			"certificate":        cert,
			"check_error":        errMessage(checkErr),
			"registration_error": errMessage(regErr),
		})
	}
}

// errMessage renders an error for a JSON body, or nil when there was none.
func errMessage(err error) *string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	return &msg
}

// CheckAllSSLCertificatesHandler handles POST /api/v1/ssl-certificates/check-all.
func CheckAllSSLCertificatesHandler(svc *services.SSLCheckerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		checked, failed, err := svc.CheckAll(c.Request.Context())
		if err != nil {
			respondInternal(c, "CheckAllSSLCertificatesHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"checked": checked,
			"failed":  failed,
			"message": fmt.Sprintf("Checked %d certificate(s), %d could not be read", checked, failed),
		})
	}
}

// RegisterSSLCertificateRoutes mounts the certificate endpoints. Reading is
// open to any authenticated user, as with monitors; changing what the instance
// watches is admin-only.
func RegisterSSLCertificateRoutes(rg *gin.RouterGroup, svc *services.SSLCheckerService, users adminChecker) {
	g := rg.Group("/ssl-certificates")
	g.GET("", ListSSLCertificatesHandler(svc))
	// Registered before "/:id" so "check-all" is not read as an id.
	g.POST("/check-all", RequireAdmin(users), CheckAllSSLCertificatesHandler(svc))
	g.GET("/:id", GetSSLCertificateHandler(svc))

	admin := g.Group("", RequireAdmin(users))
	admin.POST("", CreateSSLCertificateHandler(svc))
	admin.PATCH("/:id", UpdateSSLCertificateHandler(svc))
	admin.DELETE("/:id", DeleteSSLCertificateHandler(svc))
	admin.POST("/:id/check-now", CheckSSLCertificateNowHandler(svc))
}

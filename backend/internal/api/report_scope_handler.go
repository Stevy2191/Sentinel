package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
)

// This file holds the Metrics report's scope checks: the create-time check
// GenerateReport runs and POST /reports/scope-preview, which the editor
// calls as the user picks ports, devices and sites.

// NetworkScopes checks and sizes Metrics report scopes as a given user.
// netreport.Builder implements it; tests use a fake.
type NetworkScopes interface {
	ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error
	Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*netreport.Preview, error)
}

// SetNetworkScopes wires the Metrics scope checker in after construction.
func (h *ReportBuilder) SetNetworkScopes(n NetworkScopes) {
	h.scopes = n
}

// errScopesNotWired is a wiring mistake, reported as an internal error.
var errScopesNotWired = errors.New("network scope checker not wired")

// scopePreviewRequest is the body of POST /reports/scope-preview: a Metrics
// scope as the editor holds it. scope_data.metrics is ignored.
type scopePreviewRequest struct {
	ScopeType string             `json:"scope_type" binding:"required,oneof=ports port_roles devices sites"`
	ScopeData models.ReportScope `json:"scope_data"`
}

// checkNetworkScope runs a Metrics report's create-time scope check as its
// creator: every port, device and site it names must be one they can see,
// and a hidden one gets the same answer as a missing one. It writes the
// response and returns false when the request must stop. Other report types
// pass straight through.
func (h *ReportBuilder) checkNetworkScope(c *gin.Context, userID uuid.UUID, report *models.Report) bool {
	if report.ReportType != models.ReportTypeMetrics {
		return true
	}
	if h.scopes == nil {
		respondInternal(c, "checking report scope", errScopesNotWired)
		return false
	}
	if err := h.scopes.ValidateScope(c.Request.Context(), userID, report.ScopeType, report.ScopeData); err != nil {
		respondScopeError(c, "checking report scope", err)
		return false
	}
	return true
}

// ScopePreview handles POST /api/v1/reports/scope-preview. It sizes a
// Metrics scope as the caller ("Covers 214 ports right now") and lists the
// metrics it offers, with the defaults the editor pre-fills.
func (h *ReportBuilder) ScopePreview(c *gin.Context) {
	var req scopePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	userID, _, _, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.scopes == nil {
		respondInternal(c, "previewing report scope", errScopesNotWired)
		return
	}
	preview, err := h.scopes.Preview(c.Request.Context(), userID, req.ScopeType, req.ScopeData)
	if err != nil {
		respondScopeError(c, "previewing report scope", err)
		return
	}
	respondSuccess(c, http.StatusOK, preview)
}

// respondScopeError answers a scope problem the user can fix with 400 and
// its message, naming the field for the editor, and anything else as an
// internal error whose text stays in the server log.
func respondScopeError(c *gin.Context, op string, err error) {
	var fe *netreport.FieldError
	if errors.As(err, &fe) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fe.Message, "field": fe.Field})
		return
	}
	respondInternal(c, op, err)
}

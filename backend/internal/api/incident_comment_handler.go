package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type incidentCommentRequest struct {
	Body string `json:"body" binding:"required"`
}

// loadIncidentForComment resolves the incident in the URL and checks the caller
// may act on it at the given level.
//
// Comments are gated on the monitor rather than the incident, because that is
// where access actually lives: someone who can see a monitor can read its
// incident thread, and someone who can edit it can write to it.
func loadIncidentForComment(
	c *gin.Context,
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
	level string,
) (uuid.UUID, bool) {
	incidentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "incident id must be a UUID")
		return uuid.Nil, false
	}
	row, err := incidentService.GetIncidentByID(c.Request.Context(), incidentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "no such incident")
			return uuid.Nil, false
		}
		respondInternal(c, "loadIncidentForComment", err)
		return uuid.Nil, false
	}
	if !authorizeMonitor(c, monitorService, row.Incident.MonitorID, level) {
		return uuid.Nil, false
	}
	return incidentID, true
}

// ListIncidentCommentsHandler handles GET /api/v1/incidents/:id/comments.
func ListIncidentCommentsHandler(
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		incidentID, ok := loadIncidentForComment(c, incidentService, monitorService, "view")
		if !ok {
			return
		}
		comments, err := incidentService.ListComments(c.Request.Context(), incidentID)
		if err != nil {
			respondInternal(c, "ListIncidentCommentsHandler", err)
			return
		}
		if comments == nil {
			comments = []models.IncidentComment{}
		}
		respondSuccess(c, http.StatusOK, gin.H{"comments": comments})
	}
}

// AddIncidentCommentHandler handles POST /api/v1/incidents/:id/comments.
func AddIncidentCommentHandler(
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		incidentID, ok := loadIncidentForComment(c, incidentService, monitorService, "edit")
		if !ok {
			return
		}
		var req incidentCommentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "a comment body is required")
			return
		}
		userID, username, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}

		comment := &models.IncidentComment{
			IncidentID: incidentID,
			UserID:     &userID,
			AuthorName: username,
			Body:       req.Body,
		}
		if err := incidentService.AddComment(c.Request.Context(), comment); err != nil {
			// A rejected body is the caller's mistake, not a server fault.
			if strings.Contains(err.Error(), "comment") {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			respondInternal(c, "AddIncidentCommentHandler", err)
			return
		}
		respondSuccess(c, http.StatusCreated, gin.H{"comment": comment})
	}
}

// authorizeCommentAuthor allows the author or an admin, and nobody else.
//
// Edit access to the monitor is deliberately not enough: a comment is a
// statement by a person, and letting a colleague silently rewrite it would make
// the thread untrustworthy as a record.
func authorizeCommentAuthor(c *gin.Context, comment *models.IncidentComment) bool {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return false
	}
	if isAdmin {
		return true
	}
	if comment.UserID != nil && *comment.UserID == userID {
		return true
	}
	respondError(c, http.StatusForbidden, "only the author can change their own comment")
	return false
}

// loadOwnComment resolves the comment in the URL and checks it belongs to the
// incident and that the caller may change it.
func loadOwnComment(
	c *gin.Context,
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
) (*models.IncidentComment, bool) {
	incidentID, ok := loadIncidentForComment(c, incidentService, monitorService, "view")
	if !ok {
		return nil, false
	}
	commentID, err := uuid.Parse(c.Param("comment_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "comment id must be a UUID")
		return nil, false
	}
	comment, err := incidentService.GetComment(c.Request.Context(), commentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "no such comment")
			return nil, false
		}
		respondInternal(c, "loadOwnComment", err)
		return nil, false
	}
	// Checked so a comment id from one incident cannot be acted on through
	// another incident's URL.
	if comment.IncidentID != incidentID {
		respondError(c, http.StatusNotFound, "no such comment")
		return nil, false
	}
	if !authorizeCommentAuthor(c, comment) {
		return nil, false
	}
	return comment, true
}

// UpdateIncidentCommentHandler handles PATCH /api/v1/incidents/:id/comments/:comment_id.
func UpdateIncidentCommentHandler(
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		comment, ok := loadOwnComment(c, incidentService, monitorService)
		if !ok {
			return
		}
		var req incidentCommentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "a comment body is required")
			return
		}
		updated, err := incidentService.UpdateComment(c.Request.Context(), comment.ID, req.Body)
		if err != nil {
			if strings.Contains(err.Error(), "comment") {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			respondInternal(c, "UpdateIncidentCommentHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"comment": updated})
	}
}

// DeleteIncidentCommentHandler handles DELETE /api/v1/incidents/:id/comments/:comment_id.
func DeleteIncidentCommentHandler(
	incidentService *services.IncidentService,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		comment, ok := loadOwnComment(c, incidentService, monitorService)
		if !ok {
			return
		}
		if err := incidentService.DeleteComment(c.Request.Context(), comment.ID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "no such comment")
				return
			}
			respondInternal(c, "DeleteIncidentCommentHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"deleted": comment.ID})
	}
}

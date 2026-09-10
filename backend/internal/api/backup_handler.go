package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// auditRecorder records who did what. Taken as an interface so these handlers
// can be built and tested without a live audit service.
type auditRecorder interface {
	Record(ctx context.Context, actor services.Actor, action, resourceType string,
		resourceID *uuid.UUID, changes models.AuditChanges)
}

// CreateBackupHandler handles POST /api/v1/backups/create.
func CreateBackupHandler(backups *services.BackupService, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Runs on the request's own context, so a dump of a large database is
		// bounded by the service's timeout rather than by the client staying
		// connected.
		info, err := backups.Create(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		recordBackupAudit(c, audit, "backup.create", info.BackupID, info.Filename)
		respondSuccess(c, http.StatusCreated, info)
	}
}

// ListBackupsHandler handles GET /api/v1/backups.
func ListBackupsHandler(backups *services.BackupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := backups.List()
		if err != nil {
			respondInternal(c, "ListBackupsHandler", err)
			return
		}
		// The tools being missing is reported alongside the list rather than
		// as an error, so the page can explain why the buttons will not work
		// instead of showing an empty table with no reason.
		var unavailable string
		if err := backups.Available(); err != nil {
			unavailable = err.Error()
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"backups":     list,
			"directory":   backups.Dir(),
			"unavailable": unavailable,
		})
	}
}

// DownloadBackupHandler handles GET /api/v1/backups/:backup_id/download.
func DownloadBackupHandler(backups *services.BackupService, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("backup_id")
		path, err := backups.Path(id)
		if err != nil {
			respondBackupError(c, err)
			return
		}
		// Downloading takes a copy of every credential in the database off the
		// server, so it is worth a line in the audit log in its own right.
		recordBackupAudit(c, audit, "backup.download", id, filepath.Base(path))
		c.FileAttachment(path, filepath.Base(path))
	}
}

// restoreRequest carries the caller's acknowledgement.
type restoreRequest struct {
	// Confirm must be true. A destructive action reached by an accidental
	// request — a retried POST, a stray click — should not proceed on the URL
	// alone.
	Confirm bool `json:"confirm"`
}

// RestoreBackupHandler handles POST /api/v1/backups/:backup_id/restore.
func RestoreBackupHandler(backups *services.BackupService, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("backup_id")

		var req restoreRequest
		if err := c.ShouldBindJSON(&req); err != nil || !req.Confirm {
			respondError(c, http.StatusBadRequest,
				`restoring replaces all current data; send {"confirm": true} to proceed`)
			return
		}

		info, err := backups.Get(id)
		if err != nil {
			respondBackupError(c, err)
			return
		}

		// Logged before the restore, not after: if it fails halfway there is
		// still a record that it was attempted and by whom.
		recordBackupAudit(c, audit, "backup.restore", id, info.Filename)

		result, err := backups.Restore(c.Request.Context(), id)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"message":             "database restored from " + result.Restored,
			"restored":            result.Restored,
			"safety_backup_id":    result.SafetyBackupID,
			"safety_backup_error": result.SafetyError,
		})
	}
}

// DeleteBackupHandler handles DELETE /api/v1/backups/:backup_id.
func DeleteBackupHandler(backups *services.BackupService, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("backup_id")
		name, err := backups.Delete(id)
		if err != nil {
			respondBackupError(c, err)
			return
		}
		recordBackupAudit(c, audit, "backup.delete", id, name)
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true, "filename": name})
	}
}

func respondBackupError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrBackupNotFound) {
		respondError(c, http.StatusNotFound, "no such backup")
		return
	}
	respondInternal(c, "backup", err)
}

// recordBackupAudit writes one audit entry.
//
// The backup id is a timestamp rather than a UUID, so it goes in the change
// record rather than the resource id column, which only holds UUIDs.
func recordBackupAudit(c *gin.Context, audit auditRecorder, action, id, filename string) {
	if audit == nil {
		return
	}
	audit.Record(c.Request.Context(), actorFrom(c), action, "backup", nil,
		models.AuditChanges{Summary: map[string]any{"backup_id": id, "filename": filename}})
}

// RegisterBackupRoutes mounts the backup routes.
//
// Every one is admin-only, including the read-only listing: the filenames
// alone say when a database was last dumped, and downloading one hands over
// every credential it holds.
func RegisterBackupRoutes(rg *gin.RouterGroup, backups *services.BackupService, audit auditRecorder, users adminChecker) {
	g := rg.Group("/backups", RequireAdmin(users))
	g.POST("/create", CreateBackupHandler(backups, audit))
	g.GET("", ListBackupsHandler(backups))
	g.GET("/:backup_id/download", DownloadBackupHandler(backups, audit))
	g.POST("/:backup_id/restore", RestoreBackupHandler(backups, audit))
	g.DELETE("/:backup_id", DeleteBackupHandler(backups, audit))
}

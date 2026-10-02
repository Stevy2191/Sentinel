package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// mibStore is MIBLibrary as the handlers use it.
type mibStore interface {
	List(ctx context.Context) ([]services.MIBModuleView, error)
	Upload(ctx context.Context, files []services.UploadFile, by uuid.UUID) (*services.MIBUploadResult, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.MIBModule, error)
	Children(ctx context.Context, parentOID string) ([]services.MIBObjectView, error)
	Search(ctx context.Context, q string, limit int) ([]services.MIBObjectView, error)
	Object(ctx context.Context, oid string) (*services.MIBObjectDetail, error)
}

// RegisterMIBRoutes mounts /network/mibs: browsing for every signed-in
// user; the library itself (list, upload, delete) for admins.
func RegisterMIBRoutes(rg *gin.RouterGroup, mibs mibStore, audit auditRecorder, users adminChecker) {
	g := rg.Group("/network/mibs")
	g.GET("/objects/children", func(c *gin.Context) {
		v, err := mibs.Children(c.Request.Context(), c.Query("oid"))
		if err != nil {
			respondInternal(c, "mibChildren", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	g.GET("/objects/search", func(c *gin.Context) {
		q := strings.TrimSpace(c.Query("q"))
		if len(q) < 2 {
			respondError(c, http.StatusBadRequest, "search needs at least 2 characters")
			return
		}
		v, err := mibs.Search(c.Request.Context(), q, 100)
		if err != nil {
			respondInternal(c, "mibSearch", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	g.GET("/objects/by-oid", func(c *gin.Context) {
		v, err := mibs.Object(c.Request.Context(), c.Query("oid"))
		if errors.Is(err, services.ErrMIBObjectNotFound) {
			respondError(c, http.StatusNotFound, "no MIB object at that OID")
			return
		}
		if err != nil {
			respondInternal(c, "mibObject", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	admin := g.Group("", RequireAdmin(users))
	admin.GET("", func(c *gin.Context) {
		v, err := mibs.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "mibList", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	})
	admin.POST("", uploadMIBHandler(mibs, audit))
	admin.DELETE("/:id", deleteMIBHandler(mibs, audit))
}

// uploadMIBHandler reads every "files" part of a multipart upload (one or
// more MIBs, or zips of them) and hands them to the library. The body is
// capped a little above the library's own 20 MB budget so a request that
// blows the budget is rejected for that reason, not cut off mid-read.
func uploadMIBHandler(mibs mibStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxMIBUploadBytes+1<<20)
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			respondMIBBodyError(c, err)
			return
		}
		headers := c.Request.MultipartForm.File["files"]
		var files []services.UploadFile
		for _, h := range headers {
			f, err := h.Open()
			if err != nil {
				respondInternal(c, "uploadMIB", err)
				return
			}
			b, err := io.ReadAll(io.LimitReader(f, services.MaxMIBUploadBytes+1))
			f.Close()
			if err != nil {
				respondMIBBodyError(c, err)
				return
			}
			files = append(files, services.UploadFile{FileName: h.Filename, Content: b})
		}
		if len(files) == 0 {
			respondError(c, http.StatusBadRequest, "choose at least one file")
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		res, err := mibs.Upload(c.Request.Context(), files, userID)
		if err != nil {
			respondMIBUploadError(c, err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionMIBUploaded, models.ResourceMIBModule,
			nil, models.AuditChanges{After: map[string]any{"saved": res.Saved, "skipped": res.Skipped}})
		respondSuccess(c, http.StatusOK, res)
	}
}

// respondMIBBodyError handles a request body that could not be read: too
// large (http.MaxBytesReader's own error) is a 413, anything else (not a
// valid multipart body) is a 400.
func respondMIBBodyError(c *gin.Context, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		respondError(c, http.StatusRequestEntityTooLarge, "the upload is larger than 20 MB")
		return
	}
	respondError(c, http.StatusBadRequest, "invalid multipart form")
}

// respondMIBUploadError maps an error from MIBLibrary.Upload to a response.
func respondMIBUploadError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrMIBUploadTooLarge) || errors.Is(err, services.ErrMIBFileTooLarge) || errors.Is(err, services.ErrMIBTooManyFiles) {
		respondError(c, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	var ue *services.MIBUploadError
	if errors.As(err, &ue) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondInternal(c, "uploadMIB", err)
}

func deleteMIBHandler(mibs mibStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid MIB module id")
			return
		}
		m, err := mibs.Delete(c.Request.Context(), id)
		if err != nil {
			respondMIBDeleteError(c, err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionMIBDeleted, models.ResourceMIBModule,
			&id, models.AuditChanges{Before: map[string]any{"name": m.Name}})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": m.Name})
	}
}

func respondMIBDeleteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrMIBNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrMIBBuiltin):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		var inUse *services.MIBInUseError
		if errors.As(err, &inUse) {
			respondError(c, http.StatusConflict, mibInUseMessage(inUse))
			return
		}
		respondInternal(c, "deleteMIB", err)
	}
}

// mibInUseMessage names what still depends on a module, omitting whichever
// of modules/metrics is empty.
func mibInUseMessage(e *services.MIBInUseError) string {
	var parts []string
	if len(e.Modules) > 0 {
		parts = append(parts, "modules "+strings.Join(e.Modules, ", "))
	}
	if len(e.Metrics) > 0 {
		parts = append(parts, "metrics "+strings.Join(e.Metrics, ", "))
	}
	return "used by " + strings.Join(parts, " and ")
}

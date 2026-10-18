package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// faceplateLayoutStore is what the handlers need from
// services.FaceplateLayoutService.
type faceplateLayoutStore interface {
	List(ctx context.Context) ([]services.FaceplateLayoutView, error)
	DeviceCount(ctx context.Context, model string) (int, error)
	Save(ctx context.Context, model string, groups []portmon.LayoutGroup, actor uuid.UUID, clearDevice *uuid.UUID) (*models.FaceplateLayout, *models.FaceplateLayout, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.FaceplateLayout, error)
}

// faceplateEditor is the PortService half: the editor's start and preview.
type faceplateEditor interface {
	FaceplateEditor(ctx context.Context, d *services.DeviceView) (*services.FaceplateEditorView, error)
	PreviewFaceplate(ctx context.Context, d *services.DeviceView, groups []portmon.LayoutGroup) (*services.FaceplatePreview, error)
}

// RegisterFaceplateLayoutRoutes mounts the faceplate layout editor and the
// Settings list. Every route is admin-only: a layout changes what everyone
// sees for every device of its model.
func RegisterFaceplateLayoutRoutes(rg *gin.RouterGroup, layouts faceplateLayoutStore, ports faceplateEditor, devices deviceStore, sites siteAccessChecker, audit auditRecorder, users adminChecker) {
	admin := rg.Group("/network/faceplate-layouts", RequireAdmin(users))
	admin.GET("", listFaceplateLayoutsHandler(layouts))
	admin.DELETE("/:id", deleteFaceplateLayoutHandler(layouts, audit))

	dev := rg.Group("/devices/:id", RequireAdmin(users))
	dev.GET("/faceplate-layout", faceplateEditorHandler(layouts, ports, devices, sites))
	dev.POST("/faceplate-preview", faceplatePreviewHandler(ports, devices, sites))
	dev.PUT("/faceplate-layout", saveFaceplateLayoutHandler(layouts, ports, devices, sites, audit))
}

type faceplateGroupsRequest struct {
	Groups              []portmon.LayoutGroup `json:"groups"`
	ClearDeviceSettings bool                  `json:"clear_device_settings"`
}

func layoutAuditFields(l *models.FaceplateLayout) map[string]any {
	if l == nil {
		return nil
	}
	return map[string]any{"model": l.Model, "groups": l.Groups}
}

func respondFaceplateError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrFaceplateLayoutNotFound):
		respondError(c, http.StatusNotFound, "faceplate layout not found")
	case errors.Is(err, services.ErrFaceplateLayoutNoModel), errors.Is(err, services.ErrFaceplateGroupsInvalid):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

// misfitMessage names what a layout leaves out, and what it places that
// the device does not have.
func misfitMessage(f portmon.Fit) string {
	join := func(ns []int) string {
		s := make([]string, len(ns))
		for i, n := range ns {
			s[i] = strconv.Itoa(n)
		}
		return strings.Join(s, ", ")
	}
	var parts []string
	if len(f.Missing) > 0 {
		parts = append(parts, "not placed "+join(f.Missing))
	}
	if len(f.Extra) > 0 {
		parts = append(parts, "not on this device "+join(f.Extra))
	}
	return "the layout must place every port of this device: " + strings.Join(parts, "; ")
}

func listFaceplateLayoutsHandler(layouts faceplateLayoutStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := layouts.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "listFaceplateLayouts", err)
			return
		}
		respondSuccess(c, http.StatusOK, rows)
	}
}

func faceplateEditorHandler(layouts faceplateLayoutStore, ports faceplateEditor, devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		v, err := ports.FaceplateEditor(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "faceplateEditor", err)
			return
		}
		n, err := layouts.DeviceCount(c.Request.Context(), d.EffectiveModel)
		if err != nil {
			respondInternal(c, "faceplateEditor", err)
			return
		}
		respondSuccess(c, http.StatusOK, struct {
			*services.FaceplateEditorView
			Devices int `json:"devices"`
		}{v, n})
	}
}

func faceplatePreviewHandler(ports faceplateEditor, devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		var req faceplateGroupsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		p, err := ports.PreviewFaceplate(c.Request.Context(), d, req.Groups)
		if err != nil {
			respondFaceplateError(c, "faceplatePreview", err)
			return
		}
		respondSuccess(c, http.StatusOK, p)
	}
}

func saveFaceplateLayoutHandler(layouts faceplateLayoutStore, ports faceplateEditor, devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		var req faceplateGroupsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if portmon.NormalizeModel(d.EffectiveModel) == "" {
			respondError(c, http.StatusBadRequest, services.ErrFaceplateLayoutNoModel.Error())
			return
		}
		p, err := ports.PreviewFaceplate(c.Request.Context(), d, req.Groups)
		if err != nil {
			respondFaceplateError(c, "saveFaceplateLayout", err)
			return
		}
		if !p.Fit.OK() {
			respondError(c, http.StatusBadRequest, misfitMessage(p.Fit))
			return
		}
		var clear *uuid.UUID
		if req.ClearDeviceSettings {
			clear = &d.ID
		}
		actor := actorFrom(c)
		before, after, err := layouts.Save(c.Request.Context(), d.EffectiveModel, req.Groups, actor.UserID, clear)
		if err != nil {
			respondFaceplateError(c, "saveFaceplateLayout", err)
			return
		}
		audit.Record(c.Request.Context(), actor, models.ActionFaceplateLayoutSaved, models.ResourceFaceplateLayout, &after.ID,
			models.AuditChanges{Before: layoutAuditFields(before), After: layoutAuditFields(after),
				Summary: map[string]any{"device_id": d.ID, "device_name": d.Name, "cleared_device_settings": req.ClearDeviceSettings}})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteFaceplateLayoutHandler(layouts faceplateLayoutStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid faceplate layout id")
		if !ok {
			return
		}
		l, err := layouts.Delete(c.Request.Context(), id)
		if err != nil {
			respondFaceplateError(c, "deleteFaceplateLayout", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionFaceplateLayoutDeleted, models.ResourceFaceplateLayout, &l.ID,
			models.AuditChanges{Before: layoutAuditFields(l)})
		respondSuccess(c, http.StatusOK, gin.H{"message": fmt.Sprintf("faceplate layout for %s removed", l.Model)})
	}
}

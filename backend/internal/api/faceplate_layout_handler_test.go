package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeFaceplateLayouts counts every call, so a test can prove a refused
// request never reached the service.
type fakeFaceplateLayouts struct{ calls int }

func (f *fakeFaceplateLayouts) List(context.Context) ([]services.FaceplateLayoutView, error) {
	f.calls++
	return []services.FaceplateLayoutView{}, nil
}
func (f *fakeFaceplateLayouts) DeviceCount(context.Context, string) (int, error) {
	f.calls++
	return 0, nil
}
func (f *fakeFaceplateLayouts) Save(context.Context, string, []portmon.LayoutGroup, uuid.UUID, *uuid.UUID) (*models.FaceplateLayout, *models.FaceplateLayout, error) {
	f.calls++
	return nil, &models.FaceplateLayout{ID: uuid.New()}, nil
}
func (f *fakeFaceplateLayouts) Delete(_ context.Context, id uuid.UUID) (*models.FaceplateLayout, error) {
	f.calls++
	return &models.FaceplateLayout{ID: id}, nil
}

type fakeFaceplateEditor struct{ calls int }

func (f *fakeFaceplateEditor) FaceplateEditor(context.Context, *services.DeviceView) (*services.FaceplateEditorView, error) {
	f.calls++
	return &services.FaceplateEditorView{}, nil
}
func (f *fakeFaceplateEditor) PreviewFaceplate(context.Context, *services.DeviceView, []portmon.LayoutGroup) (*services.FaceplatePreview, error) {
	f.calls++
	return &services.FaceplatePreview{}, nil
}

func TestFaceplateLayoutRoutesAdminOnly(t *testing.T) {
	layouts, editor, devs, audit := &fakeFaceplateLayouts{}, &fakeFaceplateEditor{}, &fakeDevices{}, &fakeAudit{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterFaceplateLayoutRoutes(r.Group("/api/v1"), layouts, editor, devs, fakeSiteLevels{}, audit, stubUsers{user: &models.User{IsAdmin: false}})
	dev, id := uuid.New().String(), uuid.New().String()
	body := `{"groups":[{"first":1,"last":4,"kind":"rj45","rows":1}]}`
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/network/faceplate-layouts", ""},
		{http.MethodDelete, "/api/v1/network/faceplate-layouts/" + id, ""},
		{http.MethodGet, "/api/v1/devices/" + dev + "/faceplate-layout", ""},
		{http.MethodPost, "/api/v1/devices/" + dev + "/faceplate-preview", body},
		{http.MethodPut, "/api/v1/devices/" + dev + "/faceplate-layout", body},
	} {
		if w := toolRequest(r, c.method, c.path, c.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin: %d, want 403", c.method, strings.TrimPrefix(c.path, "/api/v1"), w.Code)
		}
	}
	if layouts.calls+editor.calls != 0 || len(audit.entries) != 0 {
		t.Errorf("a refused request reached the handler: %d service calls, %d audit entries", layouts.calls+editor.calls, len(audit.entries))
	}
}

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakeEndpoints struct{ calls int }

func (f *fakeEndpoints) Search(context.Context, services.Viewer, string) ([]services.Endpoint, error) {
	f.calls++
	return nil, nil
}

func (f *fakeEndpoints) ForDevice(context.Context, services.Viewer, uuid.UUID) (*services.ConnectedView, error) {
	f.calls++
	return &services.ConnectedView{}, nil
}

func (f *fakeEndpoints) Get(context.Context, services.Viewer, string) (*services.Endpoint, []services.SightingView, error) {
	f.calls++
	return &services.Endpoint{}, nil, nil
}

func (f *fakeEndpoints) SetName(context.Context, services.Viewer, string, string) (string, string, error) {
	f.calls++
	return "", "", nil
}

func TestEndpointRoutes(t *testing.T) {
	eps := &fakeEndpoints{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", true)
		c.Next()
	})
	RegisterEndpointRoutes(r.Group("/api/v1"), eps, &fakeDevices{}, fakeSiteLevels{}, &fakeAudit{})
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/network/endpoints/search?q=", ""},
		{http.MethodGet, "/api/v1/network/endpoints/search?q=%20%20", ""},
		{http.MethodGet, "/api/v1/network/endpoints/not-a-mac", ""},
		{http.MethodPut, "/api/v1/network/endpoints/not-a-mac/name", `{"name":"x"}`},
		{http.MethodPut, "/api/v1/network/endpoints/aa:bb:cc:dd:ee:ff/name", `{"name":"` + strings.Repeat("x", 65) + `"}`},
		{http.MethodPut, "/api/v1/network/endpoints/aa:bb:cc:dd:ee:ff/name", `not json`},
	} {
		if w := toolRequest(r, c.method, c.path, c.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s: %d, want 400", c.method, c.path, w.Code)
		}
	}
	if eps.calls != 0 {
		t.Errorf("%d bad requests reached the service", eps.calls)
	}
	if w := toolRequest(r, http.MethodGet, "/api/v1/network/endpoints/AA-BB-CC-DD-EE-FF", ""); w.Code != http.StatusOK {
		t.Errorf("dash-form MAC: %d", w.Code)
	}
}

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakeCreds struct{ mutations int }

func (f *fakeCreds) List(context.Context) ([]services.CredentialView, error) { return nil, nil }
func (f *fakeCreds) Create(context.Context, models.CredentialInput, uuid.UUID) (services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{ID: uuid.New(), Name: "x"}, nil
}
func (f *fakeCreds) Update(context.Context, uuid.UUID, models.CredentialInput) (services.CredentialView, services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{}, services.CredentialView{}, nil
}
func (f *fakeCreds) Delete(context.Context, uuid.UUID) (services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{}, services.ErrCredentialInUse
}
func (f *fakeCreds) ForSite(context.Context, uuid.UUID) ([]services.CredentialOption, error) {
	return []services.CredentialOption{{ID: uuid.New(), Name: "Default", Version: "2c"}}, nil
}

func credRouter(creds *fakeCreds, siteLevel services.SiteAccessLevel, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterSNMPCredentialRoutes(r.Group("/api/v1"), creds, fakeSiteAccess{siteLevel}, &fakeAudit{},
		stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

// Managing profiles is admin-only, even for someone with editable access to
// every site.
func TestCredentialRoutesAdminOnly(t *testing.T) {
	id := uuid.New().String()
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/snmp-credentials"},
		{http.MethodPost, "/api/v1/snmp-credentials"},
		{http.MethodPut, "/api/v1/snmp-credentials/" + id},
		{http.MethodDelete, "/api/v1/snmp-credentials/" + id},
	} {
		creds := &fakeCreds{}
		w := do(credRouter(creds, services.SiteAccessEditable, false), rt.method, rt.path, map[string]any{"name": "x", "version": "2c", "community": "c"})
		if w.Code != http.StatusForbidden || creds.mutations != 0 {
			t.Errorf("%s %s: status %d, mutations %d; want 403, 0", rt.method, rt.path, w.Code, creds.mutations)
		}
	}
}

// Editors choose a profile by name for their site; readonly users cannot list
// them, and nothing secret is ever in the response.
func TestSiteCredentialOptions(t *testing.T) {
	path := "/api/v1/sites/" + uuid.New().String() + "/credentials"
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessEditable, false), http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Errorf("editable: %d, want 200", w.Code)
	}
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessReadonly, false), http.MethodGet, path, nil); w.Code != http.StatusForbidden {
		t.Errorf("readonly: %d, want 403", w.Code)
	}
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessNone, false), http.MethodGet, path, nil); w.Code != http.StatusNotFound {
		t.Errorf("no access: %d, want 404", w.Code)
	}
}

func TestDeleteCredentialInUseIsConflict(t *testing.T) {
	w := do(credRouter(&fakeCreds{}, services.SiteAccessAdmin, true), http.MethodDelete, "/api/v1/snmp-credentials/"+uuid.New().String(), nil)
	if w.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", w.Code)
	}
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeSites is an in-memory siteStore. mutations counts every call that would
// change data, so tests can prove a refused request changed nothing.
type fakeSites struct {
	access    services.SiteAccessLevel
	accessErr error
	createErr error
	deleteErr error
	shareErr  error
	mutations int
}

func (f *fakeSites) List(context.Context, uuid.UUID, bool) ([]models.Site, error) {
	return []models.Site{{ID: uuid.New(), Name: "HQ"}}, nil
}
func (f *fakeSites) Get(_ context.Context, id uuid.UUID) (*models.Site, error) {
	return &models.Site{ID: id, Name: "HQ"}, nil
}
func (f *fakeSites) SiteAccess(context.Context, uuid.UUID, bool, uuid.UUID) (services.SiteAccessLevel, error) {
	return f.access, f.accessErr
}
func (f *fakeSites) Create(_ context.Context, in models.SiteInput, _ uuid.UUID) (*models.Site, error) {
	f.mutations++
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &models.Site{ID: uuid.New(), Name: in.Name}, nil
}
func (f *fakeSites) Update(_ context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error) {
	f.mutations++
	return &models.Site{ID: id, Name: "old"}, &models.Site{ID: id, Name: in.Name}, nil
}
func (f *fakeSites) Delete(_ context.Context, id uuid.UUID) (*models.Site, error) {
	f.mutations++
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &models.Site{ID: id, Name: "HQ"}, nil
}
func (f *fakeSites) ListShares(context.Context, uuid.UUID) ([]services.SiteShareView, error) {
	return nil, nil
}
func (f *fakeSites) UpsertShare(_ context.Context, siteID, userID, _ uuid.UUID, p string) (*models.SiteSharing, error) {
	f.mutations++
	if f.shareErr != nil {
		return nil, f.shareErr
	}
	return &models.SiteSharing{SiteID: siteID, SharedWithUserID: userID, Permission: p}, nil
}
func (f *fakeSites) RemoveShare(context.Context, uuid.UUID, uuid.UUID) error {
	f.mutations++
	return nil
}

type fakeAudit struct {
	entries []string
	changes []models.AuditChanges
}

func (a *fakeAudit) Record(_ context.Context, _ services.Actor, action, _ string, _ *uuid.UUID, changes models.AuditChanges) {
	a.entries = append(a.entries, action)
	a.changes = append(a.changes, changes)
}

// siteRouter mounts the site routes as a signed-in user with the given admin
// claim. stubUsers (auth_middleware_test.go) confirms the claim.
func siteRouter(store *fakeSites, audit *fakeAudit, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterSiteRoutes(r.Group("/api/v1"), store, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func do(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// A non-admin must be refused by every admin route, with a well-formed body,
// and nothing may change: no store mutation and no audit entry.
func TestSiteAdminRoutesDoNotRunForNonAdmins(t *testing.T) {
	id := uuid.New().String()
	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/sites", map[string]any{"name": "HQ"}},
		{http.MethodPut, "/api/v1/sites/" + id, map[string]any{"name": "HQ"}},
		{http.MethodDelete, "/api/v1/sites/" + id, nil},
		{http.MethodGet, "/api/v1/sites/" + id + "/shares", nil},
		{http.MethodPost, "/api/v1/sites/" + id + "/shares", map[string]any{"user_id": uuid.New(), "permission": "readonly"}},
		{http.MethodDelete, "/api/v1/sites/" + id + "/shares/" + uuid.New().String(), nil},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			store := &fakeSites{access: services.SiteAccessEditable}
			audit := &fakeAudit{}
			w := do(siteRouter(store, audit, false), rt.method, rt.path, rt.body)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
			if store.mutations != 0 || len(audit.entries) != 0 {
				t.Errorf("refused request changed data: %d mutations, audit %v", store.mutations, audit.entries)
			}
		})
	}
}

func TestSiteHandlers(t *testing.T) {
	id := uuid.New().String()

	t.Run("site without access is 404, not 403", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{access: services.SiteAccessNone}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("missing site is 404", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{accessErr: services.ErrSiteNotFound}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("shared site reports the caller's access", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{access: services.SiteAccessReadonly}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var body struct {
			Data struct {
				Access string `json:"access"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Data.Access != "readonly" {
			t.Errorf("access = %q, want readonly", body.Data.Access)
		}
	})

	t.Run("malformed id is 400", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{}, &fakeAudit{}, true), http.MethodGet, "/api/v1/sites/not-a-uuid", nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("blank name is 400 and never reaches the store", func(t *testing.T) {
		store := &fakeSites{}
		w := do(siteRouter(store, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "   "})
		if w.Code != http.StatusBadRequest || store.mutations != 0 {
			t.Errorf("status = %d, mutations = %d; want 400 and 0", w.Code, store.mutations)
		}
	})

	t.Run("duplicate name is 409", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{createErr: services.ErrSiteNameTaken}, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "hq"})
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
	})

	t.Run("admin create is 201 and audited", func(t *testing.T) {
		audit := &fakeAudit{}
		w := do(siteRouter(&fakeSites{}, audit, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "HQ"})
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", w.Code)
		}
		if len(audit.entries) != 1 || audit.entries[0] != models.ActionSiteCreated {
			t.Errorf("audit = %v, want [%s]", audit.entries, models.ActionSiteCreated)
		}
	})

	t.Run("deleting a non-empty site is 409", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{deleteErr: services.ErrSiteNotEmpty}, &fakeAudit{}, true), http.MethodDelete, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
	})

	t.Run("invalid share permission is 400", func(t *testing.T) {
		store := &fakeSites{}
		w := do(siteRouter(store, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites/"+id+"/shares",
			map[string]any{"user_id": uuid.New(), "permission": "owner"})
		if w.Code != http.StatusBadRequest || store.mutations != 0 {
			t.Errorf("status = %d, mutations = %d; want 400 and 0", w.Code, store.mutations)
		}
	})

	t.Run("unknown user is 400", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{shareErr: services.ErrSiteShareUnknownUser}, &fakeAudit{}, true), http.MethodPost,
			"/api/v1/sites/"+id+"/shares", map[string]any{"user_id": uuid.New(), "permission": "readonly"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// fakeDashboards is an in-memory dashboardStore. err, when set, is what every
// call returns; mutations counts calls that would change data.
type fakeDashboards struct {
	err         error
	tokenErr    error
	mutations   int
	lastViewer  dashboards.Viewer
	dashboardID uuid.UUID
	// hasLink: the dashboard has a public link, so creating one replaces it.
	hasLink bool
}

func (f *fakeDashboards) detail() *dashboards.DashboardDetail {
	return &dashboards.DashboardDetail{DashboardView: dashboards.DashboardView{
		Dashboard: models.Dashboard{ID: f.dashboardID, Name: "HQ", Version: 1}, Access: "edit", CanEdit: true}}
}
func (f *fakeDashboards) List(_ context.Context, v dashboards.Viewer, _ *uuid.UUID) ([]dashboards.DashboardView, error) {
	f.lastViewer = v
	return []dashboards.DashboardView{f.detail().DashboardView}, f.err
}
func (f *fakeDashboards) Get(_ context.Context, v dashboards.Viewer, _ uuid.UUID) (*dashboards.DashboardDetail, error) {
	f.lastViewer = v
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Create(context.Context, dashboards.Viewer, dashboards.CreateInput) (*dashboards.DashboardDetail, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Save(context.Context, dashboards.Viewer, uuid.UUID, dashboards.SaveInput) (*dashboards.DashboardDetail, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Delete(context.Context, dashboards.Viewer, uuid.UUID) (*models.Dashboard, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return &models.Dashboard{ID: f.dashboardID, Name: "HQ"}, nil
}
func (f *fakeDashboards) ListShares(context.Context, dashboards.Viewer, uuid.UUID) ([]dashboards.ShareView, error) {
	return nil, f.err
}
func (f *fakeDashboards) UpsertShare(context.Context, dashboards.Viewer, uuid.UUID, uuid.UUID, string) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) RemoveShare(context.Context, dashboards.Viewer, uuid.UUID, uuid.UUID) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) PublicLink(context.Context, dashboards.Viewer, uuid.UUID) (*dashboards.LinkView, error) {
	return &dashboards.LinkView{}, f.err
}
func (f *fakeDashboards) CreatePublicLink(context.Context, dashboards.Viewer, uuid.UUID) (*models.DashboardPublicLink, bool, error) {
	f.mutations++
	replaced := f.hasLink
	f.hasLink = true
	return &models.DashboardPublicLink{Token: strings.Repeat("t", 43)}, replaced, f.err
}
func (f *fakeDashboards) RevokePublicLink(context.Context, dashboards.Viewer, uuid.UUID) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) Widget(_ context.Context, v dashboards.Viewer, _, wid uuid.UUID) (*models.DashboardWidget, *dashboards.DashboardView, error) {
	f.lastViewer = v
	if f.err != nil {
		return nil, nil, f.err
	}
	return &models.DashboardWidget{ID: wid, Type: "label"}, &f.detail().DashboardView, nil
}
func (f *fakeDashboards) ResolveToken(context.Context, string) (*models.Dashboard, error) {
	if f.tokenErr != nil {
		return nil, f.tokenErr
	}
	return &models.Dashboard{ID: f.dashboardID, Version: 1}, nil
}
func (f *fakeDashboards) PublicLayout(context.Context, *models.Dashboard) (*dashboards.PublicDashboard, error) {
	return &dashboards.PublicDashboard{Name: "HQ"}, nil
}
func (f *fakeDashboards) PublicWidget(_ context.Context, _ *models.Dashboard, wid uuid.UUID) (*models.DashboardWidget, error) {
	return &models.DashboardWidget{ID: wid, Type: "label"}, nil
}

type fakeWidgetResolver struct {
	calls        int
	lastOverride string
	publicCalls  int
	previewErr   error
}

func (r *fakeWidgetResolver) Resolve(_ context.Context, _ dashboards.Viewer, _ *models.DashboardWidget, override string) (*dashboards.Response, error) {
	r.calls++
	r.lastOverride = override
	return &dashboards.Response{State: dashboards.StateOK}, nil
}
func (r *fakeWidgetResolver) ResolvePublic(context.Context, *models.Dashboard, *models.DashboardWidget) (*dashboards.Response, error) {
	r.publicCalls++
	return &dashboards.Response{State: dashboards.StateOK}, nil
}
func (r *fakeWidgetResolver) Preview(context.Context, dashboards.Viewer, string, json.RawMessage, string) (*dashboards.Response, error) {
	if r.previewErr != nil {
		return nil, r.previewErr
	}
	return &dashboards.Response{State: dashboards.StateOK}, nil
}

func dashboardRouter(store *fakeDashboards, res *fakeWidgetResolver, isAdmin bool) *gin.Engine {
	return dashboardRouterAudited(store, res, isAdmin, &fakeAudit{})
}

func dashboardRouterAudited(store *fakeDashboards, res *fakeWidgetResolver, isAdmin bool, audit *fakeAudit) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPublicDashboardRoutes(r, store, res)
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterDashboardRoutes(v1, store, res, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func TestDashboardErrorStatuses(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		err    error
		status int
		extra  string
	}{
		{dashboards.ErrNotFound, http.StatusNotFound, ""},
		{dashboards.ErrPublished, http.StatusForbidden, ""},
		{dashboards.ErrForbidden, http.StatusForbidden, ""},
		{&dashboards.VersionConflictError{Current: 7}, http.StatusConflict, `"current_version":7`},
		{&dashboards.WidgetError{Index: 2, Field: "metrics", Msg: "needs 1 to 10"}, http.StatusBadRequest, `"widget_index":2`},
		{fmt.Errorf("%w: name is required", dashboards.ErrInvalid), http.StatusBadRequest, ""},
	}
	for _, c := range cases {
		store := &fakeDashboards{err: c.err, dashboardID: id}
		w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), http.MethodPut, "/api/v1/dashboards/"+id.String(), map[string]any{"version": 1, "name": "x"})
		if w.Code != c.status {
			t.Errorf("%v: status %d, want %d", c.err, w.Code, c.status)
		}
		if c.extra != "" && !strings.Contains(w.Body.String(), c.extra) {
			t.Errorf("%v: body %s lacks %s", c.err, w.Body.String(), c.extra)
		}
	}
}

func TestDashboardInternalErrorsAreGeneric(t *testing.T) {
	store := &fakeDashboards{err: errors.New(`pq: relation "secret_table" does not exist`)}
	w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), http.MethodGet, "/api/v1/dashboards", nil)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret_table") {
		t.Errorf("status %d body %s: want a 500 without the database's text", w.Code, w.Body.String())
	}
}

func TestDashboardPublicLinkRoutesAreAdminOnly(t *testing.T) {
	id := uuid.New().String()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		store := &fakeDashboards{}
		w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), method, "/api/v1/dashboards/"+id+"/public-link", nil)
		if w.Code != http.StatusForbidden || store.mutations != 0 {
			t.Errorf("%s by a member: status %d, mutations %d; want 403 and none", method, w.Code, store.mutations)
		}
	}
	store := &fakeDashboards{}
	w := do(dashboardRouter(store, &fakeWidgetResolver{}, true), http.MethodPost, "/api/v1/dashboards/"+id+"/public-link", nil)
	if w.Code != http.StatusOK || store.mutations != 1 {
		t.Errorf("POST by an admin: status %d, mutations %d", w.Code, store.mutations)
	}
}

// The audit log tells a regenerated link from a new one.
func TestCreatePublicLinkAuditsRegeneration(t *testing.T) {
	id := uuid.New().String()
	store, audit := &fakeDashboards{}, &fakeAudit{}
	r := dashboardRouterAudited(store, &fakeWidgetResolver{}, true, audit)
	for i := 0; i < 2; i++ {
		if w := do(r, http.MethodPost, "/api/v1/dashboards/"+id+"/public-link", nil); w.Code != http.StatusOK {
			t.Fatalf("POST %d: status %d", i+1, w.Code)
		}
	}
	if len(audit.changes) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(audit.changes))
	}
	if _, ok := audit.changes[0].Summary["regenerated"]; ok {
		t.Errorf("first link's summary = %v, want no regenerated flag", audit.changes[0].Summary)
	}
	if audit.changes[1].Summary["regenerated"] != true {
		t.Errorf("replacing link's summary = %v, want regenerated: true", audit.changes[1].Summary)
	}
}

func TestWidgetDataRange(t *testing.T) {
	id, wid := uuid.New().String(), uuid.New().String()
	res := &fakeWidgetResolver{}
	r := dashboardRouter(&fakeDashboards{}, res, false)
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/"+wid+"/data?range=2h", nil); w.Code != http.StatusBadRequest {
		t.Errorf("range=2h: status %d, want 400", w.Code)
	}
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/"+wid+"/data?range=7d", nil); w.Code != http.StatusOK || res.lastOverride != "7d" {
		t.Errorf("range=7d: status %d, override %q", w.Code, res.lastOverride)
	}
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/not-a-uuid/data", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad widget id: status %d, want 400", w.Code)
	}
}

func TestPreviewFieldErrorIs400(t *testing.T) {
	res := &fakeWidgetResolver{previewErr: &dashboards.FieldError{Field: "metrics", Msg: "needs 1 to 10"}}
	w := do(dashboardRouter(&fakeDashboards{}, res, false), http.MethodPost, "/api/v1/dashboards/"+uuid.NewString()+"/widgets/preview",
		map[string]any{"type": "timeseries", "config": map[string]any{}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"field":"metrics"`) {
		t.Errorf("status %d body %s, want 400 naming the field", w.Code, w.Body.String())
	}
}

func TestPublicWidgetDataChecksTokenEveryRequest(t *testing.T) {
	store := &fakeDashboards{}
	res := &fakeWidgetResolver{}
	r := dashboardRouter(store, res, false)
	path := "/api/v1/public/dashboards/" + strings.Repeat("t", 43) + "/widgets/" + uuid.NewString() + "/data"
	if w := do(r, http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Fatalf("first request: status %d", w.Code)
	}
	store.tokenErr = dashboards.ErrNotFound // revoked, or the creator was demoted
	w := do(r, http.MethodGet, path, nil)
	if w.Code != http.StatusNotFound || res.publicCalls != 1 {
		t.Errorf("after revoking: status %d, resolves %d; want 404 without resolving", w.Code, res.publicCalls)
	}
	if !strings.Contains(w.Body.String(), "no longer available") {
		t.Errorf("body %s, want the link-unavailable message", w.Body.String())
	}
}

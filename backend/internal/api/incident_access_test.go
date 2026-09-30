package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeIncidents records what the handlers asked for.
type fakeIncidents struct {
	listOpts     *services.IncidentListOptions
	row          *services.IncidentWithMonitor
	detailLoaded bool // checks/notifications/comments were fetched
}

func (f *fakeIncidents) ListIncidents(_ context.Context, opts services.IncidentListOptions) ([]services.IncidentWithMonitor, int64, error) {
	f.listOpts = &opts
	return nil, 0, nil
}
func (f *fakeIncidents) GetIncidentByID(context.Context, uuid.UUID) (*services.IncidentWithMonitor, error) {
	return f.row, nil
}
func (f *fakeIncidents) ChecksDuringIncident(context.Context, *models.Incident, int) ([]models.Check, error) {
	f.detailLoaded = true
	return nil, nil
}
func (f *fakeIncidents) NotificationsForIncident(context.Context, uuid.UUID) ([]models.Notification, error) {
	f.detailLoaded = true
	return nil, nil
}
func (f *fakeIncidents) ListComments(context.Context, uuid.UUID) ([]models.IncidentComment, error) {
	f.detailLoaded = true
	return nil, nil
}

type fakeMonitorViews struct{ canView, canEdit bool }

func (f fakeMonitorViews) CanUserViewMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canView, nil
}
func (f fakeMonitorViews) CanUserEditMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canEdit, nil
}

type fakeSiteAccess struct{ level services.SiteAccessLevel }

func (f fakeSiteAccess) SiteAccess(context.Context, uuid.UUID, bool, uuid.UUID) (services.SiteAccessLevel, error) {
	return f.level, nil
}

func incidentRouter(inc *fakeIncidents, mon fakeMonitorViews, sites fakeSiteAccess, userID uuid.UUID, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	r.GET("/incidents", ListIncidentsHandler(inc))
	r.GET("/incidents/:id", GetIncidentHandler(inc, mon, sites))
	return r
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// The list must tell the service who is asking, so it can filter to the
// monitors that user owns or has been shared.
func TestListIncidentsPassesViewer(t *testing.T) {
	for _, isAdmin := range []bool{false, true} {
		user := uuid.New()
		inc := &fakeIncidents{}
		w := get(incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{}, user, isAdmin), "/incidents")
		if w.Code != http.StatusOK {
			t.Fatalf("admin=%v: status %d, want 200", isAdmin, w.Code)
		}
		if inc.listOpts == nil || inc.listOpts.Viewer == nil {
			t.Fatalf("admin=%v: list called without a viewer", isAdmin)
		}
		if inc.listOpts.Viewer.UserID != user || inc.listOpts.Viewer.IsAdmin != isAdmin {
			t.Errorf("admin=%v: viewer = %+v, want user %s admin %v", isAdmin, *inc.listOpts.Viewer, user, isAdmin)
		}
	}
}

// An incident on a monitor the caller cannot see answers exactly like one that
// does not exist, and none of its detail (checks, notifications, comments) is
// loaded, let alone returned.
func TestGetIncidentHidesOtherUsersIncidents(t *testing.T) {
	row := &services.IncidentWithMonitor{
		Incident:    models.Incident{ID: uuid.New(), MonitorID: ptr(uuid.New())},
		MonitorName: "someone else's monitor",
		MonitorURL:  "https://private.example",
	}
	path := "/incidents/" + row.Incident.ID.String()

	inc := &fakeIncidents{row: row}
	w := get(incidentRouter(inc, fakeMonitorViews{canView: false}, fakeSiteAccess{}, uuid.New(), false), path)
	if w.Code != http.StatusNotFound {
		t.Errorf("no access: status %d, want 404", w.Code)
	}
	if inc.detailLoaded {
		t.Error("no access: incident detail was loaded")
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, leaked := body["data"]; leaked {
		t.Errorf("no access: response carried data: %s", w.Body.String())
	}

	inc = &fakeIncidents{row: row}
	if w := get(incidentRouter(inc, fakeMonitorViews{canView: true}, fakeSiteAccess{}, uuid.New(), false), path); w.Code != http.StatusOK {
		t.Errorf("shared monitor: status %d, want 200", w.Code)
	}

	// Admins see everything, without consulting sharing at all.
	inc = &fakeIncidents{row: row}
	if w := get(incidentRouter(inc, fakeMonitorViews{canView: false}, fakeSiteAccess{}, uuid.New(), true), path); w.Code != http.StatusOK {
		t.Errorf("admin: status %d, want 200", w.Code)
	}
}

func ptr[T any](v T) *T { return &v }

// A device incident follows site sharing: readonly on the site shows it,
// no access is a 404 with nothing loaded.
func TestGetDeviceIncidentFollowsSiteAccess(t *testing.T) {
	site := uuid.New()
	row := &services.IncidentWithMonitor{
		Incident:    models.Incident{ID: uuid.New(), DeviceID: ptr(uuid.New())},
		SubjectType: "device", SiteID: &site,
	}
	path := "/incidents/" + row.Incident.ID.String()

	inc := &fakeIncidents{row: row}
	w := get(incidentRouter(inc, fakeMonitorViews{canView: true}, fakeSiteAccess{services.SiteAccessNone}, uuid.New(), false), path)
	if w.Code != http.StatusNotFound || inc.detailLoaded {
		t.Errorf("no site access: status %d, detail loaded %v; want 404, false", w.Code, inc.detailLoaded)
	}
	inc = &fakeIncidents{row: row}
	if w := get(incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{services.SiteAccessReadonly}, uuid.New(), false), path); w.Code != http.StatusOK {
		t.Errorf("readonly site: status %d, want 200", w.Code)
	}
}

// A device incident has no checks (they belong to monitors, not devices), and
// ChecksDuringIncident/NotificationsForIncident/ListComments all return nil
// slices from the fake, exactly like a real failed lookup would. The response
// must still carry "checks": [] and "notifications": [], never null - the
// frontend runs .filter/.map on both unconditionally and null crashes it.
func TestGetDeviceIncidentChecksAndNotificationsAreNeverNull(t *testing.T) {
	row := &services.IncidentWithMonitor{
		Incident:    models.Incident{ID: uuid.New(), DeviceID: ptr(uuid.New())},
		SubjectType: "device", SiteID: ptr(uuid.New()),
	}
	path := "/incidents/" + row.Incident.ID.String()

	inc := &fakeIncidents{row: row}
	w := get(incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{services.SiteAccessReadonly}, uuid.New(), false), path)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Data struct {
			Checks        json.RawMessage `json:"checks"`
			Notifications json.RawMessage `json:"notifications"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (body: %s)", err, w.Body.String())
	}
	if string(body.Data.Checks) != "[]" {
		t.Errorf(`checks = %s, want []`, body.Data.Checks)
	}
	if string(body.Data.Notifications) != "[]" {
		t.Errorf(`notifications = %s, want []`, body.Data.Notifications)
	}
}

// Device incidents open and close automatically from the poller's own
// up/down transitions (R1). A PATCH that tries to set status on one is
// rejected before anything is written - checked here with fakes and a nil db,
// since the handler must never reach the database on this path.
func TestUpdateDeviceIncidentRejectsStatusChange(t *testing.T) {
	row := &services.IncidentWithMonitor{
		Incident:    models.Incident{ID: uuid.New(), DeviceID: ptr(uuid.New())},
		SubjectType: "device", SiteID: ptr(uuid.New()),
	}
	inc := &fakeIncidents{row: row}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "admin")
		c.Set("is_admin", true) // admin: sees/edits everything, no site lookup needed
		c.Next()
	})
	r.PATCH("/incidents/:id", UpdateIncidentHandler(inc, fakeMonitorViews{}, fakeSiteAccess{}, nil))

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"status":"resolved"}`)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/incidents/"+row.Incident.ID.String(), body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "device incidents open and close automatically") {
		t.Errorf("body = %s, want the controller-ruling message", w.Body.String())
	}
}

// A bogus subject is a 400, and device_id/subject filters reach the service
// options unmodified.
func TestListIncidentsDeviceFilters(t *testing.T) {
	inc := &fakeIncidents{}
	r := incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{}, uuid.New(), false)
	if w := get(r, "/incidents?subject=bogus"); w.Code != http.StatusBadRequest {
		t.Errorf("bogus subject: status %d, want 400", w.Code)
	}
	dev := uuid.New()
	if w := get(r, "/incidents?subject=device&device_id="+dev.String()); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if inc.listOpts.Subject != "device" || inc.listOpts.DeviceID == nil || *inc.listOpts.DeviceID != dev {
		t.Errorf("opts = %+v", *inc.listOpts)
	}
}

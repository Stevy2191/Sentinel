package api

import (
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

type fakeMonitorViews struct{ canView bool }

func (f fakeMonitorViews) CanUserViewMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canView, nil
}

func incidentRouter(inc *fakeIncidents, mon fakeMonitorViews, userID uuid.UUID, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	r.GET("/incidents", ListIncidentsHandler(inc))
	r.GET("/incidents/:id", GetIncidentHandler(inc, mon))
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
		w := get(incidentRouter(inc, fakeMonitorViews{}, user, isAdmin), "/incidents")
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
		Incident:    models.Incident{ID: uuid.New(), MonitorID: uuid.New()},
		MonitorName: "someone else's monitor",
		MonitorURL:  "https://private.example",
	}
	path := "/incidents/" + row.Incident.ID.String()

	inc := &fakeIncidents{row: row}
	w := get(incidentRouter(inc, fakeMonitorViews{canView: false}, uuid.New(), false), path)
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
	if w := get(incidentRouter(inc, fakeMonitorViews{canView: true}, uuid.New(), false), path); w.Code != http.StatusOK {
		t.Errorf("shared monitor: status %d, want 200", w.Code)
	}

	// Admins see everything, without consulting sharing at all.
	inc = &fakeIncidents{row: row}
	if w := get(incidentRouter(inc, fakeMonitorViews{canView: false}, uuid.New(), true), path); w.Code != http.StatusOK {
		t.Errorf("admin: status %d, want 200", w.Code)
	}
}

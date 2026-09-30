package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// R1's controller ruling only blocks a manual status change on a device
// incident; its annotations (root_cause, notes, resolution_notes, severity)
// stay editable. Run against a real device incident end to end, since the
// handler's write and reload both go straight through *gorm.DB.
func TestDBUpdateDeviceIncidentKeepsAnnotationsEditable(t *testing.T) {
	db := testdb.Open(t)
	incidentSvc := services.NewIncidentService(db)
	siteSvc := services.NewSiteService(db)

	site := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'HQ')`, site)
	cred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)
	deviceID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'core-sw-1', '10.0.0.2')`,
		deviceID, site, cred)

	incident, _, err := incidentSvc.OpenDeviceIncident(context.Background(), deviceID, time.Now(), "down")
	testdb.Must(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "admin")
		c.Set("is_admin", true)
		c.Next()
	})
	r.PATCH("/incidents/:id", UpdateIncidentHandler(incidentSvc, fakeMonitorViews{}, siteSvc, db))

	patch := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/incidents/"+incident.ID.String(), strings.NewReader(body)))
		return w
	}

	if w := patch(`{"status":"resolved"}`); w.Code != http.StatusBadRequest {
		t.Errorf("status change on device incident: %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := patch(`{"notes":"looked into it"}`); w.Code != http.StatusOK {
		t.Errorf("notes-only update on device incident: %d, want 200 (not blocked): %s", w.Code, w.Body.String())
	}
	if w := patch(`{"severity":"high"}`); w.Code != http.StatusOK {
		t.Errorf("severity update on device incident: %d, want 200 (not blocked): %s", w.Code, w.Body.String())
	}
}

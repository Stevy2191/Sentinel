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
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// siteTestGroup is a router whose /api/v1 group acts as caller (read from the
// database), as the auth middleware would.
func siteTestGroup(t *testing.T, db *gorm.DB, caller uuid.UUID) (*gin.Engine, *gin.RouterGroup, *services.AuthService) {
	t.Helper()
	auth := services.NewAuthService(db, testJWTSecret)
	user, err := auth.GetUserByID(context.Background(), caller)
	testdb.Must(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("is_admin", user.IsAdmin)
		c.Next()
	})
	return r, v1, auth
}

// dataOf decodes the "data" member of a success response.
func dataOf[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return body.Data
}

func monitorSiteRouter(t *testing.T, db *gorm.DB, caller uuid.UUID) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	monitors := services.NewMonitorService(db)
	checks := services.NewCheckService(db)
	RegisterMonitorRoutes(v1, monitors, checks, services.NewSettingsService(db), services.NewSiteService(db))
	RegisterMonitorCreationRoutes(v1, monitors, checks)
	return r
}

type monitorSiteView struct {
	ID       string  `json:"id"`
	SiteID   *string `json:"site_id"`
	SiteName *string `json:"site_name"`
}

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

func pingMonitorBody(extra string) string {
	return `{"name":"core","type":"ping","url":"10.0.0.1","interval_seconds":60,"timeout_seconds":5` + extra + `}`
}

// An admin sets a site on create, an edit that leaves the field out keeps
// it, and an explicit null clears it. Every response carries the site name.
func TestDBMonitorSiteSetKeptAndCleared(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, true))

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+site.String()+`"`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[monitorSiteView](t, w)
	if created.SiteID == nil || *created.SiteID != site.String() || created.SiteName == nil || *created.SiteName != "Annex" {
		t.Fatalf("created = %+v", created)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"name":"core-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	w = toolRequest(r, http.MethodGet, "/api/v1/monitors/"+created.ID, "")
	if got := dataOf[monitorSiteView](t, w); got.SiteID == nil || *got.SiteID != site.String() || got.SiteName == nil || *got.SiteName != "Annex" {
		t.Errorf("after an edit without site_id: %+v, want the site kept", got)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"site_id":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[monitorSiteView](t, w); got.SiteID != nil || got.SiteName != nil {
		t.Errorf("after site_id null: %+v, want no site", got)
	}
}

// A non-admin may only use a site shared with them; a missing site gets the
// same refusal.
func TestDBMonitorSiteRefusedWhenNotShared(t *testing.T) {
	db := testdb.Open(t)
	shared, other := newSite(t, db, "Annex"), newSite(t, db, "Courthouse")
	user := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared, user)
	r := monitorSiteRouter(t, db, user)

	for _, id := range []uuid.UUID{other, uuid.New()} {
		w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+id.String()+`"`))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
			t.Errorf("site %s: %d %s, want 400 site not found", id, w.Code, w.Body.String())
		}
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors`).Scan(&n).Error)
	if n != 0 {
		t.Fatalf("%d monitors created by refused requests", n)
	}

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+shared.String()+`"`))
	if w.Code != http.StatusCreated {
		t.Fatalf("shared site: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[monitorSiteView](t, w)
	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"site_id":"`+other.String()+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Errorf("edit to an unshared site: %d %s, want 400 site not found", w.Code, w.Body.String())
	}
}

// The edit form always sends site_id. A monitor labelled with a site the
// editor cannot see must still save when that site is sent back unchanged.
func TestDBMonitorEditKeepsASiteTheEditorCannotSee(t *testing.T) {
	db := testdb.Open(t)
	hidden := newSite(t, db, "Courthouse")
	user := testdb.NewUser(t, db, false)
	r := monitorSiteRouter(t, db, user)

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(""))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := dataOf[monitorSiteView](t, w).ID
	testdb.Exec(t, db, `UPDATE monitors SET site_id = ? WHERE id = ?`, hidden, id)

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+id, `{"name":"core-renamed","site_id":"`+hidden.String()+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("saving with the unchanged site: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[monitorSiteView](t, w); got.SiteID == nil || *got.SiteID != hidden.String() {
		t.Errorf("site after save = %v, want it kept", got.SiteID)
	}
}

// The list carries each monitor's site name.
func TestDBMonitorListCarriesSiteName(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, true))
	toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+site.String()+`"`))
	toolRequest(r, http.MethodPost, "/api/v1/monitors", `{"name":"edge","type":"ping","url":"10.0.0.2","interval_seconds":60,"timeout_seconds":5}`)

	w := toolRequest(r, http.MethodGet, "/api/v1/monitors", "")
	list := dataOf[struct {
		Monitors []monitorSiteView `json:"monitors"`
	}](t, w)
	if len(list.Monitors) != 2 {
		t.Fatalf("listed %d monitors, want 2", len(list.Monitors))
	}
	for _, m := range list.Monitors {
		labelled := m.SiteName != nil && *m.SiteName == "Annex"
		if (m.SiteID != nil) != labelled {
			t.Errorf("monitor %+v: site id and name disagree", m)
		}
	}
}

// Bulk upload never sets a site, so it cannot be used to skip the check.
func TestDBBulkCreateIgnoresSite(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, false))

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors/bulk",
		`{"monitors":[{"name":"core","type":"ping","url":"10.0.0.1","interval_seconds":60,"timeout_seconds":5,"site_id":"`+site.String()+`"}]}`)
	if w.Code >= 300 {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	var total, labelled int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors`).Scan(&total).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors WHERE site_id IS NOT NULL`).Scan(&labelled).Error)
	if total != 1 || labelled != 0 {
		t.Errorf("monitors = %d, with a site = %d; want 1 and 0", total, labelled)
	}
}

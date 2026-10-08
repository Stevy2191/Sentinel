package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func siteProfileRouter(t *testing.T, db *gorm.DB, caller uuid.UUID, audit *recordingAudit) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	metrics := services.NewMetricsStore(db)
	ports := services.NewPortService(db, metrics, services.NewIncidentService(db), services.NewSettingsService(db))
	RegisterSiteProfileRoutes(v1, services.NewSiteProfileService(db, ports), services.NewSiteService(db), audit)
	return r
}

func shareSiteWith(t *testing.T, db *gorm.DB, site, user uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`, site, user, permission)
}

// A read-only sharer reads the profile and is refused every change; someone
// the site is not shared with gets "not found"; an editable sharer changes
// it, each change audited against the site without sensitive circuit fields.
func TestDBSiteProfileAccess(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Courthouse")
	readonly, editable, stranger := testdb.NewUser(t, db, false), testdb.NewUser(t, db, false), testdb.NewUser(t, db, false)
	shareSiteWith(t, db, site, readonly, "readonly")
	shareSiteWith(t, db, site, editable, "editable")
	base := "/api/v1/sites/" + site.String()
	changes := []struct{ method, path, body string }{
		{http.MethodPost, "/networks", `{"name":"Staff","cidr":"10.20.0.0/24"}`},
		{http.MethodPut, "/notes", `{"notes":"Closet: room 104"}`},
		{http.MethodPost, "/circuits", `{"provider":"Spectrum","kind":"fiber","account_number":"8347-SECRET","support_phone":"1-800-555-0100","notes":"PIN 4411"}`},
	}
	audit := &recordingAudit{}

	r := siteProfileRouter(t, db, readonly, audit)
	if w := toolRequest(r, http.MethodGet, base+"/profile", ""); w.Code != http.StatusOK {
		t.Errorf("read-only reading the profile: %d %s", w.Code, w.Body.String())
	}
	for _, c := range changes {
		if w := toolRequest(r, c.method, base+c.path, c.body); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "you need edit access to this site") {
			t.Errorf("read-only %s %s: %d %s, want 403", c.method, c.path, w.Code, w.Body.String())
		}
	}

	r = siteProfileRouter(t, db, stranger, audit)
	if w := toolRequest(r, http.MethodGet, base+"/profile", ""); w.Code != http.StatusNotFound {
		t.Errorf("stranger reading: %d, want 404", w.Code)
	}
	if w := toolRequest(r, http.MethodPost, base+"/networks", changes[0].body); w.Code != http.StatusNotFound {
		t.Errorf("stranger changing: %d, want 404", w.Code)
	}
	if len(audit.calls) != 0 {
		t.Fatalf("refused requests were audited: %+v", audit.calls)
	}

	r = siteProfileRouter(t, db, editable, audit)
	for _, c := range changes {
		if w := toolRequest(r, c.method, base+c.path, c.body); w.Code >= 300 {
			t.Fatalf("editable %s %s: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
	}
	want := []string{"site_network_created", "site_notes_updated", "site_circuit_created"}
	if len(audit.calls) != len(want) {
		t.Fatalf("audit calls = %+v", audit.calls)
	}
	for i, call := range audit.calls {
		if call.action != want[i] || call.resourceType != "site" || call.resourceID == nil || *call.resourceID != site {
			t.Errorf("audit %d = %+v, want %s on the site", i, call, want[i])
		}
		logged, _ := json.Marshal(call.changes)
		for _, secret := range []string{"8347-SECRET", "1-800-555-0100", "PIN 4411"} {
			if strings.Contains(string(logged), secret) {
				t.Errorf("audit %s carries %q: %s", call.action, secret, logged)
			}
		}
	}
}

// Through the API: a host address is saved as its network, a duplicate and
// an outside gateway are refused with their messages, an unknown circuit
// type is refused, and another site's ids are not found.
func TestDBSiteProfileThroughAPI(t *testing.T) {
	db := testdb.Open(t)
	court, annex := newSite(t, db, "Courthouse"), newSite(t, db, "Annex")
	r := siteProfileRouter(t, db, testdb.NewUser(t, db, true), &recordingAudit{})
	base := "/api/v1/sites/" + court.String()

	w := toolRequest(r, http.MethodPost, base+"/networks", `{"name":"Staff","cidr":"10.20.0.5/24","vlan":10,"gateway":"10.20.0.1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create network: %d %s", w.Code, w.Body.String())
	}
	net := dataOf[struct {
		ID   string `json:"id"`
		CIDR string `json:"cidr"`
	}](t, w)
	if net.CIDR != "10.20.0.0/24" {
		t.Errorf("cidr = %q, want 10.20.0.0/24", net.CIDR)
	}
	for _, c := range []struct{ body, want string }{
		{`{"name":"Again","cidr":"10.20.0.0/24"}`, "Courthouse already has 10.20.0.0/24"},
		{`{"name":"Bad","cidr":"10.30.0.0/24","gateway":"10.40.0.1"}`, "10.40.0.1 is outside 10.30.0.0/24"},
	} {
		if w := toolRequest(r, http.MethodPost, base+"/networks", c.body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s, want 400 %q", c.body, w.Code, w.Body.String(), c.want)
		}
	}
	if w := toolRequest(r, http.MethodPost, base+"/circuits", `{"provider":"Spectrum","kind":"satellite"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "type must be one of") {
		t.Errorf("unknown circuit type: %d %s", w.Code, w.Body.String())
	}
	w = toolRequest(r, http.MethodPost, base+"/circuits", `{"provider":"Spectrum","kind":"fiber","download_mbps":500}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create circuit: %d %s", w.Code, w.Body.String())
	}
	ckt := dataOf[struct {
		ID string `json:"id"`
	}](t, w)

	other := "/api/v1/sites/" + annex.String()
	if w := toolRequest(r, http.MethodPut, other+"/networks/"+net.ID, `{"name":"x","cidr":"10.9.0.0/24"}`); w.Code != http.StatusNotFound {
		t.Errorf("another site's network: %d, want 404", w.Code)
	}
	if w := toolRequest(r, http.MethodDelete, other+"/circuits/"+ckt.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("another site's circuit: %d, want 404", w.Code)
	}

	if w := toolRequest(r, http.MethodDelete, base+"/networks/"+net.ID, ""); w.Code != http.StatusOK {
		t.Errorf("delete network: %d %s", w.Code, w.Body.String())
	}
	profile := dataOf[struct {
		Networks []json.RawMessage `json:"networks"`
		Circuits []json.RawMessage `json:"circuits"`
	}](t, toolRequest(r, http.MethodGet, base+"/profile", ""))
	if len(profile.Networks) != 0 || len(profile.Circuits) != 1 {
		t.Errorf("profile after delete: %d networks, %d circuits", len(profile.Networks), len(profile.Circuits))
	}
}

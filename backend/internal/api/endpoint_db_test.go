package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func endpointRouter(t *testing.T, db *gorm.DB, caller uuid.UUID, audit *recordingAudit) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	sites := services.NewSiteService(db)
	devices := services.NewDeviceService(db, services.NewSNMPCredentialService(db), services.NewIncidentService(db))
	RegisterEndpointRoutes(v1, services.NewEndpointService(db, sites, nil), devices, sites, audit)
	return r
}

func TestDBEndpointRoutes(t *testing.T) {
	db := testdb.Open(t)
	dev := seedSwitch(t, db, "UBNT-ES16XG")
	var siteID string
	testdb.Must(t, db.Raw(`SELECT site_id::text FROM devices WHERE id = ?`, dev).Scan(&siteID).Error)
	site := uuid.MustParse(siteID)
	const mac = "aa:bb:cc:dd:ee:ff"
	testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, mac) VALUES (?, 5, ?)`, dev, mac)
	testdb.Exec(t, db, `INSERT INTO l2.dns_names (ip, name) VALUES ('10.0.0.50', '')`)
	testdb.Exec(t, db, `INSERT INTO l2.arp (device_id, ip, mac) VALUES (?, '10.0.0.50', ?)`, dev, mac)

	stranger := testdb.NewUser(t, db, false)
	r := endpointRouter(t, db, stranger, &recordingAudit{})
	if w := toolRequest(r, http.MethodGet, "/api/v1/network/endpoints/search?q=10.0.0.50", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("stranger search: %d %s", w.Code, w.Body.String())
	}
	if w := toolRequest(r, http.MethodGet, "/api/v1/network/endpoints/"+mac, ""); w.Code != http.StatusNotFound {
		t.Errorf("stranger record: %d", w.Code)
	}
	if w := toolRequest(r, http.MethodGet, "/api/v1/devices/"+dev.String()+"/endpoints", ""); w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
		t.Errorf("stranger connected view: %d", w.Code)
	}

	reader := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, site, reader)
	r = endpointRouter(t, db, reader, &recordingAudit{})
	if w := toolRequest(r, http.MethodGet, "/api/v1/network/endpoints/"+mac, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sightings":[`) {
		t.Errorf("reader record: %d %s", w.Code, w.Body.String())
	}
	if w := toolRequest(r, http.MethodPut, "/api/v1/network/endpoints/"+mac+"/name", `{"name":"Printer"}`); w.Code != http.StatusForbidden {
		t.Errorf("reader rename: %d", w.Code)
	}

	editor := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'editable')`, site, editor)
	audit := &recordingAudit{}
	r = endpointRouter(t, db, editor, audit)
	if w := toolRequest(r, http.MethodPut, "/api/v1/network/endpoints/"+mac+"/name", `{"name":"Printer"}`); w.Code != http.StatusOK {
		t.Fatalf("editor rename: %d %s", w.Code, w.Body.String())
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionEndpointNamed || audit.calls[0].resourceType != models.ResourceEndpoint ||
		audit.calls[0].changes.After["name"] != "Printer" || audit.calls[0].changes.Summary["mac"] != mac {
		t.Errorf("audit %+v", audit.calls)
	}
	w := toolRequest(r, http.MethodGet, "/api/v1/devices/"+dev.String()+"/endpoints", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"Printer"`) {
		t.Errorf("connected view: %d %s", w.Code, w.Body.String())
	}
	if w := toolRequest(r, http.MethodGet, "/api/v1/devices/"+uuid.NewString()+"/endpoints", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown device: %d", w.Code)
	}
}

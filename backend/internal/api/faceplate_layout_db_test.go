package api

import (
	"fmt"
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

// faceplateRouter mounts the faceplate layout routes for caller, wired as
// main.go does.
func faceplateRouter(t *testing.T, db *gorm.DB, caller uuid.UUID, audit *recordingAudit) *gin.Engine {
	t.Helper()
	r, v1, auth := siteTestGroup(t, db, caller)
	incidents := services.NewIncidentService(db)
	layouts := services.NewFaceplateLayoutService(db)
	testdb.Must(t, layouts.Load(t.Context()))
	ports := services.NewPortService(db, services.NewMetricsStore(db), incidents, services.NewSettingsService(db))
	ports.SetFaceplateLayouts(layouts)
	devices := services.NewDeviceService(db, services.NewSNMPCredentialService(db), incidents)
	RegisterFaceplateLayoutRoutes(v1, layouts, ports, devices, services.NewSiteService(db), audit, auth)
	return r
}

// seedSwitch adds a 16-port device of model in a new site.
func seedSwitch(t *testing.T, db *gorm.DB, model string) uuid.UUID {
	t.Helper()
	site, cred, dev := newSite(t, db, "site-"+uuid.NewString()[:8]), uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`, cred, "cred-"+cred.String()[:8])
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host, model) VALUES (?, ?, ?, 'core-sw', '10.0.0.2', ?)`, dev, site, cred, model)
	for i := 1; i <= 16; i++ {
		testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type) VALUES (?, ?, ?, 6)`, dev, i, fmt.Sprintf("0/%d", i))
	}
	return dev
}

const es16XGBody = `{"groups":[{"first":1,"last":12,"kind":"sfp","rows":2,"numbering":"alternate"},{"first":13,"last":16,"kind":"rj45","rows":1}]}`

func TestDBFaceplateLayoutReplaceAudited(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	dev := seedSwitch(t, db, "UBNT-ES16XG")
	audit := &recordingAudit{}
	r := faceplateRouter(t, db, admin, audit)
	path := "/api/v1/devices/" + dev.String() + "/faceplate-layout"

	if w := toolRequest(r, http.MethodPut, path, es16XGBody); w.Code != http.StatusOK {
		t.Fatalf("first save: %d %s", w.Code, w.Body.String())
	}
	one := `{"groups":[{"first":1,"last":16,"kind":"rj45","rows":1}]}`
	if w := toolRequest(r, http.MethodPut, path, one); w.Code != http.StatusOK {
		t.Fatalf("second save: %d %s", w.Code, w.Body.String())
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM faceplate_layouts`).Scan(&n).Error)
	if n != 1 || len(audit.calls) != 2 {
		t.Fatalf("%d rows, %d audit calls", n, len(audit.calls))
	}
	second := audit.calls[1]
	if second.action != models.ActionFaceplateLayoutSaved || second.resourceType != models.ResourceFaceplateLayout || second.resourceID == nil {
		t.Errorf("audit %+v", second)
	}
	if before, ok := second.changes.Before["groups"].(models.FaceplateGroups); !ok || len(before) != 2 {
		t.Errorf("before.groups %#v", second.changes.Before["groups"])
	}
	if second.changes.Summary["device_id"] != dev {
		t.Errorf("summary %#v", second.changes.Summary)
	}

	w := toolRequest(r, http.MethodGet, path, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"devices":1`) {
		t.Errorf("editor: %d %s", w.Code, w.Body.String())
	}
}

func TestDBFaceplateLayoutPutRefusesMisfit(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	dev := seedSwitch(t, db, "USW-16")
	audit := &recordingAudit{}
	r := faceplateRouter(t, db, admin, audit)
	w := toolRequest(r, http.MethodPut, "/api/v1/devices/"+dev.String()+"/faceplate-layout",
		`{"groups":[{"first":1,"last":15,"kind":"rj45","rows":1}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not placed 16") {
		t.Errorf("misfit: %d %s", w.Code, w.Body.String())
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM faceplate_layouts`).Scan(&n).Error)
	if n != 0 || len(audit.calls) != 0 {
		t.Errorf("%d rows, %d audit calls after a refused save", n, len(audit.calls))
	}
	w = toolRequest(r, http.MethodPost, "/api/v1/devices/"+dev.String()+"/faceplate-preview", `{"groups":[{"first":1,"last":4,"rows":1}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "SFP or RJ45") {
		t.Errorf("invalid preview: %d %s", w.Code, w.Body.String())
	}
	testdb.Exec(t, db, `UPDATE devices SET model = '' WHERE id = ?`, dev)
	w = toolRequest(r, http.MethodPut, "/api/v1/devices/"+dev.String()+"/faceplate-layout", es16XGBody)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Edit details") {
		t.Errorf("no model: %d %s", w.Code, w.Body.String())
	}
}

func TestDBFaceplateLayoutPutClearsSettings(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	dev := seedSwitch(t, db, "UBNT-ES16XG")
	testdb.Exec(t, db, `UPDATE devices SET faceplate_rows = 2, faceplate_port_style = 'rj45' WHERE id = ?`, dev)
	r := faceplateRouter(t, db, admin, &recordingAudit{})
	body := strings.TrimSuffix(es16XGBody, "}") + `,"clear_device_settings":true}`
	if w := toolRequest(r, http.MethodPut, "/api/v1/devices/"+dev.String()+"/faceplate-layout", body); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var cleared int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM devices WHERE id = ? AND faceplate_rows IS NULL AND faceplate_port_style IS NULL`, dev).Scan(&cleared).Error)
	if cleared != 1 {
		t.Error("device settings not cleared")
	}
}

func TestDBFaceplateLayoutDelete(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	dev := seedSwitch(t, db, "UBNT-ES16XG")
	audit := &recordingAudit{}
	r := faceplateRouter(t, db, admin, audit)
	w := toolRequest(r, http.MethodPut, "/api/v1/devices/"+dev.String()+"/faceplate-layout", es16XGBody)
	saved := dataOf[models.FaceplateLayout](t, w)
	path := "/api/v1/network/faceplate-layouts/" + saved.ID.String()
	if w := toolRequest(r, http.MethodDelete, path, ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := toolRequest(r, http.MethodDelete, path, ""); w.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", w.Code)
	}
	if last := audit.calls[len(audit.calls)-1]; last.action != models.ActionFaceplateLayoutDeleted || last.changes.Before == nil {
		t.Errorf("audit %+v", last)
	}
	if w := toolRequest(r, http.MethodGet, "/api/v1/network/faceplate-layouts", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"source":"builtin"`) {
		t.Errorf("list: %d %s", w.Code, w.Body.String())
	}
}

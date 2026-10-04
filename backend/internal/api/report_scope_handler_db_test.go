package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A port in a site the user cannot see and a port that does not exist must
// look the same over HTTP, byte for byte, in the preview and on create: the
// editor cannot be used to probe ids.
func TestDBScopeHiddenAndMissingLookAlike(t *testing.T) {
	db := testdb.Open(t)
	user := testdb.NewUser(t, db, false)
	site, cred, device, port := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Hidden')`, site)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'core-sw1', '10.0.0.2')`,
		device, site, cred)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, alias, if_type, present, oper_status, admin_status)
		VALUES (?, ?, 1, 'Gi1/0/1', 'uplink', 6, true, 'up', 'up')`, port, device)

	h := &ReportBuilder{db: db}
	h.SetNetworkScopes(netreport.NewBuilder(db, services.NewMetricsStore(db)))
	r := reportBuilderRouter(h, user)

	pairs := []struct {
		name           string
		hidden, absent map[string]any
		path           string
	}{
		{"preview ports",
			map[string]any{"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{port.String()}}},
			map[string]any{"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"preview sites",
			map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{site.String()}}},
			map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"preview devices",
			map[string]any{"scope_type": "devices", "scope_data": map[string]any{"device_ids": []string{device.String()}}},
			map[string]any{"scope_type": "devices", "scope_data": map[string]any{"device_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"create", metricsReportBody(port), metricsReportBody(uuid.New()), "/api/v1/reports/generate"},
	}
	for _, p := range pairs {
		hidden := do(r, http.MethodPost, p.path, p.hidden)
		absent := do(r, http.MethodPost, p.path, p.absent)
		if hidden.Code != http.StatusBadRequest || absent.Code != http.StatusBadRequest {
			t.Errorf("%s: statuses %d and %d, want 400 for both", p.name, hidden.Code, absent.Code)
			continue
		}
		if hidden.Body.String() != absent.Body.String() {
			t.Errorf("%s: hidden %s differs from missing %s", p.name, hidden.Body.String(), absent.Body.String())
		}
		if got := errorText(t, hidden.Body.String()); got != netreport.MsgNotAvailable {
			t.Errorf("%s: error %q, want %q", p.name, got, netreport.MsgNotAvailable)
		}
	}

	// Shared with the user, the same port is sized.
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, site, user)
	w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", pairs[0].hidden)
	if w.Code != http.StatusOK {
		t.Fatalf("visible port: status %d: %s", w.Code, w.Body.String())
	}
}

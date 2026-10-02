package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TestDBPublicLinkLeaksNothing builds a dashboard with one widget of every
// registered type over subjects full of sensitive strings, publishes it, and
// checks that no public response contains any of them. A new widget type
// fails here until it is given a config below.
func TestDBPublicLinkLeaksNothing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	deps := realDeps(t, db)
	registry := NewDefaultRegistry(deps)
	sites := services.NewSiteService(db)
	checker := NewDBChecker(db, sites, services.NewMonitorService(db))
	svc := NewService(db, sites, registry, checker)
	resolver := NewResolver(registry, checker)

	admin := testdb.NewUser(t, db, true)
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.77.0.9")
	testdb.Exec(t, db, `UPDATE devices SET status = 'down', status_detail = 'dial udp 10.77.0.9:161: i/o timeout' WHERE id = ?`, dev)
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	testdb.Exec(t, db, `UPDATE device_interfaces SET mac = 'aa:bb:cc:dd:ee:ff' WHERE id = ?`, port)
	ups := newDevice(t, db, site, "ups-1", "10.77.0.50")
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'uplink', neighbor_device_id = ? WHERE id = ?`, ups, port)
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups' WHERE id = ?`, ups)
	mon := newMonitor(t, db, admin, "Intranet", "https://intranet.secret.test/health")
	agent := newAgent(t, db, "fileserver")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, response_time_ms, status_code, error_message, timestamp) VALUES (?, 'failed', 0, 0, 'dial tcp 10.77.0.9:443: connection refused', ?)`,
		mon, now.Add(-5*time.Minute))
	writePortBps(t, deps, dev, port, 1, now.Add(-10*time.Minute), 5000, 100)
	m := deps.Metrics.(*services.MetricsStore)
	testdb.Must(t, m.Write(ctx, ups, now.Add(-time.Minute), []services.SamplePoint{{Metric: services.MetricUPSChargePct, Value: 90}}))
	incident := uuid.New()
	testdb.Exec(t, db, `INSERT INTO incidents (id, device_id, start_time, severity, root_cause, notes) VALUES (?, ?, ?, 'high', 'cannot reach 10.77.0.9', 'call Bob at x123')`,
		incident, dev, now.Add(-time.Hour))
	testdb.Exec(t, db, `INSERT INTO port_events (device_id, interface_id, if_index, kind, started_at, detail) VALUES (?, ?, 1, 'link_down', ?, '{}')`,
		dev, port, now.Add(-30*time.Minute))

	configs := map[string]string{
		"label":          `{"text":"HQ"}`,
		"timeseries":     fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s"],"range":"1h"}`, dev),
		"stat":           fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","range":"1h"}`, dev),
		"port_grid":      fmt.Sprintf(`{"device_id":"%s"}`, dev),
		"device_health":  fmt.Sprintf(`{"device_id":"%s"}`, dev),
		"site_power":     fmt.Sprintf(`{"site_id":"%s"}`, site),
		"top_n":          fmt.Sprintf(`{"site_id":"%s","measure":"traffic","n":5,"range":"1h"}`, site),
		"event_log":      fmt.Sprintf(`{"site_id":"%s","limit":10}`, site),
		"device_table":   fmt.Sprintf(`{"site_id":"%s"}`, site),
		"open_incidents": `{"scope":"all","limit":10}`,
		"monitors":       fmt.Sprintf(`{"monitors":["%s"],"agents":["%s"],"style":"bars","window":"24h"}`, mon, agent),
	}
	dash := newDashboard(t, db, "Wall", &site, nil)
	for _, typ := range registry.Types() {
		cfg, ok := configs[typ]
		if !ok {
			t.Fatalf("widget type %q has no config in this test; add one so the public trim is checked for it", typ)
		}
		wd, _ := registry.Get(typ)
		clean, err := wd.Validate(ctx, json.RawMessage(cfg))
		testdb.Must(t, err)
		newWidget(t, db, dash, typ, string(clean))
	}
	publish(t, db, dash, admin)

	secrets := []string{"10.77.0.9", "10.77.0.50", "i/o timeout", "intranet.secret.test", "cannot reach", "call Bob", "connection refused",
		"aa:bb:cc:dd:ee:ff", site.String(), dev.String(), ups.String(), port.String(), mon.String(), agent.String(), incident.String()}

	d, err := svc.Get(ctx, Viewer{UserID: admin, IsAdmin: true}, dash)
	testdb.Must(t, err)
	layout, err := svc.PublicLayout(ctx, &d.Dashboard)
	testdb.Must(t, err)
	check := func(what string, v any) {
		raw, err := json.Marshal(v)
		testdb.Must(t, err)
		for _, s := range secrets {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s leaks %q: %s", what, s, raw)
			}
		}
	}
	check("public layout", layout)
	if len(layout.Widgets) != len(registry.Types()) {
		t.Fatalf("public layout has %d widgets, want one per type (%d)", len(layout.Widgets), len(registry.Types()))
	}
	sawDevice := false
	for _, pw := range layout.Widgets {
		w, err := svc.PublicWidget(ctx, &d.Dashboard, pw.ID)
		testdb.Must(t, err)
		resp, err := resolver.ResolvePublic(ctx, &d.Dashboard, w)
		testdb.Must(t, err)
		check(pw.Type+" widget", resp)
		if pw.Type == "device_table" {
			raw, err := json.Marshal(resp)
			testdb.Must(t, err)
			sawDevice = strings.Contains(string(raw), `"core"`)
		}
	}
	// Positive control: the trim must not be passing because everything is empty.
	if !sawDevice {
		t.Error("public device_table response does not carry the device name; the leak checks may be vacuous")
	}
}

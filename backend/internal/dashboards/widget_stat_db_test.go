package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBStat(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "")
	now := time.Now().UTC()
	in := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now}

	_, err := resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h"}`, dev), in)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("no samples: err = %v, want ErrNoData", err)
	}
	writePortBps(t, d, dev, port, 1, now.Add(-30*time.Minute), 1000, 0)
	writePortBps(t, d, dev, port, 1, now.Add(-5*time.Minute), 3000, 0)

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h","warn":2000,"crit":5000,"sparkline":true}`, dev), in)
	testdb.Must(t, err)
	s := data.(StatData)
	if s.Value != 3000 || s.Level != "warn" || s.Unit != "bps" || len(s.Spark) != 2 || s.Mode != "latest" {
		t.Errorf("latest = %+v, want 3000 bps, warn, two sparkline points", s)
	}
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h","mode":"average"}`, dev), in)
	testdb.Must(t, err)
	if s = data.(StatData); s.Value != 2000 || s.Spark != nil {
		t.Errorf("average = %+v, want 2000 and no sparkline", s)
	}
}

func TestDBStatSiteTotal(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	a, b := newDevice(t, db, site, "a", "10.0.0.1"), newDevice(t, db, site, "b", "10.0.0.2")
	now := time.Now().UTC()
	pa, pb := newPort(t, db, a, 1, "Gi1", ""), newPort(t, db, b, 1, "Gi1", "")
	writePortBps(t, d, a, pa, 1, now.Add(-5*time.Minute), 100, 0)
	writePortBps(t, d, b, pb, 1, now.Add(-5*time.Minute), 30, 0)
	writePortMetric(t, d, a, pa, 1, now.Add(-5*time.Minute), services.MetricIfInUtilPct, 20)
	writePortMetric(t, d, b, pb, 1, now.Add(-5*time.Minute), services.MetricIfInUtilPct, 70)
	cfg := fmt.Sprintf(`{"metric":"if_in_bps","site_id":"%s","range":"1h"}`, site)
	data, err := resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	if s := data.(StatData); s.Value != 130 || s.Label != "Site total · Traffic in" {
		t.Errorf("site total = %+v, want 130", s)
	}
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_util_pct","site_id":"%s","range":"1h"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	if s := data.(StatData); s.Value != 45 {
		t.Errorf("site busy in = %v, want 45 (averaged, not 90)", s.Value)
	}
	_, err = resolveWidget(t, w, cfg, ResolveInput{Now: now})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("site not visible: err = %v, want ErrNoData", err)
	}
}

// A device total (no row chosen) combines the device's physical ports: a
// percentage is averaged, a rate is added up, and a virtual interface is left
// out of both.
func TestDBStatDeviceTotal(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	p1, p2 := newPort(t, db, dev, 1, "Gi1/0/1", ""), newPort(t, db, dev, 2, "Gi1/0/2", "")
	vlan := newPort(t, db, dev, 100, "Vlan10", "")
	now := time.Now().UTC()
	at := now.Add(-5 * time.Minute)
	writePortMetric(t, d, dev, p1, 1, at, services.MetricIfInUtilPct, 40)
	writePortMetric(t, d, dev, p2, 2, at, services.MetricIfInUtilPct, 60)
	writePortMetric(t, d, dev, vlan, 100, at, services.MetricIfInUtilPct, 100)
	writePortBps(t, d, dev, p1, 1, at, 100, 0)
	writePortBps(t, d, dev, p2, 2, at, 250, 0)
	writePortBps(t, d, dev, vlan, 100, at, 5000, 0)
	in := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_util_pct","device_id":"%s","range":"1h","sparkline":true}`, dev), in)
	testdb.Must(t, err)
	if s := data.(StatData); s.Value != 50 || len(s.Spark) != 1 || s.Spark[0].Avg != 50 {
		t.Errorf("busy in over two ports = %+v, want 50 for the value and the sparkline (averaged, not 100)", s)
	}
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_util_pct","device_id":"%s","range":"1h","mode":"average"}`, dev), in)
	testdb.Must(t, err)
	if s := data.(StatData); s.Value != 50 {
		t.Errorf("average busy in = %v, want 50", s.Value)
	}
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","range":"1h"}`, dev), in)
	testdb.Must(t, err)
	if s := data.(StatData); s.Value != 350 {
		t.Errorf("traffic in = %v, want 350 (two physical ports summed, Vlan10 left out)", s.Value)
	}
}

func TestDBStatUnknownMetricIsNoData(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	cfg := json.RawMessage(fmt.Sprintf(`{"metric":"fan_rpm","device_id":"%s","range":"1h"}`, dev))
	_, err := w.Resolve(context.Background(), cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: time.Now()})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
}

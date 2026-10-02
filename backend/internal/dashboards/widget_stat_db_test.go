package dashboards

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

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

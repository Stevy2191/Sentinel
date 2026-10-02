package dashboards

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBPortGridTrimsForPublic(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := portGridWidget{devices: d.Devices, ports: d.Ports}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	testdb.Exec(t, db, `UPDATE device_interfaces SET mac = 'aa:bb:cc:dd:ee:ff' WHERE id = ?`, port)
	cfg := fmt.Sprintf(`{"device_id":"%s"}`, dev)

	data, err := resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}})
	testdb.Must(t, err)
	pg := data.(PortGridData)
	if pg.DeviceID == nil || *pg.DeviceID != dev || len(pg.Ports) != 1 || pg.Ports[0].MAC != "aa:bb:cc:dd:ee:ff" || pg.DeviceName != "core" {
		t.Errorf("logged in = %+v", pg)
	}
	data, err = resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Viewer: PublicViewer})
	testdb.Must(t, err)
	pg = data.(PortGridData)
	if pg.DeviceID != nil || pg.Ports[0].ID != uuid.Nil || pg.Ports[0].DeviceID != uuid.Nil || pg.Ports[0].MAC != "" {
		t.Errorf("public = %+v, want ids and MAC blanked", pg)
	}
	if pg.Ports[0].Alias != "uplink" || pg.Ports[0].Name != "Gi1/0/1" {
		t.Errorf("public port lost its name or alias: %+v", pg.Ports[0])
	}
}

func TestDBDeviceHealthNoDataWithoutProfiles(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := deviceHealthWidget{devices: d.Devices, health: d.Health}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	_, err := resolveWidget(t, w, fmt.Sprintf(`{"device_id":"%s"}`, dev), ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData for a device with no health metrics", err)
	}
}

func TestDBSitePower(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := sitePowerWidget{devices: d.Devices, ports: d.Ports}
	site := newSite(t, db, "HQ")
	ups := newDevice(t, db, site, "ups-1", "10.0.0.50")
	newDevice(t, db, site, "core", "10.0.0.1")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups' WHERE id = ?`, ups)
	m := d.Metrics.(*services.MetricsStore)
	testdb.Must(t, m.Write(context.Background(), ups, time.Now().UTC().Add(-time.Minute), []services.SamplePoint{
		{Metric: services.MetricUPSChargePct, Value: 97}, {Metric: services.MetricUPSOnBattery, Value: 0},
	}))
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}}
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, site), in)
	testdb.Must(t, err)
	sp := data.(SitePowerData)
	if len(sp.UPSes) != 1 || sp.UPSes[0].Name != "ups-1" || sp.UPSes[0].Readings[services.MetricUPSChargePct] != 97 || sp.UPSes[0].DeviceID == nil {
		t.Errorf("site power = %+v, want the one UPS with its charge", sp)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, site), in)
	testdb.Must(t, err)
	if data.(SitePowerData).UPSes[0].DeviceID != nil {
		t.Error("public site power carries the device id")
	}
	empty := newSite(t, db, "Closet")
	if _, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, empty), ResolveInput{Visible: Subjects{Sites: []uuid.UUID{empty}}}); !errors.Is(err, ErrNoData) {
		t.Errorf("a site without UPSes: err = %v, want ErrNoData", err)
	}
}

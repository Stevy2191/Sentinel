package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func portsFixture(t *testing.T) (*PortService, *DeviceService, *DeviceView, *MetricsStore) {
	t.Helper()
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ctx := context.Background()
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, incidents, NewSettingsService(db))
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	return ports, devices, d, metrics
}

func TestDBDevicePortsView(t *testing.T) {
	ports, _, d, metrics := portsFixture(t)
	ctx := context.Background()
	testdb.Must(t, metrics.Write(ctx, d.ID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInBps, Instance: "49", Value: 4e9},
		{Metric: MetricIfInUtilPct, Instance: "49", Value: 40},
		{Metric: MetricIfInErrorsPM, Instance: "49", Value: 3},
	}))
	v, err := ports.DevicePorts(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Ports) != 53 || v.Defaults.UtilThresholdPct != 80 {
		t.Fatalf("ports %d defaults %+v", len(v.Ports), v.Defaults)
	}
	var p49, cpu PortView
	for _, p := range v.Ports {
		switch p.IfIndex {
		case 49:
			p49 = p
		case 65:
			cpu = p
		}
	}
	if p49.Number != 49 || !p49.Physical || !p49.Collected || p49.InBps == nil || *p49.InBps != 4e9 ||
		p49.ErrorsPerMin == nil || *p49.ErrorsPerMin != 3 || p49.OutBps != nil {
		t.Errorf("port 49: %+v", p49)
	}
	if cpu.Physical || cpu.Collected {
		t.Errorf("cpu interface: %+v", cpu)
	}
	if len(v.Faceplates) != 1 || v.Faceplates[0].Unit != 0 || v.Faceplates[0].Label != "" ||
		v.Faceplates[0].Rows != 2 || len(v.Faceplates[0].Blocks) != 5 {
		t.Errorf("faceplates %+v", v.Faceplates)
	}
}

// DevicePorts and Port resolve a port's neighbor device name and port label
// without an N+1 of queries (one for devices, one for ports, regardless of
// how many ports have a neighbor).
func TestDBDevicePortsViewResolvesNeighbor(t *testing.T) {
	ports, _, d, _ := portsFixture(t)
	ctx := context.Background()
	router := seedDeviceInSite(t, ports.db, d.SiteID, "10.0.0.1")
	testdb.Exec(t, ports.db, `UPDATE devices SET name = 'Quantum-Gate' WHERE id = ?`, router)
	seedPort(t, ports.db, router, 10, "0/10", "Core uplink")

	ten := 10
	_, _, err := ports.UpdatePort(ctx, d.ID, 49, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)

	v, err := ports.DevicePorts(ctx, d)
	testdb.Must(t, err)
	var p49 PortView
	for _, p := range v.Ports {
		if p.IfIndex == 49 {
			p49 = p
		}
	}
	if p49.NeighborDeviceName != "Quantum-Gate" || p49.NeighborPortNumber == nil || *p49.NeighborPortNumber != 10 ||
		p49.NeighborPortUnit != 0 || p49.NeighborPortAlias != "Core uplink" {
		t.Errorf("port 49 neighbor: %+v", p49)
	}

	detail, err := ports.Port(ctx, d, 49)
	testdb.Must(t, err)
	if detail.NeighborDeviceName != "Quantum-Gate" || detail.NeighborPortNumber == nil || *detail.NeighborPortNumber != 10 ||
		detail.NeighborPortUnit != 0 || detail.NeighborPortAlias != "Core uplink" {
		t.Errorf("port detail neighbor: %+v", detail)
	}
}

// Fix round 1, Important 3: resolveNeighbors must not show a name or label
// for a neighbor outside this device's site — defense in depth beyond
// UpdatePort's same-site validation and DeviceService.Update's clearing on a
// site move, for any row that ends up cross-site regardless (e.g. legacy
// data, or a direct database edit).
func TestDBDevicePortsViewHidesCrossSiteNeighbor(t *testing.T) {
	ports, _, d, _ := portsFixture(t)
	ctx := context.Background()
	other := seedDevice(t, ports.db, "Branch", "10.0.0.9")
	testdb.Exec(t, ports.db, `UPDATE devices SET name = 'Branch-Core' WHERE id = ?`, other.DeviceID)
	testdb.Exec(t, ports.db, `UPDATE device_interfaces SET role = 'uplink', neighbor_device_id = ? WHERE device_id = ? AND if_index = 1`,
		other.DeviceID, d.ID)

	v, err := ports.DevicePorts(ctx, d)
	testdb.Must(t, err)
	var p1 PortView
	for _, p := range v.Ports {
		if p.IfIndex == 1 {
			p1 = p
		}
	}
	if p1.NeighborDeviceName != "" || p1.NeighborPortNumber != nil {
		t.Errorf("cross-site neighbor should show no name or port: %+v", p1)
	}

	detail, err := ports.Port(ctx, d, 1)
	testdb.Must(t, err)
	if detail.NeighborDeviceName != "" || detail.NeighborPortNumber != nil {
		t.Errorf("cross-site neighbor (port detail) should show no name or port: %+v", detail)
	}
}

// A stacked device's DevicePorts gives one faceplate per member.
func TestDBDevicePortsViewStackedFaceplates(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, stackInventory(), time.Now()))
	ports := NewPortService(db, NewMetricsStore(db), incidents, NewSettingsService(db))
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)

	v, err := ports.DevicePorts(ctx, d)
	testdb.Must(t, err)
	if len(v.Faceplates) != 2 || v.Faceplates[0].Unit != 1 || v.Faceplates[0].Label != "Switch 1" ||
		v.Faceplates[1].Unit != 2 || v.Faceplates[1].Label != "Switch 2" {
		t.Fatalf("faceplates %+v", v.Faceplates)
	}
}

func TestDBUpdatePortClosesIncidentsWhenUnmarked(t *testing.T) {
	ports, _, d, _ := portsFixture(t)
	ctx := context.Background()
	yes, no := true, false
	_, after, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &yes}})
	if err != nil || !after.Important {
		t.Fatalf("mark: %+v %v", after, err)
	}
	if _, _, err := ports.incidents.OpenPortIncident(ctx, d.ID, after.ID, "link_down", time.Now().UTC(), "down"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &no}}); err != nil {
		t.Fatal(err)
	}
	open, _ := ports.incidents.OpenPortIncidents(ctx, after.ID)
	if len(open) != 0 {
		t.Fatalf("un-marking left %d incidents open", len(open))
	}
	if _, _, err := ports.UpdatePort(ctx, d.ID, 999, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &yes}}); !errors.Is(err, ErrPortNotFound) {
		t.Errorf("missing port: %v", err)
	}
}

// Turning collection off must not just close incidents: the port is no
// longer polled, so nothing else will ever clear its stale conditions or end
// its open event spans. UpdatePort must do that itself, in the same
// transaction as the column change and the incident close.
func TestDBUpdatePortClosesIncidentsWhenCollectionStops(t *testing.T) {
	ports, _, d, _ := portsFixture(t)
	ctx := context.Background()
	yes, no := true, false

	if _, _, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Collect: models.Opt[bool]{Set: true, Value: &yes}}); err != nil {
		t.Fatal(err)
	}
	var before models.DeviceInterface
	testdb.Must(t, ports.db.First(&before, "device_id = ? AND if_index = ?", d.ID, 3).Error)
	testdb.Exec(t, ports.db, `UPDATE device_interfaces SET conditions = '["errors"]',
		conditions_since = '{"errors": "2020-01-01T00:00:00Z"}' WHERE id = ?`, before.ID)
	testdb.Must(t, ports.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: d.ID, InterfaceID: before.ID, IfIndex: 3, Kind: "errors", StartedAt: time.Now().Add(-time.Hour)},
	}, nil))
	if _, _, err := ports.incidents.OpenPortIncident(ctx, d.ID, before.ID, "errors", time.Now().UTC(), "errors"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Collect: models.Opt[bool]{Set: true, Value: &no}}); err != nil {
		t.Fatal(err)
	}

	var after models.DeviceInterface
	testdb.Must(t, ports.db.First(&after, "id = ?", before.ID).Error)
	if len(after.Conditions) != 0 {
		t.Errorf("conditions not cleared: %v", after.Conditions)
	}
	if len(after.ConditionsSince) != 0 {
		t.Errorf("conditions_since not cleared: %v", after.ConditionsSince)
	}
	var ev models.PortEvent
	testdb.Must(t, ports.db.First(&ev, "interface_id = ? AND kind = 'errors'", before.ID).Error)
	if ev.EndedAt == nil {
		t.Error("open event span left running")
	}
	open, err := ports.incidents.OpenPortIncidents(ctx, before.ID)
	testdb.Must(t, err)
	if len(open) != 0 {
		t.Fatalf("turning off collection left %d incidents open", len(open))
	}
	var closed models.Incident
	testdb.Must(t, ports.db.Where("interface_id = ?", before.ID).Order("start_time DESC").First(&closed).Error)
	if closed.ResolutionNotes != "Statistics are no longer collected for this port." {
		t.Errorf("resolution notes = %q", closed.ResolutionNotes)
	}

	// collect: null reverts to the classifier's default. The CPU interface
	// (ifIndex 65) defaults to not collected, so setting collect=true and
	// then clearing it back to null also stops collection, and gets the
	// same cleanup.
	if _, _, err := ports.UpdatePort(ctx, d.ID, 65, models.PortPatch{Collect: models.Opt[bool]{Set: true, Value: &yes}}); err != nil {
		t.Fatal(err)
	}
	var cpu models.DeviceInterface
	testdb.Must(t, ports.db.First(&cpu, "device_id = ? AND if_index = ?", d.ID, 65).Error)
	if _, _, err := ports.incidents.OpenPortIncident(ctx, d.ID, cpu.ID, "link_down", time.Now().UTC(), "down"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ports.UpdatePort(ctx, d.ID, 65, models.PortPatch{Collect: models.Opt[bool]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	openCPU, err := ports.incidents.OpenPortIncidents(ctx, cpu.ID)
	testdb.Must(t, err)
	if len(openCPU) != 0 {
		t.Fatalf("collect:null left %d incidents open", len(openCPU))
	}
}

func TestDBEventsAndSiteSummary(t *testing.T) {
	ports, _, d, metrics := portsFixture(t)
	ctx := context.Background()
	var p5 models.DeviceInterface
	testdb.Must(t, ports.db.First(&p5, "device_id = ? AND if_index = 5", d.ID).Error)
	testdb.Must(t, ports.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: d.ID, InterfaceID: p5.ID, IfIndex: 5, Kind: "link_down", StartedAt: time.Now().Add(-time.Hour)},
		{DeviceID: d.ID, InterfaceID: p5.ID, IfIndex: 5, Kind: "errors", StartedAt: time.Now().Add(-time.Minute)},
	}, nil))
	testdb.Exec(t, ports.db, `UPDATE device_interfaces SET conditions = '["errors"]' WHERE id = ?`, p5.ID)
	testdb.Must(t, metrics.Write(ctx, d.ID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInUtilPct, Instance: "7", Value: 91}, {Metric: MetricIfOutUtilPct, Instance: "7", Value: 2},
		{Metric: MetricIfInUtilPct, Instance: "8", Value: 12},
	}))

	five := 5
	evs, total, err := ports.Events(ctx, PortEventFilter{DeviceID: &d.ID, IfIndex: &five})
	if err != nil || total != 2 || len(evs) != 2 || evs[0].Kind != "errors" || evs[0].PortNumber != 5 || evs[0].DeviceName == "" {
		t.Fatalf("events %+v total %d err %v", evs, total, err)
	}
	other := uuid.New()
	if evs, _, _ := ports.Events(ctx, PortEventFilter{SiteID: &other}); len(evs) != 0 {
		t.Errorf("another site's filter returned %d events", len(evs))
	}

	sum, err := ports.SiteSummary(ctx, d.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Busiest) == 0 || sum.Busiest[0].IfIndex != 7 || *sum.Busiest[0].UtilPct != 91 {
		t.Errorf("busiest %+v", sum.Busiest)
	}
	if len(sum.Problems) != 1 || sum.Problems[0].IfIndex != 5 {
		t.Errorf("problems %+v", sum.Problems)
	}
}

func TestDBUpdateDetailsAndEffectiveValues(t *testing.T) {
	_, devices, d, _ := portsFixture(t)
	ctx := context.Background()
	model := "UNVR (4-bay)"
	nvr := "nvr"
	_, _, err := devices.UpdateDetails(ctx, d.ID, models.DeviceDetailsPatch{
		ModelOverride: models.Opt[string]{Set: true, Value: &model},
		DeviceType:    models.Opt[string]{Set: true, Value: &nvr},
	})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := devices.Get(ctx, d.ID)
	if v.EffectiveModel != model || v.Model != "USW-Pro-48-PoE" || v.EffectiveType != "nvr" || v.EffectiveVendor != "Ubiquiti (EdgeSwitch)" {
		t.Errorf("effective %q/%q/%q, reported model %q", v.EffectiveVendor, v.EffectiveModel, v.EffectiveType, v.Model)
	}
	// Clearing brings the SNMP value back.
	if _, _, err := devices.UpdateDetails(ctx, d.ID, models.DeviceDetailsPatch{ModelOverride: models.Opt[string]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	v, _ = devices.Get(ctx, d.ID)
	if v.EffectiveModel != "USW-Pro-48-PoE" {
		t.Errorf("after clearing: %q", v.EffectiveModel)
	}
}

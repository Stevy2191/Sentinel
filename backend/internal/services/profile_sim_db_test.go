package services

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TestDBSimCiscoHealthEndToEnd polls a real Cisco profile run against the
// simulated 2960X (community "cisco"): MIB builtins, the starter profile and
// every metric kind (gauge columns, used/free percentages, status rows from
// two different tables, and the entity-sensor temperature/voltage split)
// land as labelled samples, exactly one rule fires (Fan 2 critical; PS B
// notPresent is OK), and the device's profile run is recorded OK.
func TestDBSimCiscoHealthEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	db := testdb.Open(t)
	ctx := context.Background()
	lib := NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))
	s := seedDevice(t, db, "HQ", host)
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208', name = 'sim-cisco' WHERE id = ?`, s.DeviceID)
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "cisco"}, Timeout: 2 * time.Second, Retries: 1}

	incidents := NewIncidentService(db)
	metrics := NewMetricsStore(db)
	notif := &fakeNotifier{}
	mon := NewProfileMonitor(profiles, metrics, incidents, notif, snmp.GoSNMPClient{}, NewPortService(db, metrics, incidents, NewSettingsService(db)))
	mon.PollProfiles(ctx, d, target)

	type row struct {
		Metric, Instance, Label string
		Value                   float64
	}
	var got []row
	testdb.Must(t, db.Raw(`SELECT s.metric, s.instance, s.label, x.value FROM metrics.series s
		JOIN LATERAL (SELECT value FROM metrics.samples WHERE series_id = s.id ORDER BY time DESC LIMIT 1) x ON true
		WHERE s.device_id = ? ORDER BY s.metric, s.instance`, d.ID).Scan(&got).Error)
	want := map[string]row{
		"cisco_cpu_5min|1":          {Label: "Switch 1", Value: 23},
		"cisco_cpu_5min|2":          {Label: "Row 2", Value: 4},
		"cisco_mem_used_pct|1.1":    {Label: "Processor", Value: 30},
		"cisco_mem_pool_used_pct|1": {Label: "Processor", Value: 60},
		"cisco_temp_envmon|1":       {Label: "Switch 1 Inlet", Value: 31},
		"cisco_temp_sensor|1010":    {Label: "Switch 1 - Inlet Temp Sensor", Value: 41.5},
		"cisco_fan_envmon|2":        {Label: "Switch 1 Fan 2", Value: 3},
		"cisco_psu_envmon|2":        {Label: "Switch 1 PS B", Value: 5},
		"cisco_fan_fru|1020":        {Label: "Switch 1 - FAN-T1", Value: 2},
		"cisco_psu_fru|1030":        {Label: "Switch 1 - Power Supply A", Value: 2},
	}
	have := map[string]row{}
	for _, r := range got {
		have[r.Metric+"|"+r.Instance] = r
	}
	for k, w := range want {
		if h, ok := have[k]; !ok || h.Label != w.Label || h.Value != w.Value {
			t.Errorf("%s: got %+v, want label %q value %v", k, h, w.Label, w.Value)
		}
	}
	if _, ok := have["cisco_temp_sensor|1011"]; ok {
		t.Error("the volts sensor was stored as a temperature")
	}
	// Exactly one alert: Fan 2 critical (PS B notPresent counts as OK).
	open, err := incidents.OpenMetricIncidents(ctx, d.ID)
	testdb.Must(t, err)
	if len(open) != 1 || *open[0].MetricKey != "cisco_fan_envmon" || *open[0].MetricInstance != "2" || len(notif.sent) != 1 {
		t.Fatalf("open %d, sent %d", len(open), len(notif.sent))
	}
	runs, err := profiles.DeviceProfiles(ctx, d)
	testdb.Must(t, err)
	if len(runs) != 1 || runs[0].LastRun == nil || !runs[0].LastRun.OK {
		t.Errorf("run %+v", runs)
	}
}

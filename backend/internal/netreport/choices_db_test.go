package netreport

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func choiceKeys(cs []MetricChoice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key
	}
	return out
}

func choiceSource(cs []MetricChoice, key string) string {
	for _, c := range cs {
		if c.Key == key {
			return c.Source
		}
	}
	return ""
}

// The Metrics step offers what the scope's devices or ports have stored,
// without text metrics or link speed, and pre-fills the defaults of spec §1.
func TestDBPreviewChoicesAndDefaults(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw, ups, plain := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, hq, "ups1"), newDevice(t, db, hq, "edge-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	for _, m := range []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct,
		services.MetricIfOutUtilPct, services.MetricIfInErrorsPM, services.MetricIfSpeedBps} {
		portSeries(t, db, sw, gi1, 1, m)
	}
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	newProfileMetric(t, db, "Core health", true, "core_fan", "Fan", "", "status", 1)
	newProfileMetric(t, db, "Lab sensors", false, "lab_temp", "Room temperature", "°C", "gauge", 0)
	newSeries(t, db, sw, "core_cpu", "1", nil, "Switch 1")
	newSeries(t, db, sw, "core_fan", "1", nil, "Fan 1")
	newSeries(t, db, sw, "lab_temp", "1", nil, "Rack A")
	for _, m := range services.UPSMetrics {
		newSeries(t, db, ups, m, "", nil, "")
	}
	edge := newPort(t, db, plain, 1, "Gi1", gigabit)
	portSeries(t, db, plain, edge, 1, services.MetricIfInBps)
	portSeries(t, db, plain, edge, 1, services.MetricIfOutBps)
	b := newBuilder(db)

	// Devices with profiles and a UPS.
	p, err := b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw, ups}})
	testdb.Must(t, err)
	wantChoices := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct,
		services.MetricIfInErrorsPM, services.MetricUPSChargePct, services.MetricUPSRuntimeMin, services.MetricUPSLoadPct,
		services.MetricUPSInputV, services.MetricUPSOutputV, services.MetricUPSBatteryTempC, services.MetricUPSOnBattery,
		"core_cpu", "lab_temp"}
	if !slices.Equal(choiceKeys(p.Metrics), wantChoices) {
		t.Errorf("device choices = %v, want %v (no enum, no status metric, no link speed)", choiceKeys(p.Metrics), wantChoices)
	}
	if choiceSource(p.Metrics, "core_cpu") != "profile" || choiceSource(p.Metrics, "lab_temp") != "custom" ||
		choiceSource(p.Metrics, services.MetricIfInBps) != "builtin" {
		t.Errorf("sources = %+v", p.Metrics)
	}
	wantDefaults := []string{"core_cpu", "lab_temp", services.MetricUPSChargePct, services.MetricUPSLoadPct,
		services.MetricUPSRuntimeMin, services.MetricUPSOnBattery}
	if !slices.Equal(p.Defaults, wantDefaults) {
		t.Errorf("device defaults = %v, want %v", p.Defaults, wantDefaults)
	}
	if p.Ports != 1 || p.Devices != 2 || p.Capped {
		t.Errorf("devices size = %d ports, %d devices, capped %v; want 1, 2, false", p.Ports, p.Devices, p.Capped)
	}

	// A plain switch: traffic in and out, as device totals.
	p, err = b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{plain}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps}; !slices.Equal(p.Defaults, want) || !slices.Equal(choiceKeys(p.Metrics), want) {
		t.Errorf("plain switch: choices %v, defaults %v; want both %v", choiceKeys(p.Metrics), p.Defaults, want)
	}

	// Ports: port metrics only, and the four port defaults.
	p, err = b.Preview(ctx, admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct,
		services.MetricIfInErrorsPM}; !slices.Equal(choiceKeys(p.Metrics), want) {
		t.Errorf("port choices = %v, want %v", choiceKeys(p.Metrics), want)
	}
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}; !slices.Equal(p.Defaults, want) {
		t.Errorf("port defaults = %v, want %v", p.Defaults, want)
	}
	if p.Ports != 1 || p.Devices != 1 {
		t.Errorf("ports size = %d ports, %d devices; want 1, 1", p.Ports, p.Devices)
	}

	// Sites: device metrics are offered too; traffic in and out by default.
	p, err = b.Preview(ctx, admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps}; !slices.Equal(p.Defaults, want) {
		t.Errorf("site defaults = %v, want %v", p.Defaults, want)
	}
	if !slices.Contains(choiceKeys(p.Metrics), "core_cpu") || slices.Contains(choiceKeys(p.Metrics), services.MetricUPSBatteryStatus) {
		t.Errorf("site choices = %v, want core_cpu and no enum", choiceKeys(p.Metrics))
	}
	if p.Ports != 2 || p.Devices != 3 || p.Capped {
		t.Errorf("site size = %d ports, %d devices, capped %v; want 2, 3, false", p.Ports, p.Devices, p.Capped)
	}

	// Port roles: every port is "access" until someone says otherwise.
	p, err = b.Preview(ctx, admin, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq}, Roles: []string{models.PortRoleAccess}})
	testdb.Must(t, err)
	if p.Ports != 2 || p.Devices != 2 {
		t.Errorf("access ports size = %d ports, %d devices; want 2, 2", p.Ports, p.Devices)
	}
}

// Capped only when the subjects exceed 500: ports for a port_roles scope,
// devices (not their ports) for a devices scope.
func TestDBPreviewCapsAbove500(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type, present, collect_default, role)
		SELECT ?, g, 'Gi' || g, 6, true, true, 'wan' FROM generate_series(1, 501) g`, sw)
	b := newBuilder(db)
	wan := models.ReportScope{SiteIDs: []uuid.UUID{hq}, Roles: []string{models.PortRoleWAN}}

	p, err := b.Preview(ctx, admin, models.ScopeTypePortRoles, wan)
	testdb.Must(t, err)
	if p.Ports != 501 || p.Devices != 1 || !p.Capped {
		t.Errorf("501 WAN ports: %d ports, %d devices, capped %v; want 501, 1, true", p.Ports, p.Devices, p.Capped)
	}
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'access' WHERE device_id = ? AND if_index = 501`, sw)
	p, err = b.Preview(ctx, admin, models.ScopeTypePortRoles, wan)
	testdb.Must(t, err)
	if p.Ports != 500 || p.Capped {
		t.Errorf("500 WAN ports: %d ports, capped %v; want 500, false", p.Ports, p.Capped)
	}
	p, err = b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw}})
	testdb.Must(t, err)
	if p.Devices != 1 || p.Ports != 501 || p.Capped {
		t.Errorf("one device of 501 ports: %d devices, %d ports, capped %v; want 1, 501, false", p.Devices, p.Ports, p.Capped)
	}
}

// Preview fails like ValidateScope: one identical error for hidden and
// missing subjects, and a FieldError for a bad scope.
func TestDBPreviewErrorsMatchValidateScope(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	far := newDevice(t, db, annex, "annex-sw1")
	b := newBuilder(db)

	_, hiddenErr := b.Preview(ctx, user, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{far}})
	_, missingErr := b.Preview(ctx, user, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{uuid.New()}})
	validateErr := b.ValidateScope(ctx, user, models.ScopeTypeDevices,
		models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: []string{services.MetricIfInBps}})
	var hidden, missing, validate *FieldError
	if !errors.As(hiddenErr, &hidden) || !errors.As(missingErr, &missing) || !errors.As(validateErr, &validate) {
		t.Fatalf("errors %v / %v / %v, want three FieldErrors", hiddenErr, missingErr, validateErr)
	}
	want := FieldError{Field: "scope_data", Message: MsgNotAvailable}
	if *hidden != want || *missing != want || *validate != want {
		t.Errorf("hidden %+v, missing %+v, validate %+v; want all %+v", *hidden, *missing, *validate, want)
	}

	var fe *FieldError
	if _, err := b.Preview(ctx, user, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq}}); !errors.As(err, &fe) ||
		fe.Message != `scope_data.roles is required when scope_type is "port_roles"` {
		t.Errorf("no roles: %v", err)
	}
	if _, err := b.Preview(ctx, user, models.ScopeTypeMonitors, models.ReportScope{}); !errors.As(err, &fe) ||
		fe.Message != "scope_type must be one of: ports, port_roles, devices, sites" {
		t.Errorf("a monitor scope: %v", err)
	}
	// Metrics are ignored: the preview sizes a scope before any is chosen.
	if _, err := b.Preview(ctx, user, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq}, Metrics: []string{"nope_metric"}}); err != nil {
		t.Errorf("a preview with an unknown metric: %v", err)
	}
}

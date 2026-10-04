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

const gigabit = int64(1_000_000_000)

func portIDsOf(ps []port) []uuid.UUID {
	out := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func namesOf(rows []row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.name
	}
	return out
}

// The scope is resolved as its owner: a port on a site the owner cannot see
// and a port that no longer exists are counted, never shown.
func TestDBResolvePortsAsTheOwner(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	sw := newDevice(t, db, hq, "core-sw1")
	far := newDevice(t, db, annex, "annex-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	testdb.Exec(t, db, `UPDATE device_interfaces SET alias = 'uplink to annex' WHERE id = ?`, gi1)
	hidden := newPort(t, db, far, 1, "Gi1/0/1", gigabit)
	b := newBuilder(db)

	res, err := b.resolve(ctx, user, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, hidden, uuid.New()}})
	testdb.Must(t, err)
	if len(res.ports) != 1 || res.ports[0].ID != gi1 || res.ports[0].Name != "core-sw1 · Gi1/0/1 (uplink to annex)" ||
		res.ports[0].SpeedBps != gigabit || res.ports[0].DeviceID != sw {
		t.Errorf("ports = %+v, want only the visible one, named with its alias", res.ports)
	}
	if res.unavailable != 2 {
		t.Errorf("unavailable = %d, want 2 (one hidden, one missing)", res.unavailable)
	}

	admin := testdb.NewUser(t, db, true)
	res, err = b.resolve(ctx, admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, hidden}})
	testdb.Must(t, err)
	if len(res.ports) != 2 || res.unavailable != 0 {
		t.Errorf("admin: %d ports, %d unavailable; want both ports and none unavailable", len(res.ports), res.unavailable)
	}
	if _, err := b.resolve(ctx, uuid.New(), models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}}); err == nil {
		t.Error("a user that does not exist resolved a scope")
	}
}

func deviceIDsOf(ds []device) []uuid.UUID {
	out := make([]uuid.UUID, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}

// A scheduled run carries on past unavailable subjects, so resolve itself is
// the access gate: on every scope type a non-admin gets only what a site
// share (readonly or editable) lets them see, and the hidden site's subjects
// are counted, never listed.
func TestDBResolveEveryScopeAsANonAdmin(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	sw, far := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, annex, "annex-sw1")
	hqUp := newPort(t, db, sw, 1, "Gi1", gigabit)
	setRole(t, db, hqUp, models.PortRoleUplink)
	hqAcc := newPort(t, db, sw, 2, "Gi2", gigabit)
	farUp := newPort(t, db, far, 1, "Gi1", gigabit)
	setRole(t, db, farUp, models.PortRoleUplink)
	b := newBuilder(db)

	for _, permission := range []string{"readonly", "editable"} {
		user := testdb.NewUser(t, db, false)
		shareSite(t, db, hq, user, permission)
		cases := []struct {
			name, scopeType string
			scope           models.ReportScope
			ports, devices  []uuid.UUID
			siteNames       []string
		}{
			{"sites", models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq, annex}},
				[]uuid.UUID{hqUp, hqAcc}, []uuid.UUID{sw}, []string{"HQ"}},
			{"port roles", models.ScopeTypePortRoles,
				models.ReportScope{SiteIDs: []uuid.UUID{hq, annex}, Roles: []string{models.PortRoleUplink}},
				[]uuid.UUID{hqUp}, []uuid.UUID{}, []string{"HQ"}},
			{"devices", models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw, far}},
				[]uuid.UUID{}, []uuid.UUID{sw}, []string{}},
		}
		for _, c := range cases {
			res, err := b.resolve(ctx, user, c.scopeType, c.scope)
			testdb.Must(t, err)
			if !slices.Equal(portIDsOf(res.ports), c.ports) || !slices.Equal(deviceIDsOf(res.devices), c.devices) ||
				!slices.Equal(res.siteNames, c.siteNames) || res.unavailable != 1 {
				t.Errorf("%s share, %s: ports %v, devices %v, sites %v, unavailable %d; want ports %v, devices %v, sites %v, unavailable 1",
					permission, c.name, portIDsOf(res.ports), deviceIDsOf(res.devices), res.siteNames, res.unavailable,
					c.ports, c.devices, c.siteNames)
			}
			if c.scopeType == models.ScopeTypeSites && (len(res.sites) != 1 || res.sites[0].ID != hq ||
				!slices.Equal(res.sites[0].Devices, []uuid.UUID{sw})) {
				t.Errorf("%s share, sites: totals %+v, want HQ's alone, of core-sw1", permission, res.sites)
			}
		}
	}
}

// port_roles is expanded at each run: a role set after the report was made
// counts; ports the poll does not read, or that left the device, do not.
func TestDBResolvePortRolesAtRunTime(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	wan := newPort(t, db, sw, 1, "Gi1", gigabit)
	setRole(t, db, wan, models.PortRoleWAN)
	up := newPort(t, db, sw, 2, "Gi2", gigabit)
	setRole(t, db, up, models.PortRoleUplink)
	acc := newPort(t, db, sw, 3, "Gi3", gigabit) // access, the default
	off := newPort(t, db, sw, 4, "Gi4", gigabit)
	setRole(t, db, off, models.PortRoleWAN)
	testdb.Exec(t, db, `UPDATE device_interfaces SET collect = false WHERE id = ?`, off)
	gone := newPort(t, db, sw, 5, "Gi5", gigabit)
	setRole(t, db, gone, models.PortRoleWAN)
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = false WHERE id = ?`, gone)
	b := newBuilder(db)
	scope := models.ReportScope{SiteIDs: []uuid.UUID{hq, uuid.New()}, Roles: []string{models.PortRoleWAN, models.PortRoleUplink}}

	res, err := b.resolve(ctx, admin, models.ScopeTypePortRoles, scope)
	testdb.Must(t, err)
	if !slices.Equal(portIDsOf(res.ports), []uuid.UUID{wan, up}) || res.unavailable != 1 || !slices.Equal(res.siteNames, []string{"HQ"}) {
		t.Fatalf("ports %v, unavailable %d, sites %v; want the WAN and uplink ports, the missing site counted",
			portIDsOf(res.ports), res.unavailable, res.siteNames)
	}

	setRole(t, db, acc, models.PortRoleWAN)
	res, err = b.resolve(ctx, admin, models.ScopeTypePortRoles, scope)
	testdb.Must(t, err)
	if !slices.Equal(portIDsOf(res.ports), []uuid.UUID{wan, up, acc}) {
		t.Errorf("after a role change: %v, want the newly WAN port included", portIDsOf(res.ports))
	}
}

// A device total adds up the device's physical ports only: not a VLAN
// interface, not a port-channel, not a port no longer present.
func TestDBDeviceTotalsCountPhysicalPortsOnly(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	vlan := newVirtualPort(t, db, sw, 100, "Vlan10", 53)
	po := newVirtualPort(t, db, sw, 200, "Port-channel1", 161)
	old := newPort(t, db, sw, 2, "Gi1/0/2", gigabit)
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = false WHERE id = ?`, old)
	physical := portSeries(t, db, sw, gi1, 1, services.MetricIfInBps)
	portSeries(t, db, sw, vlan, 100, services.MetricIfInBps)
	portSeries(t, db, sw, po, 200, services.MetricIfInBps)
	portSeries(t, db, sw, old, 2, services.MetricIfInBps)
	b := newBuilder(db)

	res, err := b.resolve(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw}})
	testdb.Must(t, err)
	if len(res.devices) != 1 || !slices.Equal(res.devices[0].Physical, []uuid.UUID{gi1}) || res.physicalPorts() != 1 {
		t.Fatalf("devices = %+v, want core-sw1 with Gi1/0/1 as its only physical port", res.devices)
	}
	idx, err := b.loadSeries(ctx, res, []string{services.MetricIfInBps})
	testdb.Must(t, err)
	m, _ := builtinInfo(services.MetricIfInBps)
	rows := rowsFor(res, m, idx)
	if len(rows) != 1 || rows[0].key != "d:"+sw.String() || rows[0].subject != "d:"+sw.String() || rows[0].name != "core-sw1" ||
		!rows[0].total || !slices.Equal(rows[0].series, []int64{physical}) {
		t.Errorf("lines = %+v, want one device total of the physical port's series", rows)
	}
}

// A sites scope: one total per site (in the order chosen), then each port;
// a device metric has a line per instance, and its site total adds them all.
func TestDBResolveSitesLines(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	sw1, sw2 := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, annex, "annex-sw1")
	a1, a2, b1 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw1, 2, "Gi2", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	s1 := portSeries(t, db, sw1, a1, 1, services.MetricIfInBps)
	s2 := portSeries(t, db, sw1, a2, 2, services.MetricIfInBps)
	s3 := portSeries(t, db, sw2, b1, 1, services.MetricIfInBps)
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	c1 := newSeries(t, db, sw1, "core_cpu", "1", nil, "Switch 1")
	c2 := newSeries(t, db, sw1, "core_cpu", "2", nil, "Switch 2")
	b := newBuilder(db)

	res, err := b.resolve(ctx, admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{annex, hq}})
	testdb.Must(t, err)
	if !slices.Equal(res.siteNames, []string{"Annex", "HQ"}) || len(res.sites) != 2 || len(res.ports) != 3 || len(res.devices) != 2 {
		t.Fatalf("resolved = %+v", res)
	}
	keys := []string{services.MetricIfInBps, "core_cpu"}
	idx, err := b.loadSeries(ctx, res, keys)
	testdb.Must(t, err)
	infos, err := b.lookupMetrics(ctx, keys)
	testdb.Must(t, err)

	traffic := rowsFor(res, infos[services.MetricIfInBps], idx)
	if want := []string{"Annex (site total)", "HQ (site total)", "annex-sw1 · Gi1", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(namesOf(traffic), want) {
		t.Fatalf("traffic lines = %v, want %v", namesOf(traffic), want)
	}
	if !traffic[0].site || traffic[0].subject != "" || !slices.Equal(traffic[0].series, []int64{s3}) ||
		!slices.Equal(traffic[1].series, []int64{s1, s2}) || traffic[2].subject != "p:"+b1.String() ||
		traffic[2].port == nil || !slices.Equal(traffic[2].series, []int64{s3}) {
		t.Errorf("traffic lines = %+v", traffic)
	}

	cpu := rowsFor(res, infos["core_cpu"], idx)
	if want := []string{"Annex (site total)", "HQ (site total)", "core-sw1 · Switch 1", "core-sw1 · Switch 2"}; !slices.Equal(namesOf(cpu), want) {
		t.Fatalf("cpu lines = %v, want %v", namesOf(cpu), want)
	}
	if len(cpu[0].series) != 0 || !slices.Equal(cpu[1].series, []int64{c1, c2}) || cpu[2].subject != "d:"+sw1.String() ||
		cpu[2].total || !slices.Equal(cpu[2].series, []int64{c1}) {
		t.Errorf("cpu lines = %+v", cpu)
	}
}

// Hidden and missing subjects get the very same error, so the editor cannot
// be used to find out which ids exist.
func TestDBValidateScopeOneMessageForHiddenAndMissing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	sw, far := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, annex, "annex-sw1")
	gi1, farPort := newPort(t, db, sw, 1, "Gi1", gigabit), newPort(t, db, far, 1, "Gi1", gigabit)
	b := newBuilder(db)
	m := []string{services.MetricIfInBps}
	wan := []string{models.PortRoleWAN}
	cases := []struct {
		name, scopeType string
		hidden, missing models.ReportScope
	}{
		{"ports", models.ScopeTypePorts,
			models.ReportScope{PortIDs: []uuid.UUID{gi1, farPort}, Metrics: m},
			models.ReportScope{PortIDs: []uuid.UUID{gi1, uuid.New()}, Metrics: m}},
		{"devices", models.ScopeTypeDevices,
			models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: m},
			models.ReportScope{DeviceIDs: []uuid.UUID{uuid.New()}, Metrics: m}},
		{"port roles", models.ScopeTypePortRoles,
			models.ReportScope{SiteIDs: []uuid.UUID{annex}, Roles: wan, Metrics: m},
			models.ReportScope{SiteIDs: []uuid.UUID{uuid.New()}, Roles: wan, Metrics: m}},
		{"sites", models.ScopeTypeSites,
			models.ReportScope{SiteIDs: []uuid.UUID{hq, annex}, Metrics: m},
			models.ReportScope{SiteIDs: []uuid.UUID{hq, uuid.New()}, Metrics: m}},
	}
	for _, c := range cases {
		var hidden, missing *FieldError
		if err := b.ValidateScope(ctx, user, c.scopeType, c.hidden); !errors.As(err, &hidden) {
			t.Fatalf("%s hidden: %v, want a FieldError", c.name, err)
		}
		if err := b.ValidateScope(ctx, user, c.scopeType, c.missing); !errors.As(err, &missing) {
			t.Fatalf("%s missing: %v, want a FieldError", c.name, err)
		}
		if *hidden != *missing || hidden.Field != "scope_data" || hidden.Message != MsgNotAvailable {
			t.Errorf("%s: hidden %+v, missing %+v; want both {scope_data, %q}", c.name, *hidden, *missing, MsgNotAvailable)
		}
	}
	if err := b.ValidateScope(ctx, user, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: m}); err != nil {
		t.Errorf("a visible port: %v", err)
	}
}

// Metrics a report cannot show are refused with a message the user can act on.
func TestDBValidateScopeMetrics(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	newProfileMetric(t, db, "Core health", true, "core_fan", "Fan", "", "status", 1)
	b := newBuilder(db)
	ports := models.ReportScope{PortIDs: []uuid.UUID{gi1}}
	devices := models.ReportScope{DeviceIDs: []uuid.UUID{sw}}
	with := func(s models.ReportScope, metrics ...string) models.ReportScope { s.Metrics = metrics; return s }
	cases := []struct {
		name, scopeType string
		scope           models.ReportScope
		want            string // "" = valid
	}{
		{"traffic on ports", models.ScopeTypePorts, with(ports, services.MetricIfInBps, services.MetricIfOutBps), ""},
		{"cpu and on battery on devices", models.ScopeTypeDevices, with(devices, "core_cpu", services.MetricUPSOnBattery), ""},
		{"an unknown metric", models.ScopeTypePorts, with(ports, services.MetricIfInBps, "nope_metric"), "nope_metric is not a known metric"},
		{"an enum", models.ScopeTypeDevices, with(devices, services.MetricUPSBatteryStatus), MsgTextMetric},
		{"a status metric", models.ScopeTypeDevices, with(devices, "core_fan"), MsgTextMetric},
		{"link speed", models.ScopeTypeDevices, with(devices, services.MetricIfSpeedBps), "link speed cannot be reported"},
		{"a device metric on ports", models.ScopeTypePorts, with(ports, "core_cpu"), "CPU is a device metric: report it on devices or sites"},
		{"no metrics", models.ScopeTypePorts, ports, "scope_data.metrics is required for a metrics report"},
		{"a monitor scope", models.ScopeTypeMonitors, with(models.ReportScope{MonitorIDs: []uuid.UUID{uuid.New()}}, services.MetricIfInBps),
			"scope_type must be one of: ports, port_roles, devices, sites"},
	}
	for _, c := range cases {
		err := b.ValidateScope(ctx, admin, c.scopeType, c.scope)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Message != c.want {
			t.Errorf("%s: %v, want a FieldError %q", c.name, err, c.want)
		}
	}
}

package netreport

import (
	"bytes"
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// buildWindow is one whole hour two days ago: 12 complete buckets. With a
// rolling report the previous period is the hour before it.
func buildWindow() (time.Time, time.Time) {
	start := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	return start, start.Add(time.Hour)
}

// metricsReport is a metrics report with a rolling period, so its previous
// period is the same length right before the start.
func metricsReport(owner uuid.UUID, scopeType string, scope models.ReportScope) *models.Report {
	return &models.Report{ID: uuid.New(), UserID: owner, CreatedBy: owner, Name: "Network", ReportType: models.ReportTypeMetrics,
		ScopeType: scopeType, ScopeData: scope, PeriodKind: models.PeriodRolling, TimeRangeDays: 1}
}

func rowNames(rows []services.MetricsRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

// Review Focus 3: a port of unknown speed has no busy data. It leads the
// traffic ranking on its traffic alone, its busy line says "no data", and it
// is never running hot. Every figure by hand (bps per bucket, 12 buckets):
//
//	       in    out   billable   busy in / out   the hour before
//	Gi1    100   300   300        10 / 85         100 / 150 -> +100%
//	Gi2    200    50   200        20 / 5          none      -> new
//	Gi3   1000    10   1000       (no speed)      800 / 10  -> +25%
func TestDBBuildPortsTrafficBusyAndRunningHot(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1, gi2 := newPort(t, db, sw, 1, "Gi1", gigabit), newPort(t, db, sw, 2, "Gi2", gigabit)
	gi3 := newPort(t, db, sw, 3, "Gi3", 0) // speed unknown: no busy % is ever recorded
	start, end := buildWindow()
	prev := start.Add(-time.Hour)
	write := func(port uuid.UUID, ifIndex int, metric string, now float64, before ...float64) {
		s := portSeries(t, db, sw, port, ifIndex, metric)
		steady(t, db, s, start, 12, now)
		for _, v := range before {
			steady(t, db, s, prev, 12, v)
		}
	}
	write(gi1, 1, services.MetricIfInBps, 100, 100)
	write(gi1, 1, services.MetricIfOutBps, 300, 150)
	write(gi1, 1, services.MetricIfInUtilPct, 10)
	write(gi1, 1, services.MetricIfOutUtilPct, 85)
	write(gi2, 2, services.MetricIfInBps, 200)
	write(gi2, 2, services.MetricIfOutBps, 50)
	write(gi2, 2, services.MetricIfInUtilPct, 20)
	write(gi2, 2, services.MetricIfOutUtilPct, 5)
	write(gi3, 3, services.MetricIfInBps, 1000, 800)
	write(gi3, 3, services.MetricIfOutBps, 10, 10)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, gi2, gi3},
		Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	if data.ScopeType != models.ScopeTypePorts || data.ScopeLabel != "3 ports" || data.RankLabel != "Traffic" ||
		!data.PrevStart.Equal(prev) || !data.PrevEnd.Equal(start) {
		t.Errorf("header = %q %q %q %v to %v", data.ScopeType, data.ScopeLabel, data.RankLabel, data.PrevStart, data.PrevEnd)
	}
	if data.Rows != 3 || data.Ports != 3 || data.Devices != 1 || data.CappedOut != 0 || data.Unavailable != 0 ||
		data.LowCoverage != 0 || data.NoData || data.Empty || len(data.Skipped) != 0 {
		t.Errorf("counts = rows %d, ports %d, devices %d, cut %d, unavailable %d, low %d, no data %v, empty %v, skipped %v",
			data.Rows, data.Ports, data.Devices, data.CappedOut, data.Unavailable, data.LowCoverage, data.NoData, data.Empty, data.Skipped)
	}
	if len(data.Tables) != 2 || len(data.Tiles) != 2 {
		t.Fatalf("%d tables, %d tiles; want Traffic and Busy", len(data.Tables), len(data.Tiles))
	}
	traffic, busyTable := data.Tables[0], data.Tables[1]
	if traffic.Title != "Traffic" || !traffic.Paired || !traffic.Billable || !traffic.HasTotal || traffic.Unit != "bps" ||
		busyTable.Title != "Busy" || !busyTable.Paired || busyTable.Billable || busyTable.HasTotal {
		t.Errorf("table headers = %+v / %+v", traffic, busyTable)
	}
	if want := []string{"core-sw1 · Gi3", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(rowNames(traffic.Rows), want) {
		t.Fatalf("traffic order = %v, want %v (Gi3 has no busy data; its traffic still ranks it first)", rowNames(traffic.Rows), want)
	}
	g3, g1, g2 := traffic.Rows[0], traffic.Rows[1], traffic.Rows[2]
	if *g3.Billable != 1000 || g3.Change == nil || !near(*g3.Change, 25) || g3.New || !near(*g3.In.Total, 1000*12*300/8) {
		t.Errorf("Gi3 = %+v, want billable 1000, +25%%", g3)
	}
	if *g1.Billable != 300 || g1.Change == nil || !near(*g1.Change, 100) || !near(*g1.In.Total, 45000) || !near(*g1.Out.Total, 135000) {
		t.Errorf("Gi1 = %+v, want billable 300, +100%%, 45000 bytes in and 135000 out", g1)
	}
	if *g2.Billable != 200 || g2.Change != nil || !g2.New || g2.Coverage != 100 {
		t.Errorf("Gi2 = %+v, want billable 200 and new", g2)
	}
	if want := []string{"core-sw1 · Gi1", "core-sw1 · Gi2", "core-sw1 · Gi3"}; !slices.Equal(rowNames(busyTable.Rows), want) {
		t.Errorf("busy order = %v, want %v", rowNames(busyTable.Rows), want)
	}
	if gi3Busy := busyTable.Rows[2]; !gi3Busy.NoData || gi3Busy.In != nil || gi3Busy.Out != nil {
		t.Errorf("Gi3 busy = %+v, want no data", gi3Busy)
	}
	if !slices.Equal(data.RunningHot, []services.HotPort{{Name: "core-sw1 · Gi1", P95In: 10, P95Out: 85, HasIn: true, HasOut: true}}) {
		t.Errorf("running hot = %+v, want Gi1 only", data.RunningHot)
	}
	if want := []string{"core-sw1 · Gi3", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v, want %v", data.Busiest, want)
	}
	// The whole scope: 1300 bps in and 360 out; 900 and 160 the hour before.
	if tile := data.Tiles[0]; tile.Kind != "traffic" || tile.First != 1300 || tile.Second == nil || !near(*tile.Second, 747000) ||
		tile.Change == nil || !near(*tile.Change, 400.0/9) {
		t.Errorf("traffic tile = %+v, want 1300, 747000 bytes, +44.44%%", tile)
	}
	// Busy averages over the ports that have it: (10+20)/2 in, (85+5)/2 out.
	if tile := data.Tiles[1]; tile.Kind != "percent" || tile.First != 45 || tile.Second == nil || *tile.Second != 45 || !tile.New {
		t.Errorf("busy tile = %+v, want 45, 45, new", tile)
	}
	if len(data.Charts) != 2 || len(data.Charts[0].Lines) != 2 || data.Charts[0].Reference == nil ||
		*data.Charts[0].Reference != 1300 || len(data.Charts[0].Lines[0].Points) != 12 || data.Charts[0].Lines[0].Points[0].V != 1300 {
		t.Errorf("charts = %+v", data.Charts)
	}
	if len(data.RowCharts) != 3 || data.RowCharts[0].Title != "core-sw1 · Gi3" || data.RowCharts[0].Reference == nil ||
		*data.RowCharts[0].Reference != 1000 || len(data.RowCharts[0].Lines) != 2 || data.RowCharts[0].Lines[0].Label != "Traffic in" ||
		len(data.RowCharts[0].Lines[0].Points) != 12 || data.RowCharts[0].Lines[0].Points[0].V != 1000 {
		t.Errorf("row charts = %+v", data.RowCharts)
	}
}

// A site total sums traffic but averages busy %, over the physical ports
// only: a VLAN interface the user collects has its own line, and no part in
// the total.
func TestDBBuildSiteTotalsSumTrafficAndAverageBusy(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw1, sw2 := newDevice(t, db, hq, "sw1"), newDevice(t, db, hq, "sw2")
	g1, g2 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	vlan := newVirtualPort(t, db, sw1, 100, "Vlan10", 53)
	start, end := buildWindow()
	for _, w := range []struct {
		device, port uuid.UUID
		ifIndex      int
		metric       string
		v            float64
	}{
		{sw1, g1, 1, services.MetricIfInBps, 100},
		{sw2, g2, 1, services.MetricIfInBps, 300},
		{sw1, vlan, 100, services.MetricIfInBps, 5000},
		{sw1, g1, 1, services.MetricIfInUtilPct, 40},
		{sw2, g2, 1, services.MetricIfInUtilPct, 60},
	} {
		steady(t, db, portSeries(t, db, w.device, w.port, w.ifIndex, w.metric), start, 12, w.v)
	}
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq},
		Metrics: []string{services.MetricIfInBps, services.MetricIfInUtilPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	traffic := data.Tables[0]
	if want := []string{"HQ (site total)", "sw1 · Vlan10", "sw2 · Gi1", "sw1 · Gi1"}; traffic.Title != "Traffic in" ||
		!slices.Equal(rowNames(traffic.Rows), want) {
		t.Fatalf("traffic = %q %v, want %v", traffic.Title, rowNames(traffic.Rows), want)
	}
	if site := traffic.Rows[0]; site.In == nil || site.In.P95 != 400 || site.In.Total == nil || !near(*site.In.Total, 400*12*300/8) {
		t.Errorf("site traffic = %+v, want 100 + 300 = 400 bps (not 5400: the VLAN is not physical)", site.In)
	}
	if tile := data.Tiles[0]; tile.Kind != "traffic" || tile.First != 400 {
		t.Errorf("traffic tile = %+v, want 400", tile)
	}
	busyTable := data.Tables[1]
	if want := []string{"HQ (site total)", "sw2 · Gi1", "sw1 · Gi1", "sw1 · Vlan10"}; !slices.Equal(rowNames(busyTable.Rows), want) {
		t.Fatalf("busy = %v, want %v", rowNames(busyTable.Rows), want)
	}
	if site := busyTable.Rows[0]; site.In == nil || site.In.Avg != 50 || site.In.P95 != 50 || site.In.Total != nil {
		t.Errorf("site busy = %+v, want (40 + 60) / 2 = 50, no total", site.In)
	}
	if tile := data.Tiles[1]; tile.Kind != "percent" || tile.First != 50 {
		t.Errorf("busy tile = %+v, want 50", tile)
	}
	if data.ScopeLabel != "All of HQ" || data.Rows != 3 || data.Ports != 3 || data.Devices != 2 || len(data.RunningHot) != 0 {
		t.Errorf("label %q, rows %d, ports %d, devices %d, hot %v", data.ScopeLabel, data.Rows, data.Ports, data.Devices, data.RunningHot)
	}
	if want := []string{"sw1 · Vlan10", "sw2 · Gi1", "sw1 · Gi1"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v, want %v (site totals are not listed)", data.Busiest, want)
	}
}

// A port_roles scope of 502 ports keeps the 500 busiest by the first metric
// and says how many it left out; the headline still covers them all.
func TestDBBuildCapKeepsTheBusiest500(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	start, end := buildWindow()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type, speed_bps, present, collect_default, role)
		SELECT ?, g, 'Gi' || g, 6, 1000000000, true, true, 'wan' FROM generate_series(1, 502) g`, sw)
	testdb.Exec(t, db, `INSERT INTO metrics.series (device_id, metric, instance, interface_id)
		SELECT device_id, 'if_in_bps', if_index::text, id FROM device_interfaces WHERE device_id = ?`, sw)
	// Port Gi<n> moves n bps in every bucket of the hour.
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT ?::timestamptz + make_interval(mins => 5 * b + 1), s.id, s.instance::int
		FROM metrics.series s, generate_series(0, 11) b WHERE s.device_id = ?`, start, sw)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq},
		Roles: []string{models.PortRoleWAN}, Metrics: []string{services.MetricIfInBps}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	rows := data.Tables[0].Rows
	if data.Rows != 500 || data.CappedOut != 2 || data.Ports != 500 || data.Devices != 1 || len(rows) != 500 {
		t.Fatalf("rows %d, cut %d, ports %d, devices %d, lines %d; want 500 kept, 2 cut", data.Rows, data.CappedOut, data.Ports, data.Devices, len(rows))
	}
	if rows[0].Name != "core-sw1 · Gi502" || rows[499].Name != "core-sw1 · Gi3" {
		t.Errorf("lines run %q to %q, want Gi502 down to Gi3", rows[0].Name, rows[499].Name)
	}
	if want := []string{"core-sw1 · Gi502", "core-sw1 · Gi501", "core-sw1 · Gi500", "core-sw1 · Gi499", "core-sw1 · Gi498"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v", data.Busiest)
	}
	// 1 + 2 + ... + 502 = 126253 bps: the cut ports still count in the headline.
	if data.Tiles[0].First != 126253 || len(data.RowCharts) != 10 || data.ScopeLabel != "WAN ports at HQ" {
		t.Errorf("tile %v, row charts %d, label %q", data.Tiles[0].First, len(data.RowCharts), data.ScopeLabel)
	}
}

// On a sites scope the cap cuts port lines only. Each site total still
// combines every port of the site, those the cap cut included, in the first
// table (read before the cap) and in the others (read after it).
func TestDBBuildSiteTotalsKeepThePortsTheCapCut(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	start, end := buildWindow()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type, speed_bps, present, collect_default)
		SELECT ?, g, 'Gi' || g, 6, 1000000000, true, true FROM generate_series(1, 502) g`, sw)
	testdb.Exec(t, db, `INSERT INTO metrics.series (device_id, metric, instance, interface_id)
		SELECT di.device_id, m.metric, di.if_index::text, di.id
		FROM device_interfaces di, unnest(ARRAY['if_in_bps', 'if_in_util_pct']) AS m(metric) WHERE di.device_id = ?`, sw)
	// Port Gi<n> moves n bps and is n/10 % busy in every bucket of the hour.
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT ?::timestamptz + make_interval(mins => 5 * b + 1), s.id,
			CASE s.metric WHEN 'if_in_bps' THEN s.instance::int ELSE s.instance::int / 10.0 END
		FROM metrics.series s, generate_series(0, 11) b WHERE s.device_id = ?`, start, sw)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq},
		Metrics: []string{services.MetricIfInBps, services.MetricIfInUtilPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	if data.Rows != 500 || data.CappedOut != 2 || data.Ports != 500 || data.Devices != 1 || len(data.Tables) != 2 {
		t.Fatalf("rows %d, cut %d, ports %d, devices %d, tables %d; want 500 kept, 2 cut, 2 tables",
			data.Rows, data.CappedOut, data.Ports, data.Devices, len(data.Tables))
	}
	traffic, busyTable := data.Tables[0], data.Tables[1]
	if len(traffic.Rows) != 501 || len(busyTable.Rows) != 501 {
		t.Fatalf("%d traffic lines, %d busy lines; want 501 each: the site total and 500 ports", len(traffic.Rows), len(busyTable.Rows))
	}
	if last := traffic.Rows[500].Name; last != "core-sw1 · Gi3" {
		t.Errorf("traffic ends with %q, want core-sw1 · Gi3 (Gi1 and Gi2 cut)", last)
	}
	// 1 + 2 + ... + 502 = 126253 bps, Gi1 and Gi2 included.
	if site := traffic.Rows[0]; site.Name != "HQ (site total)" || site.In == nil || site.In.P95 != 126253 {
		t.Errorf("site traffic = %q %+v, want 126253 bps", site.Name, site.In)
	}
	// (0.1 + 0.2 + ... + 50.2) / 502 = 25.15 % busy; the kept ports alone average 25.25.
	if site := busyTable.Rows[0]; site.Name != "HQ (site total)" || site.In == nil || !near(site.In.Avg, 25.15) {
		t.Errorf("site busy = %q %+v, want 25.15%%", site.Name, site.In)
	}
	if data.Tiles[0].First != 126253 || !near(data.Tiles[1].First, 25.15) {
		t.Errorf("tiles = %v / %v, want 126253 and 25.15", data.Tiles[0].First, data.Tiles[1].First)
	}
}

// Review Focus 5: a device deleted after the period but before the run. Its
// series went with it, so its lines vanish; it is counted as unavailable and
// the run goes on, before and after the nightly cleanup removes its samples.
func TestDBBuildDeletedDeviceIsUnavailable(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw1, sw2 := newDevice(t, db, hq, "sw1"), newDevice(t, db, hq, "sw2")
	p1, p2 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	start, end := buildWindow()
	steady(t, db, portSeries(t, db, sw1, p1, 1, services.MetricIfInBps), start, 12, 100)
	steady(t, db, portSeries(t, db, sw2, p2, 1, services.MetricIfInBps), start, 12, 200)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw1, sw2},
		Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps}})
	b := newBuilder(db)

	data, err := b.Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)
	if want := []string{"sw2", "sw1"}; !slices.Equal(rowNames(data.Tables[0].Rows), want) || data.Ports != 2 || data.Devices != 2 {
		t.Fatalf("before the delete: %v, %d ports, %d devices; want %v, 2, 2", rowNames(data.Tables[0].Rows), data.Ports, data.Devices, want)
	}

	devices := services.NewDeviceService(db, services.NewSNMPCredentialService(db), services.NewIncidentService(db))
	_, err = devices.Delete(ctx, sw2)
	testdb.Must(t, err)
	check := func(when string) {
		t.Helper()
		data, err := b.Build(ctx, report, admin, start, end, time.UTC)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		rows := data.Tables[0].Rows
		if data.Unavailable != 1 || data.Rows != 1 || data.Devices != 1 || len(rows) != 1 || rows[0].Name != "sw1" ||
			rows[0].Billable == nil || *rows[0].Billable != 100 {
			t.Errorf("%s: unavailable %d, rows %d, lines %+v; want sw1 alone and one unavailable", when, data.Unavailable, data.Rows, rows)
		}
	}
	check("after the delete")
	_, err = services.NewMetricsStore(db).Cleanup(ctx, 365)
	testdb.Must(t, err)
	refresh(t, db)
	check("after the nightly cleanup")
}

// A calendar month compares with the month before. August's room
// temperature averages 30 °C and July's 20 °C: +50%. The UPS was on battery
// for 6 whole buckets and 3 of the 5 minutes of a seventh, then 5 buckets on
// mains: 6.6 x 300 s = 1980 s; 9 of its 16 samples were "on", a share of
// 0.5625. Each line has 12 of August's 8928 buckets: both are flagged.
func TestDBBuildCalendarPreviousCustomMetricAndBool(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	// The months below are fixed dates; keep the retention policy away from them.
	testdb.Exec(t, db, `SELECT remove_retention_policy('metrics.samples', if_exists => true)`)
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	ups := newDevice(t, db, hq, "ups1")
	newProfileMetric(t, db, "Lab sensors", false, "lab_temp", "Room temperature", "°C", "gauge", 0)
	temp := newSeries(t, db, ups, "lab_temp", "1", nil, "Rack A")
	batt := newSeries(t, db, ups, services.MetricUPSOnBattery, "", nil, "")
	start, end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	steady(t, db, temp, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), 12, 30)
	steady(t, db, temp, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), 12, 20)
	day := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	steady(t, db, batt, day, 6, 1)
	for i, v := range []float64{1, 1, 1, 0, 0} {
		sampleAt(t, db, batt, day.Add(30*time.Minute+time.Duration(i)*time.Minute), v)
	}
	steady(t, db, batt, day.Add(35*time.Minute), 5, 0)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{ups},
		Metrics: []string{"lab_temp", services.MetricUPSOnBattery}})
	report.PeriodKind, report.PeriodUnit, report.PeriodOffset, report.TimeRangeDays = models.PeriodCalendar, models.UnitMonth, 1, 0

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	if !data.PrevStart.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) || !data.PrevEnd.Equal(start) {
		t.Errorf("previous = %v to %v, want July", data.PrevStart, data.PrevEnd)
	}
	if data.RankLabel != "Room temperature" || data.ScopeLabel != "ups1" || data.Rows != 1 || data.Devices != 1 ||
		data.Ports != 0 || data.LowCoverage != 2 {
		t.Errorf("rank %q, label %q, rows %d, devices %d, ports %d, low %d", data.RankLabel, data.ScopeLabel, data.Rows,
			data.Devices, data.Ports, data.LowCoverage)
	}
	temps := data.Tables[0]
	if temps.Title != "Room temperature" || temps.Unit != "°C" || temps.Paired || temps.HasTotal || len(temps.Rows) != 1 {
		t.Fatalf("temperature table = %+v", temps)
	}
	if r := temps.Rows[0]; r.Name != "ups1 · Rack A" || r.In == nil || r.In.Avg != 30 || r.Change == nil || !near(*r.Change, 50) ||
		!r.LowCoverage || !near(r.Coverage, 1200.0/8928) {
		t.Errorf("temperature = %+v, want 30 °C, +50%%, flagged at 0.13%%", r)
	}
	batts := data.Tables[1]
	if batts.Title != "On battery" || batts.Unit != "bool" || len(batts.Rows) != 1 {
		t.Fatalf("battery table = %+v", batts)
	}
	if r := batts.Rows[0]; r.Name != "ups1" || r.In == nil || r.In.TrueSeconds == nil || !near(*r.In.TrueSeconds, 1980) ||
		!near(r.In.Avg, 0.5625) || !r.New {
		t.Errorf("on battery = %+v, want 1980 s true, a share of 0.5625, new", r)
	}
	// The tile reads the scope's combined series: the mean of its bucket shares, 6.6 / 12 = 0.55.
	if tile := data.Tiles[1]; tile.Kind != "other" || !near(tile.First, 0.55) {
		t.Errorf("battery tile = %+v, want 0.55", tile)
	}
	// A chart's reference line is its 95th. A bool chart has none: its
	// ranking figure is the share of time true, not a 95th.
	if len(data.Charts) != 2 || data.Charts[0].Reference == nil || *data.Charts[0].Reference != 30 || data.Charts[1].Reference != nil {
		t.Errorf("charts = %+v, want the temperature's 95th (30) and no line on the battery chart", data.Charts)
	}
	if len(data.RowCharts) != 1 || data.RowCharts[0].Reference == nil || *data.RowCharts[0].Reference != 30 {
		t.Errorf("row charts = %+v, want ups1 · Rack A with its 95th (30)", data.RowCharts)
	}
	// With the bool metric first, its row charts have no reference line either.
	report.ScopeData.Metrics = []string{services.MetricUPSOnBattery, "lab_temp"}
	data, err = newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)
	if data.RankLabel != "On battery" || len(data.RowCharts) != 1 || data.RowCharts[0].Title != "ups1" ||
		data.RowCharts[0].Reference != nil || len(data.Charts) != 2 || data.Charts[0].Reference != nil {
		t.Errorf("battery first: rank %q, charts %+v, row charts %+v; want no reference line on a bool chart",
			data.RankLabel, data.Charts, data.RowCharts)
	}
}

// A chosen device without series of a device metric (a switch in a UPS
// report) still has its line, which says "No data": it counts among the
// report's devices, as the label does, and ranks after every device with data.
func TestDBBuildChosenDeviceWithoutTheMetricShowsNoData(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	ups, sw := newDevice(t, db, hq, "ups1"), newDevice(t, db, hq, "core-sw1")
	start, end := buildWindow()
	steady(t, db, newSeries(t, db, ups, services.MetricUPSLoadPct, "", nil, ""), start, 12, 40)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{ups, sw},
		Metrics: []string{services.MetricUPSLoadPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	rows := data.Tables[0].Rows
	if want := []string{"ups1", "core-sw1"}; !slices.Equal(rowNames(rows), want) {
		t.Fatalf("load lines = %v, want %v", rowNames(rows), want)
	}
	if rows[0].NoData || rows[0].In == nil || rows[0].In.Avg != 40 || !rows[1].NoData || rows[1].In != nil {
		t.Errorf("lines = %+v / %+v, want ups1 at 40%% and core-sw1 with no data", rows[0], rows[1])
	}
	if data.Rows != 2 || data.Devices != 2 || data.ScopeLabel != "2 devices" || data.NoData || data.Unavailable != 0 ||
		!slices.Equal(data.Busiest, []string{"ups1"}) || len(data.RowCharts) != 1 {
		t.Errorf("rows %d, devices %d, label %q, no data %v, unavailable %d, busiest %v, row charts %d",
			data.Rows, data.Devices, data.ScopeLabel, data.NoData, data.Unavailable, data.Busiest, len(data.RowCharts))
	}
}

// Every chosen subject hidden: a one-page report that says so. A metric
// deleted since the report was made is skipped and named; with no data at
// all, the headline says so.
func TestDBBuildEmptySkippedAndNoData(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	admin := testdb.NewUser(t, db, true)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	far := newDevice(t, db, annex, "annex-sw1")
	farPort := newPort(t, db, far, 1, "Gi1", gigabit)
	start, end := buildWindow()
	// Annex is busy and running hot; none of it may reach the user's reports.
	steady(t, db, portSeries(t, db, far, farPort, 1, services.MetricIfInBps), start, 12, 500)
	steady(t, db, portSeries(t, db, far, farPort, 1, services.MetricIfInUtilPct), start, 12, 95)
	refresh(t, db)
	b := newBuilder(db)

	// Every chosen subject hidden: Empty, and the label names nothing.
	for _, c := range []struct {
		scopeType string
		scope     models.ReportScope
		label     string
	}{
		{models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{farPort}}, "No available ports"},
		{models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{far}}, "No available devices"},
		{models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{annex}, Roles: []string{models.PortRoleWAN}}, "No available sites"},
		{models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{annex}}, "No available sites"},
	} {
		c.scope.Metrics = []string{services.MetricIfInBps}
		data, err := b.Build(ctx, metricsReport(user, c.scopeType, c.scope), user, start, end, time.UTC)
		testdb.Must(t, err)
		if !data.Empty || data.Unavailable != 1 || len(data.Tables) != 0 || data.ScopeType != c.scopeType || data.ScopeLabel != c.label {
			t.Errorf("hidden %s: empty %v, unavailable %d, tables %d, type %q, label %q; want empty, 1, 0, %q",
				c.scopeType, data.Empty, data.Unavailable, len(data.Tables), data.ScopeType, data.ScopeLabel, c.label)
		}
	}
	// The run resolves the scope as the user it is given, whoever made the report.
	data, err := b.Build(ctx, metricsReport(admin, models.ScopeTypeDevices,
		models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: []string{services.MetricIfInBps}}), user, start, end, time.UTC)
	testdb.Must(t, err)
	if !data.Empty || data.Unavailable != 1 {
		t.Errorf("an admin's report run for the user: empty %v, unavailable %d; want the user's view", data.Empty, data.Unavailable)
	}
	// Only the sites the user may see are named, and nothing of the others
	// reaches a line, the busiest, running hot or a tile.
	data, err = b.Build(ctx, metricsReport(user, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq, annex},
		Metrics: []string{services.MetricIfInBps, services.MetricIfInUtilPct}}), user, start, end, time.UTC)
	testdb.Must(t, err)
	if data.Empty || data.Unavailable != 1 || data.ScopeLabel != "All of HQ" || len(data.Tables) != 2 || len(data.Tiles) != 2 {
		t.Fatalf("HQ and a hidden Annex: empty %v, unavailable %d, label %q, %d tables; want \"All of HQ\" and 2 tables",
			data.Empty, data.Unavailable, data.ScopeLabel, len(data.Tables))
	}
	var names []string
	for _, tb := range data.Tables {
		names = append(names, rowNames(tb.Rows)...)
	}
	names = append(names, data.Busiest...)
	for _, h := range data.RunningHot {
		names = append(names, h.Name)
	}
	for _, c := range data.RowCharts {
		names = append(names, c.Title)
	}
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), "annex") {
			t.Errorf("%q names the hidden Annex (all names: %v)", n, names)
		}
	}
	if !data.Tiles[0].NoData || !data.Tiles[1].NoData || len(data.RunningHot) != 0 {
		t.Errorf("tiles %+v, running hot %v; want no data: HQ has none, Annex's is hidden", data.Tiles, data.RunningHot)
	}

	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	data, err = b.Build(ctx, metricsReport(user, models.ScopeTypePorts,
		models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps, "gone_metric"}}), user, start, end, time.UTC)
	testdb.Must(t, err)
	if data.Empty || !data.NoData || !slices.Equal(data.Skipped, []string{"gone_metric"}) || len(data.Tables) != 1 ||
		len(data.Tables[0].Rows) != 1 || !data.Tables[0].Rows[0].NoData || !data.Tiles[0].NoData ||
		len(data.Busiest) != 0 || len(data.RowCharts) != 0 || data.Rows != 1 {
		t.Errorf("no data = %+v", data)
	}
}

// A build that runs out of time is a report too large to build, with the
// message the user reads; the job queue does not retry it (Task 2). The
// cause is logged once, naming the report.
func TestDBBuildRunningOutOfTimeIsTooLarge(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	start, end := buildWindow()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	report := metricsReport(admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps}})
	var logged bytes.Buffer
	stderr := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(stderr) })

	_, err := newBuilder(db).Build(expired, report, admin, start, end, time.UTC)
	if !errors.Is(err, services.ErrReportTooLarge) || err.Error() != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("err = %v, want ErrReportTooLarge", err)
	}
	lines := strings.Split(strings.TrimSpace(logged.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], report.ID.String()) || !strings.Contains(lines[0], context.DeadlineExceeded.Error()) {
		t.Errorf("log = %q, want one line naming the report and the deadline", logged.String())
	}
}

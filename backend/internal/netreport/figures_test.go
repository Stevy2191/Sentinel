package netreport

import (
	"math"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func info(t *testing.T, key string) metricInfo {
	t.Helper()
	m, ok := builtinInfo(key)
	if !ok {
		t.Fatalf("%s is not a built-in metric", key)
	}
	return m
}

// full is one side's statistics with all 12 of its buckets covered.
func full(avg, p95, bucketSum float64) services.SeriesStat {
	return services.SeriesStat{Avg: avg, P95: p95, BucketSum: bucketSum, Buckets: 12, Expected: 12}
}

func TestGroupMetrics(t *testing.T) {
	cpu := metricInfo{Key: "core_cpu", Label: "CPU", Unit: "%", Source: sourceProfile}
	got := groupMetrics([]metricInfo{info(t, services.MetricIfOutBps), cpu, info(t, services.MetricIfInBps), info(t, services.MetricIfInUtilPct)})
	if len(got) != 3 {
		t.Fatalf("got %d tables, want 3: %+v", len(got), got)
	}
	if got[0].title != "Traffic" || got[0].in.Key != services.MetricIfInBps || got[0].out == nil ||
		got[0].out.Key != services.MetricIfOutBps || !got[0].billable() || got[0].unit() != "bps" {
		t.Errorf("first table = %+v, want the traffic pair where the out side stood", got[0])
	}
	if got[1].title != "CPU" || got[1].out != nil {
		t.Errorf("second table = %+v, want CPU alone", got[1])
	}
	if got[2].title != "Busy in" || got[2].out != nil || got[2].billable() {
		t.Errorf("third table = %+v, want busy in alone (its out side is not in the report)", got[2])
	}
	errs := groupMetrics([]metricInfo{info(t, services.MetricIfInErrorsPM), info(t, services.MetricIfOutErrorsPM)})
	if len(errs) != 1 || errs[0].title != "Errors" || errs[0].billable() || len(errs[0].metrics()) != 2 {
		t.Errorf("errors = %+v, want one Errors pair without a billable column", errs)
	}
}

func TestFigures(t *testing.T) {
	// Task 3's series: bucket sum 205, so 205 x 300 / 8 = 7687.5 bytes.
	st := services.SeriesStat{Avg: 390.0 / 9, Min: 0, Peak: 100, P95: 66, BucketSum: 205, Buckets: 5, Expected: 6}
	bps := figures(st, "bps")
	if bps == nil || bps.Total == nil || !near(*bps.Total, 7687.5) || bps.TrueSeconds != nil || bps.P95 != 66 || bps.Peak != 100 || bps.Min != 0 {
		t.Errorf("bps = %+v, want 7687.5 bytes", bps)
	}
	// 12.4 errors a minute summed over buckets, x 5 minutes = 62 errors.
	if pm := figures(services.SeriesStat{BucketSum: 12.4, Buckets: 3}, "per_min"); pm == nil || pm.Total == nil || !near(*pm.Total, 62) {
		t.Errorf("per_min = %+v, want a total of 62", pm)
	}
	// "on battery 2 h 13 m": a bucket-share sum of 26.6 is 26.6 x 300 s = 7980 s.
	if b := figures(services.SeriesStat{Avg: 0.1, BucketSum: 26.6, Buckets: 50}, "bool"); b == nil || b.TrueSeconds == nil ||
		!near(*b.TrueSeconds, 7980) || b.Total != nil {
		t.Errorf("bool = %+v, want 7980 seconds true", b)
	}
	if pct := figures(st, "%"); pct == nil || pct.Total != nil || pct.TrueSeconds != nil {
		t.Errorf("%% = %+v, want no total", pct)
	}
	if none := figures(services.SeriesStat{Expected: 6}, "bps"); none != nil {
		t.Errorf("no data = %+v, want nil", none)
	}
}

func TestChange(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	cases := []struct {
		name            string
		cur, prev       float64
		hasCur, hasPrev bool
		want            *float64
		isNew           bool
	}{
		{"up 30%", 130, 100, true, true, ptr(30), false},
		{"down 50%", 50, 100, true, true, ptr(-50), false},
		{"from zero", 10, 0, true, true, nil, true},
		{"zero to zero", 0, 0, true, true, ptr(0), false},
		{"no data before", 10, 0, true, false, nil, true},
		{"no data now", 0, 100, false, true, nil, false},
	}
	for _, c := range cases {
		got, isNew := change(c.cur, c.prev, c.hasCur, c.hasPrev)
		if isNew != c.isNew || (got == nil) != (c.want == nil) || (got != nil && !near(*got, *c.want)) {
			t.Errorf("%s: change %v, new %v; want %v, %v", c.name, got, isNew, c.want, c.isNew)
		}
	}
}

// A traffic pair's line, by hand: billable 95th = max(300, 500) = 500, up from
// max(200, 250) = 250, so +100%. Bytes 3000 x 300 / 8 = 112500 in and
// 5000 x 300 / 8 = 187500 out. Coverage is the worse side's: 11 of 12 = 91.67%.
func TestLineFiguresTrafficPair(t *testing.T) {
	out := info(t, services.MetricIfOutBps)
	g := group{title: "Traffic", in: info(t, services.MetricIfInBps), out: &out}
	cur := [2]services.SeriesStat{full(250, 300, 3000), {Avg: 450, P95: 500, BucketSum: 5000, Buckets: 11, Expected: 12}}
	prev := [2]services.SeriesStat{full(150, 200, 1800), full(200, 250, 2400)}
	r := lineFigures(g, "core-sw1 · Gi1", cur, prev)
	if r.Name != "core-sw1 · Gi1" || r.NoData || r.Billable == nil || *r.Billable != 500 || r.Change == nil || !near(*r.Change, 100) || r.New {
		t.Errorf("line = %+v, want billable 500, +100%%", r)
	}
	if r.In == nil || r.In.Total == nil || !near(*r.In.Total, 112500) || r.Out == nil || r.Out.Total == nil || !near(*r.Out.Total, 187500) {
		t.Errorf("totals = %+v / %+v, want 112500 and 187500 bytes", r.In, r.Out)
	}
	if !near(r.Coverage, 1100.0/12) || r.LowCoverage {
		t.Errorf("coverage = %v (low %v), want 91.67%%, not flagged", r.Coverage, r.LowCoverage)
	}
}

// A percentage alone: its change is on the average, and an average of 0
// before is "new", never a division by zero.
func TestLineFiguresFromZeroIsNew(t *testing.T) {
	g := group{title: "Busy in", in: info(t, services.MetricIfInUtilPct)}
	r := lineFigures(g, "x", [2]services.SeriesStat{full(40, 70, 480)}, [2]services.SeriesStat{full(0, 0, 0)})
	if r.Billable != nil || r.Change != nil || !r.New || r.In == nil || r.In.Avg != 40 || r.In.Total != nil || r.Out != nil {
		t.Errorf("line = %+v, want 40%% average, new", r)
	}
	if r.Coverage != 100 || r.LowCoverage {
		t.Errorf("coverage = %v, want 100", r.Coverage)
	}
	// 10 of 12 buckets is 83%: flagged. No data before: new.
	low := lineFigures(g, "x", [2]services.SeriesStat{{Avg: 40, P95: 70, Buckets: 10, Expected: 12}}, [2]services.SeriesStat{})
	if !low.LowCoverage || !low.New || low.Change != nil {
		t.Errorf("low = %+v, want flagged and new", low)
	}
}

func TestLineFiguresNoData(t *testing.T) {
	out := info(t, services.MetricIfOutUtilPct)
	g := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &out}
	r := lineFigures(g, "core-sw1 · Gi3", [2]services.SeriesStat{{Expected: 12}, {Expected: 12}},
		[2]services.SeriesStat{full(10, 10, 120), full(10, 10, 120)})
	if !r.NoData || r.In != nil || r.Out != nil || r.Change != nil || r.New || r.LowCoverage || r.Billable != nil {
		t.Errorf("line = %+v, want no data and nothing else", r)
	}
}

func TestTileFor(t *testing.T) {
	outBps := info(t, services.MetricIfOutBps)
	traffic := group{title: "Traffic", in: info(t, services.MetricIfInBps), out: &outBps}
	// 1300 bps in and 360 out in each of 12 buckets; 900 and 160 the period before.
	// Billable 95th 1300; bytes (15600 + 4320) x 300 / 8 = 747000; (1300 - 900) / 900 = +44.44%.
	tile := tileFor(traffic, [2]services.SeriesStat{full(1300, 1300, 15600), full(360, 360, 4320)},
		[2]services.SeriesStat{full(900, 900, 10800), full(160, 160, 1920)})
	if tile.Kind != "traffic" || tile.Label != "Traffic" || tile.Unit != "bps" || tile.First != 1300 || tile.Second == nil ||
		!near(*tile.Second, 747000) || tile.Change == nil || !near(*tile.Change, 400.0/9) || tile.New || tile.NoData {
		t.Errorf("traffic tile = %+v", tile)
	}

	outUtil := info(t, services.MetricIfOutUtilPct)
	busyPair := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &outUtil}
	tile = tileFor(busyPair, [2]services.SeriesStat{full(15, 15, 180), full(45, 45, 540)}, [2]services.SeriesStat{})
	if tile.Kind != "percent" || tile.First != 45 || tile.Second == nil || *tile.Second != 45 || tile.Change != nil || !tile.New {
		t.Errorf("busy tile = %+v, want average 45, 95th 45, new", tile)
	}

	temp := group{title: "Room temperature", in: metricInfo{Key: "lab_temp", Label: "Room temperature", Unit: "°C"}}
	tile = tileFor(temp, [2]services.SeriesStat{full(30, 31, 360)}, [2]services.SeriesStat{full(20, 21, 240)})
	if tile.Kind != "other" || tile.First != 30 || tile.Second != nil || tile.Change == nil || !near(*tile.Change, 50) {
		t.Errorf("temperature tile = %+v, want average 30, +50%%", tile)
	}

	if none := tileFor(traffic, [2]services.SeriesStat{{Expected: 12}, {Expected: 12}}, [2]services.SeriesStat{}); !none.NoData {
		t.Errorf("no data tile = %+v", none)
	}
}

// A bool metric ranks by its share of time true: its 95th is only 0 or 1.
func TestRankOfBool(t *testing.T) {
	g := group{title: "On battery", in: info(t, services.MetricUPSOnBattery)}
	if v, ok := rankOf(g, [2]services.SeriesStat{{Avg: 0.5625, P95: 1, Buckets: 12, Expected: 12}}); !ok || v != 0.5625 {
		t.Errorf("rank = %v, %v; want 0.5625", v, ok)
	}
	if _, ok := rankOf(g, [2]services.SeriesStat{{Expected: 12}}); ok {
		t.Error("a line without data was ranked")
	}
}

func TestKeepBusiest(t *testing.T) {
	cands := []candidate{{"p:a", "a", 10, true}, {"p:b", "b", 30, true}, {"p:c", "c", 0, false}, {"p:e", "e", 20, true}, {"p:d", "d", 20, true}}
	keep, cut := keepBusiest(cands, 3)
	if cut != 2 || len(keep) != 3 || !keep["p:b"] || !keep["p:d"] || !keep["p:e"] {
		t.Errorf("keep %v, cut %d; want b, d, e kept (d before e on the tie) and 2 cut", keep, cut)
	}
	if keep, cut := keepBusiest(cands, 500); cut != 0 || len(keep) != 5 {
		t.Errorf("under the cap: keep %v, cut %d", keep, cut)
	}
}

func TestHotPort(t *testing.T) {
	outUtil := info(t, services.MetricIfOutUtilPct)
	pair := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &outUtil}
	if !isBusy(pair) {
		t.Error("the busy pair is not recognised")
	}
	h, hot := hotPort(pair, "core-sw1 · Gi1", [2]services.SeriesStat{full(10, 10, 120), full(85, 85, 1020)})
	if !hot || h != (services.HotPort{Name: "core-sw1 · Gi1", P95In: 10, P95Out: 85}) {
		t.Errorf("hot = %v, %+v; want running hot at 85%% out", hot, h)
	}
	// A port of unknown speed has no busy data, so it is never hot.
	if _, hot := hotPort(pair, "x", [2]services.SeriesStat{{Expected: 12}, {Expected: 12}}); hot {
		t.Error("a port without busy data is running hot")
	}
	outOnly := group{title: "Busy out", in: info(t, services.MetricIfOutUtilPct)}
	if h, hot := hotPort(outOnly, "y", [2]services.SeriesStat{full(80, 80, 960)}); !hot || h.P95Out != 80 || h.P95In != 0 {
		t.Errorf("out only = %v, %+v; want hot at exactly 80%% out", hot, h)
	}
	inOnly := group{title: "Busy in", in: info(t, services.MetricIfInUtilPct)}
	if _, hot := hotPort(inOnly, "z", [2]services.SeriesStat{full(79.9, 79.9, 958.8)}); hot {
		t.Error("79.9% is running hot")
	}
	if isBusy(group{title: "Traffic in", in: info(t, services.MetricIfInBps)}) {
		t.Error("traffic is not the busy table")
	}
}

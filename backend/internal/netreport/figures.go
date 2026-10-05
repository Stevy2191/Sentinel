package netreport

import (
	"math"
	"slices"
	"sort"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	// lowCoverage: a line with data for less of the period than this (%) is flagged.
	lowCoverage = 90.0
	// hotThreshold: a port whose 95th busy reaches this (%) in either
	// direction is running hot.
	hotThreshold = 80.0
)

// Tile kinds (services.MetricsTile.Kind).
const (
	kindTraffic = "traffic"
	kindPercent = "percent"
	kindOther   = "other"
)

// pairs maps each in metric to its out side and the pair's title.
var pairs = map[string]struct{ out, title string }{
	services.MetricIfInBps:        {services.MetricIfOutBps, "Traffic"},
	services.MetricIfInUtilPct:    {services.MetricIfOutUtilPct, "Busy"},
	services.MetricIfInErrorsPM:   {services.MetricIfOutErrorsPM, "Errors"},
	services.MetricIfInDiscardsPM: {services.MetricIfOutDiscardsPM, "Discards"},
}

// group is one table of the report: an in/out pair, or a single metric.
type group struct {
	title string
	in    metricInfo  // the single metric, or the pair's in side
	out   *metricInfo // the pair's out side; nil for a single metric
}

func (g group) metrics() []metricInfo {
	if g.out == nil {
		return []metricInfo{g.in}
	}
	return []metricInfo{g.in, *g.out}
}

func (g group) unit() string { return g.in.Unit }

// billable reports the traffic pair, whose table has a Billable 95th column.
func (g group) billable() bool { return g.out != nil && g.in.Unit == "bps" }

// groupMetrics turns the report's metrics into its tables, in metric order:
// an in/out pair whose two sides are both listed becomes one table, standing
// where the first of the two stands; anything else is a table of its own.
func groupMetrics(ms []metricInfo) []group {
	listed := make(map[string]metricInfo, len(ms))
	for _, m := range ms {
		listed[m.Key] = m
	}
	inOf := map[string]string{}
	for in, p := range pairs {
		inOf[p.out] = in
	}
	done := map[string]bool{}
	var out []group
	for _, m := range ms {
		if done[m.Key] {
			continue
		}
		inKey, outKey := m.Key, ""
		if p, ok := pairs[m.Key]; ok {
			outKey = p.out
		} else if in, ok := inOf[m.Key]; ok {
			inKey, outKey = in, m.Key
		}
		inM, hasIn := listed[inKey]
		outM, hasOut := listed[outKey]
		if outKey != "" && hasIn && hasOut {
			o := outM
			out = append(out, group{title: pairs[inKey].title, in: inM, out: &o})
			done[inKey], done[outKey] = true, true
			continue
		}
		out = append(out, group{title: m.Label, in: m})
		done[m.Key] = true
	}
	return out
}

// figures turns one side's statistics into a line's numbers; nil without
// data. A rate gets its total: bytes for bps (Σ bucket average × 300 s ÷ 8),
// a count for per_min (Σ bucket average × 5 min). A bool metric gets the
// seconds it was true (Σ bucket share × 300 s); its Avg is the share.
func figures(st services.SeriesStat, unit string) *services.RowStats {
	if st.Buckets == 0 {
		return nil
	}
	rs := &services.RowStats{Avg: st.Avg, Min: st.Min, Peak: st.Peak, P95: st.P95}
	switch unit {
	case "bps":
		v := st.BucketSum * 300 / 8
		rs.Total = &v
	case "per_min":
		v := st.BucketSum * 5
		rs.Total = &v
	case "bool":
		v := st.BucketSum * 300
		rs.TrueSeconds = &v
	}
	return rs
}

// larger picks a figure from each side that has data and keeps the larger.
func larger(in, out *services.RowStats, pick func(*services.RowStats) float64) (float64, bool) {
	switch {
	case in == nil && out == nil:
		return 0, false
	case in == nil:
		return pick(out), true
	case out == nil:
		return pick(in), true
	}
	return math.Max(pick(in), pick(out)), true
}

// busy is the figure lines are ranked and sorted by: the 95th; for a pair the
// busier direction's (for traffic, the billable 95th = max(95th in, 95th
// out)); for a bool metric the share of time true, its 95th being only 0 or 1.
func busy(in, out *services.RowStats, unit string) (float64, bool) {
	if unit == "bool" {
		return larger(in, out, func(s *services.RowStats) float64 { return s.Avg })
	}
	return larger(in, out, func(s *services.RowStats) float64 { return s.P95 })
}

// average is a line's average; for a pair the busier direction's.
func average(in, out *services.RowStats) (float64, bool) {
	return larger(in, out, func(s *services.RowStats) float64 { return s.Avg })
}

// basis is the figure a line's change is worked out on: the busy figure (the
// billable 95th) for traffic, the average otherwise.
func basis(in, out *services.RowStats, unit string) (float64, bool) {
	if unit == "bps" {
		return busy(in, out, unit)
	}
	return average(in, out)
}

// change compares a figure with the previous period's: the change in
// percent, or isNew when the previous period had no data or was zero (never
// a division by zero). Zero to zero is no change. Without current data there
// is neither. The divisor is |prev|, so the sign follows the move even for a
// negative figure (a dBm-style custom metric): -5 to -7 is -40%.
func change(cur, prev float64, hasCur, hasPrev bool) (*float64, bool) {
	switch {
	case !hasCur:
		return nil, false
	case !hasPrev:
		return nil, true
	case prev == 0 && cur == 0:
		v := 0.0
		return &v, false
	case prev == 0:
		return nil, true
	}
	v := (cur - prev) / math.Abs(prev) * 100
	return &v, false
}

// sides is a line's figures in one period; out is nil for a single metric.
func sides(g group, st [2]services.SeriesStat) (in, out *services.RowStats) {
	in = figures(st[0], g.unit())
	if g.out != nil {
		out = figures(st[1], g.unit())
	}
	return in, out
}

// rankOf is a line's ranking figure in a period (see busy); false without data.
func rankOf(g group, st [2]services.SeriesStat) (float64, bool) {
	in, out := sides(g, st)
	return busy(in, out, g.unit())
}

// lineFigures is one table line's numbers from its statistics in the period
// (cur) and the one before (prev).
func lineFigures(g group, name string, cur, prev [2]services.SeriesStat) services.MetricsRow {
	unit := g.unit()
	in, out := sides(g, cur)
	r := services.MetricsRow{Name: name, In: in, Out: out, NoData: in == nil && out == nil}
	if r.NoData {
		return r
	}
	if g.billable() {
		v, _ := busy(in, out, unit)
		r.Billable = &v
	}
	pin, pout := sides(g, prev)
	c, _ := basis(in, out, unit)
	p, hasPrev := basis(pin, pout, unit)
	r.Change, r.New = change(c, p, true, hasPrev)
	r.Coverage = cur[0].Coverage()
	if g.out != nil {
		r.Coverage = math.Min(r.Coverage, cur[1].Coverage())
	}
	r.LowCoverage = r.Coverage < lowCoverage
	return r
}

func kindOf(unit string) string {
	switch unit {
	case "bps":
		return kindTraffic
	case "%":
		return kindPercent
	}
	return kindOther
}

func totalOf(s *services.RowStats) float64 {
	if s == nil || s.Total == nil {
		return 0
	}
	return *s.Total
}

// tileFor is a table's headline tile from the whole scope's combined
// statistics: traffic shows its 95th (billable for a pair) and the bytes
// moved; a percentage its average and 95th; anything else its average. For a
// pair the busier direction stands for both, except the bytes, which add up.
func tileFor(g group, cur, prev [2]services.SeriesStat) services.MetricsTile {
	unit := g.unit()
	t := services.MetricsTile{Label: g.title, Unit: unit, Kind: kindOf(unit)}
	in, out := sides(g, cur)
	if in == nil && out == nil {
		t.NoData = true
		return t
	}
	pin, pout := sides(g, prev)
	var p float64
	var hasPrev bool
	switch t.Kind {
	case kindTraffic:
		t.First, _ = busy(in, out, unit)
		total := totalOf(in) + totalOf(out)
		t.Second = &total
		p, hasPrev = busy(pin, pout, unit)
	case kindPercent:
		t.First, _ = average(in, out)
		p95, _ := busy(in, out, unit)
		t.Second = &p95
		p, hasPrev = average(pin, pout)
	default:
		t.First, _ = average(in, out)
		p, hasPrev = average(pin, pout)
	}
	t.Change, t.New = change(t.First, p, true, hasPrev)
	return t
}

// candidate is a subject (a port or a device) competing for a place in the
// report, by its busiest line in the first table.
type candidate struct {
	subject, name string
	value         float64
	ok            bool // false: no data there, so it ranks after every subject with data
}

// lessBusy orders busiest first: figures with data before those without,
// higher figures first, then by name.
func lessBusy(aOK bool, aValue float64, aName string, bOK bool, bValue float64, bName string) bool {
	if aOK != bOK {
		return aOK
	}
	if aOK && aValue != bValue {
		return aValue > bValue
	}
	return aName < bName
}

// keepBusiest orders the candidates busiest first (no data last, then by
// name and id so the order never wobbles) and keeps the first limit. It
// returns the kept subjects and how many were cut.
func keepBusiest(cands []candidate, limit int) (map[string]bool, int) {
	sorted := slices.Clone(cands)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if lessBusy(a.ok, a.value, a.name, b.ok, b.value, b.name) {
			return true
		}
		if lessBusy(b.ok, b.value, b.name, a.ok, a.value, a.name) {
			return false
		}
		return a.subject < b.subject
	})
	keep := make(map[string]bool, min(limit, len(sorted)))
	for i := 0; i < len(sorted) && i < limit; i++ {
		keep[sorted[i].subject] = true
	}
	return keep, max(0, len(sorted)-limit)
}

// isBusy reports the busy table: the in/out pair, or one direction alone.
func isBusy(g group) bool {
	return g.in.Key == services.MetricIfInUtilPct || g.in.Key == services.MetricIfOutUtilPct
}

// hotPort reports a port running hot: its 95th busy in either direction at
// or above 80%. g is the busy table. A port with no busy data (its speed is
// unknown, so no busy % is ever recorded) is never hot.
func hotPort(g group, name string, cur [2]services.SeriesStat) (services.HotPort, bool) {
	in, out := cur[0], cur[1]
	switch {
	case g.out == nil && g.in.Key == services.MetricIfOutUtilPct:
		in, out = services.SeriesStat{}, cur[0]
	case g.out == nil:
		out = services.SeriesStat{}
	}
	h := services.HotPort{Name: name}
	hot := false
	if in.Buckets > 0 {
		h.P95In = in.P95
		hot = hot || in.P95 >= hotThreshold
	}
	if out.Buckets > 0 {
		h.P95Out = out.P95
		hot = hot || out.P95 >= hotThreshold
	}
	return h, hot
}

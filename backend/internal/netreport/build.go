package netreport

import (
	"context"
	"errors"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Check that Builder implements services.NetworkReportBuilder.
var _ services.NetworkReportBuilder = (*Builder)(nil)

// line is one table line: its row, its series per side (0: in or the single
// metric, 1: out), and its statistics in the period and the one before.
type line struct {
	row
	sides     [2][]int64
	cur, prev [2]services.SeriesStat
}

// table is one group's lines, and the whole scope's series and statistics
// per side (its tile and its chart).
type table struct {
	group     group
	lines     []line
	scope     [2][]int64
	scopeCur  [2]services.SeriesStat
	scopePrev [2]services.SeriesStat
}

// Build implements services.NetworkReportBuilder. It resolves the report's
// scope as requestedBy, reads [start, end) and the period before it, and
// fills the report. A statistics query that runs out of time (its statement
// timeout, or the job's own deadline) fails the report with
// services.ErrReportTooLarge, which the job queue does not retry.
func (b *Builder) Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error) {
	data, err := b.build(ctx, report, requestedBy, start, end, loc)
	switch {
	case err == nil:
		return data, nil
	case errors.Is(err, services.ErrReportTooLarge):
		// The statement timeout behind it is logged where it was mapped.
		return nil, services.ErrReportTooLarge
	case errors.Is(err, context.DeadlineExceeded):
		log.Printf("[metrics-report] report %s (%q) ran out of time, so it is too large to build: %v", report.ID, report.Name, err)
		return nil, services.ErrReportTooLarge
	}
	return nil, err
}

func (b *Builder) build(ctx context.Context, report *models.Report, user uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error) {
	prevStart, prevEnd := report.PreviousPeriod(start, end, loc)
	scope := report.ScopeData
	res, err := b.resolve(ctx, user, report.ScopeType, scope)
	if err != nil {
		return nil, err
	}
	data := &services.MetricsReportData{
		ScopeType: report.ScopeType, ScopeLabel: scopeLabel(report.ScopeType, scope, res),
		PrevStart: prevStart, PrevEnd: prevEnd, Unavailable: res.unavailable,
	}
	if res.empty() {
		data.Empty = true
		return data, nil
	}
	groups, skipped, err := b.groups(ctx, report.ScopeType, scope.Metrics)
	if err != nil {
		return nil, err
	}
	data.Skipped = skipped
	if len(groups) == 0 {
		data.NoData = true
		return data, nil
	}
	data.RankLabel = groups[0].title

	var keys []string
	for _, g := range groups {
		for _, m := range g.metrics() {
			keys = append(keys, m.Key)
		}
	}
	idx, err := b.loadSeries(ctx, res, keys)
	if err != nil {
		return nil, err
	}
	tables := make([]table, len(groups))
	for i, g := range groups {
		tables[i] = tableFor(res, g, idx)
	}
	// The first table ranks every subject, so it is read in full before the cap.
	if err := b.fill(ctx, &tables[0], start, end, false); err != nil {
		return nil, err
	}
	keep, cut := keepBusiest(candidates(tables), models.MaxReportSubjects)
	data.Rows, data.CappedOut = len(keep), cut
	countSubjects(data, res, keep)
	for i := range tables {
		tables[i].lines = keptLines(tables[i].lines, keep)
		if i > 0 {
			if err := b.fill(ctx, &tables[i], start, end, false); err != nil {
				return nil, err
			}
		}
		if err := b.fill(ctx, &tables[i], prevStart, prevEnd, true); err != nil {
			return nil, err
		}
	}
	if err := b.assemble(ctx, data, tables, start, end); err != nil {
		return nil, err
	}
	return data, nil
}

// groups describes the report's metrics and pairs them into tables. A metric
// that no longer exists, or that is not allowedOn this scope (a report cannot
// show it, or the scope has no lines for it), is skipped and named, rather
// than failing the run.
func (b *Builder) groups(ctx context.Context, scopeType string, keys []string) ([]group, []string, error) {
	infos, err := b.lookupMetrics(ctx, keys)
	if err != nil {
		return nil, nil, err
	}
	var ms []metricInfo
	var skipped []string
	for _, k := range keys {
		m, ok := infos[k]
		if !ok || !m.allowedOn(scopeType) {
			skipped = append(skipped, k)
			continue
		}
		ms = append(ms, m)
	}
	return groupMetrics(ms), skipped, nil
}

// tableFor lays out a group's lines (a pair's two metrics share their lines,
// matched by key) and the whole scope's series per side: the site totals' on
// a sites scope, otherwise every line's.
func tableFor(res *resolved, g group, idx *seriesIndex) table {
	t := table{group: g}
	at := map[string]int{}
	for side, m := range g.metrics() {
		for _, r := range rowsFor(res, m, idx) {
			i, ok := at[r.key]
			if !ok {
				i = len(t.lines)
				at[r.key] = i
				t.lines = append(t.lines, line{row: r})
			}
			t.lines[i].sides[side] = r.series
		}
		seen := map[int64]bool{}
		for _, l := range t.lines {
			if l.site != (len(res.sites) > 0) {
				continue
			}
			for _, id := range l.sides[side] {
				if !seen[id] {
					seen[id] = true
					t.scope[side] = append(t.scope[side], id)
				}
			}
		}
	}
	return t
}

// fill reads the statistics of a table's lines, and of the whole scope, over
// [from, to): per side, one query for the single-series lines, one for the
// totals, one for the scope.
func (b *Builder) fill(ctx context.Context, t *table, from, to time.Time, prev bool) error {
	for side, m := range t.group.metrics() {
		stats, err := b.lineStats(ctx, t.lines, side, !m.additive(), from, to)
		if err != nil {
			return err
		}
		scope, err := b.metrics.CombinedStats(ctx, services.CombinedQuery{SeriesIDs: t.scope[side], From: from, To: to, Average: !m.additive()})
		if err != nil {
			return err
		}
		for i := range t.lines {
			if prev {
				t.lines[i].prev[side] = stats[i]
			} else {
				t.lines[i].cur[side] = stats[i]
			}
		}
		if prev {
			t.scopePrev[side] = scope
		} else {
			t.scopeCur[side] = scope
		}
	}
	return nil
}

// lineStats reads one side's statistics for these lines: SeriesStats for the
// lines of one series, GroupedStats for the totals. A line without series,
// or without data, gets a zero SeriesStat (Buckets 0: no data).
func (b *Builder) lineStats(ctx context.Context, lines []line, side int, average bool, from, to time.Time) ([]services.SeriesStat, error) {
	out := make([]services.SeriesStat, len(lines))
	var singles []int64
	var groups [][]int64
	var grouped []int
	for i, l := range lines {
		ids := l.sides[side]
		switch {
		case l.total:
			groups = append(groups, ids)
			grouped = append(grouped, i)
		case len(ids) > 0:
			singles = append(singles, ids[0])
		}
	}
	if len(singles) > 0 {
		stats, err := b.metrics.SeriesStats(ctx, services.StatsQuery{SeriesIDs: singles, From: from, To: to})
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]services.SeriesStat, len(stats))
		for _, s := range stats {
			byID[s.SeriesID] = s
		}
		for i, l := range lines {
			if !l.total && len(l.sides[side]) > 0 {
				out[i] = byID[l.sides[side][0]]
			}
		}
	}
	if len(groups) > 0 {
		stats, err := b.metrics.GroupedStats(ctx, groups, from, to, average)
		if err != nil {
			return nil, err
		}
		for j, s := range stats {
			out[grouped[j]] = s
		}
	}
	return out, nil
}

// candidates lists every subject with a line in any table, valued by its
// busiest line in the first table (the report's first metric). A subject
// with no line or no data there is unranked and goes last.
func candidates(tables []table) []candidate {
	var out []candidate
	at := map[string]int{}
	for _, t := range tables {
		for _, l := range t.lines {
			if l.subject == "" {
				continue
			}
			if _, ok := at[l.subject]; !ok {
				at[l.subject] = len(out)
				out = append(out, candidate{subject: l.subject, name: l.name})
			}
		}
	}
	first := tables[0]
	for _, l := range first.lines {
		if l.subject == "" {
			continue
		}
		v, ok := rankOf(first.group, l.cur)
		c := &out[at[l.subject]]
		if ok && (!c.ok || v > c.value) {
			c.value, c.ok = v, true
		}
	}
	return out
}

// keptLines drops the lines of subjects the cap cut; site totals stay.
func keptLines(lines []line, keep map[string]bool) []line {
	out := make([]line, 0, len(lines))
	for _, l := range lines {
		if l.site || keep[l.subject] {
			out = append(out, l)
		}
	}
	return out
}

// countSubjects fills the report's port and device counts from the subjects
// kept: ports and their devices; devices and their physical ports on a
// devices scope; the visible sites' devices on a sites scope.
func countSubjects(data *services.MetricsReportData, res *resolved, keep map[string]bool) {
	devices := map[uuid.UUID]bool{}
	for _, p := range res.ports {
		if keep["p:"+p.ID.String()] {
			data.Ports++
			devices[p.DeviceID] = true
		}
	}
	for _, d := range res.devices {
		if keep["d:"+d.ID.String()] {
			devices[d.ID] = true
			if res.scopeType == models.ScopeTypeDevices {
				data.Ports += len(d.Physical)
			}
		}
	}
	data.Devices = len(devices)
	if res.scopeType == models.ScopeTypeSites {
		data.Devices = len(res.devices)
	}
}

// roleNames are the port roles as the scope label writes them.
var roleNames = map[string]string{models.PortRoleWAN: "WAN", models.PortRoleUplink: "Uplink", models.PortRoleAccess: "Access"}

// The labels of a scope none of whose chosen ports, devices or sites the
// owner can see: never "0 ports" or "All of" nothing.
const (
	noAvailablePorts   = "No available ports"
	noAvailableDevices = "No available devices"
	noAvailableSites   = "No available sites"
)

// scopeLabel names the scope in words, naming only what the owner can see:
// "WAN, Uplink ports at HQ, Annex", "All of HQ", "core-sw1 · Gi1/0/1",
// "12 ports", "3 devices".
func scopeLabel(scopeType string, scope models.ReportScope, res *resolved) string {
	switch scopeType {
	case models.ScopeTypePorts:
		switch len(res.ports) {
		case 0:
			return noAvailablePorts
		case 1:
			return res.ports[0].Name
		}
		return plural(len(res.ports), "port")
	case models.ScopeTypeDevices:
		switch len(res.devices) {
		case 0:
			return noAvailableDevices
		case 1:
			return res.devices[0].Name
		}
		return plural(len(res.devices), "device")
	case models.ScopeTypePortRoles, models.ScopeTypeSites:
		if len(res.siteNames) == 0 {
			return noAvailableSites
		}
		sites := strings.Join(res.siteNames, ", ")
		if scopeType == models.ScopeTypeSites {
			return "All of " + sites
		}
		var roles []string
		for _, r := range []string{models.PortRoleWAN, models.PortRoleUplink, models.PortRoleAccess} {
			if slices.Contains(scope.Roles, r) {
				roles = append(roles, roleNames[r])
			}
		}
		return strings.Join(roles, ", ") + " ports at " + sites
	}
	return ""
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

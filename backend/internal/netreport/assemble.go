package netreport

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	// chartPoints is about how many points each chart has.
	chartPoints = 200
	// rowChartCount bounds the busiest lines charted one by one.
	rowChartCount = 10
	// busiestCount is how many names the headline's "Busiest" lists.
	busiestCount = 5
)

// assemble turns the filled tables into the report: tables, tiles and charts
// in metric order, running hot from the busy table, and the headline list
// and small charts from the first table's ranking.
func (b *Builder) assemble(ctx context.Context, data *services.MetricsReportData, tables []table, start, end time.Time) error {
	flagged := map[string]bool{}
	hasData := false
	for i := range tables {
		t := &tables[i]
		sortLines(t)
		mt := services.MetricsTable{Title: t.group.title, Unit: t.group.unit(), Paired: t.group.out != nil,
			Billable: t.group.billable(), HasTotal: t.group.in.additive(), Rows: []services.MetricsRow{}}
		for _, l := range t.lines {
			r := lineFigures(t.group, l.name, l.cur, l.prev)
			hasData = hasData || !r.NoData
			if r.LowCoverage {
				flagged[l.key] = true
			}
			mt.Rows = append(mt.Rows, r)
		}
		data.Tables = append(data.Tables, mt)
		data.Tiles = append(data.Tiles, tileFor(t.group, t.scopeCur, t.scopePrev))
		var ref *float64
		if v, ok := rankOf(t.group, t.scopeCur); ok {
			ref = &v
		}
		c, err := b.chart(ctx, t.group.title, t.group, t.scope, ref, start, end)
		if err != nil {
			return err
		}
		data.Charts = append(data.Charts, c)
		if isBusy(t.group) {
			for _, l := range t.lines {
				if l.port == nil {
					continue
				}
				if h, hot := hotPort(t.group, l.name, l.cur); hot {
					data.RunningHot = append(data.RunningHot, h)
				}
			}
		}
	}
	data.LowCoverage = len(flagged)
	data.NoData = !hasData
	sort.SliceStable(data.RunningHot, func(i, j int) bool {
		x, y := data.RunningHot[i], data.RunningHot[j]
		return lessBusy(true, math.Max(x.P95In, x.P95Out), x.Name, true, math.Max(y.P95In, y.P95Out), y.Name)
	})

	first := tables[0]
	for _, l := range first.lines {
		v, ok := rankOf(first.group, l.cur)
		if l.site || !ok {
			continue
		}
		if len(data.Busiest) < busiestCount {
			data.Busiest = append(data.Busiest, l.name)
		}
		if len(data.RowCharts) < rowChartCount {
			ref := v
			c, err := b.chart(ctx, l.name, first.group, l.sides, &ref, start, end)
			if err != nil {
				return err
			}
			data.RowCharts = append(data.RowCharts, c)
		}
	}
	return nil
}

// sortLines orders a table: site totals first, then busiest first by the
// ranking figure (the 95th, billable for traffic), lines without data last,
// ties by name.
func sortLines(t *table) {
	sort.SliceStable(t.lines, func(i, j int) bool {
		x, y := t.lines[i], t.lines[j]
		if x.site != y.site {
			return x.site
		}
		xv, xok := rankOf(t.group, x.cur)
		yv, yok := rankOf(t.group, y.cur)
		return lessBusy(xok, xv, x.name, yok, yv, y.name)
	})
}

// chart draws a group over [from, to): one line per side from these series,
// combined per bucket by the unit's rule, with ref as the reference line.
func (b *Builder) chart(ctx context.Context, title string, g group, sides [2][]int64, ref *float64, from, to time.Time) (services.MetricsChart, error) {
	c := services.MetricsChart{Title: title, Unit: g.unit(), Reference: ref}
	for side, m := range g.metrics() {
		pts, err := b.metrics.CombinedSeries(ctx, services.CombinedQuery{SeriesIDs: sides[side], From: from, To: to,
			Average: !m.additive()}, chartPoints)
		if err != nil {
			return c, err
		}
		c.Lines = append(c.Lines, services.ChartLine{Label: m.Label, Points: pts})
	}
	return c, nil
}

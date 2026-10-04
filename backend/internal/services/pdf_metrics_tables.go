package services

import (
	"math"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The Metrics report's tables (one per metric or in/out pair, every row,
// busiest first) and the small charts of its busiest rows.

const (
	metricsRowH      = 5.5
	metricsHeaderH   = 7.0
	metricsMaxNumW   = 22.0 // widest a figure column gets
	metricsMinNameW  = 46.0 // narrowest the name column gets
	metricsRowChartH = 32.0
	// metricsEmptyMessage is the whole report when every subject is gone.
	metricsEmptyMessage = "None of the ports, devices or sites in this report are available to you."
)

// metricsColumn is one figure column of a metrics table.
type metricsColumn struct {
	header string
	value  func(r MetricsRow) string
}

// metricsColumns picks a table's figure columns. A traffic pair shows the
// billing view (95th each way, billable 95th, peak, volume each way); other
// pairs average and 95th each way, peak and, for rates, totals; a single
// metric its average, 95th, peak and total, except a bool, which shows the
// share and the length of time it was true.
func metricsColumns(t MetricsTable) []metricsColumn {
	u := t.Unit
	stat := func(pick func(*MetricsRow) *RowStats, f func(*RowStats) float64) func(MetricsRow) string {
		return func(r MetricsRow) string {
			s := pick(&r)
			if s == nil {
				return "-"
			}
			return formatValue(f(s), u)
		}
	}
	total := func(pick func(*MetricsRow) *RowStats) func(MetricsRow) string {
		return func(r MetricsRow) string {
			s := pick(&r)
			if s == nil || s.Total == nil {
				return "-"
			}
			return formatTotal(*s.Total, u)
		}
	}
	in := func(r *MetricsRow) *RowStats { return r.In }
	out := func(r *MetricsRow) *RowStats { return r.Out }
	avg := func(s *RowStats) float64 { return s.Avg }
	p95 := func(s *RowStats) float64 { return s.P95 }
	peakOf := func(s *RowStats) float64 { return s.Peak }
	peak := func(r MetricsRow) string {
		best, ok := math.Inf(-1), false
		for _, s := range []*RowStats{r.In, r.Out} {
			if s != nil {
				best, ok = math.Max(best, s.Peak), true
			}
		}
		if !ok {
			return "-"
		}
		return formatValue(best, u)
	}
	change := func(r MetricsRow) string {
		if c := formatChange(r.Change, r.New); c != "" {
			return c
		}
		return "-"
	}

	var cols []metricsColumn
	switch {
	case t.Paired && t.Billable:
		cols = []metricsColumn{
			{"95th in", stat(in, p95)}, {"95th out", stat(out, p95)},
			{"Billable 95th", func(r MetricsRow) string {
				if r.Billable == nil {
					return "-"
				}
				return formatValue(*r.Billable, u)
			}},
			{"Peak", peak}, {"Total in", total(in)}, {"Total out", total(out)},
		}
	case t.Paired:
		cols = []metricsColumn{
			{"Avg in", stat(in, avg)}, {"Avg out", stat(out, avg)},
			{"95th in", stat(in, p95)}, {"95th out", stat(out, p95)}, {"Peak", peak},
		}
		if t.HasTotal {
			cols = append(cols, metricsColumn{"Total in", total(in)}, metricsColumn{"Total out", total(out)})
		}
	case u == "bool":
		cols = []metricsColumn{
			{"Share of time", stat(in, avg)},
			{"Time true", func(r MetricsRow) string {
				if r.In == nil || r.In.TrueSeconds == nil {
					return "-"
				}
				return formatDurationHM(*r.In.TrueSeconds)
			}},
		}
	default:
		cols = []metricsColumn{{"Average", stat(in, avg)}, {"95th", stat(in, p95)}, {"Peak", stat(in, peakOf)}}
		if t.HasTotal {
			cols = append(cols, metricsColumn{"Total", total(in)})
		}
	}
	return append(cols, metricsColumn{"Change", change})
}

// drawMetricsTables draws every table, starting on a new page.
func drawMetricsTables(pdf *fpdf.Fpdf, data *ReportData) {
	tables := data.Network.Tables
	if len(tables) == 0 {
		return
	}
	pdf.AddPage()
	for _, t := range tables {
		drawMetricsTable(pdf, t, data.Network.ScopeType)
	}
}

// drawMetricsTable draws one table, repeating its header on every page it
// runs onto.
func drawMetricsTable(pdf *fpdf.Fpdf, t MetricsTable, scopeType string) {
	cols := metricsColumns(t)
	numW := math.Min(metricsMaxNumW, (pdfContentW-metricsMinNameW)/float64(len(cols)))
	nameW := pdfContentW - numW*float64(len(cols))
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	limit := pageH - bottom

	// The heading (12 mm), the header and a first row stay together.
	if pdf.GetY()+12+metricsHeaderH+metricsRowH > limit {
		pdf.AddPage()
	}
	drawSectionHeading(pdf, t.Title)

	// First column header: "Port" for port scopes, "Name" otherwise
	nameHeader := "Name"
	if scopeType == models.ScopeTypePorts || scopeType == models.ScopeTypePortRoles {
		nameHeader = "Port"
	}

	header := func() {
		pdf.SetFont("Helvetica", "B", 7)
		setColor(pdf, pdfPanel, true)
		setColor(pdf, pdfInk, false)
		pdf.SetX(pdfMarginLeft)
		pdf.CellFormat(nameW, metricsHeaderH, nameHeader, "", 0, "L", true, 0, "")
		for _, c := range cols {
			pdf.CellFormat(numW, metricsHeaderH, fitText(pdf, c.header, numW-2), "", 0, "R", true, 0, "")
		}
		pdf.Ln(-1)
	}
	header()
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	for _, r := range t.Rows {
		if pdf.GetY()+metricsRowH > limit {
			pdf.AddPage()
			header()
		}
		drawMetricsRow(pdf, r, cols, nameW, numW)
	}
	pdf.Ln(2)
}

// drawMetricsRow draws one row. A row under 90% coverage carries a short
// note after its name; a row without data says so across its figures.
func drawMetricsRow(pdf *fpdf.Fpdf, r MetricsRow, cols []metricsColumn, nameW, numW float64) {
	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfInk, false)
	pdf.SetX(pdfMarginLeft)
	if r.LowCoverage && !r.NoData {
		note := formatPercent(r.Coverage) + " data"
		noteW := pdf.GetStringWidth(note) + 3
		pdf.CellFormat(nameW-noteW, metricsRowH, fitText(pdf, r.Name, nameW-noteW-2), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfWarning, false)
		pdf.CellFormat(noteW, metricsRowH, note, "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
	} else {
		pdf.CellFormat(nameW, metricsRowH, fitText(pdf, r.Name, nameW-2), "B", 0, "L", false, 0, "")
	}
	if r.NoData {
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(numW*float64(len(cols)), metricsRowH, "No data", "B", 1, "R", false, 0, "")
		return
	}
	for i, c := range cols {
		ln := 0
		if i == len(cols)-1 {
			ln = 1
		}
		pdf.CellFormat(numW, metricsRowH, fitText(pdf, c.value(r), numW-2), "B", ln, "R", false, 0, "")
	}
}

// drawMetricsRowCharts draws the busiest rows' small charts, starting on a
// new page.
func drawMetricsRowCharts(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	if len(n.RowCharts) == 0 {
		return
	}
	pdf.AddPage()
	title := "Busiest rows"
	if n.RankLabel != "" {
		title = "Busiest by " + n.RankLabel
	}
	drawSectionHeading(pdf, title)
	for _, c := range n.RowCharts {
		drawMetricsChart(pdf, data, c, metricsRowChartH)
	}
}

package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The Metrics report layout: a headline page (scope, tiles, running hot,
// busiest, notes), one chart per metric for the whole scope
// (pdf_metrics_charts.go), the tables and the busiest rows' small charts
// (pdf_metrics_tables.go).

const (
	metricsTileH    = 26.0
	metricsHotShown = 10 // running-hot rows listed on the headline page
)

// drawPDFMetricsReport assembles the fixed Metrics report layout.
func drawPDFMetricsReport(pdf *fpdf.Fpdf, data *ReportData) {
	if data.Network == nil {
		drawPDFNotice(pdf, "No network data was gathered for this report.", pdfWarning)
		return
	}
	drawMetricsHeadline(pdf, data)
	drawMetricsCharts(pdf, data)
}

// drawMetricsTitle writes the scope in words and the period with the one it
// is compared with.
func drawMetricsTitle(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	if n.ScopeLabel != "" {
		pdf.SetFont("Helvetica", "B", 13)
		setColor(pdf, pdfInk, false)
		pdf.MultiCell(pdfContentW, 6, pdfText(n.ScopeLabel), "", "L", false)
	}
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfMuted, false)
	pdf.MultiCell(pdfContentW, 5, pdfText(metricsPeriodLine(data)), "", "L", false)
	pdf.Ln(3)
}

// metricsPeriodLine is "214 ports · September 2026, compared with August 2026".
func metricsPeriodLine(data *ReportData) string {
	n := data.Network
	loc := data.ReportLocation()
	line := windowLabel(data.TimeRangeStart, data.TimeRangeEnd, loc)
	if n.PrevEnd.After(n.PrevStart) {
		line += ", compared with " + windowLabel(n.PrevStart, n.PrevEnd, loc)
	}
	if subjects := metricsSubjects(n); subjects != "" {
		line = subjects + " · " + line
	}
	return line
}

// metricsSubjects counts what the report covers: "214 ports" or "3 devices".
// The wording depends on ScopeType to match the controller ruling.
func metricsSubjects(n *MetricsReportData) string {
	switch n.ScopeType {
	case models.ScopeTypeDevices:
		if n.Devices > 0 {
			return countNoun(n.Devices, "device", "devices")
		}
	case models.ScopeTypeSites:
		// On sites scope, don't show the count as subject (rely on ScopeLabel)
		return ""
	default:
		// ports and port_roles both show as "N ports"
		if n.Ports > 0 {
			return countNoun(n.Ports, "port", "ports")
		}
	}
	return ""
}

// windowLabel names a reporting window: "September 2026", "Q3 2026",
// "2026", "Week of September 7, 2026", or its two ends.
func windowLabel(start, end time.Time, loc *time.Location) string {
	s, e := start.In(loc), end.In(loc)
	midnight := func(t time.Time) bool {
		return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
	}
	if midnight(s) {
		switch {
		case s.Day() == 1 && s.Month() == time.January && e.Equal(s.AddDate(1, 0, 0)):
			return s.Format("2006")
		case s.Day() == 1 && (s.Month()-1)%3 == 0 && e.Equal(s.AddDate(0, 3, 0)):
			return fmt.Sprintf("Q%d %d", (int(s.Month())-1)/3+1, s.Year())
		case s.Day() == 1 && e.Equal(s.AddDate(0, 1, 0)):
			return s.Format("January 2006")
		case e.Equal(s.AddDate(0, 0, 7)):
			return "Week of " + s.Format("January 2, 2006")
		}
		if midnight(e) {
			return s.Format("January 2, 2006") + " to " + e.Format("January 2, 2006")
		}
	}
	return s.Format("Jan 2, 2006 15:04") + " to " + e.Format("Jan 2, 2006 15:04")
}

// drawMetricsHeadline draws page one after the shared header.
func drawMetricsHeadline(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	drawMetricsTitle(pdf, data)
	prev := windowLabel(n.PrevStart, n.PrevEnd, data.ReportLocation())
	drawMetricsTiles(pdf, n.Tiles, n.Tables, prev)
	drawMetricsRunningHot(pdf, n.RunningHot)
	drawMetricsBusiest(pdf, n)
	drawMetricsNotes(pdf, n)
}

// drawMetricsTiles lays the tiles out three to a row.
func drawMetricsTiles(pdf *fpdf.Fpdf, tiles []MetricsTile, tables []MetricsTable, prev string) {
	if len(tiles) == 0 {
		return
	}
	const perRow, gap = 3, 4.0
	w := (pdfContentW - gap*(perRow-1)) / perRow
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	y := pdf.GetY()
	for i, t := range tiles {
		col := i % perRow
		if col == 0 && i > 0 {
			y += metricsTileH + gap
		}
		if col == 0 && y+metricsTileH > pageH-bottom {
			pdf.AddPage()
			y = pdf.GetY()
		}
		// Get the corresponding table for billable check
		var table *MetricsTable
		if i < len(tables) {
			table = &tables[i]
		}
		drawMetricsTile(pdf, pdfMarginLeft+float64(col)*(w+gap), y, w, t, table, prev)
	}
	pdf.SetY(y + metricsTileH + 4)
}

func drawMetricsTile(pdf *fpdf.Fpdf, x, y, w float64, t MetricsTile, table *MetricsTable, prev string) {
	setColor(pdf, pdfPanel, true)
	pdf.Rect(x, y, w, metricsTileH, "F")
	setColor(pdf, pdfAccent, true)
	pdf.Rect(x, y, 1.2, metricsTileH, "F")
	textW := w - 6

	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	pdf.SetXY(x+4, y+2.5)
	pdf.CellFormat(textW, 4, fitText(pdf, strings.ToUpper(t.Label), textW-2), "", 0, "L", false, 0, "")

	value, caption := tileFigures(t, table)
	pdf.SetFont("Helvetica", "B", 14)
	setColor(pdf, pdfInk, false)
	pdf.SetXY(x+4, y+7)
	pdf.CellFormat(textW, 7, fitText(pdf, value, textW-2), "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 7.5)
	setColor(pdf, pdfMuted, false)
	pdf.SetXY(x+4, y+15)
	pdf.CellFormat(textW, 4, fitText(pdf, caption, textW-2), "", 0, "L", false, 0, "")
	pdf.SetXY(x+4, y+19.5)
	pdf.CellFormat(textW, 4, fitText(pdf, tileChange(t, prev), textW-2), "", 0, "L", false, 0, "")
}

// tileFigures is a tile's big number and the line under it. Traffic shows
// the billable 95th and the volume moved; a percentage its average and 95th;
// anything else its average. Kind values are the contract's: "traffic",
// "percent", "other".
// Controller ruling P5: billable caption only when the table has Billable true.
func tileFigures(t MetricsTile, table *MetricsTable) (value, caption string) {
	if t.NoData {
		return "No data", ""
	}
	value = formatValue(t.First, t.Unit)
	switch t.Kind {
	case "traffic":
		caption = "95th"
		// Only say "billable" if the table exists and has Billable set to true
		if table != nil && table.Billable {
			caption = "billable 95th"
		}
		if t.Second != nil {
			caption += ", " + formatBytes(*t.Second) + " moved"
		}
	case "percent":
		caption = "average"
		if t.Second != nil {
			caption += ", 95th " + formatValue(*t.Second, t.Unit)
		}
	default:
		caption = "average"
	}
	return value, caption
}

// tileChange is "+18% vs August 2026", "new", or nothing.
func tileChange(t MetricsTile, prev string) string {
	switch {
	case t.NoData:
		return ""
	case t.New:
		return "new"
	case t.Change != nil:
		return formatChange(t.Change, false) + " vs " + prev
	}
	return ""
}

func drawMetricsRunningHot(pdf *fpdf.Fpdf, hot []HotPort) {
	if len(hot) == 0 {
		return
	}
	drawSectionHeading(pdf, "Running hot")
	pdf.SetFont("Helvetica", "", 8.5)
	setColor(pdf, pdfMuted, false)
	pdf.CellFormat(pdfContentW, 5, "Ports whose 95th busy reached 80% or more in either direction.", "", 1, "L", false, 0, "")
	const figuresW = 45.0
	for i, h := range hot {
		if i == metricsHotShown {
			setColor(pdf, pdfMuted, false)
			pdf.CellFormat(pdfContentW, 5, fmt.Sprintf("and %d more: the Busy table lists every port.", len(hot)-i), "", 1, "L", false, 0, "")
			break
		}
		pdf.SetFont("Helvetica", "", 9)
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(pdfContentW-figuresW, 5, fitText(pdf, h.Name, pdfContentW-figuresW-2), "", 0, "L", false, 0, "")
		setColor(pdf, pdfDanger, false)
		pdf.CellFormat(figuresW, 5, fmt.Sprintf("in %s, out %s", formatPercent(h.P95In), formatPercent(h.P95Out)), "", 1, "R", false, 0, "")
	}
}

func drawMetricsBusiest(pdf *fpdf.Fpdf, n *MetricsReportData) {
	if len(n.Busiest) == 0 {
		return
	}
	title := fmt.Sprintf("Busiest %d", len(n.Busiest))
	if n.RankLabel != "" {
		title += " by " + n.RankLabel
	}
	drawSectionHeading(pdf, title)
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfInk, false)
	for i, name := range n.Busiest {
		pdf.CellFormat(pdfContentW, 5, fitText(pdf, fmt.Sprintf("%d. %s", i+1, name), pdfContentW-2), "", 1, "L", false, 0, "")
	}
}

// metricsNotes says what the report left out or could not measure fully.
// Unavailable subjects are counted, never named.
// Controller ruling P3: cap note uses ScopeType for its noun.
func metricsNotes(n *MetricsReportData) []string {
	var notes []string
	if n.LowCoverage > 0 {
		notes = append(notes, fmt.Sprintf("%s with data for less than 90%% of the period, marked in the tables.",
			countNoun(n.LowCoverage, "row", "rows")))
	}
	if n.CappedOut > 0 {
		var singular, plural, limit string
		switch n.ScopeType {
		case models.ScopeTypeDevices:
			singular, plural, limit = "device", "devices", "device"
		case models.ScopeTypeSites:
			singular, plural, limit = "port or device", "ports or devices", "port or device"
		default:
			// ports and port_roles
			singular, plural, limit = "port", "ports", "port"
		}
		notes = append(notes, fmt.Sprintf("Left out %s beyond the %d-%s limit; the report includes the %d busiest.",
			countNoun(n.CappedOut, singular, plural), models.MaxReportSubjects, limit, models.MaxReportSubjects))
	}
	if n.Unavailable > 0 {
		verb := "are"
		if n.Unavailable == 1 {
			verb = "is"
		}
		notes = append(notes, fmt.Sprintf("Left out %s that %s no longer available to you.",
			countNoun(n.Unavailable, "chosen port, device or site", "chosen ports, devices or sites"), verb))
	}
	if len(n.Skipped) > 0 {
		notes = append(notes, "Left out metrics that no longer exist: "+strings.Join(n.Skipped, ", ")+".")
	}
	return notes
}

func drawMetricsNotes(pdf *fpdf.Fpdf, n *MetricsReportData) {
	notes := metricsNotes(n)
	if len(notes) == 0 {
		return
	}
	drawSectionHeading(pdf, "Notes")
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfWarning, false)
	for _, note := range notes {
		pdf.MultiCell(pdfContentW, 4.5, pdfText("- "+note), "", "L", false)
	}
}

// drawPDFNotice draws a one-line message in a panel with a coloured edge,
// on a new page when it would not fit.
func drawPDFNotice(pdf *fpdf.Fpdf, message string, edge [3]int) {
	const h = 18.0
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+h > pageH-bottom {
		pdf.AddPage()
	}
	y := pdf.GetY()
	setColor(pdf, pdfPanel, true)
	pdf.Rect(pdfMarginLeft, y, pdfContentW, h, "F")
	setColor(pdf, edge, true)
	pdf.Rect(pdfMarginLeft, y, 1.4, h, "F")
	pdf.SetXY(pdfMarginLeft+6, y)
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW-12, h, fitText(pdf, message, pdfContentW-14), "", 0, "L", false, 0, "")
	pdf.SetY(y + h + 4)
}

// fitText converts s for the page and, when it is wider than w mm in the
// current font, cuts it to the longest prefix that fits with an ellipsis.
// The result is already converted: draw it as it is, never through pdfText
// again (pdfText reads its input as UTF-8).
func fitText(pdf *fpdf.Fpdf, s string, w float64) string {
	if out := pdfText(s); pdf.GetStringWidth(out) <= w {
		return out
	}
	r := []rune(s)
	cut := func(k int) string { return pdfText(strings.TrimRight(string(r[:k]), " ") + "…") }
	lo, hi := 0, len(r)-1 // the answer keeps lo runes; hi is the most it could keep
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if pdf.GetStringWidth(cut(mid)) <= w {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return cut(lo)
}

// countNoun is "1 port" or "214 ports".
func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

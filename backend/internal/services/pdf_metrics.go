package services

import (
	"fmt"
	"math"
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
	n := data.Network
	switch {
	case n == nil:
		drawPDFNotice(pdf, "No network data was gathered for this report.", pdfWarning)
		return
	case n.Empty:
		// Every subject is gone or hidden: one page that says so.
		drawMetricsTitle(pdf, data)
		drawPDFNotice(pdf, metricsEmptyMessage, pdfWarning)
		return
	}
	drawMetricsHeadline(pdf, data)
	if n.NoData {
		return
	}
	drawMetricsCharts(pdf, data)
	drawMetricsTables(pdf, data)
	drawMetricsRowCharts(pdf, data)
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
// For devices scope, shows device count. For all other scopes (ports, port_roles,
// sites), shows port count when available, otherwise device count.
func metricsSubjects(n *MetricsReportData) string {
	if n.ScopeType == models.ScopeTypeDevices {
		if n.Devices > 0 {
			return countNoun(n.Devices, "device", "devices")
		}
	} else {
		// ports, port_roles, and sites all show port count when available
		if n.Ports > 0 {
			return countNoun(n.Ports, "port", "ports")
		}
		// If no ports, show device count (for sites scope)
		if n.Devices > 0 {
			return countNoun(n.Devices, "device", "devices")
		}
	}
	return ""
}

// midnight reports whether t is exactly midnight in its own location.
func midnight(t time.Time) bool {
	return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}

// calendarLabel names a window that is one calendar unit in loc: "September
// 2026", "Q3 2026", "2026" or "Week of September 7, 2026"; false otherwise.
func calendarLabel(start, end time.Time, loc *time.Location) (string, bool) {
	s, e := start.In(loc), end.In(loc)
	if !midnight(s) {
		return "", false
	}
	switch {
	case s.Day() == 1 && s.Month() == time.January && e.Equal(s.AddDate(1, 0, 0)):
		return s.Format("2006"), true
	case s.Day() == 1 && (s.Month()-1)%3 == 0 && e.Equal(s.AddDate(0, 3, 0)):
		return fmt.Sprintf("Q%d %d", (int(s.Month())-1)/3+1, s.Year()), true
	case s.Day() == 1 && e.Equal(s.AddDate(0, 1, 0)):
		return s.Format("January 2006"), true
	case e.Equal(s.AddDate(0, 0, 7)):
		return "Week of " + s.Format("January 2, 2006"), true
	}
	return "", false
}

// windowLabel names a reporting window: "September 2026", "Q3 2026",
// "2026", "Week of September 7, 2026", or its two ends.
func windowLabel(start, end time.Time, loc *time.Location) string {
	if label, ok := calendarLabel(start, end, loc); ok {
		return label
	}
	s, e := start.In(loc), end.In(loc)
	if midnight(s) && midnight(e) {
		return s.Format("January 2, 2006") + " to " + e.Format("January 2, 2006")
	}
	return s.Format("Jan 2, 2006 15:04") + " to " + e.Format("Jan 2, 2006 15:04")
}

// comparedWith names the previous period on a tile's change line: the
// calendar unit when it is one ("August 2026"), otherwise its length ("the
// previous 30 days"), since a rolling or custom window's two ends do not fit
// on a tile. A run of days counts as days though a DST change adds or drops
// an hour; one day is "24 hours", and a window that is not whole days is
// counted in hours.
func comparedWith(start, end time.Time, loc *time.Location) string {
	if label, ok := calendarLabel(start, end, loc); ok {
		return label
	}
	hours := end.Sub(start).Hours()
	if days := math.Round(hours / 24); days >= 2 && math.Abs(hours-days*24) <= 1 {
		return fmt.Sprintf("the previous %d days", int(days))
	}
	return "the previous " + countNoun(max(1, int(math.Round(hours))), "hour", "hours")
}

// drawMetricsHeadline draws page one after the shared header.
func drawMetricsHeadline(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	drawMetricsTitle(pdf, data)
	if n.NoData {
		drawPDFNotice(pdf, "No data for this period", pdfMuted)
		drawMetricsNotes(pdf, n)
		return
	}
	prev := comparedWith(n.PrevStart, n.PrevEnd, data.ReportLocation())
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

// tileFigures is a tile's big number and the line under it. For traffic
// metrics, shows "billable 95th" when the table is an in/out pair (Billable=true),
// otherwise "95th"; also shows volume moved. For percentage metrics shows average
// and 95th. For all other metrics shows average. Kind values are the contract's:
// "traffic", "percent", "other".
func tileFigures(t MetricsTile, table *MetricsTable) (value, caption string) {
	if t.NoData {
		return "No data", ""
	}
	value = formatValue(t.First, t.Unit)
	switch t.Kind {
	case "traffic":
		caption = trafficCaption(table)
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

// tileChange is "+18% vs August 2026", "+18% vs the previous 30 days", "new",
// or nothing. prev is comparedWith's name for the previous period.
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
		pdf.CellFormat(figuresW, 5, hotFigures(h), "", 1, "R", false, 0, "")
	}
}

// hotFigures is a running-hot port's 95th busy each way, "in 92.5%, out 41%".
// A direction with no busy data (only the other busy metric was chosen)
// reads "-", not "0%".
func hotFigures(h HotPort) string {
	side := func(p95 float64, has bool) string {
		if !has {
			return "-"
		}
		return formatPercent(p95)
	}
	return fmt.Sprintf("in %s, out %s", side(h.P95In, h.HasIn), side(h.P95Out, h.HasOut))
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
// Unavailable subjects are counted, never named. Cap note wording depends on
// ScopeType: uses "device/devices" for devices scope, "port or device/ports
// or devices" for sites scope, and "port/ports" for port scopes.
func metricsNotes(n *MetricsReportData) []string {
	var notes []string
	if n.LowCoverage > 0 {
		notes = append(notes, fmt.Sprintf("%s with data for less than 90%% of the period, marked in the tables.",
			countNoun(n.LowCoverage, "row", "rows")))
	}
	if n.CappedOut > 0 {
		var singular, plural, limitPlural string
		switch n.ScopeType {
		case models.ScopeTypeDevices:
			singular, plural, limitPlural = "device", "devices", "devices"
		case models.ScopeTypeSites:
			singular, plural, limitPlural = "port or device", "ports or devices", "ports or devices"
		default:
			// ports and port_roles
			singular, plural, limitPlural = "port", "ports", "ports"
		}
		notes = append(notes, fmt.Sprintf("Left out %s beyond the limit of %d %s; the report includes the %d busiest.",
			countNoun(n.CappedOut, singular, plural), models.MaxReportSubjects, limitPlural, models.MaxReportSubjects))
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

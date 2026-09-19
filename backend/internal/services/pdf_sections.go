package services

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// How many rows each list section prints before summarising the remainder. A
// quarter of incidents can run to hundreds; past a point a reader is served
// better by "and 340 more" than by 340 more rows.
const (
	maxTimelineRows     = 60
	maxPerformanceRows  = 25
	maxAvailabilityRows = 40
)

// drawPDFExecutiveSummary is the headline figures with their movement against
// the previous equivalent period.
func drawPDFExecutiveSummary(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Executive Summary")

	current := summarize(data.Metrics)
	prev := data.Previous

	type row struct {
		label string
		value string
		delta string
		color [3]int
	}
	rows := []row{
		{"Availability", formatUptimePercent(current.uptime), "", uptimeColor(current.uptime)},
		{"Incidents", fmt.Sprintf("%d", current.incidents), "", pdfInk},
		{"Total downtime", formatMinutes(current.downtime), "", pdfInk},
		{"Services covered", fmt.Sprintf("%d", len(data.Metrics)), "", pdfInk},
	}
	if prev != nil {
		// More uptime is better; more incidents and more downtime are worse.
		rows[0].delta = signedDelta(prev.UptimeDelta, "pp", true)
		rows[1].delta = signedCountDelta(prev.IncidentDelta)
		rows[2].delta = signedDeltaMinutes(prev.DowntimeDelta)
	}

	pdf.SetFont("Helvetica", "", 9)
	for _, r := range rows {
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(55, 6, pdfText(r.label), "", 0, "L", false, 0, "")
		setColor(pdf, r.color, false)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.CellFormat(35, 6, pdfText(r.value), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(pdfContentW-90, 6, pdfText(r.delta), "", 1, "L", false, 0, "")
	}

	if prev != nil {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "I", 8)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 4.5,
			pdfText(fmt.Sprintf("Compared with %s.", prev.Label)), "", "L", false)
	}

	// Naming the worst performer is the single most useful line in the section:
	// an average hides which service dragged it down.
	if worst, ok := worstMonitor(data.Metrics); ok && worst.Uptime < 100 {
		pdf.SetFont("Helvetica", "", 9)
		setColor(pdf, pdfInk, false)
		pdf.MultiCell(pdfContentW, 5, pdfText(fmt.Sprintf(
			"Needs attention: %s at %s over %s of downtime across %d incident(s).",
			worst.MonitorName, formatUptimePercent(worst.Uptime),
			formatMinutes(worst.DowntimeMinutes), worst.IncidentCount)), "", "L", false)
	}

	if perfect := perfectMonitors(data.Metrics); len(perfect) > 0 {
		pdf.SetFont("Helvetica", "", 9)
		setColor(pdf, pdfSuccess, false)
		pdf.MultiCell(pdfContentW, 5, pdfText(fmt.Sprintf(
			"No recorded downtime: %s.", joinCapped(perfect, 6))), "", "L", false)
	}
	pdf.Ln(2)
}

// drawPDFTimeline is every incident in the scope, in the order they happened.
func drawPDFTimeline(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "What Happened")

	if len(data.Timeline) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, pdfText("Nothing was recorded during this period."), "", "L", false)
		return
	}

	loc := data.ReportLocation()
	widths := []float64{34, pdfContentW - 34 - 26 - 24, 26, 24}
	drawTableHeader(pdf, []string{"When", "Service", "Duration", "Status"}, widths)

	pdf.SetFont("Helvetica", "", 8.5)
	shown := data.Timeline
	if len(shown) > maxTimelineRows {
		shown = shown[:maxTimelineRows]
	}
	for _, e := range shown {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 6, pdfText(e.StartTime.In(loc).Format("Jan 02 15:04")), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 6, pdfText(truncate(e.MonitorName, 44)), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[2], 6, pdfText(formatMinutes(e.DurationMinutes)), "B", 0, "L", false, 0, "")
		if e.Status == "ongoing" {
			setColor(pdf, pdfDanger, false)
		}
		pdf.CellFormat(widths[3], 6, pdfText(e.Status), "B", 1, "L", false, 0, "")

		// The cause is why the row is worth reading, so it gets its own line
		// rather than being truncated into a column.
		if e.Cause != "" {
			pdf.SetFont("Helvetica", "", 7.5)
			setColor(pdf, pdfMuted, false)
			pdf.SetX(pdfMarginLeft + widths[0])
			pdf.MultiCell(pdfContentW-widths[0], 4, pdfText(truncate(e.Cause, 150)), "", "L", false)
			pdf.SetFont("Helvetica", "", 8.5)
		}
	}

	if len(data.Timeline) > maxTimelineRows {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "I", 8)
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(pdfContentW, 5, pdfText(fmt.Sprintf(
			"and %d more incidents in this period", len(data.Timeline)-maxTimelineRows)),
			"", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
}

// drawPDFAvailability is the per-day (or per-week) breakdown.
func drawPDFAvailability(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Availability Breakdown")

	if len(data.Availability) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, pdfText("No availability data for this period."), "", "L", false)
		return
	}

	widths := []float64{50, 30, 34, pdfContentW - 114}
	drawTableHeader(pdf, []string{"Period", "Uptime", "Downtime", "Incidents"}, widths)

	pdf.SetFont("Helvetica", "", 8.5)
	shown := data.Availability
	if len(shown) > maxAvailabilityRows {
		shown = shown[:maxAvailabilityRows]
	}
	for _, b := range shown {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 6, pdfText(b.Label), "B", 0, "L", false, 0, "")
		setColor(pdf, uptimeColor(b.Uptime), false)
		pdf.CellFormat(widths[1], 6, pdfText(formatUptimePercent(b.Uptime)), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[2], 6, pdfText(formatMinutes(b.DowntimeMinutes)), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[3], 6, pdfText(fmt.Sprintf("%d", b.IncidentCount)), "B", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
}

// drawPDFPerformance is response-time behaviour per monitor.
func drawPDFPerformance(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Response Time")

	if len(data.Performance) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5,
			pdfText("No successful checks were recorded for these services during this period."),
			"", "L", false)
		return
	}

	widths := []float64{pdfContentW - 110, 28, 28, 28, 26}
	drawTableHeader(pdf, []string{"Service", "Average", "95th pct", "Slowest", "Samples"}, widths)

	pdf.SetFont("Helvetica", "", 8.5)
	shown := data.Performance
	if len(shown) > maxPerformanceRows {
		shown = shown[:maxPerformanceRows]
	}
	for _, p := range shown {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 6, pdfText(truncate(p.MonitorName, 44)), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 6, pdfText(formatMillis(p.AvgMs)), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[2], 6, pdfText(formatMillis(p.P95Ms)), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[3], 6, pdfText(formatMillis(float64(p.MaxMs))), "B", 0, "L", false, 0, "")
		pdf.CellFormat(widths[4], 6, pdfText(fmt.Sprintf("%d", p.Samples)), "B", 1, "L", false, 0, "")
	}

	pdf.Ln(1)
	pdf.SetFont("Helvetica", "I", 8)
	setColor(pdf, pdfMuted, false)
	// Said plainly: otherwise a reader reasonably assumes timeouts are included
	// and reads the averages as far worse than they are.
	pdf.MultiCell(pdfContentW, 4.5,
		pdfText("Measured from successful checks only. A timed-out check records the timeout setting rather than the service's speed, so including it would turn an outage into a slowdown."),
		"", "L", false)
	pdf.Ln(1)
}

func drawTableHeader(pdf *fpdf.Fpdf, headers []string, widths []float64) {
	pdf.SetFont("Helvetica", "B", 8.5)
	setColor(pdf, pdfPanel, true)
	setColor(pdf, pdfInk, false)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, pdfText(h), "", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)
}

// formatMillis renders a duration in the unit that reads best at its size.
func formatMillis(ms float64) string {
	if ms <= 0 {
		return "-"
	}
	if ms >= 1000 {
		return fmt.Sprintf("%.2fs", ms/1000)
	}
	return fmt.Sprintf("%.0fms", ms)
}

// signedDelta renders a change with its direction. better says which sign is an
// improvement, so the reader is not left working out whether up is good.
func signedDelta(delta float64, unit string, higherIsBetter bool) string {
	if delta == 0 {
		return "no change"
	}
	word := "worse"
	if (delta > 0) == higherIsBetter {
		word = "better"
	}
	sign := "+"
	if delta < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.2f%s %s", sign, delta, unit, word)
}

// signedCountDelta renders a change in a count. Counts are whole things, so
// "+7 worse" rather than the "+7.00 worse" a decimal format produces.
func signedCountDelta(delta int) string {
	switch {
	case delta == 0:
		return "no change"
	case delta > 0:
		return fmt.Sprintf("+%d worse", delta)
	default:
		return fmt.Sprintf("%d better", delta)
	}
}

func signedDeltaMinutes(delta float64) string {
	if delta == 0 {
		return "no change"
	}
	if delta > 0 {
		return fmt.Sprintf("+%s worse", formatMinutes(delta))
	}
	return fmt.Sprintf("-%s better", formatMinutes(-delta))
}

func worstMonitor(metrics []ReportMetrics) (ReportMetrics, bool) {
	if len(metrics) == 0 {
		return ReportMetrics{}, false
	}
	worst := metrics[0]
	for _, m := range metrics[1:] {
		if m.Uptime < worst.Uptime {
			worst = m
		}
	}
	return worst, true
}

func perfectMonitors(metrics []ReportMetrics) []string {
	var names []string
	for _, m := range metrics {
		if m.DowntimeMinutes == 0 && m.IncidentCount == 0 {
			names = append(names, m.MonitorName)
		}
	}
	return names
}

func joinCapped(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:limit], ", "), len(names)-limit)
}

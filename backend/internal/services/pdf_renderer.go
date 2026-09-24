// Package services - pdf_renderer.go renders aggregated report data to a PDF on
// disk.
//
// The original design shelled out to wkhtmltopdf. That is not viable here: the
// runtime image is Alpine, which has no wkhtmltopdf package at all, and the
// project was archived upstream in 2023 with open CVEs while parsing HTML we
// generate. Drawing the PDF directly keeps the image at ~20MB with no external
// binary and no subprocess to sandbox, at the cost of CSS fidelity - the layout
// here is code rather than a stylesheet.
package services

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Page geometry and palette, kept close to the HTML report so the two renderings
// of the same data look like siblings.
const (
	pdfMarginLeft  = 15.0
	pdfMarginTop   = 15.0
	pdfMarginRight = 15.0
	pdfPageWidth   = 210.0 // A4 portrait, mm
	pdfContentW    = pdfPageWidth - pdfMarginLeft - pdfMarginRight
)

var (
	pdfInk     = [3]int{26, 26, 26}
	pdfMuted   = [3]int{102, 102, 102}
	pdfRule    = [3]int{229, 231, 235}
	pdfPanel   = [3]int{243, 244, 246}
	pdfAccent  = [3]int{59, 130, 246}
	pdfSuccess = [3]int{16, 185, 129}
	pdfWarning = [3]int{245, 158, 11}
	pdfDanger  = [3]int{239, 68, 68}
)

// PDFRendererService writes report PDFs into a single output directory.
type PDFRendererService struct {
	outputDir string
}

// NewPDFRendererService returns a renderer writing to outputDir, creating it if
// needed. An unusable directory is reported now rather than at the first
// generation attempt.
func NewPDFRendererService(outputDir string) (*PDFRendererService, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating report output directory %q: %w", outputDir, err)
	}
	return &PDFRendererService{outputDir: outputDir}, nil
}

// RenderReportToPDF draws data to a PDF and returns the generated file's base
// name (not its path - callers store the name and resolve it via GetPDFPath).
// reportType selects the fixed layout: models.ReportTypeUptime or
// models.ReportTypeIncident.
func (s *PDFRendererService) RenderReportToPDF(data *ReportData, reportType string, nameHint string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("report data is nil")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetTitle(reportTitle(data), true)
	pdf.AddPage()

	drawPDFHeader(pdf, data)
	switch reportType {
	case models.ReportTypeIncident:
		drawPDFIncidentReport(pdf, data)
	default:
		// Uptime is also the fallback for an unrecognized value, which
		// Report.Validate already prevents from ever being stored.
		drawPDFUptimeReport(pdf, data)
	}
	drawPDFWarnings(pdf, data)
	drawPDFFooter(pdf)

	filename := pdfFilename(nameHint)
	outputPath := filepath.Join(s.outputDir, filename)
	if err := pdf.OutputFileAndClose(outputPath); err != nil {
		return "", fmt.Errorf("writing report PDF: %w", err)
	}
	return filename, nil
}

// pdfFilename builds a collision-resistant, filesystem-safe file name.
func pdfFilename(nameHint string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, nameHint)
	if safe == "" {
		safe = "report"
	}
	if len(safe) > 60 {
		safe = safe[:60]
	}
	return fmt.Sprintf("%d_%s.pdf", time.Now().UnixNano(), safe)
}

// GetPDFPath resolves a stored file name to its path on disk.
//
// The name is treated as untrusted: only a bare base name is accepted, so a
// stored value containing a path separator or ".." cannot escape outputDir.
func (s *PDFRendererService) GetPDFPath(pdfFilename string) (string, error) {
	base := filepath.Base(pdfFilename)
	if base != pdfFilename || base == "." || base == ".." || base == "" {
		return "", fmt.Errorf("invalid report file name %q", pdfFilename)
	}
	return filepath.Join(s.outputDir, base), nil
}

// DeletePDF removes a generated report file. A file that is already gone is not
// an error: the database row is the record that matters, and a half-deleted
// report is worse than a missing file.
func (s *PDFRendererService) DeletePDF(pdfFilename string) error {
	path, err := s.GetPDFPath(pdfFilename)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetPDFFileSize returns the size of a generated report in bytes.
func (s *PDFRendererService) GetPDFFileSize(pdfFilename string) (int, error) {
	path, err := s.GetPDFPath(pdfFilename)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

// ---- report-type layouts ---------------------------------------------------

// drawPDFUptimeReport assembles the fixed Uptime Report layout: the summary
// tiles, the cumulative-uptime-vs-SLA graph (or a plain-language banner when
// there is no meaningful trend to chart), and the per-monitor SLA table.
func drawPDFUptimeReport(pdf *fpdf.Fpdf, data *ReportData) {
	drawPDFSummary(pdf, data)
	drawPDFUptimeTrend(pdf, data.UptimeSeries, data.EffectiveSLA, data.ReportLocation())
	drawPDFSLASection(pdf, data)
}

// drawPDFIncidentReport assembles the fixed Incident Report layout: the
// summary tiles and the full incident detail list.
func drawPDFIncidentReport(pdf *fpdf.Fpdf, data *ReportData) {
	drawPDFSummary(pdf, data)
	drawPDFIncidentSection(pdf, data)
}

// ---- drawing helpers -------------------------------------------------------

func setColor(pdf *fpdf.Fpdf, c [3]int, fill bool) {
	if fill {
		pdf.SetFillColor(c[0], c[1], c[2])
		return
	}
	pdf.SetTextColor(c[0], c[1], c[2])
}

// setDrawColor sets the stroke color fpdf uses for Line and the "D"/"DF"
// rectangle styles - distinct from setColor's fill/text targets, neither of
// which affects a stroked line or border.
func setDrawColor(pdf *fpdf.Fpdf, c [3]int) {
	pdf.SetDrawColor(c[0], c[1], c[2])
}

// uptimeColor grades an uptime percentage the same way the HTML report does.
func uptimeColor(pct float64) [3]int {
	switch {
	case pct < 95:
		return pdfDanger
	case pct < 99:
		return pdfWarning
	default:
		return pdfSuccess
	}
}

func drawPDFHeader(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 22)
	setColor(pdf, pdfInk, false)
	pdf.MultiCell(pdfContentW, 9, pdfText(reportTitle(data)), "", "L", false)

	if data.CustomDescription != nil && *data.CustomDescription != "" {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, pdfText(*data.CustomDescription), "", "L", false)
	}

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfMuted, false)
	// Every timestamp in the document is written in the report's configured
	// zone. Without this they rendered in the server process's zone, which in a
	// container is UTC by default — a timezone nobody chose.
	loc := data.ReportLocation()
	pdf.MultiCell(pdfContentW, 5, pdfText(fmt.Sprintf(
		"Report period: %s to %s\nGenerated: %s",
		data.TimeRangeStart.In(loc).Format("January 02, 2006"),
		data.TimeRangeEnd.In(loc).Format("January 02, 2006"),
		time.Now().In(loc).Format("January 02, 2006 at 15:04 MST"),
	)), "", "L", false)

	pdf.Ln(2)
	setColor(pdf, pdfRule, true)
	pdf.Rect(pdfMarginLeft, pdf.GetY(), pdfContentW, 0.6, "F")
	pdf.Ln(5)
}

func drawSectionHeading(pdf *fpdf.Fpdf, title string) {
	pdf.Ln(3)
	y := pdf.GetY()
	setColor(pdf, pdfAccent, true)
	pdf.Rect(pdfMarginLeft, y, 1.4, 7, "F")
	pdf.SetX(pdfMarginLeft + 4)
	pdf.SetFont("Helvetica", "B", 14)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW-4, 7, pdfText(title), "", 1, "L", false, 0, "")
	pdf.Ln(2)
}

func drawPDFSummary(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Summary")

	total := len(data.Metrics)
	healthy, incidents, avgUptime := 0, 0, 0.0
	for _, m := range data.Metrics {
		if m.Uptime >= 99.0 {
			healthy++
		}
		incidents += m.IncidentCount
		avgUptime += m.Uptime
	}
	if total > 0 {
		avgUptime /= float64(total)
	}

	tiles := []struct {
		label string
		value string
		color [3]int
	}{
		{"Services monitored", fmt.Sprintf("%d", total), pdfInk},
		{"Average uptime", formatUptimePercent(avgUptime), uptimeColor(avgUptime)},
		{"Total incidents", fmt.Sprintf("%d", incidents), pdfInk},
		{"Healthy services", fmt.Sprintf("%d", healthy), pdfSuccess},
	}

	const gap = 4.0
	w := (pdfContentW - gap*3) / 4
	y := pdf.GetY()
	for i, t := range tiles {
		x := pdfMarginLeft + float64(i)*(w+gap)
		setColor(pdf, pdfPanel, true)
		pdf.Rect(x, y, w, 20, "F")
		setColor(pdf, pdfAccent, true)
		pdf.Rect(x, y, 1.2, 20, "F")

		pdf.SetXY(x+4, y+3.5)
		pdf.SetFont("Helvetica", "", 7)
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(w-6, 4, pdfText(strings.ToUpper(t.label)), "", 0, "L", false, 0, "")

		pdf.SetXY(x+4, y+9.5)
		pdf.SetFont("Helvetica", "B", 15)
		setColor(pdf, t.color, false)
		pdf.CellFormat(w-6, 8, pdfText(t.value), "", 0, "L", false, 0, "")
	}
	pdf.SetY(y + 24)
}

// allPerfect reports whether every sample in series is exactly 100% uptime -
// a genuinely perfect record, not just a value that rounds to it.
// aggregateUptimePercent runs every value through round2, so a true 100%
// arrives here as the exact float 100, never 99.9954-rounds-to-100.
func allPerfect(series []UptimeSeriesPoint) bool {
	for _, p := range series {
		if p.Uptime != 100 {
			return false
		}
	}
	return true
}

// drawPDFUptimeTrend draws the "Uptime vs. SLA target" section: the
// cumulative-uptime-vs-SLA line chart when there is a real trend to show, or
// a plain-language banner in its place when there isn't - either because too
// little of the window is measurable yet to plot anything, or because uptime
// was a flat, genuine 100% and a chart would only draw two lines pinned to
// the very top, which reads as a missing graph rather than a good one.
func drawPDFUptimeTrend(pdf *fpdf.Fpdf, series []UptimeSeriesPoint, slaTarget float64, loc *time.Location) {
	switch {
	case len(series) < 2:
		drawPDFUptimeBanner(pdf, "Not enough measurable history yet to chart a trend for this period.")
	case allPerfect(series):
		drawPDFUptimeBanner(pdf, "100% uptime throughout this period - no incidents to chart.")
	default:
		drawPDFUptimeGraph(pdf, series, slaTarget, loc)
	}
}

// drawPDFUptimeBanner draws a short status message under the trend section's
// own heading, sized and styled to sit where the graph otherwise would so
// the report's layout does not jump around depending on which one renders.
func drawPDFUptimeBanner(pdf *fpdf.Fpdf, message string) {
	drawSectionHeading(pdf, "Uptime vs. SLA target")

	const bannerH = 18.0
	// Same page-fit guard as the graph itself: force a fresh page rather than
	// let a long custom_description push this past the bottom margin.
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+bannerH > pageH-bottom {
		pdf.AddPage()
		drawSectionHeading(pdf, "Uptime vs. SLA target")
	}

	y := pdf.GetY()
	setColor(pdf, pdfPanel, true)
	pdf.Rect(pdfMarginLeft, y, pdfContentW, bannerH, "F")
	setColor(pdf, pdfSuccess, true)
	pdf.Rect(pdfMarginLeft, y, 1.4, bannerH, "F")

	pdf.SetXY(pdfMarginLeft+6, y)
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW-12, bannerH, pdfText(message), "", 0, "L", false, 0, "")

	pdf.SetY(y + bannerH + 4)
}

// drawPDFUptimeGraph draws the cumulative-uptime-vs-SLA line chart: an axis
// box, a flat SLA reference bar, and the cumulative-uptime line connecting
// each sample point. Built from fpdf's own line/rect primitives - no image
// pipeline, matching why this package draws PDFs directly at all (see the
// package doc comment above). Callers should route through
// drawPDFUptimeTrend rather than call this directly - it assumes a real,
// non-flat-100% series worth charting.
func drawPDFUptimeGraph(pdf *fpdf.Fpdf, series []UptimeSeriesPoint, slaTarget float64, loc *time.Location) {
	if len(series) < 2 {
		return
	}

	const chartH = 55.0

	// fpdf's auto-page-break only fires on Cell/MultiCell, never on the
	// Rect/Line primitives this whole function draws with. A long
	// custom_description rendered earlier via MultiCell can leave GetY() deep
	// enough down the page that the chart would draw past the bottom margin
	// (or overlap the footer) with no error. Force a fresh page first when
	// the heading plus the chart itself would not fit in what's left.
	const chartMargin = 20.0 // heading + axis labels + breathing room
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+chartH+chartMargin > pageH-bottom {
		pdf.AddPage()
	}

	drawSectionHeading(pdf, "Uptime vs. SLA target")

	const axisLabelW = 14.0
	x0 := pdfMarginLeft + axisLabelW
	chartW := pdfContentW - axisLabelW
	y0 := pdf.GetY()

	// The floor is the lowest value actually on the chart rather than always
	// zero: uptime rarely drops below the high nineties, and a 0-100 axis would
	// draw every report as a flat line pinned to the top, hiding the one thing
	// an SLA chart exists to show.
	lo := slaTarget
	for _, p := range series {
		if p.Uptime < lo {
			lo = p.Uptime
		}
	}
	lo = math.Floor(lo) - 1
	if lo < 0 {
		lo = 0
	}
	hi := 100.0
	if hi <= lo {
		hi = lo + 1
	}
	// A small top pad keeps a genuine 100% value's line visually separated
	// from the axis box's own top border. Without it, a flat 100% record
	// (the common "everything's fine" case) draws its line exactly on top of
	// the box's top edge - two things at the same coordinate read as one,
	// which looks like the graph never rendered at all. Applied through the
	// same yFor every reader (gridlines, the SLA line, the uptime line) goes
	// through, so nothing drifts out of alignment with anything else.
	const topPad = 3.0
	usableH := chartH - topPad
	yFor := func(pct float64) float64 {
		frac := (pct - lo) / (hi - lo)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return y0 + topPad + usableH*(1-frac)
	}

	pdf.SetFont("Helvetica", "", 7)
	for i := 0; i <= 4; i++ {
		frac := float64(i) / 4
		val := lo + (hi-lo)*frac
		y := yFor(val)
		setColor(pdf, pdfRule, true)
		pdf.Rect(x0, y, chartW, 0.15, "F")
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(pdfMarginLeft, y-2)
		pdf.CellFormat(axisLabelW-1, 4, fmt.Sprintf("%.0f%%", val), "", 0, "R", false, 0, "")
	}
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	pdf.Rect(x0, y0, chartW, chartH, "D")

	// SLA reference line: a thin flat bar the full width of the chart.
	setColor(pdf, pdfWarning, true)
	pdf.Rect(x0, yFor(slaTarget)-0.25, chartW, 0.5, "F")
	pdf.SetXY(x0+2, yFor(slaTarget)-5)
	pdf.SetFont("Helvetica", "B", 7)
	setColor(pdf, pdfWarning, false)
	pdf.CellFormat(60, 4, pdfText(fmt.Sprintf("SLA target %.2f%%", slaTarget)), "", 0, "L", false, 0, "")

	// The cumulative-uptime line: one segment between each pair of consecutive
	// samples.
	setDrawColor(pdf, pdfAccent)
	pdf.SetLineWidth(0.6)
	n := len(series)
	for i := 0; i < n-1; i++ {
		x1 := x0 + chartW*float64(i)/float64(n-1)
		x2 := x0 + chartW*float64(i+1)/float64(n-1)
		pdf.Line(x1, yFor(series[i].Uptime), x2, yFor(series[i+1].Uptime))
	}
	pdf.SetLineWidth(0.2)

	// X-axis date labels: first, middle, and last sample only, so the axis
	// stays readable instead of crowding thirty overlapping labels.
	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	label := func(i int, align string) {
		x := x0 + chartW*float64(i)/float64(n-1)
		pdf.SetXY(x-15, y0+chartH+1.5)
		pdf.CellFormat(30, 4, series[i].Date.In(loc).Format("Jan 2"), "", 0, align, false, 0, "")
	}
	label(0, "L")
	label(n-1, "R")
	if mid := (n - 1) / 2; mid > 0 && mid < n-1 {
		label(mid, "C")
	}

	pdf.SetY(y0 + chartH + 9)
}

func drawPDFSLASection(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "SLA Compliance")

	if len(data.Metrics) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, "No monitors in scope for this report.", "", "L", false)
		return
	}

	widths := []float64{pdfContentW - 105, 35, 35, 35}
	headers := []string{"Service", "Uptime", "SLA target", "Status"}

	pdf.SetFont("Helvetica", "B", 9)
	setColor(pdf, pdfPanel, true)
	setColor(pdf, pdfInk, false)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 8, pdfText(h), "", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 9)
	for _, m := range data.Metrics {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 7, pdfText(truncate(m.MonitorName, 46)), "B", 0, "L", false, 0, "")
		setColor(pdf, uptimeColor(m.Uptime), false)
		pdf.CellFormat(widths[1], 7, formatUptimePercent(m.Uptime), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
		target := 0.0
		if m.SLATarget != nil {
			target = *m.SLATarget
		}
		pdf.CellFormat(widths[2], 7, fmt.Sprintf("%.2f%%", target), "B", 0, "L", false, 0, "")

		status, color := "Missed", pdfDanger
		if m.SLAMet {
			status, color = "Met", pdfSuccess
		}
		setColor(pdf, color, false)
		pdf.CellFormat(widths[3], 7, pdfText(status), "B", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
}

func drawPDFIncidentSection(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Incidents")

	total := 0
	for _, m := range data.Metrics {
		total += m.IncidentCount
	}
	if total == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, "No incidents recorded during this period.", "", "L", false)
		return
	}

	pdf.SetFont("Helvetica", "B", 10)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW, 6, fmt.Sprintf("Total incidents: %d", total), "", 1, "L", false, 0, "")
	pdf.Ln(1)

	for _, m := range data.Metrics {
		if len(m.Incidents) == 0 {
			continue
		}
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 11)
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(pdfContentW, 6,
			pdfText(fmt.Sprintf("%s (%d)", truncate(m.MonitorName, 60), len(m.Incidents))), "", 1, "L", false, 0, "")

		for _, inc := range m.Incidents {
			y := pdf.GetY()
			setColor(pdf, pdfDanger, true)
			pdf.Rect(pdfMarginLeft, y, 1.2, 6, "F")
			pdf.SetX(pdfMarginLeft + 4)

			pdf.SetFont("Helvetica", "B", 9)
			setColor(pdf, pdfInk, false)
			pdf.CellFormat(pdfContentW-44, 6, pdfText(inc.StartTime.In(data.ReportLocation()).Format("Jan 02, 2006 15:04")), "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			setColor(pdf, pdfMuted, false)
			pdf.CellFormat(40, 6, pdfText(fmt.Sprintf("%s  %s", formatMinutes(inc.Duration), inc.Status)), "", 1, "R", false, 0, "")

			for _, detail := range [][2]string{
				{"Root cause", inc.RootCause},
				{"Resolution", inc.ResolutionNotes},
			} {
				if detail[1] == "" {
					continue
				}
				pdf.SetX(pdfMarginLeft + 4)
				pdf.SetFont("Helvetica", "", 8)
				setColor(pdf, pdfMuted, false)
				pdf.MultiCell(pdfContentW-4, 4, pdfText(detail[0]+": "+detail[1]), "", "L", false)
			}
			pdf.Ln(1.5)
		}
	}
}

// drawPDFWarnings surfaces monitors the aggregator could not include. A report
// is a compliance artifact, so an omission is stated on its face rather than
// left to be noticed by its absence.
func drawPDFWarnings(pdf *fpdf.Fpdf, data *ReportData) {
	if len(data.Warnings) == 0 {
		return
	}
	drawSectionHeading(pdf, "Data warnings")
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfWarning, false)
	for _, w := range data.Warnings {
		pdf.MultiCell(pdfContentW, 4.5, pdfText("- "+w), "", "L", false)
	}
}

func drawPDFFooter(pdf *fpdf.Fpdf) {
	pdf.SetY(-15)
	pdf.SetFont("Helvetica", "I", 8)
	setColor(pdf, pdfMuted, false)
	pdf.CellFormat(pdfContentW, 6, "Generated by Sentinel", "", 0, "C", false, 0, "")
}

// ---- shared helpers --------------------------------------------------------

// reportTitle prefers the caller's custom title over the report's name.
func reportTitle(data *ReportData) string {
	if data.CustomTitle != nil && *data.CustomTitle != "" {
		return *data.CustomTitle
	}
	return data.ReportName
}

// formatMinutes renders a downtime duration compactly (e.g. "2h 15m").
//
// Sub-minute outages are shown in seconds rather than as "0m". A monitor on a
// 30-second interval produces exactly those, and rendering four of them as "0m"
// next to a 100% uptime figure is how a real outage came to look like nothing
// happened.
func formatMinutes(minutes float64) string {
	if minutes <= 0 {
		return "0s"
	}
	seconds := int(math.Round(minutes * 60))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	total := int(math.Round(minutes))
	if total < 60 {
		return fmt.Sprintf("%dm", total)
	}
	h, m := total/60, total%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// formatUptimePercent renders an uptime figure without ever rounding a real
// outage away.
//
// "%.2f" turns 99.9954% into "100.00%", which read as a contradiction beside a
// list of incidents. Anything short of a perfect record is rounded down, so
// 100% means no recorded downtime at all and nothing else does.
func formatUptimePercent(pct float64) string {
	if pct >= 100 {
		return "100.00%"
	}
	floored := math.Floor(pct*100) / 100
	if floored >= 100 {
		floored = 99.99
	}
	return fmt.Sprintf("%.2f%%", floored)
}

// truncate shortens s to max runes, marking that it was cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

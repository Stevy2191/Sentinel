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
// sections selects which parts of the report are drawn, in the order given.
func (s *PDFRendererService) RenderReportToPDF(data *ReportData, sections []string, nameHint string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("report data is nil")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetTitle(reportTitle(data), true)
	pdf.AddPage()

	drawPDFHeader(pdf, data)
	for _, section := range sectionsOrDefault(sections) {
		switch section {
		case models.SectionSLACompliance:
			drawPDFSLASection(pdf, data)
		case models.SectionIncidentSummary:
			drawPDFIncidentSection(pdf, data)
		case models.SectionCharts:
			// Charts are not rendered yet; the summary tiles stand in for them.
			drawPDFSummary(pdf, data)
		case models.SectionCustom:
			drawPDFCustomSection(pdf, data)
		}
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

// ---- drawing helpers -------------------------------------------------------

func setColor(pdf *fpdf.Fpdf, c [3]int, fill bool) {
	if fill {
		pdf.SetFillColor(c[0], c[1], c[2])
		return
	}
	pdf.SetTextColor(c[0], c[1], c[2])
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
	pdf.MultiCell(pdfContentW, 9, reportTitle(data), "", "L", false)

	if data.CustomDescription != nil && *data.CustomDescription != "" {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, *data.CustomDescription, "", "L", false)
	}

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfMuted, false)
	pdf.MultiCell(pdfContentW, 5, fmt.Sprintf(
		"Report period: %s to %s\nGenerated: %s",
		data.TimeRangeStart.Format("January 02, 2006"),
		data.TimeRangeEnd.Format("January 02, 2006"),
		time.Now().Format("January 02, 2006 at 15:04 MST"),
	), "", "L", false)

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
	pdf.CellFormat(pdfContentW-4, 7, title, "", 1, "L", false, 0, "")
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
		{"Average uptime", fmt.Sprintf("%.2f%%", avgUptime), uptimeColor(avgUptime)},
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
		pdf.CellFormat(w-6, 4, strings.ToUpper(t.label), "", 0, "L", false, 0, "")

		pdf.SetXY(x+4, y+9.5)
		pdf.SetFont("Helvetica", "B", 15)
		setColor(pdf, t.color, false)
		pdf.CellFormat(w-6, 8, t.value, "", 0, "L", false, 0, "")
	}
	pdf.SetY(y + 24)
}

func drawPDFSLASection(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "SLA Compliance")

	withTargets := make([]ReportMetrics, 0, len(data.Metrics))
	for _, m := range data.Metrics {
		if m.SLATarget != nil {
			withTargets = append(withTargets, m)
		}
	}
	if len(withTargets) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, "No SLA targets configured for the monitored services.", "", "L", false)
		return
	}

	widths := []float64{pdfContentW - 105, 35, 35, 35}
	headers := []string{"Service", "Uptime", "SLA target", "Status"}

	pdf.SetFont("Helvetica", "B", 9)
	setColor(pdf, pdfPanel, true)
	setColor(pdf, pdfInk, false)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 8, h, "", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 9)
	for _, m := range withTargets {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 7, truncate(m.MonitorName, 46), "B", 0, "L", false, 0, "")
		setColor(pdf, uptimeColor(m.Uptime), false)
		pdf.CellFormat(widths[1], 7, fmt.Sprintf("%.2f%%", m.Uptime), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[2], 7, fmt.Sprintf("%.2f%%", *m.SLATarget), "B", 0, "L", false, 0, "")

		status, color := "Missed", pdfDanger
		if m.SLAMet {
			status, color = "Met", pdfSuccess
		}
		setColor(pdf, color, false)
		pdf.CellFormat(widths[3], 7, status, "B", 1, "L", false, 0, "")
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
			fmt.Sprintf("%s (%d)", truncate(m.MonitorName, 60), len(m.Incidents)), "", 1, "L", false, 0, "")

		for _, inc := range m.Incidents {
			y := pdf.GetY()
			setColor(pdf, pdfDanger, true)
			pdf.Rect(pdfMarginLeft, y, 1.2, 6, "F")
			pdf.SetX(pdfMarginLeft + 4)

			pdf.SetFont("Helvetica", "B", 9)
			setColor(pdf, pdfInk, false)
			pdf.CellFormat(pdfContentW-44, 6, inc.StartTime.Format("Jan 02, 2006 15:04"), "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			setColor(pdf, pdfMuted, false)
			pdf.CellFormat(40, 6, fmt.Sprintf("%s  %s", formatMinutes(inc.Duration), inc.Status), "", 1, "R", false, 0, "")

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
				pdf.MultiCell(pdfContentW-4, 4, detail[0]+": "+detail[1], "", "L", false)
			}
			pdf.Ln(1.5)
		}
	}
}

func drawPDFCustomSection(pdf *fpdf.Fpdf, data *ReportData) {
	if data.CustomDescription == nil || *data.CustomDescription == "" {
		return
	}
	drawSectionHeading(pdf, "Notes")
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfInk, false)
	pdf.MultiCell(pdfContentW, 5, *data.CustomDescription, "", "L", false)
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
		pdf.MultiCell(pdfContentW, 4.5, "- "+w, "", "L", false)
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

// sectionsOrDefault falls back to a sensible report when a template names no
// sections, so a misconfigured template still produces something useful.
func sectionsOrDefault(sections []string) []string {
	if len(sections) == 0 {
		return []string{models.SectionCharts, models.SectionSLACompliance, models.SectionIncidentSummary}
	}
	return sections
}

// formatMinutes renders a downtime duration compactly (e.g. "2h 15m").
func formatMinutes(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	h, m := minutes/60, minutes%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
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

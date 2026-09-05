// Package services - report_html_generator.go renders aggregated report data to
// a standalone HTML document, used for the in-browser view of a shared report.
//
// The PDF is drawn directly (see pdf_renderer.go) rather than converted from
// this HTML, so the two renderers are independent. They read the same
// ReportData and honour the same template sections, which is what keeps them
// showing the same report.
package services

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// HTMLReportGenerator renders ReportData as a self-contained HTML page.
type HTMLReportGenerator struct{}

// NewHTMLReportGenerator returns a ready-to-use generator.
func NewHTMLReportGenerator() *HTMLReportGenerator {
	return &HTMLReportGenerator{}
}

// GenerateHTMLReport renders data using the template's sections, in order.
// A template naming no sections falls back to the default report.
func (g *HTMLReportGenerator) GenerateHTMLReport(data *ReportData, template *models.ReportTemplate) (string, error) {
	if data == nil {
		return "", fmt.Errorf("report data is nil")
	}

	var sections []string
	if template != nil {
		sections = template.Sections
	}

	var b strings.Builder
	b.WriteString(g.header(data))
	for _, section := range sectionsOrDefault(sections) {
		switch section {
		case models.SectionSLACompliance:
			b.WriteString(g.slaSection(data))
		case models.SectionIncidentSummary:
			b.WriteString(g.incidentSection(data))
		case models.SectionCharts:
			b.WriteString(g.summarySection(data))
		case models.SectionCustom:
			b.WriteString(g.customSection(data))
		}
	}
	b.WriteString(g.warningsSection(data))
	b.WriteString(g.footer())
	return b.String(), nil
}

// esc escapes text for interpolation into HTML content.
func esc(s string) string { return html.EscapeString(s) }

func (g *HTMLReportGenerator) header(data *ReportData) string {
	title := reportTitle(data)

	description := ""
	if data.CustomDescription != nil && *data.CustomDescription != "" {
		description = fmt.Sprintf("<p class=\"lede\">%s</p>", esc(*data.CustomDescription))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;
       line-height:1.6;color:#333;background:#f9fafb}
  .container{max-width:900px;margin:0 auto;padding:40px;background:#fff}
  h1{font-size:2.2em;margin-bottom:10px;color:#1a1a1a}
  .lede{color:#666;margin-bottom:10px}
  .report-meta{color:#666;font-size:.95em;margin-bottom:30px;padding-bottom:20px;border-bottom:2px solid #eee}
  h2{font-size:1.6em;margin:30px 0 15px;color:#1a1a1a;border-left:4px solid #3b82f6;padding-left:15px}
  h3{font-size:1.1em;margin:20px 0 10px;color:#1a1a1a}
  .metric-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:20px;margin-bottom:30px}
  .metric-card{background:#f3f4f6;padding:20px;border-radius:8px;border-left:4px solid #3b82f6}
  .metric-label{font-size:.8em;color:#666;text-transform:uppercase;letter-spacing:.5px;margin-bottom:8px}
  .metric-value{font-size:1.9em;font-weight:700;color:#1a1a1a}
  .metric-value.success{color:#10b981}.metric-value.warning{color:#f59e0b}.metric-value.danger{color:#ef4444}
  table{width:100%%;border-collapse:collapse;margin-bottom:30px}
  thead{background:#f3f4f6}
  th{padding:12px;text-align:left;font-weight:600;color:#1a1a1a}
  td{padding:12px;border-bottom:1px solid #e5e7eb}
  .incident-row{margin-bottom:15px;padding:15px;background:#f9fafb;border-left:4px solid #ef4444;border-radius:4px}
  .incident-header{display:flex;justify-content:space-between;margin-bottom:8px}
  .incident-title{font-weight:600;color:#1a1a1a}
  .incident-duration{font-size:.9em;color:#666}
  .incident-detail{font-size:.9em;margin-top:8px;color:#666}
  .sla-badge{display:inline-block;padding:4px 8px;border-radius:4px;font-size:.85em;font-weight:600}
  .sla-badge.met{background:#d1fae5;color:#065f46}
  .sla-badge.not-met{background:#fee2e2;color:#991b1b}
  .warnings{background:#fffbeb;border-left:4px solid #f59e0b;padding:15px;border-radius:4px;margin-top:20px}
  .footer{margin-top:40px;padding-top:20px;border-top:2px solid #eee;color:#666;font-size:.85em;text-align:center}
  @media print{body{background:#fff}.container{padding:0}}
</style>
</head>
<body>
<div class="container">
<h1>%s</h1>
%s
<div class="report-meta">
  Report period: <strong>%s</strong> to <strong>%s</strong><br>
  Generated: <strong>%s</strong>
</div>
`,
		esc(title),
		esc(title),
		description,
		esc(data.TimeRangeStart.Format("January 02, 2006")),
		esc(data.TimeRangeEnd.Format("January 02, 2006")),
		esc(time.Now().Format("January 02, 2006 at 15:04 MST")),
	)
}

func (g *HTMLReportGenerator) summarySection(data *ReportData) string {
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

	uptimeClass := "success"
	switch {
	case avgUptime < 95:
		uptimeClass = "danger"
	case avgUptime < 99:
		uptimeClass = "warning"
	}

	return fmt.Sprintf(`<h2>Summary</h2>
<div class="metric-grid">
  <div class="metric-card"><div class="metric-label">Services monitored</div><div class="metric-value">%d</div></div>
  <div class="metric-card"><div class="metric-label">Average uptime</div><div class="metric-value %s">%.2f%%</div></div>
  <div class="metric-card"><div class="metric-label">Total incidents</div><div class="metric-value">%d</div></div>
  <div class="metric-card"><div class="metric-label">Healthy services</div><div class="metric-value success">%d</div></div>
</div>
`, total, uptimeClass, avgUptime, incidents, healthy)
}

func (g *HTMLReportGenerator) slaSection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>SLA Compliance</h2>\n")

	rows := 0
	var body strings.Builder
	for _, m := range data.Metrics {
		if m.SLATarget == nil {
			continue
		}
		rows++
		status, statusClass := "Missed", "not-met"
		if m.SLAMet {
			status, statusClass = "Met", "met"
		}
		fmt.Fprintf(&body,
			"<tr><td>%s</td><td>%.2f%%</td><td>%.2f%%</td><td><span class=\"sla-badge %s\">%s</span></td></tr>\n",
			esc(m.MonitorName), m.Uptime, *m.SLATarget, statusClass, status)
	}

	if rows == 0 {
		b.WriteString("<p>No SLA targets configured for the monitored services.</p>\n")
		return b.String()
	}

	b.WriteString("<table><thead><tr><th>Service</th><th>Uptime</th><th>SLA target</th><th>Status</th></tr></thead><tbody>\n")
	b.WriteString(body.String())
	b.WriteString("</tbody></table>\n")
	return b.String()
}

func (g *HTMLReportGenerator) incidentSection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>Incidents</h2>\n")

	total := 0
	for _, m := range data.Metrics {
		total += m.IncidentCount
	}
	if total == 0 {
		b.WriteString("<p>No incidents recorded during this period.</p>\n")
		return b.String()
	}

	fmt.Fprintf(&b, "<p><strong>Total incidents: %d</strong></p>\n", total)

	for _, m := range data.Metrics {
		if len(m.Incidents) == 0 {
			continue
		}
		fmt.Fprintf(&b, "<h3>%s (%d)</h3>\n", esc(m.MonitorName), len(m.Incidents))

		for _, inc := range m.Incidents {
			fmt.Fprintf(&b, `<div class="incident-row">
  <div class="incident-header">
    <span class="incident-title">Incident on %s</span>
    <span class="incident-duration">%s downtime &middot; %s</span>
  </div>
`, esc(inc.StartTime.Format("Jan 02, 2006 15:04")), esc(formatMinutes(inc.Duration)), esc(inc.Status))

			if inc.RootCause != "" {
				fmt.Fprintf(&b, "  <div class=\"incident-detail\"><strong>Root cause:</strong> %s</div>\n", esc(inc.RootCause))
			}
			if inc.ResolutionNotes != "" {
				fmt.Fprintf(&b, "  <div class=\"incident-detail\"><strong>Resolution:</strong> %s</div>\n", esc(inc.ResolutionNotes))
			}
			b.WriteString("</div>\n")
		}
	}
	return b.String()
}

func (g *HTMLReportGenerator) customSection(data *ReportData) string {
	if data.CustomDescription == nil || *data.CustomDescription == "" {
		return ""
	}
	return fmt.Sprintf("<h2>Notes</h2>\n<p>%s</p>\n", esc(*data.CustomDescription))
}

// warningsSection states any monitor the aggregator could not include, so an
// omission is visible on the report rather than inferred from its absence.
func (g *HTMLReportGenerator) warningsSection(data *ReportData) string {
	if len(data.Warnings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<div class=\"warnings\"><strong>Data warnings</strong><ul>\n")
	for _, w := range data.Warnings {
		fmt.Fprintf(&b, "<li>%s</li>\n", esc(w))
	}
	b.WriteString("</ul></div>\n")
	return b.String()
}

func (g *HTMLReportGenerator) footer() string {
	return `</div>
<div class="footer"><p>This report was generated automatically by Sentinel.</p></div>
</body>
</html>
`
}

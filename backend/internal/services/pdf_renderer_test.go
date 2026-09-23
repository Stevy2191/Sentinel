package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func f64(v float64) *float64 { return &v }
func sptr(s string) *string  { return &s }

func sampleReportData() *ReportData {
	end := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -30)
	incEnd := start.Add(50 * time.Hour)

	return &ReportData{
		ReportName:        "Monthly SLA",
		CustomTitle:       sptr("Production Health"),
		CustomDescription: sptr("Coverage for the production estate."),
		TimeRangeStart:    start,
		TimeRangeEnd:      end,
		Metrics: []ReportMetrics{
			{
				MonitorID: uuid.New(), MonitorName: "api.example.com",
				Uptime: 99.95, DowntimeMinutes: 21, IncidentCount: 1,
				SLATarget: f64(99.9), SLAMet: true,
				Incidents: []IncidentSummary{{
					ID: uuid.New(), StartTime: start.Add(48 * time.Hour), EndTime: &incEnd,
					Duration: 120, Severity: "high", Status: "resolved",
					RootCause: "Upstream provider outage", ResolutionNotes: "Failed over to the secondary region.",
				}},
			},
			{
				MonitorID: uuid.New(), MonitorName: "db.example.com",
				Uptime: 97.10, IncidentCount: 0, SLATarget: f64(99.5), SLAMet: false,
			},
		},
		Warnings: []string{"monitor \"legacy\" omitted: loading incidents: timeout"},
	}
}

// A generated report must actually be a readable PDF on disk, not merely a call
// that returned no error.
func TestRenderReportToPDFWritesAValidFile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPDFRendererService(dir)
	if err != nil {
		t.Fatalf("NewPDFRendererService: %v", err)
	}

	name, err := r.RenderReportToPDF(sampleReportData(), models.ReportTypeUptime, "monthly")
	if err != nil {
		t.Fatalf("RenderReportToPDF: %v", err)
	}
	if filepath.Base(name) != name {
		t.Errorf("returned name %q should be a bare file name", name)
	}

	path := filepath.Join(dir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated PDF: %v", err)
	}
	if !strings.HasPrefix(string(content), "%PDF-") {
		t.Errorf("file does not start with the PDF magic bytes: %q", content[:min(8, len(content))])
	}
	if len(content) < 1000 {
		t.Errorf("PDF is suspiciously small (%d bytes) - sections may not have rendered", len(content))
	}

	size, err := r.GetPDFFileSize(name)
	if err != nil {
		t.Fatalf("GetPDFFileSize: %v", err)
	}
	if size != len(content) {
		t.Errorf("GetPDFFileSize = %d, want %d", size, len(content))
	}
}

// Each report type must produce a genuinely different document, or the type
// selection is a lie.
func TestRenderReportToPDFHonoursReportType(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewPDFRendererService(dir)
	data := sampleReportData()

	render := func(reportType, hint string) string {
		name, err := r.RenderReportToPDF(data, reportType, hint)
		if err != nil {
			t.Fatalf("render %v: %v", reportType, err)
		}
		path, err := r.GetPDFPath(name)
		if err != nil {
			t.Fatalf("path: %v", err)
		}
		return pdfDrawnText(t, path)
	}

	uptime := render(models.ReportTypeUptime, "uptime")
	if !strings.Contains(uptime, "SLA Compliance") {
		t.Error("uptime report is missing the SLA Compliance section")
	}
	if strings.Contains(uptime, "Root cause") {
		t.Error("uptime report should not include incident detail")
	}

	incident := render(models.ReportTypeIncident, "incident")
	if !strings.Contains(incident, "Upstream provider outage") {
		t.Error("incident report is missing incident detail")
	}
	if strings.Contains(incident, "SLA Compliance") {
		t.Error("incident report should not include the SLA Compliance section")
	}
}

// A stored file name is untrusted input by the time it reaches the filesystem.
func TestGetPDFPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewPDFRendererService(dir)

	for _, bad := range []string{
		"../../etc/passwd",
		"/etc/passwd",
		"sub/dir/report.pdf",
		"..",
		"",
	} {
		if _, err := r.GetPDFPath(bad); err == nil {
			t.Errorf("GetPDFPath(%q) should have been rejected", bad)
		}
	}

	if got, err := r.GetPDFPath("123_report.pdf"); err != nil {
		t.Errorf("a plain file name should be accepted: %v", err)
	} else if got != filepath.Join(dir, "123_report.pdf") {
		t.Errorf("got %q", got)
	}
}

func TestPDFFilenameIsSanitized(t *testing.T) {
	name := pdfFilename("../../evil name/with slashes")
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		t.Errorf("generated name %q still contains path characters", name)
	}
	if !strings.HasSuffix(name, ".pdf") {
		t.Errorf("generated name %q should end in .pdf", name)
	}
}

func TestFormatMinutes(t *testing.T) {
	cases := map[float64]string{
		0: "0s", 45: "45m", 60: "1h", 90: "1h 30m", 125: "2h 5m",
		// Sub-minute outages read as seconds rather than "0m": a 30s check
		// interval produces them routinely, and "0m" made them look like
		// nothing happened.
		0.5: "30s", 0.75: "45s", 0.0166: "1s",
	}
	for in, want := range cases {
		if got := formatMinutes(in); got != want {
			t.Errorf("formatMinutes(%v) = %q, want %q", in, got, want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

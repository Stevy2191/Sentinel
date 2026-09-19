package services

import (
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// A report rendered with no zone configured must say UTC, never the server
// process's zone — the container's default is not a choice anyone made.
func TestReportData_ReportLocationDefaultsToUTC(t *testing.T) {
	var nilData *ReportData
	if nilData.ReportLocation() != time.UTC {
		t.Error("nil ReportData should report UTC")
	}
	if (&ReportData{}).ReportLocation() != time.UTC {
		t.Error("unset Location should report UTC")
	}
}

func TestHTMLReport_RendersInConfiguredZone(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// A window whose UTC and Chicago dates differ, so a zone that was ignored
	// shows up as the wrong date rather than only the wrong hour.
	start := time.Date(2026, 9, 19, 2, 30, 0, 0, time.UTC)
	data := &ReportData{
		ReportName:     "Zone probe",
		TimeRangeStart: start,
		TimeRangeEnd:   start.Add(24 * time.Hour),
		Location:       chicago,
	}

	tmpl := &models.ReportTemplate{Name: "SLA", Sections: models.StringSlice{models.SectionSLACompliance}}
	html, err := NewHTMLReportGenerator().GenerateHTMLReport(data, tmpl)
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}
	if !strings.Contains(html, "September 18, 2026") {
		t.Errorf("period start not rendered in America/Chicago (expected Sep 18):\n%s", firstLines(html, 12))
	}
	if strings.Contains(html, "UTC") {
		t.Errorf("report still mentions UTC despite a configured zone:\n%s", firstLines(html, 12))
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

package services

import (
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestGenerateHTMLReportHonoursSections(t *testing.T) {
	g := NewHTMLReportGenerator()
	data := sampleReportData()

	slaOnly, err := g.GenerateHTMLReport(data, &models.ReportTemplate{
		Sections: models.StringSlice{models.SectionSLACompliance},
	})
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}
	if !strings.Contains(slaOnly, "SLA Compliance") {
		t.Error("SLA section missing")
	}
	if strings.Contains(slaOnly, "<h2>Incidents</h2>") {
		t.Error("incident section rendered despite not being selected")
	}

	both, err := g.GenerateHTMLReport(data, &models.ReportTemplate{
		Sections: models.StringSlice{models.SectionSLACompliance, models.SectionIncidentSummary},
	})
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}
	if !strings.Contains(both, "<h2>Incidents</h2>") {
		t.Error("incident section missing when selected")
	}
}

// A monitor name is operator-supplied and lands in the report body.
func TestGenerateHTMLReportEscapesUserContent(t *testing.T) {
	data := sampleReportData()
	data.Metrics[0].MonitorName = `<script>alert("xss")</script>`
	data.Metrics[0].Incidents[0].RootCause = `<img src=x onerror=alert(1)>`
	data.CustomTitle = sptr(`<b>title</b>`)

	out, err := NewHTMLReportGenerator().GenerateHTMLReport(data, &models.ReportTemplate{
		Sections: models.StringSlice{models.SectionSLACompliance, models.SectionIncidentSummary},
	})
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}

	// What matters is that no attacker-supplied tag survives as markup. The
	// angle brackets are the payload; inert substrings like "onerror=" remain as
	// plain text once the brackets are escaped, which is the correct outcome.
	for _, injected := range []string{"<script>", "</script>", "<img ", "<b>title</b>"} {
		if strings.Contains(out, injected) {
			t.Errorf("unescaped markup %q made it into the document", injected)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;", "&lt;img ", "&lt;b&gt;title"} {
		if !strings.Contains(out, escaped) {
			t.Errorf("expected %q to appear escaped in the document", escaped)
		}
	}
}

func TestGenerateHTMLReportEmptyStates(t *testing.T) {
	g := NewHTMLReportGenerator()
	empty := &ReportData{ReportName: "Empty", TimeRangeStart: sampleReportData().TimeRangeStart, TimeRangeEnd: sampleReportData().TimeRangeEnd}

	out, err := g.GenerateHTMLReport(empty, nil)
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}
	if !strings.Contains(out, "No SLA targets configured") {
		t.Error("expected the empty SLA state")
	}
	if !strings.Contains(out, "No incidents recorded") {
		t.Error("expected the empty incident state")
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "</html>") {
		t.Error("document is not closed")
	}
}

// Warnings must be visible on the report itself, not just in the API payload.
func TestGenerateHTMLReportShowsWarnings(t *testing.T) {
	out, err := NewHTMLReportGenerator().GenerateHTMLReport(sampleReportData(), nil)
	if err != nil {
		t.Fatalf("GenerateHTMLReport: %v", err)
	}
	if !strings.Contains(out, "Data warnings") || !strings.Contains(out, "legacy") {
		t.Error("aggregation warnings are not surfaced in the rendered report")
	}
}

package services

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The attachment is the whole point of scheduled delivery, and the original
// design declared one but never encoded it. This asserts it is actually in the
// message and decodes back to the file's bytes.
func TestBuildMIMEAttachesThePDF(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "report.pdf")
	want := []byte("%PDF-1.3\nfake report bytes\n%%EOF")
	if err := os.WriteFile(pdfPath, want, 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	m := NewReportMailer(nil, staticBaseURL("https://sentinel.example.com"))
	msg, err := m.buildMIME("sentinel@example.com", ReportEmail{
		To:             []string{"ops@example.com"},
		ReportName:     "Weekly Report",
		AttachmentPath: pdfPath,
	})
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}

	if !strings.Contains(msg, "Content-Type: multipart/mixed") {
		t.Error("a message with an attachment must be multipart/mixed")
	}
	if !strings.Contains(msg, "Content-Type: application/pdf") {
		t.Error("attachment part is missing")
	}
	if !strings.Contains(msg, `filename="Weekly Report.pdf"`) {
		t.Errorf("attachment filename missing or wrong:\n%s", headOf(msg, 800))
	}
	if !strings.Contains(msg, "Content-Transfer-Encoding: 8bit") {
		t.Error("text/plain and text/html parts must have Content-Transfer-Encoding: 8bit")
	}

	// Recover the base64 payload and confirm it round-trips to the real bytes.
	idx := strings.Index(msg, "Content-Transfer-Encoding: base64")
	if idx < 0 {
		t.Fatal("no base64 part found")
	}
	rest := msg[idx:]
	start := strings.Index(rest, "\r\n\r\n")
	end := strings.Index(rest, "\r\n--sentinel-mixed")
	if start < 0 || end < 0 {
		t.Fatal("could not delimit the attachment body")
	}
	payload := strings.ReplaceAll(rest[start+4:end], "\r\n", "")
	got, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("attachment is not valid base64: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("attachment bytes differ:\ngot  %q\nwant %q", got, want)
	}
}

func TestBuildMIMEWithoutAttachment(t *testing.T) {
	m := NewReportMailer(nil, nil)
	msg, err := m.buildMIME("sentinel@example.com", ReportEmail{
		To: []string{"ops@example.com"}, ReportName: "No Attachment",
	})
	if err != nil {
		t.Fatalf("buildMIME: %v", err)
	}
	if strings.Contains(msg, "multipart/mixed") {
		t.Error("no attachment should mean no multipart/mixed wrapper")
	}
	if !strings.Contains(msg, "multipart/alternative") {
		t.Error("expected a text+html alternative body")
	}
	if !strings.Contains(msg, "Content-Type: text/plain") || !strings.Contains(msg, "Content-Type: text/html") {
		t.Error("both body parts should be present")
	}
}

func TestBuildMIMEMissingAttachmentIsAnError(t *testing.T) {
	m := NewReportMailer(nil, nil)
	_, err := m.buildMIME("s@example.com", ReportEmail{
		To: []string{"ops@example.com"}, ReportName: "Gone",
		AttachmentPath: filepath.Join(t.TempDir(), "nope.pdf"),
	})
	if err == nil {
		t.Error("a missing attachment file should fail rather than send a silently empty message")
	}
}

// A report name reaches a mail header, so it must not be able to break out of
// the quoted filename or inject a header line.
func TestAttachmentNameIsSafe(t *testing.T) {
	cases := []string{
		`evil"; name="x`,
		"line\r\nInjected-Header: yes",
		"../../etc/passwd",
		"",
	}
	for _, in := range cases {
		got := attachmentName(in)
		if strings.ContainsAny(got, "\"\r\n/\\") {
			t.Errorf("attachmentName(%q) = %q, still contains dangerous characters", in, got)
		}
		if !strings.HasSuffix(got, ".pdf") {
			t.Errorf("attachmentName(%q) = %q, should end in .pdf", in, got)
		}
	}
}

func TestSummaryLines(t *testing.T) {
	lines := SummaryLines(sampleReportData())
	if len(lines) == 0 {
		t.Fatal("expected summary lines")
	}
	if !strings.Contains(lines[0], "2 services") {
		t.Errorf("first line should count the services, got %q", lines[0])
	}
	// db.example.com is below its SLA in the fixture and should be called out.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "db.example.com") {
		t.Errorf("an SLA miss should be named in the summary:\n%s", joined)
	}

	t.Run("empty report", func(t *testing.T) {
		got := SummaryLines(&ReportData{})
		if len(got) != 1 || !strings.Contains(got[0], "No monitors") {
			t.Errorf("expected an empty-state line, got %v", got)
		}
	})
}

func headOf(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// staticBaseURL returns a resolver that always yields the same URL, for tests
// that do not care about runtime reconfiguration.
func staticBaseURL(u string) BaseURLFunc {
	return func() string { return u }
}

func TestSummaryLinesMetrics(t *testing.T) {
	traffic := MetricsTile{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 640e6, Second: f64(2.1e12), Change: f64(18)}
	cases := []struct {
		name string
		data MetricsReportData
		want []string
	}{
		{"traffic", MetricsReportData{Ports: 12, Tiles: []MetricsTile{traffic}, Tables: []MetricsTable{{Billable: true}}},
			[]string{"12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"traffic first even when listed second", MetricsReportData{Ports: 1, Tiles: []MetricsTile{
			{Label: "Busy", Unit: "%", Kind: "percent", First: 12.34}, traffic}, Tables: []MetricsTable{{}, {Billable: true}}},
			[]string{"1 port · billable 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"traffic new, no volume", MetricsReportData{Ports: 2, Tiles: []MetricsTile{
			{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 1.25e9, New: true}}, Tables: []MetricsTable{{Billable: true}}},
			[]string{"2 ports · billable 95th 1.25 Gbps (new)"}},
		{"single traffic metric (non-billable)", MetricsReportData{Ports: 12, Tiles: []MetricsTile{traffic}, Tables: []MetricsTable{{Title: "Traffic in", Unit: "bps"}}},
			[]string{"12 ports · 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"no traffic: the first metric's average", MetricsReportData{Devices: 3, Tiles: []MetricsTile{
			{Label: "CPU", Unit: "%", Kind: "percent", NoData: true},
			{Label: "Memory used", Unit: "%", Kind: "percent", First: 23, Change: f64(-3.46)}}},
			[]string{"3 devices · Memory used average 23% (-3.5%)"}},
		{"devices scope", MetricsReportData{ScopeType: models.ScopeTypeDevices, Devices: 3, Ports: 40, Tiles: []MetricsTile{traffic}, Tables: []MetricsTable{{Billable: true}}},
			[]string{"3 devices · billable 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"running hot", MetricsReportData{Ports: 12, Tiles: []MetricsTile{traffic}, Tables: []MetricsTable{{Billable: true}}, RunningHot: []HotPort{{}, {}}},
			[]string{"12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved", "2 ports running hot (95th busy at or above 80%)"}},
		{"no data", MetricsReportData{Ports: 3, NoData: true}, []string{"3 ports · No data for this period"}},
		{"empty", MetricsReportData{Empty: true}, []string{metricsEmptyMessage}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := c.data
			got := SummaryLines(&ReportData{Network: &data})
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

// Uptime and Incident reports keep the monitor summary, word for word.
func TestSummaryLinesMonitorReportsUnchanged(t *testing.T) {
	got := SummaryLines(&ReportData{Metrics: []ReportMetrics{
		{MonitorName: "api", Uptime: 99, IncidentCount: 1, SLATarget: f64(99.5), SLAMet: false},
		{MonitorName: "web", Uptime: 97, IncidentCount: 0, SLATarget: f64(95), SLAMet: true},
	}})
	want := []string{"2 services, 98.00% average uptime, 1 incidents", "api missed its 99.50% SLA at 99.00%"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

package services

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	m := NewReportMailer(nil, "https://sentinel.example.com")
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
	m := NewReportMailer(nil, "")
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
	m := NewReportMailer(nil, "")
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

package services

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// pdfDrawnText returns every string the PDF actually draws, decompressing the
// content streams. Reading the file as-is would find nothing: fpdf compresses.
func pdfDrawnText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`).FindAllSubmatch(raw, -1) {
		chunk := m[1]
		if zr, err := zlib.NewReader(bytes.NewReader(chunk)); err == nil {
			if inflated, err := io.ReadAll(zr); err == nil {
				chunk = inflated
			}
			zr.Close()
		}
		for _, tj := range regexp.MustCompile(`\((.*?)\)\s*Tj`).FindAllSubmatch(chunk, -1) {
			out = append(out, string(tj[1]))
		}
	}
	return strings.Join(out, "\n")
}

func renderProbe(t *testing.T, data *ReportData) string {
	t.Helper()
	dir := t.TempDir()
	r, err := NewPDFRendererService(dir)
	if err != nil {
		t.Fatal(err)
	}
	name, err := r.RenderReportToPDF(data, models.ReportTypeUptime, "probe")
	if err != nil {
		t.Fatal(err)
	}
	path, err := r.GetPDFPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return pdfDrawnText(t, path)
}

// The reported bug: a middle dot in the description rendered as "Â·" because
// its UTF-8 bytes went into a Latin-1 encoded page unchanged.
func TestPDF_MiddleDotIsNotMojibake(t *testing.T) {
	desc := "Last 30 days · https://example.com"
	drawn := renderProbe(t, &ReportData{
		ReportName:        "Encoding probe",
		CustomDescription: &desc,
		TimeRangeStart:    time.Now().Add(-24 * time.Hour),
		TimeRangeEnd:      time.Now(),
	})

	if strings.Contains(drawn, "Â") {
		t.Errorf("mojibake: page contains 0xC2 (renders as Â)\n%s", drawn)
	}
	// cp1252 puts the middle dot at 0xB7, the same byte the font draws it from.
	if !strings.Contains(drawn, "\xb7") {
		t.Errorf("middle dot missing entirely; expected byte 0xB7\n%s", drawn)
	}
}

// Monitor names are user data and reach the page directly.
func TestPDF_AccentedMonitorNameSurvives(t *testing.T) {
	slaTarget := 99.5
	drawn := renderProbe(t, &ReportData{
		ReportName:     "Accents",
		TimeRangeStart: time.Now().Add(-24 * time.Hour),
		TimeRangeEnd:   time.Now(),
		Metrics: []ReportMetrics{{
			MonitorName: "Café Müller",
			Uptime:      99.9,
			SLATarget:   &slaTarget,
		}},
	})
	if strings.Contains(drawn, "Â") || strings.Contains(drawn, "Ã") {
		t.Errorf("accented name rendered as mojibake:\n%s", drawn)
	}
	// é and ü are 0xE9 and 0xFC in cp1252.
	if !strings.Contains(drawn, "\xe9") || !strings.Contains(drawn, "\xfc") {
		t.Errorf("accented characters lost:\n%s", drawn)
	}
}

func TestPDFText(t *testing.T) {
	// ASCII is untouched.
	if got := pdfText("plain ascii"); got != "plain ascii" {
		t.Errorf("ASCII changed: %q", got)
	}
	// Mapped into the single-byte range rather than left as UTF-8.
	for _, in := range []string{"·", "é", "…", "’"} {
		got := pdfText(in)
		if strings.Contains(got, "Â") || strings.Contains(got, "â") {
			t.Errorf("pdfText(%q) = %q, still mojibake", in, got)
		}
		if len(got) != 1 {
			t.Errorf("pdfText(%q) = %q (%d bytes), want a single byte", in, got, len(got))
		}
	}
	// A non-breaking space becomes a real space rather than vanishing.
	if got := pdfText("a b"); got != "a b" {
		t.Errorf("pdfText(nbsp) = %q, want %q", got, "a b")
	}
	// Undrawable characters are dropped, not emitted as raw bytes.
	got := pdfText("ok 日本語")
	for i := 0; i < len(got); i++ {
		if got[i] > 0x7F {
			t.Errorf("CJK left a high byte in %q", got)
			break
		}
	}
}

package services

import (
	"bytes"
	"flag"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the golden files under testdata/")

// chartTestDoc is an uncompressed A4 page with fixed dates, so its bytes are
// the same on every run.
func chartTestDoc() *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetCompression(false)
	fixed := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pdf.SetCreationDate(fixed)
	pdf.SetModificationDate(fixed)
	pdf.AddPage()
	return pdf
}

// pageStreams returns the drawing operators of every page of an uncompressed document.
func pageStreams(t *testing.T, pdf *fpdf.Fpdf) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, m := range regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`).FindAllSubmatch(buf.Bytes(), -1) {
		out = append(out, m[1]...)
	}
	return out
}

// The uptime graph moved onto the shared line-chart helper; its page must
// come out byte for byte as it did before the move.
func TestUptimeGraphRendersAsBefore(t *testing.T) {
	pdf := chartTestDoc()
	data := sampleReportData()
	drawPDFUptimeGraph(pdf, data.UptimeSeries, data.EffectiveSLA, time.UTC)
	got := pageStreams(t, pdf)

	golden := filepath.Join("testdata", "uptime_graph.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading %s (run once with -update-golden on the code before the move): %v", golden, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("uptime graph drawing changed:\n got %d bytes\nwant %d bytes", len(got), len(want))
	}
}

func TestDrawLineChartDrawsTicksLinesAndReference(t *testing.T) {
	pdf := chartTestDoc()
	y0 := pdf.GetY()
	drawLineChart(pdf, lineChart{
		Height: 40, AxisLabelW: 18, Lo: 0, Hi: 1e9, TopPad: 3, Ticks: 4,
		TickLabel: func(v float64) string { return formatValue(v, "bps") },
		Ref:       &chartRef{Value: 640e6, Label: "Billable 95th 640 Mbps", Color: pdfWarning},
		Lines: []chartLine{
			{X: []float64{0, 0.5, 1}, Y: []float64{100e6, 640e6, 200e6}, Color: pdfAccent, Width: 0.5},
			{X: []float64{0, 1}, Y: []float64{50e6, 300e6}, Color: pdfSuccess, Width: 0.5},
		},
		XLabels:      []chartXLabel{{At: 0, Text: "Sep 1", Align: "L"}, {At: 1, Text: "Oct 1", Align: "R"}},
		LabelsInside: true,
	})
	if got, want := pdf.GetY(), y0+40+9; got != want {
		t.Errorf("Y after the chart = %.2f, want %.2f (height + 9 mm of labels)", got, want)
	}
	content := string(pageStreams(t, pdf))
	for _, want := range []string{"(0 bps)Tj", "(250 Mbps)Tj", "(500 Mbps)Tj", "(750 Mbps)Tj", "(1 Gbps)Tj",
		"(Billable 95th 640 Mbps)Tj", "(Sep 1)Tj", "(Oct 1)Tj"} {
		if !strings.Contains(content, want) {
			t.Errorf("chart is missing %s", want)
		}
	}
	// Two segments for the three-point line, one for the two-point line.
	if got := strings.Count(content, " l S"); got != 3 {
		t.Errorf("drew %d line segments, want 3", got)
	}
}

func TestNiceCeil(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 1}, {-5, 1}, {0.37, 0.5}, {1, 1}, {7, 10}, {92.5, 100}, {130, 200}, {240, 250}, {640e6, 1e9}, {1.2e9, 2e9},
	}
	for _, c := range cases {
		if got := niceCeil(c.in); math.Abs(got-c.want) > 1e-9*c.want {
			t.Errorf("niceCeil(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

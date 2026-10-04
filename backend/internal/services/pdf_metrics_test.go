package services

import (
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// sampleMetricsData is a hand-built Metrics report for September 2026 (UTC),
// compared with August: three ports, a traffic pair, a busy pair and a UPS's
// on-battery metric.
func sampleMetricsData() *ReportData {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	core, edge2, edge3 := "core-sw1 · Gi1/0/1 (uplink to annex)", "edge-sw2 · Gi0/48", "edge-sw3 · Gi0/1"
	pts := func(vs ...float64) []ChartPoint {
		out := make([]ChartPoint, len(vs))
		for i, v := range vs {
			out[i] = ChartPoint{T: start.Add(time.Duration(i) * 10 * 24 * time.Hour), V: v}
		}
		return out
	}
	return &ReportData{
		ReportName:     "WAN monthly",
		TimeRangeStart: start,
		TimeRangeEnd:   end,
		Location:       time.UTC,
		Network: &MetricsReportData{
			ScopeType:  models.ScopeTypePorts,
			ScopeLabel: "WAN, Uplink ports at HQ, Annex",
			PrevStart:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			PrevEnd:    start,
			RankLabel:  "Traffic",
			Tiles: []MetricsTile{
				{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 640e6, Second: f64(2.1e12), Change: f64(18)},
				{Label: "Busy", Unit: "%", Kind: "percent", First: 12.34, Second: f64(40), New: true},
				{Label: "Errors", Unit: "per_min", Kind: "other", First: 1.5, Change: f64(-25)},
				{Label: "Battery charge", Unit: "%", Kind: "percent", NoData: true},
			},
			Tables: []MetricsTable{
				{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true, Rows: []MetricsRow{
					{Name: core, In: &RowStats{Avg: 200e6, Peak: 900e6, P95: 640e6, Total: f64(1.2e12)},
						Out:      &RowStats{Avg: 100e6, Peak: 500e6, P95: 300e6, Total: f64(0.9e12)},
						Billable: f64(640e6), Change: f64(18), Coverage: 100},
					{Name: edge2, In: &RowStats{Avg: 40e6, Peak: 150e6, P95: 120e6, Total: f64(9e10)},
						Out:      &RowStats{Avg: 20e6, Peak: 90e6, P95: 60e6, Total: f64(4e10)},
						Billable: f64(120e6), New: true, Coverage: 72, LowCoverage: true},
					{Name: edge3, NoData: true},
				}},
				{Title: "Busy", Unit: "%", Paired: true, Rows: []MetricsRow{
					{Name: core, In: &RowStats{Avg: 20, Peak: 99, P95: 92.5}, Out: &RowStats{Avg: 10, Peak: 50, P95: 41},
						Change: f64(5), Coverage: 100},
				}},
				{Title: "On battery", Unit: "bool", Rows: []MetricsRow{
					{Name: "ups-1", In: &RowStats{Avg: 0.0030787, Peak: 1, P95: 0, TrueSeconds: f64(7980)}, Coverage: 100},
				}},
			},
			Busiest:    []string{core, edge2},
			RunningHot: []HotPort{{Name: core, P95In: 92.5, P95Out: 41}},
			Charts: []MetricsChart{
				{Title: "Traffic", Unit: "bps", Reference: f64(640e6), Lines: []ChartLine{
					{Label: "In", Points: pts(100e6, 640e6, 300e6, 200e6)}, {Label: "Out", Points: pts(50e6, 300e6, 100e6, 80e6)}}},
				{Title: "Busy", Unit: "%", Reference: f64(92.5), Lines: []ChartLine{
					{Label: "In", Points: pts(10, 92.5, 30, 20)}, {Label: "Out", Points: pts(5, 41, 12, 9)}}},
				{Title: "On battery", Unit: "bool", Reference: f64(0), Lines: []ChartLine{
					{Label: "On battery", Points: pts(0, 0, 1, 0)}}},
			},
			RowCharts: []MetricsChart{
				{Title: core, Unit: "bps", Lines: []ChartLine{
					{Label: "In", Points: pts(100e6, 640e6, 300e6, 200e6)}, {Label: "Out", Points: pts(50e6, 300e6, 100e6, 80e6)}}},
			},
			Rows: 3, Ports: 3, CappedOut: 140, Unavailable: 2, Skipped: []string{"old_metric"}, LowCoverage: 1,
		},
	}
}

// hasLine reports whether text, as pdfDrawnText returns it, has a drawn
// string equal to line.
func hasLine(text, line string) bool {
	return strings.Contains("\n"+text+"\n", "\n"+line+"\n")
}

// renderMetrics renders data as a Metrics report and returns the drawn text
// and the file's path.
func renderMetrics(t *testing.T, data *ReportData) (string, string) {
	t.Helper()
	r, err := NewPDFRendererService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := r.RenderReportToPDF(data, models.ReportTypeMetrics, "metrics")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	path, err := r.GetPDFPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return pdfDrawnText(t, path), path
}

func TestMetricsPDFHeadline(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	for _, want := range []string{
		"WAN, Uplink ports at HQ, Annex",
		"September 2026, compared with August 2026",
		// Traffic tile: billable 95th, volume, change.
		"TRAFFIC", "640 Mbps", "billable 95th, 2.1 TB moved", "+18% vs August 2026",
		// Percent tile: average, 95th.
		"12.3%", "average, 95th 40%",
		// Other tile: average in its unit.
		"1.5/min", "-25% vs August 2026",
		"Running hot", "in 92.5%, out 41%",
		"Busiest 2 by Traffic", "edge-sw2",
		"1 row with data for less than 90% of the period, marked in the tables.",
		"Left out 140 ports beyond the 500-port limit; the report includes the 500 busiest.",
		"Left out 2 chosen ports, devices or sites that are no longer available to you.",
		"Left out metrics that no longer exist: old_metric.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("headline is missing %q", want)
		}
	}
	// The percent tile is new this period; the battery tile has no data.
	for _, want := range []string{"new", "No data"} {
		if !hasLine(text, want) {
			t.Errorf("headline is missing a tile reading %q", want)
		}
	}
}

func TestMetricsPDFChartsCarryTheReferenceLine(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	for _, want := range []string{"Charts", "Billable 95th 640 Mbps", "95th 92.5%", "95th 0%", "1 Gbps", "Sep 1", "Oct 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("charts are missing %q", want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	d := func(y int, m time.Month, day, h, mi int, loc *time.Location) time.Time {
		return time.Date(y, m, day, h, mi, 0, 0, loc)
	}
	cases := []struct {
		start, end time.Time
		loc        *time.Location
		want       string
	}{
		{d(2026, 9, 1, 0, 0, time.UTC), d(2026, 10, 1, 0, 0, time.UTC), time.UTC, "September 2026"},
		// November 2026 in Chicago is 721 hours long (DST ends on the 1st).
		{d(2026, 11, 1, 0, 0, chicago), d(2026, 12, 1, 0, 0, chicago), chicago, "November 2026"},
		{d(2026, 7, 1, 0, 0, time.UTC), d(2026, 10, 1, 0, 0, time.UTC), time.UTC, "Q3 2026"},
		{d(2026, 1, 1, 0, 0, time.UTC), d(2027, 1, 1, 0, 0, time.UTC), time.UTC, "2026"},
		{d(2026, 9, 7, 0, 0, time.UTC), d(2026, 9, 14, 0, 0, time.UTC), time.UTC, "Week of September 7, 2026"},
		{d(2026, 9, 3, 0, 0, time.UTC), d(2026, 10, 3, 0, 0, time.UTC), time.UTC, "September 3, 2026 to October 3, 2026"},
		{d(2026, 9, 3, 14, 5, time.UTC), d(2026, 10, 3, 14, 5, time.UTC), time.UTC, "Sep 3, 2026 14:05 to Oct 3, 2026 14:05"},
		// The same instants, read in Chicago.
		{d(2026, 9, 1, 5, 0, time.UTC), d(2026, 10, 1, 5, 0, time.UTC), chicago, "September 2026"},
	}
	for _, c := range cases {
		if got := windowLabel(c.start, c.end, c.loc); got != c.want {
			t.Errorf("windowLabel(%v, %v) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}

func TestFitText(t *testing.T) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 10)
	if got := fitText(pdf, "core-sw1", 50); got != "core-sw1" {
		t.Errorf("a name that fits changed: %q", got)
	}
	if got := fitText(pdf, "Café", 50); got != "Caf\xe9" {
		t.Errorf("fitText(Café) = %q, want it converted to cp1252", got)
	}
	long := strings.Repeat("very-long-device-name-", 10) + "END"
	got := fitText(pdf, long, 40)
	if !strings.HasSuffix(got, "\x85") || strings.Contains(got, "END") {
		t.Errorf("long name = %q, want it cut with an ellipsis", got)
	}
	if w := pdf.GetStringWidth(got); w > 40 {
		t.Errorf("cut name is %.1f mm wide, more than 40", w)
	}
	if more := fitText(pdf, long, 41); len(more) < len(got) {
		t.Errorf("a wider column kept less: %q vs %q", more, got)
	}
}

// Test the controller rulings

func TestMetricsPDFDevicesScopeLabel(t *testing.T) {
	data := sampleMetricsData()
	data.Network.ScopeType = models.ScopeTypeDevices
	data.Network.Devices = 5
	data.Network.Ports = 0
	text, _ := renderMetrics(t, data)
	if !strings.Contains(text, "5 devices") {
		t.Errorf("devices scope should say '5 devices', but missing in: %s", text)
	}
}

func TestMetricsPDFSitesScopeCapNote(t *testing.T) {
	data := sampleMetricsData()
	data.Network.ScopeType = models.ScopeTypeSites
	data.Network.Ports = 0
	data.Network.Devices = 0
	data.Network.CappedOut = 10
	text, _ := renderMetrics(t, data)
	if !strings.Contains(text, "ports or devices") {
		t.Errorf("sites scope cap note should mention 'ports or devices'")
	}
}

func TestMetricsPDFSingleTrafficTileNoBillable(t *testing.T) {
	data := sampleMetricsData()
	// Create a report with only one traffic metric (no in/out pair)
	data.Network.Tiles = []MetricsTile{
		{Label: "Traffic In", Unit: "bps", Kind: "traffic", First: 640e6, Second: f64(2.1e12), Change: f64(18)},
	}
	data.Network.Tables = []MetricsTable{
		{Title: "Traffic In", Unit: "bps", Paired: false, Billable: false, HasTotal: true, Rows: []MetricsRow{}},
	}
	text, _ := renderMetrics(t, data)
	if strings.Contains(text, "billable 95th") {
		t.Errorf("single traffic tile should not say 'billable 95th'")
	}
	if !strings.Contains(text, "95th") {
		t.Errorf("single traffic tile should say '95th'")
	}
}

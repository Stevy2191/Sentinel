package services

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// pdfPageCount counts the page objects in a rendered file.
func pdfPageCount(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(regexp.MustCompile(`/Type /Page\b`).FindAll(raw, -1))
}

func TestMetricsPDFTables(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	// Traffic pair: the billing columns, in order, and the core port's figures.
	for _, want := range []string{"95th in", "95th out", "Billable 95th", "Peak", "Total in", "Total out", "Change",
		"640 Mbps", "300 Mbps", "900 Mbps", "1.2 TB", "900 GB", "+18%"} {
		if !hasLine(text, want) {
			t.Errorf("traffic table is missing %q", want)
		}
	}
	// The flagged row carries its coverage; the new row says so; the empty row says no data.
	for _, want := range []string{"72% data", "new", "No data"} {
		if !hasLine(text, want) {
			t.Errorf("traffic rows are missing %q", want)
		}
	}
	// Busy pair: averages and 95ths both ways, no totals.
	for _, want := range []string{"Avg in", "Avg out", "92.5%", "41%", "99%", "+5%"} {
		if !hasLine(text, want) {
			t.Errorf("busy table is missing %q", want)
		}
	}
	// A bool metric: share of time and how long.
	for _, want := range []string{"Share of time", "Time true", "0.3%", "2 h 13 m"} {
		if !hasLine(text, want) {
			t.Errorf("on-battery table is missing %q", want)
		}
	}
	// The busiest rows' small charts.
	if !hasLine(text, "Busiest by Traffic") {
		t.Error("missing the busiest rows' charts")
	}
}

func TestMetricsPDFRepeatsTheHeaderOnEveryPage(t *testing.T) {
	data := sampleMetricsData()
	traffic := &data.Network.Tables[0]
	traffic.Rows = nil
	for i := 0; i < 120; i++ {
		traffic.Rows = append(traffic.Rows, MetricsRow{
			Name: fmt.Sprintf("sw%d · Gi1/0/%d", i, i),
			In:   &RowStats{P95: 1e6, Total: f64(1e9)}, Out: &RowStats{P95: 1e6, Total: f64(1e9)}, Billable: f64(1e6), Coverage: 100,
		})
	}
	text, _ := renderMetrics(t, data)
	if n := strings.Count("\n"+text+"\n", "\nBillable 95th\n"); n < 3 {
		t.Errorf("the traffic header was drawn %d times for 120 rows, want it on each of at least 3 pages", n)
	}
}

func TestMetricsPDFNoData(t *testing.T) {
	data := sampleMetricsData()
	data.Network.NoData = true
	text, _ := renderMetrics(t, data)
	if !hasLine(text, "No data for this period") {
		t.Error("missing the no-data headline")
	}
	if !strings.Contains(text, "September 2026, compared with August 2026") {
		t.Error("the no-data page should still say which period it covers")
	}
	for _, absent := range []string{"Running hot", "Charts", "Billable 95th"} {
		if strings.Contains(text, absent) {
			t.Errorf("a report without data should not draw %q", absent)
		}
	}
}

func TestMetricsPDFEmptyIsOnePage(t *testing.T) {
	data := sampleMetricsData()
	data.Network.Empty = true
	text, path := renderMetrics(t, data)
	if !hasLine(text, metricsEmptyMessage) {
		t.Errorf("missing %q", metricsEmptyMessage)
	}
	if strings.Contains(text, "Billable 95th") || strings.Contains(text, "Running hot") {
		t.Error("an empty report should draw no tables or headline figures")
	}
	if n := pdfPageCount(t, path); n != 1 {
		t.Errorf("empty report has %d pages, want 1", n)
	}
}

// bigMetricsData is the largest report a run can produce: 500 rows in each
// of 10 tables, with long names and names in scripts the PDF fonts cannot
// draw, every table kind and 200-point charts.
func bigMetricsData() *ReportData {
	data := sampleMetricsData()
	n := data.Network
	start := data.TimeRangeStart
	long := strings.Repeat("very-long-device-name-", 8)
	name := func(i int) string {
		switch i % 4 {
		case 0:
			return fmt.Sprintf("%s%d · Gi1/0/%d (an alias that goes on and on and on)", long, i, i)
		case 1:
			return fmt.Sprintf("交换机-%d · 端口 %d", i, i)
		case 2:
			return fmt.Sprintf("Коммутатор-%d · порт %d", i, i)
		}
		return fmt.Sprintf("Café-Müller-%d · Ü%d 🚀 שלום", i, i)
	}
	kinds := []MetricsTable{
		{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true},
		{Title: "Busy", Unit: "%", Paired: true},
		{Title: "Errors", Unit: "per_min", Paired: true, HasTotal: true},
		{Title: "Temperature", Unit: "°C"},
		{Title: "On battery", Unit: "bool"},
		{Title: "Fan speed", Unit: "rpm"},
	}
	n.Tables, n.Charts, n.RowCharts, n.Tiles = nil, nil, nil, nil
	for ti := 0; ti < 10; ti++ {
		table := kinds[ti%len(kinds)]
		for i := 0; i < 500; i++ {
			v := float64(500-i) * 1.7e6
			s := &RowStats{Avg: v / 3, Min: 0, Peak: v * 1.5, P95: v, Total: f64(v * 2.6e6), TrueSeconds: f64(float64(i * 60))}
			row := MetricsRow{Name: name(i), In: s, Change: f64(float64(i%40) - 20), Coverage: 100}
			if table.Paired {
				row.Out = s
			}
			if table.Billable {
				row.Billable = f64(v)
			}
			switch {
			case i%11 == 0:
				row = MetricsRow{Name: name(i), NoData: true}
			case i%7 == 0:
				row.LowCoverage, row.Coverage = true, 61
			case i%13 == 0:
				row.Change, row.New = nil, true
			}
			table.Rows = append(table.Rows, row)
		}
		n.Tables = append(n.Tables, table)
		points := make([]ChartPoint, 200)
		for p := range points {
			points[p] = ChartPoint{T: start.Add(time.Duration(p) * 3 * time.Hour), V: float64(p%37) * 1e6}
		}
		chart := MetricsChart{Title: table.Title, Unit: table.Unit, Reference: f64(30e6),
			Lines: []ChartLine{{Label: "In", Points: points}, {Label: "Out", Points: points}}}
		n.Charts = append(n.Charts, chart)
		chart.Title = name(ti)
		n.RowCharts = append(n.RowCharts, chart)
		n.Tiles = append(n.Tiles, MetricsTile{Label: table.Title, Unit: table.Unit, Kind: "other", First: 42})
	}
	n.Busiest = []string{name(0), name(1), name(2), name(3), name(4)}
	n.RunningHot = nil
	for i := 0; i < 30; i++ {
		n.RunningHot = append(n.RunningHot, HotPort{Name: name(i), P95In: 95, P95Out: 81, HasIn: true, HasOut: true})
	}
	n.Rows, n.Ports = 500, 500
	return data
}

// Review Focus 4: the largest report renders without an fpdf panic, its long
// names cut to fit, its non-Latin names converted the way every report does
// (pdfText), and every table's header repeated on each page.
func TestMetricsPDFLargestReport(t *testing.T) {
	text, path := renderMetrics(t, bigMetricsData())
	info, err := os.Stat(path)
	if err != nil || info.Size() < 50_000 {
		t.Fatalf("PDF %v, %v: want a substantial file", info, err)
	}
	if pages := pdfPageCount(t, path); pages < 100 {
		t.Errorf("5,000 rows fit on %d pages; the tables did not render", pages)
	}
	if strings.Contains(text, strings.Repeat("very-long-device-name-", 8)) {
		t.Error("a long name was drawn whole instead of cut to its column")
	}
	if !strings.Contains(text, "very-long-device-name-") || !strings.Contains(text, "\x85") {
		t.Error("long names should be drawn cut with an ellipsis")
	}
	if !strings.Contains(text, "Caf\xe9-M\xfcller-3") {
		t.Error("accented names should survive as cp1252")
	}
	if strings.Contains(text, "交") || strings.Contains(text, "Ком") {
		t.Error("characters the fonts cannot draw reached the page as raw UTF-8")
	}
	if !hasLine(text, "and 20 more: the Busy table lists every port.") {
		t.Error("running hot should list 10 ports and count the rest")
	}
	if n := strings.Count("\n"+text+"\n", "\nBillable 95th\n"); n < 10 {
		t.Errorf("the traffic header was drawn %d times, want it on every page of the two traffic tables", n)
	}
}

// The footer is drawn below the page-break line; it must land on the last
// page rather than start an empty one.
func TestPDFFooterDoesNotAddAPage(t *testing.T) {
	r, err := NewPDFRendererService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := r.RenderReportToPDF(sampleReportData(), models.ReportTypeUptime, "footer")
	if err != nil {
		t.Fatal(err)
	}
	path, err := r.GetPDFPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if n := pdfPageCount(t, path); n != 1 {
		t.Errorf("the one-page uptime sample has %d pages", n)
	}
	if text := pdfDrawnText(t, path); !strings.Contains(text, "Generated by Sentinel") {
		t.Error("the footer is gone")
	}
}

// TestMetricsColumnsHeadersByTableKind verifies the exact header order for
// each table type on different scopes.
func TestMetricsColumnsHeadersByTableKind(t *testing.T) {
	cases := []struct {
		name  string
		table MetricsTable
		want  []string
	}{
		{
			name:  "traffic pair on ports scope",
			table: MetricsTable{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true},
			want:  []string{"95th in", "95th out", "Billable 95th", "Peak", "Total in", "Total out", "Change"},
		},
		{
			name:  "traffic pair on devices scope",
			table: MetricsTable{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true},
			want:  []string{"95th in", "95th out", "Billable 95th", "Peak", "Total in", "Total out", "Change"},
		},
		{
			name:  "busy pair",
			table: MetricsTable{Title: "Busy", Unit: "%", Paired: true},
			want:  []string{"Avg in", "Avg out", "95th in", "95th out", "Peak", "Change"},
		},
		{
			name:  "errors pair with totals",
			table: MetricsTable{Title: "Errors", Unit: "per_min", Paired: true, HasTotal: true},
			want:  []string{"Avg in", "Avg out", "95th in", "95th out", "Peak", "Total in", "Total out", "Change"},
		},
		{
			name:  "single metric with total",
			table: MetricsTable{Title: "Temperature", Unit: "°C", HasTotal: true},
			want:  []string{"Average", "95th", "Peak", "Total", "Change"},
		},
		{
			name:  "single metric without total",
			table: MetricsTable{Title: "Fan speed", Unit: "rpm"},
			want:  []string{"Average", "95th", "Peak", "Change"},
		},
		{
			name:  "bool table",
			table: MetricsTable{Title: "On battery", Unit: "bool"},
			want:  []string{"Share of time", "Time true", "Change"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cols := metricsColumns(c.table)
			if len(cols) != len(c.want) {
				t.Errorf("got %d columns, want %d", len(cols), len(c.want))
			}
			for i, col := range cols {
				if i < len(c.want) && col.header != c.want[i] {
					t.Errorf("column %d: got %q, want %q", i, col.header, c.want[i])
				}
			}
		})
	}
}

// TestMetricsRowCoverageFormatting verifies that coverage notes use formatPercent.
func TestMetricsRowCoverageFormatting(t *testing.T) {
	data := sampleMetricsData()
	data.Network.Tables = []MetricsTable{
		{Title: "Test", Unit: "bps", Rows: []MetricsRow{
			{Name: "row1", In: &RowStats{P95: 100}, Coverage: 89.6, LowCoverage: true},
		}},
	}
	text, _ := renderMetrics(t, data)
	if !hasLine(text, "89.6% data") {
		t.Errorf("coverage note should print as '89.6%% data', not '%.0f'", 89.6)
	}
}

// TestMetricsTableHeaderColumnName verifies that the first column header is "Port"
// for port scopes and "Name" for other scopes.
func TestMetricsTableHeaderColumnName(t *testing.T) {
	// Port_roles scope (from sampleMetricsData) should show "Port" as first column header
	data := sampleMetricsData()
	text, _ := renderMetrics(t, data)
	if !hasLine(text, "Port") {
		t.Error("port_roles scope should have 'Port' as a header line")
	}
	if hasLine(text, "Name") {
		t.Error("port_roles scope should not have 'Name' as a header line (should be 'Port')")
	}

	// Devices scope should show "Name" as first column header
	deviceData := sampleMetricsData()
	deviceData.Network.ScopeType = models.ScopeTypeDevices
	deviceData.Network.Ports = 0
	deviceData.Network.Devices = 3
	deviceText, _ := renderMetrics(t, deviceData)
	if !hasLine(deviceText, "Name") {
		t.Error("devices scope should have 'Name' as a header line")
	}
	if hasLine(deviceText, "Port") {
		t.Error("devices scope should not have 'Port' as a header line (should be 'Name')")
	}
}

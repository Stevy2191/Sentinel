package services

import (
	"math"
	"time"

	"github.com/go-pdf/fpdf"
)

// The Metrics report's charts: one per metric for the whole scope, two to a
// page, with the period's 95th as a reference line. The busiest rows' small
// charts (pdf_metrics_tables.go) use the same drawing.

const (
	metricsChartH      = 95.0
	metricsLegendRoomW = 50.0 // kept free right of a chart title for the legend
)

// metricsLineColors colour a chart's lines: in, then out.
var metricsLineColors = [2][3]int{pdfAccent, pdfSuccess}

// drawMetricsCharts draws one chart per metric for the whole scope, two to
// a page.
func drawMetricsCharts(pdf *fpdf.Fpdf, data *ReportData) {
	charts := data.Network.Charts
	if len(charts) == 0 {
		return
	}
	pdf.AddPage()
	drawSectionHeading(pdf, "Charts")
	for i, c := range charts {
		if i > 0 && i%2 == 0 {
			pdf.AddPage()
		}
		drawMetricsChart(pdf, data, c, metricsChartH)
	}
}

// drawMetricsChart draws a title line with its legend, then the chart, on a
// new page when the two would not fit together.
func drawMetricsChart(pdf *fpdf.Fpdf, data *ReportData, c MetricsChart, height float64) {
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+height+18 > pageH-bottom {
		pdf.AddPage()
	}
	y := pdf.GetY()
	pdf.SetFont("Helvetica", "B", 10)
	setColor(pdf, pdfInk, false)
	pdf.SetX(pdfMarginLeft)
	pdf.CellFormat(pdfContentW-metricsLegendRoomW, 6, fitText(pdf, c.Title, pdfContentW-metricsLegendRoomW-2), "", 0, "L", false, 0, "")
	drawChartLegend(pdf, c.Lines, y)
	pdf.SetY(y + 7)

	lc, ok := metricsLineChart(data, c, height)
	if !ok {
		drawPDFNotice(pdf, "Not enough data in this period to chart.", pdfMuted)
		return
	}
	drawLineChart(pdf, lc)
	pdf.Ln(2)
}

// drawChartLegend names a pair's lines at the right end of the title line.
func drawChartLegend(pdf *fpdf.Fpdf, lines []ChartLine, y float64) {
	if len(lines) < 2 {
		return
	}
	pdf.SetFont("Helvetica", "", 8)
	x := pdfMarginLeft + pdfContentW
	for i := len(lines) - 1; i >= 0; i-- {
		label := fitText(pdf, lines[i].Label, metricsLegendRoomW/2-8)
		w := pdf.GetStringWidth(label) + 2
		x -= w
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(x, y+1)
		pdf.CellFormat(w, 4, label, "", 0, "L", false, 0, "")
		x -= 5
		setColor(pdf, metricsLineColors[i%2], true)
		pdf.Rect(x+0.5, y+2.6, 4, 1, "F")
		x -= 3
	}
}

// metricsLineChart places a chart's points across the report period and
// picks a y range from zero (or the lowest value, when negative) to a round
// number above the highest value and the reference line. ok is false when no
// line has two points to join.
func metricsLineChart(data *ReportData, c MetricsChart, height float64) (lineChart, bool) {
	start, end := data.TimeRangeStart, data.TimeRangeEnd
	span := end.Sub(start).Seconds()
	if span <= 0 {
		return lineChart{}, false
	}
	lc := lineChart{
		Height: height, AxisLabelW: 18, TopPad: 3, Ticks: 4, LabelsInside: true,
		TickLabel: func(v float64) string { return formatValue(v, c.Unit) },
		XLabels:   timeAxisLabels(start, end, data.ReportLocation()),
	}
	lo, hi, longest := 0.0, 0.0, 0
	for i, l := range c.Lines {
		line := chartLine{Color: metricsLineColors[i%2], Width: 0.5}
		for _, p := range l.Points {
			if !finite(p.V) {
				continue
			}
			x := math.Min(math.Max(p.T.Sub(start).Seconds()/span, 0), 1)
			line.X = append(line.X, x)
			line.Y = append(line.Y, p.V)
			lo, hi = math.Min(lo, p.V), math.Max(hi, p.V)
		}
		longest = max(longest, len(line.X))
		lc.Lines = append(lc.Lines, line)
	}
	if longest < 2 {
		return lineChart{}, false
	}
	if c.Reference != nil && finite(*c.Reference) {
		hi = math.Max(hi, *c.Reference)
		lc.Ref = &chartRef{Value: *c.Reference, Label: referenceLabel(c), Color: pdfWarning}
	}
	lc.Hi = niceCeil(hi)
	if lo < 0 {
		lc.Lo = -niceCeil(-lo)
	}
	return lc, true
}

// referenceLabel is "Billable 95th 640 Mbps" for a traffic pair, otherwise
// "95th 92.5%".
func referenceLabel(c MetricsChart) string {
	label := "95th "
	if c.Unit == "bps" && len(c.Lines) == 2 {
		label = "Billable 95th "
	}
	return label + formatValue(*c.Reference, c.Unit)
}

// timeAxisLabels marks the start, middle and end of the period, with times
// when it is two days or shorter.
func timeAxisLabels(start, end time.Time, loc *time.Location) []chartXLabel {
	layout := "Jan 2"
	if end.Sub(start) <= 48*time.Hour {
		layout = "Jan 2 15:04"
	}
	mid := start.Add(end.Sub(start) / 2)
	return []chartXLabel{
		{At: 0, Text: start.In(loc).Format(layout), Align: "L"},
		{At: 1, Text: end.In(loc).Format(layout), Align: "R"},
		{At: 0.5, Text: mid.In(loc).Format(layout), Align: "C"},
	}
}

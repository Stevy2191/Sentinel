package services

import (
	"math"

	"github.com/go-pdf/fpdf"
)

// A line chart drawn from fpdf's own line and rectangle primitives, shared by
// the Uptime report's SLA graph and the Metrics report's charts. The caller
// decides the y range, the tick labels and where the points sit; this only
// draws.

// chartLine is one line: points at X (0 = the plot's left edge, 1 = its
// right edge) with values Y.
type chartLine struct {
	X, Y  []float64
	Color [3]int
	Width float64 // mm
}

// chartRef is a flat reference line across the plot, labelled above its
// left end.
type chartRef struct {
	Value float64
	Label string
	Color [3]int
}

// chartXLabel is an x-axis label at At (0-1 across the plot).
type chartXLabel struct {
	At    float64
	Text  string
	Align string // "L", "C" or "R"
}

// lineChart is everything drawLineChart needs.
type lineChart struct {
	Height     float64 // plot box height, mm
	AxisLabelW float64 // room left of the plot for the y-axis labels, mm
	Lo, Hi     float64 // y range; Hi must be above Lo
	// TopPad keeps a value at Hi off the box's top border, where it would
	// read as part of the frame rather than as data.
	TopPad    float64
	Ticks     int // gridlines from Lo to Hi: Ticks+1 of them
	TickLabel func(v float64) string
	Ref       *chartRef // drawn under the lines
	Lines     []chartLine
	XLabels   []chartXLabel
	// LabelsInside keeps the x labels within the plot: "L" starts at its
	// point and "R" ends at it. Off, every label sits in a 30 mm box centred
	// on its point, the uptime graph's original placement.
	LabelsInside bool
}

// drawLineChart draws c across the content width from the current Y, then
// moves Y below the x-axis labels (Height + 9 mm further down). It does not
// check for room on the page: callers do, because only they know what has to
// stay together with the chart (a heading, a legend).
func drawLineChart(pdf *fpdf.Fpdf, c lineChart) {
	x0 := pdfMarginLeft + c.AxisLabelW
	w := pdfContentW - c.AxisLabelW
	y0 := pdf.GetY()
	usableH := c.Height - c.TopPad
	yFor := func(v float64) float64 {
		frac := (v - c.Lo) / (c.Hi - c.Lo)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return y0 + c.TopPad + usableH*(1-frac)
	}

	pdf.SetFont("Helvetica", "", 7)
	for i := 0; i <= c.Ticks; i++ {
		frac := float64(i) / float64(c.Ticks)
		val := c.Lo + (c.Hi-c.Lo)*frac
		y := yFor(val)
		setColor(pdf, pdfRule, true)
		pdf.Rect(x0, y, w, 0.15, "F")
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(pdfMarginLeft, y-2)
		pdf.CellFormat(c.AxisLabelW-1, 4, pdfText(c.TickLabel(val)), "", 0, "R", false, 0, "")
	}
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	pdf.Rect(x0, y0, w, c.Height, "D")

	if c.Ref != nil {
		ry := yFor(c.Ref.Value)
		setColor(pdf, c.Ref.Color, true)
		pdf.Rect(x0, ry-0.25, w, 0.5, "F")
		pdf.SetXY(x0+2, ry-5)
		pdf.SetFont("Helvetica", "B", 7)
		setColor(pdf, c.Ref.Color, false)
		pdf.CellFormat(60, 4, pdfText(c.Ref.Label), "", 0, "L", false, 0, "")
	}

	for _, l := range c.Lines {
		setDrawColor(pdf, l.Color)
		pdf.SetLineWidth(l.Width)
		for i := 0; i+1 < len(l.X) && i+1 < len(l.Y); i++ {
			pdf.Line(x0+w*l.X[i], yFor(l.Y[i]), x0+w*l.X[i+1], yFor(l.Y[i+1]))
		}
	}
	pdf.SetLineWidth(0.2)

	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	for _, l := range c.XLabels {
		x := x0 + w*l.At
		left := x - 15
		if c.LabelsInside {
			switch l.Align {
			case "L":
				left = x
			case "R":
				left = x - 30
			}
		}
		pdf.SetXY(left, y0+c.Height+1.5)
		pdf.CellFormat(30, 4, pdfText(l.Text), "", 0, l.Align, false, 0, "")
	}

	pdf.SetY(y0 + c.Height + 9)
}

// niceCeil rounds v up to the next 1, 2, 2.5 or 5 times a power of ten, so
// axis ticks land on round numbers. Zero or less gives 1.
func niceCeil(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1
	}
	base := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*base >= v {
			return m * base
		}
	}
	return 10 * base
}

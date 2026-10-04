// Package services - report_metrics_data.go holds what a Metrics report hands
// from its builder (internal/netreport) to the renderers.
package services

import (
	"errors"
	"time"
)

// ErrReportTooLarge is returned when a statistics query hits its timeout; the
// job queue does not retry it. Its text is shown to the user as it is.
var ErrReportTooLarge = errors.New("This report is too large to build: narrow the scope or shorten the period")

// ChartPoint is one point of a report chart.
type ChartPoint struct {
	T time.Time
	V float64
}

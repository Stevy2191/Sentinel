// Package services - report_metrics_data.go holds what a Metrics report hands
// from its builder (internal/netreport) to the renderers.
package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ErrReportTooLarge is returned when a statistics query hits its timeout; the
// job queue does not retry it. Its text is shown to the user as it is.
var ErrReportTooLarge = errors.New("This report is too large to build: narrow the scope or shorten the period")

// NetworkReportBuilder builds the Metrics part of a report. netreport.Builder implements it.
// It lives behind an interface because netreport imports services.
type NetworkReportBuilder interface {
	Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error)
}

// ChartPoint is one point of a report chart.
type ChartPoint struct {
	T time.Time
	V float64
}

// MetricsReportData is everything a Metrics report shows. netreport.Builder
// fills it; the PDF renderer and the email summary draw it. Notes for the
// headline (CappedOut, Unavailable, Skipped, LowCoverage) are fields only:
// nothing copies them into ReportData.Warnings.
type MetricsReportData struct {
	ScopeType   string // the report's scope_type: what Unavailable and CappedOut count
	ScopeLabel  string // "WAN, Uplink ports at HQ, Annex"
	PrevStart   time.Time
	PrevEnd     time.Time
	RankLabel   string         // label of the ranking metric, e.g. "Traffic"
	Ports       int            // ports in the report, after the cap
	Devices     int            // devices in the report, after the cap
	Tiles       []MetricsTile  // one per table, whole scope combined
	Tables      []MetricsTable // one per metric or in/out pair, in metric order
	Busiest     []string       // up to 5 row names, busiest first
	RunningHot  []HotPort
	Charts      []MetricsChart // one per table, whole scope combined
	RowCharts   []MetricsChart // up to 10 busiest rows, first table's metric
	Rows        int            // rows included (≤ 500 subjects)
	CappedOut   int            // subjects left out by the 500 cap
	Unavailable int            // subjects dropped as unavailable to the owner or deleted
	Skipped     []string       // metric keys no longer defined
	LowCoverage int            // rows under 90% coverage
	Empty       bool           // every subject unavailable: one-page PDF
	NoData      bool           // no row has any data in the period
}

// MetricsTile is one headline tile: a table's figures for the whole scope.
// For an in/out pair the larger direction stands for both, figure by figure:
// a percent pair's tile shows the larger average and the larger 95th, which
// can come from different directions.
type MetricsTile struct {
	Label  string   // "Traffic", "Busy", "CPU", ...
	Unit   string   // "bps", "%", ...
	Kind   string   // "traffic" | "percent" | "other"
	First  float64  // traffic: 95th (billable for an in/out traffic pair); percent: average; other: average
	Second *float64 // traffic: total bytes; percent: 95th; other: nil
	Change *float64 // percent change of First vs the previous period; nil with New or no data
	New    bool     // no data (or zero) in the previous period
	NoData bool
}

// MetricsTable is one table: a metric, or an in/out pair.
type MetricsTable struct {
	Title    string       // "Traffic", "Busy", "Errors", or the metric label
	Unit     string       // the metric's unit
	Paired   bool         // in/out columns
	Billable bool         // traffic pair: Billable 95th column
	HasTotal bool         // unit is bps or per_min
	Rows     []MetricsRow // busiest first; on a sites scope the site totals lead
}

// MetricsRow is one line of a table.
type MetricsRow struct {
	Name        string    // "core-sw1 · Gi1/0/1 (uplink to annex)" or "core-sw1 · Switch 1"
	In          *RowStats // single metric: In only; nil without data
	Out         *RowStats // nil without data, and for a single metric
	Billable    *float64  // traffic pair: max(95th in, 95th out)
	Change      *float64  // traffic: on the 95th (billable for an in/out traffic pair); else on the average
	New         bool      // no data, or zero, in the previous period
	Coverage    float64   // 0-100
	LowCoverage bool      // has data, but for under 90% of the period
	NoData      bool
}

// RowStats is one side of a line over the period.
type RowStats struct {
	Avg, Min, Peak, P95 float64  // a bool metric's Avg is its share of time true (0-1)
	Total               *float64 // bytes for bps, count for per_min, nil otherwise
	TrueSeconds         *float64 // bool metrics: seconds true
}

// HotPort is a port whose 95th busy reached 80% in either direction.
type HotPort struct {
	Name          string
	P95In, P95Out float64 // percent; 0 where that direction has no data
	HasIn, HasOut bool    // that direction has busy data; one is false when only one busy metric was chosen
}

// MetricsChart is one chart: a table for the whole scope, or one busy line.
type MetricsChart struct {
	Title     string
	Unit      string
	Lines     []ChartLine // 1 line, or 2 for an in/out pair
	Reference *float64    // the 95th (billable for an in/out traffic pair); nil for bool metrics and when there is no data
}

// ChartLine is one line of a chart.
type ChartLine struct {
	Label  string
	Points []ChartPoint
}

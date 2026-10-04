package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// fakeNetworkBuilder records what the aggregator asked it for.
type fakeNetworkBuilder struct {
	user       uuid.UUID
	start, end time.Time
	loc        *time.Location
	data       *MetricsReportData
	err        error
}

func (f *fakeNetworkBuilder) Build(_ context.Context, _ *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error) {
	f.user, f.start, f.end, f.loc = requestedBy, start, end, loc
	return f.data, f.err
}

func networkReportFor(days int) *models.Report {
	return &models.Report{Name: "WAN", ReportType: models.ReportTypeMetrics, ScopeType: models.ScopeTypePortRoles,
		ScopeData:  models.ReportScope{SiteIDs: []uuid.UUID{uuid.New()}, Roles: []string{"wan"}, Metrics: []string{MetricIfInBps}},
		PeriodKind: models.PeriodRolling, TimeRangeDays: days}
}

// A metrics report goes to the network builder, with the period resolved in
// the report timezone, and never touches the monitor path: this service has
// no database here at all.
func TestAggregateMetricsReportUsesTheNetworkBuilder(t *testing.T) {
	fake := &fakeNetworkBuilder{data: &MetricsReportData{ScopeLabel: "WAN ports at HQ"}}
	s := NewReportAggregatorService(nil, nil)
	s.SetNetworkBuilder(fake)
	user := uuid.New()

	data, err := s.AggregateReportData(context.Background(), networkReportFor(7), user)
	if err != nil {
		t.Fatal(err)
	}
	if data.Network != fake.data || data.ReportName != "WAN" || len(data.Metrics) != 0 || len(data.Warnings) != 0 {
		t.Errorf("data = %+v, want the builder's network data and nothing else", data)
	}
	if fake.user != user || fake.loc != time.UTC || fake.end.Sub(fake.start) != 7*24*time.Hour {
		t.Errorf("builder got user %v, %v to %v in %v; want the requester, 7 days, UTC", fake.user, fake.start, fake.end, fake.loc)
	}
	if !data.TimeRangeStart.Equal(fake.start) || !data.TimeRangeEnd.Equal(fake.end) {
		t.Errorf("report window %v to %v, want the builder's %v to %v", data.TimeRangeStart, data.TimeRangeEnd, fake.start, fake.end)
	}
}

func TestAggregateMetricsReportPassesTooLargeThrough(t *testing.T) {
	s := NewReportAggregatorService(nil, nil)
	s.SetNetworkBuilder(&fakeNetworkBuilder{err: ErrReportTooLarge})
	if _, err := s.AggregateReportData(context.Background(), networkReportFor(7), uuid.New()); !errors.Is(err, ErrReportTooLarge) {
		t.Errorf("err = %v, want ErrReportTooLarge", err)
	}
}

func TestAggregateMetricsReportWithoutABuilder(t *testing.T) {
	if _, err := NewReportAggregatorService(nil, nil).AggregateReportData(context.Background(), networkReportFor(7), uuid.New()); err == nil {
		t.Error("a metrics report was aggregated with no network builder set")
	}
}

package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func hourAt(h int) time.Time { return time.Date(2026, 9, 10, h, 0, 0, 0, time.UTC) }

// The timeline exists to answer "what happened that afternoon", which the
// per-monitor grouping cannot: it interleaves monitors by time.
func TestBuildTimeline_OrdersAcrossMonitors(t *testing.T) {
	end := hourAt(15)
	metrics := []ReportMetrics{
		{
			MonitorID: uuid.New(), MonitorName: "API",
			Incidents: []IncidentSummary{
				{StartTime: hourAt(14), EndTime: &end, Duration: 60, Status: "resolved"},
				{StartTime: hourAt(9), Duration: 5, Status: "resolved"},
			},
		},
		{
			MonitorID: uuid.New(), MonitorName: "DNS",
			Incidents: []IncidentSummary{{StartTime: hourAt(11), Duration: 2, Status: "resolved"}},
		},
	}

	events := buildTimeline(metrics)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	wantOrder := []string{"API", "DNS", "API"}
	for i, want := range wantOrder {
		if events[i].MonitorName != want {
			t.Errorf("event %d is %s, want %s (events must be time-ordered, not grouped)",
				i, events[i].MonitorName, want)
		}
	}
	for i := 1; i < len(events); i++ {
		if events[i].StartTime.Before(events[i-1].StartTime) {
			t.Fatal("timeline is not in chronological order")
		}
	}
}

func TestAvailabilityBuckets_Daily(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 5)
	incEnd := time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC)

	metrics := []ReportMetrics{{
		MonitorName: "API",
		Incidents: []IncidentSummary{{
			StartTime: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
			EndTime:   &incEnd,
			Duration:  30,
		}},
	}}

	buckets := availabilityBuckets(metrics, start, end, time.UTC)
	if len(buckets) != 5 {
		t.Fatalf("got %d buckets, want 5 days", len(buckets))
	}
	// Only the third day carries downtime; the rest are clean.
	for i, b := range buckets {
		if i == 2 {
			if b.DowntimeMinutes != 30 {
				t.Errorf("Sep 3 downtime = %v, want 30", b.DowntimeMinutes)
			}
			if b.Uptime >= 100 {
				t.Errorf("Sep 3 uptime = %v, want below 100", b.Uptime)
			}
			if b.IncidentCount != 1 {
				t.Errorf("Sep 3 incidents = %d, want 1", b.IncidentCount)
			}
			continue
		}
		if b.DowntimeMinutes != 0 || b.Uptime != 100 {
			t.Errorf("bucket %d (%s) should be clean, got %v%% / %v min",
				i, b.Label, b.Uptime, b.DowntimeMinutes)
		}
	}
}

// A quarter of daily rows is unreadable, so long windows bucket by week.
func TestAvailabilityBuckets_LongWindowUsesWeeks(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	buckets := availabilityBuckets([]ReportMetrics{{MonitorName: "API"}}, start, end, time.UTC)
	if len(buckets) > 20 {
		t.Errorf("a quarter produced %d buckets; weekly bucketing should keep it near 13", len(buckets))
	}
	if len(buckets) < 10 {
		t.Errorf("a quarter produced only %d buckets", len(buckets))
	}
}

// An incident spanning a bucket boundary is split, not counted twice.
func TestAvailabilityBuckets_SplitsAcrossDays(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 3)
	incStart := time.Date(2026, 9, 1, 23, 30, 0, 0, time.UTC)
	incEnd := time.Date(2026, 9, 2, 0, 30, 0, 0, time.UTC)

	metrics := []ReportMetrics{{
		MonitorName: "API",
		Incidents:   []IncidentSummary{{StartTime: incStart, EndTime: &incEnd, Duration: 60}},
	}}

	buckets := availabilityBuckets(metrics, start, end, time.UTC)
	if buckets[0].DowntimeMinutes != 30 {
		t.Errorf("day 1 downtime = %v, want 30", buckets[0].DowntimeMinutes)
	}
	if buckets[1].DowntimeMinutes != 30 {
		t.Errorf("day 2 downtime = %v, want 30", buckets[1].DowntimeMinutes)
	}
	total := buckets[0].DowntimeMinutes + buckets[1].DowntimeMinutes
	if total != 60 {
		t.Errorf("split incident totals %v minutes, want 60 (not double counted)", total)
	}
}

// Two monitors down at once must not report more than 100% of the window lost.
func TestAvailabilityBuckets_ConcurrentOutagesStayInRange(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	incEnd := start.Add(12 * time.Hour)

	inc := IncidentSummary{StartTime: start, EndTime: &incEnd, Duration: 720}
	metrics := []ReportMetrics{
		{MonitorName: "A", Incidents: []IncidentSummary{inc}},
		{MonitorName: "B", Incidents: []IncidentSummary{inc}},
	}

	buckets := availabilityBuckets(metrics, start, end, time.UTC)
	if buckets[0].Uptime < 0 || buckets[0].Uptime > 100 {
		t.Errorf("uptime = %v, want within 0-100", buckets[0].Uptime)
	}
	// Both halves down for half the day averages to 50%.
	if buckets[0].Uptime != 50 {
		t.Errorf("uptime = %v, want 50", buckets[0].Uptime)
	}
}

package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The report window is [windowStart, windowEnd): a 10-day range.
var (
	windowEnd   = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	windowStart = windowEnd.AddDate(0, 0, -10)
)

func at(daysBeforeEnd float64) time.Time {
	return windowEnd.Add(-time.Duration(daysBeforeEnd * float64(24*time.Hour)))
}

func incident(start time.Time, end *time.Time) models.Incident {
	return models.Incident{ID: uuid.New(), StartTime: start, EndTime: end}
}

func TestSummarizeIncidentsClampsToWindow(t *testing.T) {
	oneDayIn := at(9) // 1 day after the window opens
	cases := []struct {
		name         string
		incidents    []models.Incident
		wantDowntime int // minutes
	}{
		{
			name:         "no incidents",
			incidents:    nil,
			wantDowntime: 0,
		},
		{
			name:         "fully inside the window",
			incidents:    []models.Incident{incident(at(5), ptrTime(at(5).Add(30*time.Minute)))},
			wantDowntime: 30,
		},
		{
			// The bug the original query had: filtering on start_time alone drops
			// this incident entirely, reporting uptime that is too high.
			name:         "started before the window, ended inside it",
			incidents:    []models.Incident{incident(windowStart.Add(-48*time.Hour), &oneDayIn)},
			wantDowntime: 24 * 60, // only the day inside the window counts
		},
		{
			name:         "started before the window and still open",
			incidents:    []models.Incident{incident(windowStart.Add(-48*time.Hour), nil)},
			wantDowntime: 10 * 24 * 60, // the whole window
		},
		{
			name:         "open incident starting inside the window",
			incidents:    []models.Incident{incident(at(2), nil)},
			wantDowntime: 2 * 24 * 60, // runs to the end of the window
		},
		{
			name:         "ended after the window closes",
			incidents:    []models.Incident{incident(at(1), ptrTime(windowEnd.Add(48*time.Hour)))},
			wantDowntime: 24 * 60, // cut at the window end
		},
		{
			name: "several incidents sum",
			incidents: []models.Incident{
				incident(at(8), ptrTime(at(8).Add(15*time.Minute))),
				incident(at(3), ptrTime(at(3).Add(45*time.Minute))),
			},
			wantDowntime: 60,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			summaries, total := summarizeIncidents(c.incidents, windowStart, windowEnd)
			if total != float64(c.wantDowntime) {
				t.Errorf("downtime = %v minutes, want %d", total, c.wantDowntime)
			}
			if len(summaries) != len(c.incidents) {
				t.Errorf("got %d summaries, want %d", len(summaries), len(c.incidents))
			}
		})
	}
}

func TestSummarizeIncidentsDerivesStatus(t *testing.T) {
	end := at(4)
	summaries, _ := summarizeIncidents([]models.Incident{
		incident(at(5), &end),
		incident(at(2), nil),
	}, windowStart, windowEnd)

	if summaries[0].Status != "resolved" {
		t.Errorf("closed incident status = %q, want \"resolved\"", summaries[0].Status)
	}
	if summaries[1].Status != "ongoing" {
		t.Errorf("open incident status = %q, want \"ongoing\"", summaries[1].Status)
	}
}

func TestUptimePercent(t *testing.T) {
	windowMinutes := 10 * 24 * 60

	cases := []struct {
		name     string
		downtime int
		want     float64
	}{
		{"no downtime is 100%", 0, 100},
		{"whole window down is 0%", windowMinutes, 0},
		{"half the window", windowMinutes / 2, 50},
		// Overlapping incidents can sum past the window length; uptime must not
		// go negative.
		{"downtime exceeding the window floors at zero", windowMinutes * 2, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := uptimePercent(windowStart, windowEnd, float64(c.downtime))
			if got != c.want {
				t.Errorf("uptimePercent(%d) = %v, want %v", c.downtime, got, c.want)
			}
		})
	}

	t.Run("zero-length window is not a division by zero", func(t *testing.T) {
		if got := uptimePercent(windowEnd, windowEnd, 0); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestMeasurableWindow(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	t.Run("a monitor older than the window is unclamped", func(t *testing.T) {
		from, ok := MeasurableWindow(start.AddDate(0, -1, 0), start, end)
		if !ok || !from.Equal(start) {
			t.Errorf("from=%v ok=%v, want %v true", from, ok, start)
		}
	})

	t.Run("a monitor created mid-window is clamped to its creation", func(t *testing.T) {
		created := start.AddDate(0, 0, 3)
		from, ok := MeasurableWindow(created, start, end)
		if !ok || !from.Equal(created) {
			t.Errorf("from=%v ok=%v, want %v true", from, ok, created)
		}
	})

	t.Run("a monitor created after the window has nothing to measure", func(t *testing.T) {
		_, ok := MeasurableWindow(end.AddDate(0, 0, 1), start, end)
		if ok {
			t.Error("expected ok=false for a monitor created after the window closed")
		}
	})

	t.Run("a monitor created exactly at the window's end has nothing to measure", func(t *testing.T) {
		_, ok := MeasurableWindow(end, start, end)
		if ok {
			t.Error("expected ok=false when creation equals the window end")
		}
	})
}

func TestEffectiveSLATarget(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }

	if got := EffectiveSLATarget(nil, 99.9); got != 99.9 {
		t.Errorf("nil override: got %v, want the system default 99.9", got)
	}
	if got := EffectiveSLATarget(f64(95), 99.9); got != 95 {
		t.Errorf("override: got %v, want 95", got)
	}
	if got := EffectiveSLATarget(f64(0), 99.9); got != 0 {
		t.Errorf("EffectiveSLATarget takes the pointer at face value: got %v, want 0", got)
	}
	if got := EffectiveSLATarget(f64(100), 99.9); got != 100 {
		t.Errorf("boundary override: got %v, want 100", got)
	}
}

func TestComputeUptimeSeries(t *testing.T) {
	s := &ReportAggregatorService{}
	monitorID := uuid.New()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 10)

	// An outage on day 3 should show up as a dip that day and partially
	// recover as later, clean days join the cumulative average - not a flat
	// line, and not a permanent drop to the outage's instantaneous percentage.
	outageStart := start.AddDate(0, 0, 3)
	outageEnd := outageStart.Add(12 * time.Hour)

	byMonitor := []monitorIncidentsForTest{{
		id:        monitorID,
		from:      start,
		incidents: []incidentWindow{{start: outageStart, end: &outageEnd}},
	}}

	points := computeUptimeSeriesFromIncidents(s, byMonitor, start, end)
	if len(points) != uptimeSeriesPoints {
		t.Fatalf("got %d points, want %d", len(points), uptimeSeriesPoints)
	}

	dayIndex := func(daysFromStart int) int {
		// Sample i covers start+((i+1)/uptimeSeriesPoints)*window; find the
		// first sample whose date has passed the given day boundary.
		target := start.AddDate(0, 0, daysFromStart)
		for i, p := range points {
			if !p.Date.Before(target) {
				return i
			}
		}
		return len(points) - 1
	}

	beforeOutage := points[dayIndex(2)]
	if beforeOutage.Uptime != 100 {
		t.Errorf("before the outage, cumulative uptime = %v, want 100", beforeOutage.Uptime)
	}

	dayOfOutage := points[dayIndex(4)]
	if dayOfOutage.Uptime >= 100 || dayOfOutage.Uptime <= 0 {
		t.Errorf("the sample covering the outage = %v, want a dip strictly between 0 and 100", dayOfOutage.Uptime)
	}

	last := points[len(points)-1]
	if last.Uptime <= dayOfOutage.Uptime {
		t.Errorf("cumulative uptime should partially recover as clean days accumulate: day-of-outage=%v last=%v",
			dayOfOutage.Uptime, last.Uptime)
	}
	if last.Uptime >= 100 {
		t.Errorf("the outage must still be reflected at the end of the window, got %v", last.Uptime)
	}
}

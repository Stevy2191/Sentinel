package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// TimelineEvent is one thing that happened, anywhere in the report's scope.
//
// The incident section groups by monitor, which answers "how did each service
// do". It cannot answer "what happened that Tuesday", because a single bad
// afternoon is split across however many monitors it touched. This is the same
// incidents ordered by time instead.
type TimelineEvent struct {
	MonitorID       uuid.UUID  `json:"monitor_id"`
	MonitorName     string     `json:"monitor_name"`
	StartTime       time.Time  `json:"start_time"`
	EndTime         *time.Time `json:"end_time"`
	DurationMinutes float64    `json:"duration_minutes"`
	Status          string     `json:"status"`
	Cause           string     `json:"cause"`
}

// PeriodComparison is this period measured against the one before it.
type PeriodComparison struct {
	Label string    `json:"label"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`

	Uptime          float64 `json:"uptime"`
	IncidentCount   int     `json:"incident_count"`
	DowntimeMinutes float64 `json:"downtime_minutes"`

	// Deltas are this period minus the previous one, so positive uptime is an
	// improvement and positive downtime is a regression.
	UptimeDelta   float64 `json:"uptime_delta"`
	IncidentDelta int     `json:"incident_delta"`
	DowntimeDelta float64 `json:"downtime_delta"`
}

// DayAvailability is one bucket of the availability breakdown.
type DayAvailability struct {
	Start           time.Time `json:"start"`
	Label           string    `json:"label"`
	Uptime          float64   `json:"uptime"`
	DowntimeMinutes float64   `json:"downtime_minutes"`
	IncidentCount   int       `json:"incident_count"`
}

// PerformanceStat is response-time behaviour for one monitor.
//
// Only successful checks are measured: a timeout's elapsed time is the timeout
// setting, not the service's speed, and averaging it in turns an outage into a
// slowdown.
type PerformanceStat struct {
	MonitorID   uuid.UUID `json:"monitor_id"`
	MonitorName string    `json:"monitor_name"`
	AvgMs       float64   `json:"avg_ms"`
	P95Ms       float64   `json:"p95_ms"`
	MaxMs       int       `json:"max_ms"`
	Samples     int       `json:"samples"`
}

// buildTimeline flattens per-monitor incidents into one chronological list.
func buildTimeline(metrics []ReportMetrics) []TimelineEvent {
	var events []TimelineEvent
	for _, m := range metrics {
		for _, inc := range m.Incidents {
			events = append(events, TimelineEvent{
				MonitorID:       m.MonitorID,
				MonitorName:     m.MonitorName,
				StartTime:       inc.StartTime,
				EndTime:         inc.EndTime,
				DurationMinutes: inc.Duration,
				Status:          inc.Status,
				Cause:           inc.RootCause,
			})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].StartTime.Equal(events[j].StartTime) {
			return events[i].MonitorName < events[j].MonitorName
		}
		return events[i].StartTime.Before(events[j].StartTime)
	})
	return events
}

// availabilityBuckets splits the window into days, or weeks when the window is
// long enough that daily rows would run to pages nobody reads.
func availabilityBuckets(
	metrics []ReportMetrics,
	start, end time.Time,
	loc *time.Location,
) []DayAvailability {
	if loc == nil {
		loc = time.UTC
	}
	start, end = start.In(loc), end.In(loc)
	if !end.After(start) {
		return nil
	}

	// A quarter is ~92 daily rows. Weekly buckets keep a quarterly report to 13.
	stepDays := 1
	layout := "Mon Jan 2"
	if end.Sub(start) > 45*24*time.Hour {
		stepDays = 7
		layout = "Jan 2"
	}

	// Bucket boundaries are midnight in the report zone, so a "day" is the day
	// the reader had, not a 24-hour slice offset from it.
	cursor := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)

	var buckets []DayAvailability
	for cursor.Before(end) {
		next := cursor.AddDate(0, 0, stepDays)
		windowStart, windowEnd := laterOf(cursor, start), earlierOf(next, end)

		var downtime float64
		incidents := 0
		for _, m := range metrics {
			for _, inc := range m.Incidents {
				overlap := overlapMinutes(inc.StartTime, inc.EndTime, windowStart, windowEnd, end)
				if overlap > 0 {
					downtime += overlap
				}
				if !inc.StartTime.Before(windowStart) && inc.StartTime.Before(windowEnd) {
					incidents++
				}
			}
		}

		// Downtime is summed across monitors, so the denominator has to be too;
		// otherwise two services down together reads as more than 100% lost.
		span := windowEnd.Sub(windowStart).Minutes() * float64(max(1, len(metrics)))
		uptime := 100.0
		if span > 0 {
			uptime = (span - downtime) / span * 100
			if uptime < 0 {
				uptime = 0
			}
		}

		label := cursor.Format(layout)
		if stepDays > 1 {
			label = fmt.Sprintf("%s - %s", cursor.Format(layout),
				earlierOf(next.AddDate(0, 0, -1), end).Format(layout))
		}

		buckets = append(buckets, DayAvailability{
			Start:           cursor,
			Label:           label,
			Uptime:          uptime,
			DowntimeMinutes: downtime,
			IncidentCount:   incidents,
		})
		cursor = next
	}
	return buckets
}

// overlapMinutes is how much of an incident falls inside a bucket.
func overlapMinutes(incStart time.Time, incEnd *time.Time, from, to, windowEnd time.Time) float64 {
	s := laterOf(incStart, from)
	// An open incident runs to the end of the report window.
	e := windowEnd
	if incEnd != nil {
		e = *incEnd
	}
	e = earlierOf(e, to)
	if !e.After(s) {
		return 0
	}
	return e.Sub(s).Minutes()
}

// performanceStats aggregates response times in SQL rather than reading every
// check into memory: a quarter across a large scope is millions of rows, and
// only four numbers per monitor are wanted.
func (s *ReportAggregatorService) performanceStats(
	ctx context.Context,
	monitors []models.Monitor,
	start, end time.Time,
) ([]PerformanceStat, error) {
	if len(monitors) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(monitors))
	names := make(map[uuid.UUID]string, len(monitors))
	for _, m := range monitors {
		ids = append(ids, m.ID)
		names[m.ID] = m.Name
	}

	var rows []struct {
		MonitorID uuid.UUID
		AvgMs     float64
		P95Ms     float64
		MaxMs     int
		Samples   int
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT monitor_id,
		       COALESCE(AVG(response_time_ms), 0)                                          AS avg_ms,
		       COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms), 0)  AS p95_ms,
		       COALESCE(MAX(response_time_ms), 0)                                           AS max_ms,
		       COUNT(*)                                                                     AS samples
		  FROM checks
		 WHERE monitor_id IN ?
		   AND timestamp >= ? AND timestamp < ?
		   AND status = 'success'
		   AND response_time_ms > 0
		 GROUP BY monitor_id`, ids, start, end).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("aggregating response times: %w", err)
	}

	stats := make([]PerformanceStat, 0, len(rows))
	for _, r := range rows {
		stats = append(stats, PerformanceStat{
			MonitorID:   r.MonitorID,
			MonitorName: names[r.MonitorID],
			AvgMs:       round2(r.AvgMs),
			P95Ms:       round2(r.P95Ms),
			MaxMs:       r.MaxMs,
			Samples:     r.Samples,
		})
	}
	// Slowest first: the point of the section is what needs attention.
	sort.Slice(stats, func(i, j int) bool { return stats[i].P95Ms > stats[j].P95Ms })
	return stats, nil
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

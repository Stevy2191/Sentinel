package services

import (
	"math"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// UptimeDailyDays is how many days DailyUptimeBuckets covers.
const UptimeDailyDays = 90

func roundUptime(f float64) float64 { return math.Round(f*100) / 100 }

// DailyUptimeBuckets buckets checks into the 90 UTC calendar days ending at
// now, each summarized purely by pass/fail counts - no incident- or
// maintenance-derived spans, since the public status page never surfaces
// exact downtime clock times. Used by the public status page and the
// dashboard monitors widget; the authenticated endpoints have their own,
// hourly, view (HourlyUptimeBuckets).
func DailyUptimeBuckets(checks []models.Check, now time.Time) []map[string]any {
	type bucket struct{ total, failed int }
	buckets := make(map[time.Time]*bucket)
	truncDay := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	for _, ch := range checks {
		k := truncDay(ch.Timestamp)
		b := buckets[k]
		if b == nil {
			b = &bucket{}
			buckets[k] = b
		}
		b.total++
		if ch.Status != "success" {
			b.failed++
		}
	}

	daily := make([]map[string]any, 0, UptimeDailyDays)
	curDay := truncDay(now)
	for i := UptimeDailyDays - 1; i >= 0; i-- {
		k := curDay.AddDate(0, 0, -i)
		b := buckets[k]
		status := "nodata"
		uptime := 0.0
		if b != nil && b.total > 0 {
			uptime = roundUptime(float64(b.total-b.failed) / float64(b.total) * 100)
			switch {
			case b.failed == 0:
				status = "up"
			case b.failed == b.total:
				status = "down"
			default:
				status = "partial"
			}
		}
		daily = append(daily, map[string]any{
			"date":   k.Format("2006-01-02"),
			"uptime": uptime,
			"status": status,
		})
	}
	return daily
}

// uptimeSpan is a stretch of time inside a single hourly bucket, together
// with how many seconds of that bucket it covers. A zero start means "nothing here".
type uptimeSpan struct {
	start, end time.Time
	seconds    int
}

// latestTime/earliestTime are the time equivalents of max/min.
func latestTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earliestTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// rfc3339OrNil renders a timestamp for JSON, or nil when it is unset, so the
// client can distinguish "no downtime" from "downtime at the epoch".
func rfc3339OrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// uptimeInterval is a period of interest — an outage or a maintenance window
// — with a nil end meaning it is still running.
type uptimeInterval struct {
	start time.Time
	end   *time.Time
}

func incidentIntervals(incidents []models.Incident) []uptimeInterval {
	out := make([]uptimeInterval, 0, len(incidents))
	for i := range incidents {
		out = append(out, uptimeInterval{start: incidents[i].StartTime, end: incidents[i].EndTime})
	}
	return out
}

func maintenanceIntervals(windows []models.MaintenanceHistory) []uptimeInterval {
	out := make([]uptimeInterval, 0, len(windows))
	for i := range windows {
		end := windows[i].EndTime
		out = append(out, uptimeInterval{start: windows[i].StartTime, end: &end})
	}
	return out
}

// spanInHour clips every interval to [hourStart, hourEnd] and returns the
// outer bounds of the clipped pieces plus their total duration. Reporting the
// outer bounds (rather than each piece) keeps the client's tooltip to a
// single "from — to" while the second count stays exact.
func spanInHour(intervals []uptimeInterval, hourStart, hourEnd, now time.Time) uptimeSpan {
	var out uptimeSpan
	for _, iv := range intervals {
		segStart := latestTime(iv.start.UTC(), hourStart)
		// An open interval is still running, so it covers the hour up to now.
		segEnd := now
		if iv.end != nil {
			segEnd = iv.end.UTC()
		}
		segEnd = earliestTime(segEnd, hourEnd)

		if !segEnd.After(segStart) {
			continue
		}
		out.seconds += int(segEnd.Sub(segStart).Seconds())
		if out.start.IsZero() || segStart.Before(out.start) {
			out.start = segStart
		}
		if segEnd.After(out.end) {
			out.end = segEnd
		}
	}
	return out
}

// HourlyUptimeBuckets buckets checks into the 24 hours ending at now, each
// annotated with two views of the hour: "uptime"/"status" summarize the
// checks recorded in it (what a sparkline draws), while the down/maintenance
// spans give the actual clock time derived from incidents and recorded
// maintenance history (what a detailed 24-hour health bar draws). Shared by
// the authenticated uptime-history endpoint, the public status page and the
// dashboard monitors widget, so all draw the same 24-hour strip.
//
// checks may span a wider window than 24 hours - only checks that fall in
// the last 24 hours affect the result. incidents and maintenanceWindows
// should already be scoped to that same 24-hour window.
func HourlyUptimeBuckets(
	checks []models.Check,
	incidents []models.Incident,
	maintenanceWindows []models.MaintenanceHistory,
	now time.Time,
	createdAt time.Time,
) []map[string]any {
	type bucket struct {
		total, failed int
		// First/last failing check in the hour: the fallback source for a
		// downtime span when no incident was recorded (e.g. failures during a
		// maintenance window, where incidents are suppressed).
		firstFail, lastFail time.Time
	}
	buckets := make(map[time.Time]*bucket)
	truncHour := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	}
	for _, ch := range checks {
		k := truncHour(ch.Timestamp)
		b := buckets[k]
		if b == nil {
			b = &bucket{}
			buckets[k] = b
		}
		b.total++
		if ch.Status != "success" {
			b.failed++
			ts := ch.Timestamp.UTC()
			if b.firstFail.IsZero() || ts.Before(b.firstFail) {
				b.firstFail = ts
			}
			if ts.After(b.lastFail) {
				b.lastFail = ts
			}
		}
	}

	downIntervals := incidentIntervals(incidents)
	maintIntervals := maintenanceIntervals(maintenanceWindows)

	hourly := make([]map[string]any, 0, 24)
	curHour := truncHour(now)
	for i := 23; i >= 0; i-- {
		k := curHour.Add(time.Duration(-i) * time.Hour)
		b := buckets[k]
		status := "nodata"
		uptime := 0.0
		if b != nil && b.total > 0 {
			uptime = roundUptime(float64(b.total-b.failed) / float64(b.total) * 100)
			switch {
			case b.failed == 0:
				status = "up"
			case b.failed == b.total:
				status = "down"
			default:
				status = "partial"
			}
		}

		// The hour is only observable up to "now" — never attribute downtime
		// to the part of the current hour that hasn't happened yet.
		hourEnd := earliestTime(k.Add(time.Hour), now.UTC())

		downSpan := spanInHour(downIntervals, k, hourEnd, now.UTC())
		if downSpan.seconds == 0 && b != nil && !b.firstFail.IsZero() {
			// No incident on record, but checks failed here: report the span
			// the failures cover so the hour still reads as degraded.
			downSpan = uptimeSpan{start: b.firstFail, end: b.lastFail, seconds: 0}
		}
		maintSpan := spanInHour(maintIntervals, k, hourEnd, now.UTC())

		hourly = append(hourly, map[string]any{
			"hour":                k.Hour(),
			"uptime":              uptime,
			"status":              status,
			"bucket_start":        k.Format(time.RFC3339),
			"down_seconds":        downSpan.seconds,
			"maintenance_seconds": maintSpan.seconds,
			"down_start":          rfc3339OrNil(downSpan.start),
			"down_end":            rfc3339OrNil(downSpan.end),
			"maintenance_start":   rfc3339OrNil(maintSpan.start),
			"maintenance_end":     rfc3339OrNil(maintSpan.end),
			// An hour entirely before the monitor existed has nothing to
			// report, as opposed to an hour that was simply quiet.
			"observed": !k.Add(time.Hour).Before(createdAt.UTC()),
		})
	}
	return hourly
}

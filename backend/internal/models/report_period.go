package models

import (
	"errors"
	"fmt"
	"time"
)

// How a report's window is worked out.
const (
	// PeriodRolling counts TimeRangeDays back from now.
	PeriodRolling = "rolling"
	// PeriodCalendar is a whole calendar unit — the month of September, not the
	// thirty days ending today.
	PeriodCalendar = "calendar"
	// PeriodCustom is an explicit range.
	PeriodCustom = "custom"
)

// Calendar units a report can cover.
const (
	UnitWeek    = "week"
	UnitMonth   = "month"
	UnitQuarter = "quarter"
	UnitYear    = "year"
)

// MaxPeriodOffset bounds how far back a calendar period may reach.
const MaxPeriodOffset = 24

var validPeriodUnits = map[string]bool{
	UnitWeek: true, UnitMonth: true, UnitQuarter: true, UnitYear: true,
}

// ValidatePeriod checks that the period fields agree with the period kind.
func (r *Report) ValidatePeriod() error {
	switch r.PeriodKind {
	case "", PeriodRolling:
		if r.TimeRangeDays <= 0 {
			return errors.New("time_range_days must be greater than zero for a rolling period")
		}
	case PeriodCalendar:
		if !validPeriodUnits[r.PeriodUnit] {
			return fmt.Errorf("period_unit must be one of week, month, quarter, year, got %q", r.PeriodUnit)
		}
		if r.PeriodOffset < 0 || r.PeriodOffset > MaxPeriodOffset {
			return fmt.Errorf("period_offset must be between 0 and %d, got %d", MaxPeriodOffset, r.PeriodOffset)
		}
	case PeriodCustom:
		if r.PeriodStart == nil || r.PeriodEnd == nil {
			return errors.New("period_start and period_end are required for a custom period")
		}
		if !r.PeriodStart.Before(*r.PeriodEnd) {
			return errors.New("period_start must be before period_end")
		}
	default:
		return fmt.Errorf("period_kind must be rolling, calendar or custom, got %q", r.PeriodKind)
	}
	return nil
}

// ResolvePeriod returns the window the report covers.
//
// Calendar boundaries are computed in loc, not UTC: "September" means September
// where the reader is, and a month boundary resolved in the wrong zone shifts
// every figure in the report by a few hours at each end. loc is the instance's
// report timezone; nil is treated as UTC.
//
// The end of a period still in progress is clamped to now — a report for "this
// month" run on the 10th covers the 10 days that have happened, not a window
// two thirds of which is in the future, which would otherwise be counted as
// uptime nobody has observed.
func (r *Report) ResolvePeriod(now time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)

	switch r.PeriodKind {
	case PeriodCustom:
		if r.PeriodStart != nil && r.PeriodEnd != nil {
			return r.PeriodStart.In(loc), r.PeriodEnd.In(loc)
		}
	case PeriodCalendar:
		start, end = calendarWindow(now, r.PeriodUnit, r.PeriodOffset, loc)
		if end.After(now) {
			end = now
		}
		return start, end
	}

	// Rolling, and the fallback for a row written before periods existed.
	days := r.TimeRangeDays
	if days <= 0 {
		days = 30
	}
	return now.AddDate(0, 0, -days), now
}

// calendarWindow is the unit containing now, shifted back by offset units.
func calendarWindow(now time.Time, unit string, offset int, loc *time.Location) (time.Time, time.Time) {
	y, m, d := now.Date()

	switch unit {
	case UnitWeek:
		// Weeks start on Monday: a report labelled "last week" that began on a
		// Sunday would not match any calendar anyone works to.
		weekday := (int(now.Weekday()) + 6) % 7 // Monday = 0
		start := time.Date(y, m, d-weekday-7*offset, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 0, 7)

	case UnitMonth:
		start := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, -offset, 0)
		return start, start.AddDate(0, 1, 0)

	case UnitQuarter:
		// Quarters start in January, April, July, October.
		qStartMonth := time.Month((int(m)-1)/3*3 + 1)
		start := time.Date(y, qStartMonth, 1, 0, 0, 0, 0, loc).AddDate(0, -3*offset, 0)
		return start, start.AddDate(0, 3, 0)

	case UnitYear:
		start := time.Date(y-offset, time.January, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(1, 0, 0)
	}

	// Unknown unit: a month is the least surprising thing to fall back to, and
	// ValidatePeriod rejects this case before it can be stored.
	start := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 1, 0)
}

// PeriodLabel names the window for a report's title, e.g. "September 2026".
func (r *Report) PeriodLabel(now time.Time, loc *time.Location) string {
	start, end := r.ResolvePeriod(now, loc)
	switch r.PeriodKind {
	case PeriodCalendar:
		switch r.PeriodUnit {
		case UnitWeek:
			return fmt.Sprintf("Week of %s", start.Format("January 2, 2006"))
		case UnitMonth:
			return start.Format("January 2006")
		case UnitQuarter:
			return fmt.Sprintf("Q%d %d", (int(start.Month())-1)/3+1, start.Year())
		case UnitYear:
			return start.Format("2006")
		}
	case PeriodCustom:
		return fmt.Sprintf("%s to %s", start.Format("January 2, 2006"), end.Format("January 2, 2006"))
	}
	if r.TimeRangeDays == 1 {
		return "Last 24 hours"
	}
	return fmt.Sprintf("Last %d days", r.TimeRangeDays)
}

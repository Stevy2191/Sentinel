package models

import (
	"testing"
	"time"
)

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	return loc
}

// A calendar month must be the month where the reader is. Resolved in UTC, a
// Chicago September would start and end five hours off, moving incidents in and
// out of the report at both edges.
func TestResolvePeriod_MonthUsesReportTimezone(t *testing.T) {
	loc := chicago(t)
	r := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth, PeriodOffset: 1}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	start, end := r.ResolvePeriod(now, loc)

	if got := start.In(loc).Format("2006-01-02 15:04 MST"); got != "2026-08-01 00:00 CDT" {
		t.Errorf("start = %s, want 2026-08-01 00:00 CDT", got)
	}
	if got := end.In(loc).Format("2006-01-02 15:04 MST"); got != "2026-09-01 00:00 CDT" {
		t.Errorf("end = %s, want 2026-09-01 00:00 CDT", got)
	}
	if got := r.PeriodLabel(now, loc); got != "August 2026" {
		t.Errorf("label = %q, want %q", got, "August 2026")
	}
}

func TestResolvePeriod_Quarter(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) // Q3

	// The quarter in progress, clamped to now.
	this := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitQuarter}
	start, end := this.ResolvePeriod(now, loc)
	if got := start.In(loc).Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("Q3 start = %s, want 2026-07-01", got)
	}
	if end.After(now.In(loc).Add(time.Second)) {
		t.Errorf("end %v runs past now; a period in progress must not include the future", end)
	}
	if got := this.PeriodLabel(now, loc); got != "Q3 2026" {
		t.Errorf("label = %q, want Q3 2026", got)
	}

	// The previous quarter is whole.
	last := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitQuarter, PeriodOffset: 1}
	start, end = last.ResolvePeriod(now, loc)
	if got := start.In(loc).Format("2006-01-02"); got != "2026-04-01" {
		t.Errorf("Q2 start = %s, want 2026-04-01", got)
	}
	if got := end.In(loc).Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("Q2 end = %s, want 2026-07-01", got)
	}
	if got := last.PeriodLabel(now, loc); got != "Q2 2026" {
		t.Errorf("label = %q, want Q2 2026", got)
	}
}

// Weeks run Monday to Monday; a week starting on Sunday matches no calendar
// anyone plans against.
func TestResolvePeriod_WeekStartsMonday(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) // a Saturday
	r := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitWeek, PeriodOffset: 1}

	start, end := r.ResolvePeriod(now, loc)
	if start.In(loc).Weekday() != time.Monday {
		t.Errorf("week starts on %v, want Monday", start.In(loc).Weekday())
	}
	if d := end.Sub(start).Hours(); d < 167 || d > 169 { // a DST week is 167 or 169
		t.Errorf("week spans %.0f hours, want about 168", d)
	}
}

// A month containing a DST change is still one calendar month, not 30 days.
func TestResolvePeriod_SpansDSTChange(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	r := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth, PeriodOffset: 1} // November

	start, end := r.ResolvePeriod(now, loc)
	if got := start.In(loc).Format("2006-01-02 MST"); got != "2026-11-01 CDT" {
		t.Errorf("start = %s, want 2026-11-01 CDT", got)
	}
	if got := end.In(loc).Format("2006-01-02 MST"); got != "2026-12-01 CST" {
		t.Errorf("end = %s, want 2026-12-01 CST", got)
	}
	// 30 days plus the hour gained when the clocks went back.
	if h := end.Sub(start).Hours(); h != 721 {
		t.Errorf("November spans %v hours, want 721 across the DST change", h)
	}
}

// Rows written before periods existed have an empty kind and must keep working.
func TestResolvePeriod_LegacyRowIsRolling(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	r := &Report{TimeRangeDays: 7}
	start, end := r.ResolvePeriod(now, nil)
	if d := end.Sub(start).Hours(); d != 7*24 {
		t.Errorf("legacy report spans %v hours, want 168", d)
	}
	if got := r.PeriodLabel(now, nil); got != "Last 7 days" {
		t.Errorf("label = %q", got)
	}
}

func TestValidatePeriod(t *testing.T) {
	ok := []*Report{
		{TimeRangeDays: 30},
		{PeriodKind: PeriodRolling, TimeRangeDays: 1},
		{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth},
		{PeriodKind: PeriodCalendar, PeriodUnit: UnitQuarter, PeriodOffset: 4},
	}
	for i, r := range ok {
		if err := r.ValidatePeriod(); err != nil {
			t.Errorf("case %d should be valid: %v", i, err)
		}
	}

	start := time.Now()
	end := start.Add(time.Hour)
	bad := []*Report{
		{PeriodKind: PeriodRolling},                           // no days
		{PeriodKind: PeriodCalendar},                          // no unit
		{PeriodKind: PeriodCalendar, PeriodUnit: "fortnight"}, // unknown unit
		{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth, PeriodOffset: 99},
		{PeriodKind: PeriodCustom},                                       // no bounds
		{PeriodKind: PeriodCustom, PeriodStart: &end, PeriodEnd: &start}, // reversed
		{PeriodKind: "yearly"},                                           // unknown kind
	}
	for i, r := range bad {
		if err := r.ValidatePeriod(); err == nil {
			t.Errorf("case %d should be rejected", i)
		}
	}
}

package models

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newIDs(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func TestReportScopeValidateNetwork(t *testing.T) {
	site := []uuid.UUID{uuid.New()}
	metrics := []string{"if_in_bps"}
	cases := []struct {
		name      string
		scopeType string
		scope     ReportScope
		wantErr   string // "" = valid; otherwise a part of the message
	}{
		{"ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(1), Metrics: metrics}, ""},
		{"500 ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(500), Metrics: metrics}, ""},
		{"501 ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(501), Metrics: metrics}, "at most 500 ports"},
		{"no ports", ScopeTypePorts, ReportScope{Metrics: metrics}, "scope_data.port_ids is required"},
		{"devices", ScopeTypeDevices, ReportScope{DeviceIDs: newIDs(1), Metrics: metrics}, ""},
		{"501 devices", ScopeTypeDevices, ReportScope{DeviceIDs: newIDs(501), Metrics: metrics}, "at most 500 devices"},
		{"no devices", ScopeTypeDevices, ReportScope{Metrics: metrics}, "scope_data.device_ids is required"},
		{"port roles", ScopeTypePortRoles, ReportScope{SiteIDs: site, Roles: []string{"wan", "uplink"}, Metrics: metrics}, ""},
		{"roles without sites", ScopeTypePortRoles, ReportScope{Roles: []string{"wan"}, Metrics: metrics}, "scope_data.site_ids is required"},
		{"sites without roles", ScopeTypePortRoles, ReportScope{SiteIDs: site, Metrics: metrics}, "scope_data.roles is required"},
		{"an unknown role", ScopeTypePortRoles, ReportScope{SiteIDs: site, Roles: []string{"core"}, Metrics: metrics}, "allowed: wan, uplink, access"},
		{"sites", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: metrics}, ""},
		{"no sites", ScopeTypeSites, ReportScope{Metrics: metrics}, "scope_data.site_ids is required"},
		{"no metrics", ScopeTypeSites, ReportScope{SiteIDs: site}, "scope_data.metrics is required for a metrics report"},
		{"11 metrics", ScopeTypeSites, ReportScope{SiteIDs: site,
			Metrics: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"}}, "at most 10 metrics"},
		{"10 metrics", ScopeTypeSites, ReportScope{SiteIDs: site,
			Metrics: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}}, ""},
		{"a metric twice", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: []string{"if_in_bps", "if_in_bps"}}, "twice"},
		{"an empty metric", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: []string{""}}, "an empty metric"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate(c.scopeType)
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("accepted, want an error containing %q", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// The scope preview sizes a scope before any metric is chosen.
func TestReportScopeValidateSubjectsIgnoresMetrics(t *testing.T) {
	s := ReportScope{SiteIDs: newIDs(1)}
	if err := s.ValidateSubjects(ScopeTypeSites); err != nil {
		t.Errorf("ValidateSubjects without metrics: %v", err)
	}
	if err := s.Validate(ScopeTypeSites); err == nil {
		t.Error("Validate accepted a metrics scope with no metrics")
	}
	if err := (ReportScope{MonitorIDs: newIDs(1)}).ValidateSubjects(ScopeTypeMonitors); err == nil {
		t.Error("ValidateSubjects accepted a monitor scope type")
	}
}

func TestIsNetworkScope(t *testing.T) {
	for _, s := range []string{ScopeTypePorts, ScopeTypePortRoles, ScopeTypeDevices, ScopeTypeSites} {
		if !IsNetworkScope(s) || !ValidScopeTypes[s] {
			t.Errorf("%s: IsNetworkScope %v, ValidScopeTypes %v; want both true", s, IsNetworkScope(s), ValidScopeTypes[s])
		}
	}
	for _, s := range []string{ScopeTypeMonitors, ScopeTypeTags, ScopeTypeGroups, ScopeTypeTypes, "everything"} {
		if IsNetworkScope(s) {
			t.Errorf("%s is not a network scope", s)
		}
	}
}

// Metrics reports take only network scopes, uptime and incident reports only
// monitor scopes, and the message names what is allowed for the report type.
func TestReportValidatePairsScopeWithType(t *testing.T) {
	network := ReportScope{PortIDs: newIDs(1), Metrics: []string{"if_in_bps"}}
	cases := []struct {
		name, reportType, scopeType string
		scope                       ReportScope
		wantErr                     string
	}{
		{"metrics on ports", ReportTypeMetrics, ScopeTypePorts, network, ""},
		{"metrics on monitors", ReportTypeMetrics, ScopeTypeMonitors, ReportScope{MonitorIDs: newIDs(1)},
			"scope_type must be one of: ports, port_roles, devices, sites for a metrics report"},
		{"uptime on ports", ReportTypeUptime, ScopeTypePorts, network,
			"scope_type must be one of: monitors, tags, groups, types for an uptime or incident report"},
		{"incident on sites", ReportTypeIncident, ScopeTypeSites, ReportScope{SiteIDs: newIDs(1), Metrics: []string{"if_in_bps"}},
			"scope_type must be one of: monitors, tags, groups, types"},
		{"uptime on types", ReportTypeUptime, ScopeTypeTypes, ReportScope{Types: []string{"dns"}}, ""},
		{"unknown report type", "weekly", ScopeTypePorts, network, "report_type must be one of: uptime, incident, metrics"},
		{"metrics, scope too big", ReportTypeMetrics, ScopeTypePorts, ReportScope{PortIDs: newIDs(501), Metrics: []string{"if_in_bps"}},
			"at most 500 ports"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Report{Name: "r", ReportType: c.reportType, ScopeType: c.scopeType, ScopeData: c.scope, TimeRangeDays: 30}
			err := r.Validate()
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("accepted, want an error containing %q", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// The previous period is the previous calendar unit for a calendar report
// (whole, even when this one is still running), otherwise the same length
// immediately before the start.
func TestPreviousPeriod(t *testing.T) {
	loc := chicago(t)
	local := func(s string) time.Time {
		t.Helper()
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ptr := func(v time.Time) *time.Time { return &v }
	cases := []struct {
		name       string
		report     *Report
		now        string
		start, end string
	}{
		{"last month", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth, PeriodOffset: 1},
			"2026-12-02 12:00", "2026-10-01 00:00", "2026-11-01 00:00"},
		{"this month, still running", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth},
			"2026-10-03 12:00", "2026-09-01 00:00", "2026-10-01 00:00"},
		{"last quarter", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitQuarter, PeriodOffset: 1},
			"2026-09-19 12:00", "2026-01-01 00:00", "2026-04-01 00:00"},
		{"last week", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitWeek, PeriodOffset: 1},
			"2026-09-19 12:00", "2026-08-31 00:00", "2026-09-07 00:00"},
		{"last year", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitYear, PeriodOffset: 1},
			"2026-09-19 12:00", "2024-01-01 00:00", "2025-01-01 00:00"},
		{"custom", &Report{PeriodKind: PeriodCustom, PeriodStart: ptr(local("2026-09-10 00:00")), PeriodEnd: ptr(local("2026-09-20 00:00"))},
			"2026-10-03 12:00", "2026-08-31 00:00", "2026-09-10 00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := c.report.ResolvePeriod(local(c.now), loc)
			ps, pe := c.report.PreviousPeriod(start, end, loc)
			if !ps.Equal(local(c.start)) || !pe.Equal(local(c.end)) {
				t.Errorf("previous = %s to %s, want %s to %s", ps.In(loc).Format("2006-01-02 15:04 MST"),
					pe.In(loc).Format("2006-01-02 15:04 MST"), c.start, c.end)
			}
		})
	}
}

// A rolling window that crosses the end of DST is 721 hours, and the window
// before it is 721 hours too, ending where it starts.
func TestPreviousPeriodRollingKeepsTheLength(t *testing.T) {
	loc := chicago(t)
	r := &Report{PeriodKind: PeriodRolling, TimeRangeDays: 30}
	start, end := r.ResolvePeriod(time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC), loc)
	if h := end.Sub(start).Hours(); h != 721 {
		t.Fatalf("30 days across the DST change span %v hours, want 721", h)
	}
	ps, pe := r.PreviousPeriod(start, end, loc)
	if !pe.Equal(start) || pe.Sub(ps) != end.Sub(start) {
		t.Errorf("previous = %v to %v, want the 721 hours ending at %v", ps, pe, start)
	}
	// nil is UTC.
	if ps2, pe2 := r.PreviousPeriod(start, end, nil); !ps2.Equal(ps) || !pe2.Equal(pe) {
		t.Errorf("nil location gave %v to %v, want %v to %v", ps2, pe2, ps, pe)
	}
}

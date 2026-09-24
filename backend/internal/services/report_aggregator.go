// Package services - report_aggregator.go assembles the data a rendered report
// needs: which monitors are in scope, and each one's uptime, downtime, incident
// history, and SLA outcome over the report's time range.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ReportAggregatorService computes report data from monitors, incidents, and
// the report's own scope definition.
type ReportAggregatorService struct {
	db *gorm.DB
	// settings supplies the report timezone and the default SLA target. Nil is
	// tolerated (keeping the service usable in tests): timezone falls back to
	// UTC, SLA target falls back to models.DefaultSLATargetPercent.
	settings *SettingsService
}

// NewReportAggregatorService returns a service bound to db.
func NewReportAggregatorService(db *gorm.DB, settings *SettingsService) *ReportAggregatorService {
	return &ReportAggregatorService{db: db, settings: settings}
}

// reportLocation is the configured report zone, or UTC.
func (s *ReportAggregatorService) reportLocation(ctx context.Context) *time.Location {
	if s.settings == nil {
		return time.UTC
	}
	return s.settings.ReportLocation(ctx)
}

// MeasurableWindow narrows a reporting window to the part of it a monitor
// actually existed for, and reports whether any of it is measurable at all.
//
// Uptime measured over the whole window regardless of the monitor's age made
// the same outage look smaller the further back the window reached: a monitor
// added yesterday reported ~100% over ninety days no matter how badly it
// behaved. Measuring from creation instead makes every window describe only
// the period the monitor was actually being watched.
//
// A monitor created at or after the window's end returns false: it has
// nothing to say about that period, and averaging it in as a perfect score
// would be inventing a result.
//
// Shared between this aggregator and GetSummaryReportHandler (api package),
// which applied this exact clamp first - api importing from services is the
// existing direction, so the definition lives here rather than there.
func MeasurableWindow(createdAt, start, end time.Time) (time.Time, bool) {
	from := start
	if createdAt.After(from) {
		from = createdAt
	}
	if !from.Before(end) {
		return time.Time{}, false
	}
	return from, true
}

// EffectiveSLATarget resolves the SLA target a monitor is held to: its own
// override when set, otherwise the instance-wide default.
func EffectiveSLATarget(monitorOverride *float64, systemDefault float64) float64 {
	if monitorOverride != nil {
		return *monitorOverride
	}
	return systemDefault
}

// ReportMetrics is one monitor's contribution to a report.
type ReportMetrics struct {
	MonitorID   uuid.UUID `json:"monitor_id"`
	MonitorName string    `json:"monitor_name"`
	Uptime      float64   `json:"uptime"` // percentage over the range
	// DowntimeMinutes is fractional on purpose. Truncating to whole minutes
	// made every outage shorter than 60s count as zero, so a report could show
	// incidents alongside 100% uptime.
	DowntimeMinutes float64 `json:"downtime_minutes"`
	IncidentCount   int     `json:"incident_count"`
	// SLATarget is always populated now - the monitor's own override, or the
	// instance-wide default when it has none. There is no longer a monitor
	// with "no SLA": every monitor is held to at least the system default.
	SLATarget *float64          `json:"sla_target"`
	SLAMet    bool              `json:"sla_met"`
	Incidents []IncidentSummary `json:"incidents"`
}

// IncidentSummary is one incident as it appears in a report.
type IncidentSummary struct {
	ID        uuid.UUID  `json:"id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	// Duration is the incident's overlap with the report window, in minutes -
	// not its total length, which may extend beyond the window on either side.
	Duration        float64 `json:"duration_minutes"`
	Severity        string  `json:"severity"`
	Status          string  `json:"status"` // "ongoing" or "resolved"
	RootCause       string  `json:"root_cause"`
	ResolutionNotes string  `json:"resolution_notes"`
}

// UptimeSeriesPoint is one sample of the cumulative-uptime-to-date line: the
// uptime percentage across every monitor in scope, from the report's start
// through Date.
type UptimeSeriesPoint struct {
	Date   time.Time `json:"date"`
	Uptime float64   `json:"uptime"`
}

// ReportData is the fully aggregated payload handed to a renderer.
type ReportData struct {
	ReportName        string          `json:"report_name"`
	CustomTitle       *string         `json:"custom_title"`
	CustomDescription *string         `json:"custom_description"`
	TimeRangeStart    time.Time       `json:"time_range_start"`
	TimeRangeEnd      time.Time       `json:"time_range_end"`
	Metrics           []ReportMetrics `json:"metrics"`
	// Warnings names monitors that could not be aggregated. A report is a
	// compliance artifact, so a dropped monitor is surfaced rather than
	// silently omitted from the results.
	Warnings []string `json:"warnings,omitempty"`
	// UptimeSeries is the cumulative-uptime-to-date line the Uptime Report
	// graphs, aggregated across every monitor in scope.
	UptimeSeries []UptimeSeriesPoint `json:"uptime_series,omitempty"`
	// EffectiveSLA is the SLA target the graph's reference line is drawn at:
	// the effective target (override or system default) averaged across every
	// monitor in scope.
	EffectiveSLA float64 `json:"effective_sla"`
	// Location is the timezone every timestamp in the rendered report is
	// written in. Carried on the data rather than read by each renderer so a
	// PDF and its HTML equivalent cannot disagree about what time it was.
	// Renderers must use ReportLocation(), which falls back to UTC when unset.
	Location *time.Location `json:"-"`
}

// ReportLocation is the zone a report renders in, defaulting to UTC.
//
// Never falls back to time.Local: that is the server process's zone, which is
// whatever the container was given, which is not a deliberate choice by
// anyone.
func (d *ReportData) ReportLocation() *time.Location {
	if d == nil || d.Location == nil {
		return time.UTC
	}
	return d.Location
}

// AggregateReportData assembles everything a report needs. Monitors that fail
// to aggregate are recorded in Warnings rather than failing the whole report.
func (s *ReportAggregatorService) AggregateReportData(ctx context.Context, report *models.Report, requestedBy uuid.UUID) (*ReportData, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}
	if err := report.ValidatePeriod(); err != nil {
		return nil, fmt.Errorf("report period: %w", err)
	}

	// A report's scope can name monitors, groups, or tags the requester does
	// not own or have been shared. Resolving scope alone is not authorization -
	// it has to be intersected with what this specific user may see, the same
	// way ListAccessibleMonitors does for the monitors list itself.
	var requester models.User
	if err := s.db.WithContext(ctx).Select("is_admin").
		First(&requester, "id = ?", requestedBy).Error; err != nil {
		return nil, fmt.Errorf("resolving report requester: %w", err)
	}

	monitorIDs, err := s.getMonitorIDsForScope(ctx, report.ScopeType, report.ScopeData)
	if err != nil {
		return nil, err
	}

	loc := s.reportLocation(ctx)
	startTime, endTime := report.ResolvePeriod(time.Now(), loc)

	data := &ReportData{
		ReportName:        report.Name,
		CustomTitle:       report.CustomTitle,
		CustomDescription: report.CustomDescription,
		TimeRangeStart:    startTime,
		TimeRangeEnd:      endTime,
		Metrics:           []ReportMetrics{},
	}

	// An empty scope resolves to no monitors; return an empty report rather than
	// querying with an empty IN clause.
	if len(monitorIDs) == 0 {
		return data, nil
	}

	q := s.db.WithContext(ctx).Where("id IN ?", monitorIDs)
	if !requester.IsAdmin {
		// Same access rule as MonitorService.ListAccessibleMonitors: owned or
		// explicitly shared. A monitor named in scope that fails this check is
		// silently excluded rather than erroring, so a stale group/tag scope
		// degrades to "fewer monitors in the report" instead of failing it.
		q = q.Where(
			"owner_id = ? OR id IN (SELECT monitor_id FROM monitor_sharing WHERE shared_with_user_id = ?)",
			requestedBy, requestedBy,
		)
	}
	var monitors []models.Monitor
	if err := q.Find(&monitors).Error; err != nil {
		return nil, fmt.Errorf("loading monitors for report: %w", err)
	}

	systemDefaultSLA := models.DefaultSLATargetPercent
	if s.settings != nil {
		systemDefaultSLA = s.settings.DefaultSLATarget(ctx)
	}

	// A monitor that did not exist for any part of the window has nothing to
	// say about it, and is left out entirely rather than averaged in as a
	// perfect score it never earned - the same clamp GetSummaryReportHandler
	// already applied, now shared via MeasurableWindow.
	measurable := make([]models.Monitor, 0, len(monitors))
	windowStart := make(map[uuid.UUID]time.Time, len(monitors))
	var slaSum float64
	for i := range monitors {
		from, ok := MeasurableWindow(monitors[i].CreatedAt, startTime, endTime)
		if !ok {
			continue
		}
		measurable = append(measurable, monitors[i])
		windowStart[monitors[i].ID] = from
		slaSum += EffectiveSLATarget(monitors[i].SLATarget, systemDefaultSLA)
	}

	for i := range measurable {
		metrics, err := s.calculateMonitorMetrics(ctx, measurable[i],
			windowStart[measurable[i].ID], endTime, systemDefaultSLA)
		if err != nil {
			data.Warnings = append(data.Warnings,
				fmt.Sprintf("monitor %q (%s) omitted: %v", measurable[i].Name, measurable[i].ID, err))
			continue
		}
		data.Metrics = append(data.Metrics, metrics)
	}

	if len(measurable) > 0 {
		data.EffectiveSLA = round2(slaSum / float64(len(measurable)))
		series, err := s.computeUptimeSeries(ctx, measurable, windowStart, startTime, endTime)
		if err != nil {
			data.Warnings = append(data.Warnings, fmt.Sprintf("uptime graph unavailable: %v", err))
		} else {
			data.UptimeSeries = series
		}
	}

	return data, nil
}

// getMonitorIDsForScope resolves a report's scope to the monitor IDs it covers.
func (s *ReportAggregatorService) getMonitorIDsForScope(ctx context.Context, scopeType string, scope models.ReportScope) ([]uuid.UUID, error) {
	if err := scope.Validate(scopeType); err != nil {
		return nil, err
	}

	switch scopeType {
	case models.ScopeTypeMonitors:
		// Taken as given; a missing monitor simply contributes nothing.
		return scope.MonitorIDs, nil

	case models.ScopeTypeGroups:
		var ids []uuid.UUID
		if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
			Where("group_id IN ?", scope.GroupIDs).
			Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving group scope: %w", err)
		}
		return ids, nil

	case models.ScopeTypeTags:
		// tags is a JSONB array on monitors; @> matches a monitor carrying the
		// tag. Any of the listed tags qualifies the monitor.
		var ids []uuid.UUID
		q := s.db.WithContext(ctx).Model(&models.Monitor{})
		conds := s.db.Session(&gorm.Session{NewDB: true})
		for i, tag := range scope.Tags {
			cond := "tags @> ?"
			arg := fmt.Sprintf("[%q]", tag)
			if i == 0 {
				conds = conds.Where(cond, arg)
			} else {
				conds = conds.Or(cond, arg)
			}
		}
		if err := q.Where(conds).Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving tag scope: %w", err)
		}
		return ids, nil

	case models.ScopeTypeTypes:
		// Resolved at generation time, not stored, so a report scoped to "all
		// DNS monitors" picks up monitors added since it was defined.
		var ids []uuid.UUID
		if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
			Where("type IN ?", scope.Types).
			Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving type scope: %w", err)
		}
		return ids, nil

	default:
		return nil, fmt.Errorf("unknown scope_type %q", scopeType)
	}
}

// calculateMonitorMetrics computes one monitor's uptime, SLA outcome, and
// incident summary over [startTime, endTime]. systemDefaultSLA is the
// instance-wide default, used whenever the monitor carries no override of its
// own.
func (s *ReportAggregatorService) calculateMonitorMetrics(
	ctx context.Context,
	monitor models.Monitor,
	startTime, endTime time.Time,
	systemDefaultSLA float64,
) (ReportMetrics, error) {
	target := EffectiveSLATarget(monitor.SLATarget, systemDefaultSLA)
	metrics := ReportMetrics{
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		SLATarget:   &target,
		Incidents:   []IncidentSummary{},
	}

	// Select incidents that OVERLAP the window, not merely those that start
	// inside it. An incident that began before the range and is still open
	// contributes real downtime to it; filtering on start_time alone drops that
	// downtime and reports uptime that is too high.
	var incidents []models.Incident
	if err := s.db.WithContext(ctx).
		Where("monitor_id = ?", monitor.ID).
		Where("start_time <= ?", endTime).
		Where("end_time IS NULL OR end_time >= ?", startTime).
		Order("start_time ASC").
		Find(&incidents).Error; err != nil {
		return metrics, fmt.Errorf("loading incidents: %w", err)
	}

	summaries, totalDowntimeMinutes := summarizeIncidents(incidents, startTime, endTime)
	metrics.Incidents = summaries
	metrics.DowntimeMinutes = totalDowntimeMinutes
	metrics.IncidentCount = len(incidents)
	metrics.Uptime = uptimePercent(startTime, endTime, totalDowntimeMinutes)
	metrics.SLAMet = metrics.Uptime >= target

	return metrics, nil
}

// summarizeIncidents converts incidents to report summaries and totals the
// downtime they contribute to [startTime, endTime]. Each incident is clamped to
// the window at both ends, so downtime falling outside the reporting range is
// never counted - an incident that started before the window contributes only
// the part inside it, and one that ended after the window is cut at the end.
//
// Kept free of the database so the window arithmetic can be tested directly;
// it is the part of reporting most likely to be quietly wrong.
func summarizeIncidents(incidents []models.Incident, startTime, endTime time.Time) ([]IncidentSummary, float64) {
	summaries := make([]IncidentSummary, 0, len(incidents))
	total := 0.0

	for i := range incidents {
		incident := incidents[i]

		startT := incident.StartTime
		if startT.Before(startTime) {
			startT = startTime
		}
		// An open incident runs to the end of the window; a closed one ends when
		// it ended, unless that is past the window.
		endT := endTime
		if incident.EndTime != nil && incident.EndTime.Before(endTime) {
			endT = *incident.EndTime
		}

		// Kept fractional rather than truncated to whole minutes: a 40-second
		// outage is not zero downtime, and four of them are not zero either.
		durationMinutes := endT.Sub(startT).Minutes()
		if durationMinutes < 0 {
			durationMinutes = 0
		}
		total += durationMinutes

		summaries = append(summaries, IncidentSummary{
			ID:              incident.ID,
			StartTime:       incident.StartTime,
			EndTime:         incident.EndTime,
			Duration:        durationMinutes,
			Severity:        incident.Severity,
			Status:          incident.Status(),
			RootCause:       incident.RootCause,
			ResolutionNotes: incident.ResolutionNotes,
		})
	}

	return summaries, total
}

// uptimePercent is the share of the window not spent in downtime. Overlapping
// incidents can sum past the window length, so the result floors at zero rather
// than going negative.
func uptimePercent(startTime, endTime time.Time, downtimeMinutes float64) float64 {
	totalMinutes := endTime.Sub(startTime).Minutes()
	if totalMinutes <= 0 {
		return 0
	}
	up := totalMinutes - downtimeMinutes
	if up < 0 {
		up = 0
	}
	return up / totalMinutes * 100
}

// uptimeSeriesPoints is how many samples the cumulative-uptime graph plots.
// Enough to show shape over a typical reporting window without turning into
// noise, and small enough that the whole series costs one incident query per
// monitor rather than one per monitor per sample.
const uptimeSeriesPoints = 30

// monitorIncidentWindows is the per-monitor intermediate representation
// computeUptimeSeries builds from the database and hands to
// computeUptimeSeriesFromIncidents - one incident query's worth of data per
// monitor, reused across every sample point. It also lets that arithmetic be
// exercised without a database in tests - see
// TestComputeUptimeSeries/computeUptimeSeriesFromIncidents.
type monitorIncidentWindows struct {
	id        uuid.UUID
	from      time.Time
	incidents []incidentWindow
}

type incidentWindow struct {
	start time.Time
	end   *time.Time
}

// computeUptimeSeries samples cumulative uptime at evenly spaced points across
// [start, end]. windowStart carries each monitor's own measurable start (its
// creation time, when that is later than start), so a monitor added mid-period
// contributes only the minutes it actually existed for at every sample.
func (s *ReportAggregatorService) computeUptimeSeries(
	ctx context.Context,
	monitors []models.Monitor,
	windowStart map[uuid.UUID]time.Time,
	start, end time.Time,
) ([]UptimeSeriesPoint, error) {
	if len(monitors) == 0 || !end.After(start) {
		return nil, nil
	}

	// One incident query per monitor, covering the whole window - reused for
	// every sample point below instead of re-querying per point.
	byMonitor := make([]monitorIncidentWindows, 0, len(monitors))
	for _, m := range monitors {
		from := windowStart[m.ID]
		var incidents []models.Incident
		if err := s.db.WithContext(ctx).
			Where("monitor_id = ?", m.ID).
			Where("start_time <= ?", end).
			Where("end_time IS NULL OR end_time >= ?", from).
			Order("start_time ASC").
			Find(&incidents).Error; err != nil {
			return nil, fmt.Errorf("loading incidents for uptime series: %w", err)
		}
		windows := make([]incidentWindow, 0, len(incidents))
		for _, inc := range incidents {
			windows = append(windows, incidentWindow{start: inc.StartTime, end: inc.EndTime})
		}
		byMonitor = append(byMonitor, monitorIncidentWindows{id: m.ID, from: from, incidents: windows})
	}

	return computeUptimeSeriesFromIncidents(byMonitor, start, end), nil
}

// computeUptimeSeriesFromIncidents is computeUptimeSeries's arithmetic, free
// of the database so it can be unit tested directly against a synthetic set
// of incidents.
func computeUptimeSeriesFromIncidents(byMonitor []monitorIncidentWindows, start, end time.Time) []UptimeSeriesPoint {
	points := make([]UptimeSeriesPoint, 0, uptimeSeriesPoints)
	step := end.Sub(start) / time.Duration(uptimeSeriesPoints)
	for i := 1; i <= uptimeSeriesPoints; i++ {
		sampleEnd := start.Add(step * time.Duration(i))
		if i == uptimeSeriesPoints {
			sampleEnd = end
		}

		var totalMinutes, downMinutes float64
		for _, mi := range byMonitor {
			if !mi.from.Before(sampleEnd) {
				// Not measurable yet at this sample point.
				continue
			}
			asIncidents := make([]models.Incident, 0, len(mi.incidents))
			for _, w := range mi.incidents {
				asIncidents = append(asIncidents, models.Incident{StartTime: w.start, EndTime: w.end})
			}
			_, minutes := summarizeIncidents(asIncidents, mi.from, sampleEnd)
			totalMinutes += sampleEnd.Sub(mi.from).Minutes()
			downMinutes += minutes
		}

		if totalMinutes <= 0 {
			// No monitor in scope existed yet at this sample - emitting a point
			// here would fabricate a perfect score for a period with no data.
			continue
		}
		points = append(points, UptimeSeriesPoint{
			Date:   sampleEnd,
			Uptime: aggregateUptimePercent(totalMinutes, downMinutes),
		})
	}
	return points
}

// aggregateUptimePercent is uptimePercent generalized to pre-summed
// total/down minutes across possibly many monitors, each contributing only
// its own measurable time, rather than a single [start,end] span.
func aggregateUptimePercent(totalMinutes, downMinutes float64) float64 {
	if totalMinutes <= 0 {
		return 100
	}
	up := totalMinutes - downMinutes
	if up < 0 {
		up = 0
	}
	return round2(up / totalMinutes * 100)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

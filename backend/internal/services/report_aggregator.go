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
}

// NewReportAggregatorService returns a service bound to db.
func NewReportAggregatorService(db *gorm.DB) *ReportAggregatorService {
	return &ReportAggregatorService{db: db}
}

// ReportMetrics is one monitor's contribution to a report.
type ReportMetrics struct {
	MonitorID       uuid.UUID `json:"monitor_id"`
	MonitorName     string    `json:"monitor_name"`
	Uptime          float64   `json:"uptime"` // percentage over the range
	DowntimeMinutes int       `json:"downtime_minutes"`
	IncidentCount   int       `json:"incident_count"`
	SLATarget       *float64  `json:"sla_target"`
	// SLAMet is meaningful only when SLATarget is non-nil; a monitor with no
	// target is not "failing", it is simply not under an SLA.
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
	Duration        int    `json:"duration_minutes"`
	Severity        string `json:"severity"`
	Status          string `json:"status"` // "ongoing" or "resolved"
	RootCause       string `json:"root_cause"`
	ResolutionNotes string `json:"resolution_notes"`
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
	// compliance artifact, so a dropped monitor is surfaced rather than silently
	// omitted from the results.
	Warnings []string `json:"warnings,omitempty"`
}

// AggregateReportData assembles everything a report needs. Monitors that fail to
// aggregate are recorded in Warnings rather than failing the whole report.
func (s *ReportAggregatorService) AggregateReportData(ctx context.Context, report *models.Report) (*ReportData, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}
	if report.TimeRangeDays <= 0 {
		return nil, fmt.Errorf("report time_range_days must be greater than zero, got %d", report.TimeRangeDays)
	}

	monitorIDs, err := s.getMonitorIDsForScope(ctx, report.ScopeType, report.ScopeData)
	if err != nil {
		return nil, err
	}

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -report.TimeRangeDays)

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

	var monitors []models.Monitor
	if err := s.db.WithContext(ctx).Where("id IN ?", monitorIDs).Find(&monitors).Error; err != nil {
		return nil, fmt.Errorf("loading monitors for report: %w", err)
	}

	for i := range monitors {
		metrics, err := s.calculateMonitorMetrics(ctx, monitors[i], startTime, endTime)
		if err != nil {
			data.Warnings = append(data.Warnings,
				fmt.Sprintf("monitor %q (%s) omitted: %v", monitors[i].Name, monitors[i].ID, err))
			continue
		}
		data.Metrics = append(data.Metrics, metrics)
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

	default:
		return nil, fmt.Errorf("unknown scope_type %q", scopeType)
	}
}

// calculateMonitorMetrics computes one monitor's uptime and incident summary
// over [startTime, endTime].
func (s *ReportAggregatorService) calculateMonitorMetrics(
	ctx context.Context,
	monitor models.Monitor,
	startTime, endTime time.Time,
) (ReportMetrics, error) {
	metrics := ReportMetrics{
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		SLATarget:   monitor.SLATarget,
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

	if monitor.SLATarget != nil {
		metrics.SLAMet = metrics.Uptime >= *monitor.SLATarget
	}

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
func summarizeIncidents(incidents []models.Incident, startTime, endTime time.Time) ([]IncidentSummary, int) {
	summaries := make([]IncidentSummary, 0, len(incidents))
	total := 0

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

		durationMinutes := int(endT.Sub(startT).Minutes())
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
func uptimePercent(startTime, endTime time.Time, downtimeMinutes int) float64 {
	totalMinutes := int(endTime.Sub(startTime).Minutes())
	if totalMinutes <= 0 {
		return 0
	}
	up := totalMinutes - downtimeMinutes
	if up < 0 {
		up = 0
	}
	return float64(up) / float64(totalMinutes) * 100
}

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// PortService stores port state and events for the PortMonitor, and (Task 9)
// serves ports, events and summaries to the API.
type PortService struct {
	db        *gorm.DB
	metrics   *MetricsStore
	incidents *IncidentService
	settings  *SettingsService
}

func NewPortService(db *gorm.DB, metrics *MetricsStore, incidents *IncidentService, settings *SettingsService) *PortService {
	return &PortService{db: db, metrics: metrics, incidents: incidents, settings: settings}
}

// PortStateUpdate is what one stats poll writes back to an interface.
type PortStateUpdate struct {
	OperStatus, AdminStatus string
	SpeedBps                int64
	// LastChangeSeconds < 0 leaves the stored value.
	LastChangeSeconds int64
	// OperChangedAt, when set, records when the link last changed state.
	OperChangedAt   *time.Time
	Conditions      []string
	ConditionsSince map[string]time.Time
}

// PortEventEnd closes an open span event (flapping, errors, saturated,
// slow_link).
type PortEventEnd struct {
	InterfaceID uuid.UUID
	Kind        string
	At          time.Time
	Detail      map[string]any
}

// CollectedInterfaces are the present interfaces the stats poll reads.
func (s *PortService) CollectedInterfaces(ctx context.Context, deviceID uuid.UUID) ([]models.DeviceInterface, error) {
	var out []models.DeviceInterface
	err := s.db.WithContext(ctx).
		Where("device_id = ? AND present AND COALESCE(collect, collect_default)", deviceID).
		Order("if_index").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing collected interfaces: %w", err)
	}
	return out, nil
}

// SavePortState writes an interface's live state and active conditions.
func (s *PortService) SavePortState(ctx context.Context, interfaceID uuid.UUID, u PortStateUpdate) error {
	updates := map[string]any{
		"oper_status":      u.OperStatus,
		"admin_status":     u.AdminStatus,
		"speed_bps":        u.SpeedBps,
		"conditions":       models.ConditionSet(u.Conditions),
		"conditions_since": models.TimeMap(u.ConditionsSince),
		"updated_at":       time.Now(),
	}
	if u.LastChangeSeconds >= 0 {
		updates["last_change_seconds"] = u.LastChangeSeconds
	}
	if u.OperChangedAt != nil {
		updates["oper_changed_at"] = *u.OperChangedAt
	}
	if err := s.db.WithContext(ctx).Model(&models.DeviceInterface{}).Where("id = ?", interfaceID).Updates(updates).Error; err != nil {
		return fmt.Errorf("saving port state: %w", err)
	}
	return nil
}

// RecordPortEvents inserts new events (span events with no end) and ends
// open span events, in one transaction. The tracker uses the same detail
// keys at start and end (e.g. "per_minute", "util_pct", "speed_bps"), so
// ending an event nests its end detail under an "end" key instead of merging
// it in at the top level: the start figures stay readable, and the clearing
// figures live at detail.end.*. When there is no end detail (e.g. the link
// went down while a traffic condition was active) the merge is skipped
// entirely — ended_at is still set, but no "end": {} is written.
func (s *PortService) RecordPortEvents(ctx context.Context, starts []models.PortEvent, ends []PortEventEnd) error {
	if len(starts) == 0 && len(ends) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(starts) > 0 {
			if err := tx.Create(&starts).Error; err != nil {
				return fmt.Errorf("recording port events: %w", err)
			}
		}
		for _, e := range ends {
			if len(e.Detail) == 0 {
				if err := tx.Exec(`UPDATE port_events SET ended_at = ?
					WHERE interface_id = ? AND kind = ? AND ended_at IS NULL`,
					e.At, e.InterfaceID, e.Kind).Error; err != nil {
					return fmt.Errorf("ending %s event: %w", e.Kind, err)
				}
				continue
			}
			detail, err := json.Marshal(e.Detail)
			if err != nil {
				return fmt.Errorf("marshalling end detail for %s event: %w", e.Kind, err)
			}
			if err := tx.Exec(`UPDATE port_events SET ended_at = ?, detail = detail || jsonb_build_object('end', ?::jsonb)
				WHERE interface_id = ? AND kind = ? AND ended_at IS NULL`,
				e.At, string(detail), e.InterfaceID, e.Kind).Error; err != nil {
				return fmt.Errorf("ending %s event: %w", e.Kind, err)
			}
		}
		return nil
	})
}

// SaveStatsRun records when a device's stats poll ran and how long it took.
func (s *PortService) SaveStatsRun(ctx context.Context, deviceID uuid.UUID, at time.Time, took time.Duration) error {
	return s.db.WithContext(ctx).Exec(`UPDATE devices SET last_stats_at = ?, last_stats_duration_ms = ? WHERE id = ?`,
		at, int(took.Milliseconds()), deviceID).Error
}

// SiteName names a site for alert text.
func (s *PortService) SiteName(ctx context.Context, siteID uuid.UUID) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM sites WHERE id = ?", siteID).Scan(&name).Error
	return name, err
}

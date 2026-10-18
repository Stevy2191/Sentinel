package services

import (
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// FaceplateDeviceState is what a realistic drawing needs to go dark when
// Sentinel is not hearing from the device: its status and when it was last
// seen and last read.
type FaceplateDeviceState struct {
	Status       string     `json:"status"`
	StatusDetail string     `json:"status_detail"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	LastStatsAt  *time.Time `json:"last_stats_at"`
	PollInterval int        `json:"poll_interval"`
}

func faceplateDeviceState(d *DeviceView) FaceplateDeviceState {
	return FaceplateDeviceState{Status: d.Status, StatusDetail: d.StatusDetail, LastSeenAt: d.LastSeenAt,
		LastStatsAt: d.LastStatsAt, PollInterval: d.PollInterval}
}

// FaceplateIndicators are a drawing's lights that are not ports.
type FaceplateIndicators struct {
	// DriveBay is a DriveBay* state, for a drawing with a drive bay.
	DriveBay string `json:"drive_bay,omitempty"`
}

// DriveBayStates is DriveBayMonitor.State.
type DriveBayStates interface {
	State(deviceID uuid.UUID) string
}

// SetDriveBays lets DevicePorts report drive bay lights.
func (s *PortService) SetDriveBays(b DriveBayStates) { s.bays = b }

// indicators is the device's non-port lights, when a unit is drawn with a
// drive bay; nil otherwise.
func (s *PortService) indicators(d *DeviceView, units []portmon.UnitFaceplate) *FaceplateIndicators {
	if s.bays == nil {
		return nil
	}
	for _, u := range units {
		if u.Drawing == "" {
			continue
		}
		if dr := portmon.DrawingFor(d.EffectiveModel, d.SysDescr); dr != nil && dr.DriveBayDisk != "" {
			return &FaceplateIndicators{DriveBay: s.bays.State(d.ID)}
		}
	}
	return nil
}

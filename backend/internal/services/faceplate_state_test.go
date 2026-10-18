package services

import (
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestFaceplateDeviceState(t *testing.T) {
	seen, stats := time.Date(2026, 10, 18, 10, 42, 0, 0, time.UTC), time.Date(2026, 10, 18, 10, 41, 0, 0, time.UTC)
	d := &DeviceView{Device: models.Device{Status: models.DeviceStatusError, StatusDetail: "timeout", LastSeenAt: &seen,
		LastStatsAt: &stats, PollInterval: 60}}
	s := faceplateDeviceState(d)
	if s.Status != "error" || s.StatusDetail != "timeout" || s.LastSeenAt != &seen || s.LastStatsAt != &stats || s.PollInterval != 60 {
		t.Errorf("state %+v", s)
	}
}

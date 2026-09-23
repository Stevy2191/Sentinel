package services

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestApplyMonitorUpdatesSLATarget(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }

	t.Run("nil update leaves the existing override alone", func(t *testing.T) {
		target := &models.Monitor{SLATarget: f64(99.5)}
		applyMonitorUpdates(target, &models.Monitor{})
		if target.SLATarget == nil || *target.SLATarget != 99.5 {
			t.Errorf("SLATarget = %v, want unchanged 99.5", target.SLATarget)
		}
	})

	t.Run("a real value sets the override", func(t *testing.T) {
		target := &models.Monitor{}
		applyMonitorUpdates(target, &models.Monitor{SLATarget: f64(99.9)})
		if target.SLATarget == nil || *target.SLATarget != 99.9 {
			t.Errorf("SLATarget = %v, want 99.9", target.SLATarget)
		}
	})

	t.Run("an explicit zero clears an existing override", func(t *testing.T) {
		target := &models.Monitor{SLATarget: f64(99.5)}
		applyMonitorUpdates(target, &models.Monitor{SLATarget: f64(0)})
		if target.SLATarget != nil {
			t.Errorf("SLATarget = %v, want nil (cleared)", target.SLATarget)
		}
	})
}

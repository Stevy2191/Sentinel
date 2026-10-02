package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// DeviceHealthData is a device's Health section: its live custom metrics.
// The profiles it lists on the device page are left out (they carry ids).
type DeviceHealthData struct {
	DeviceID   *uuid.UUID              `json:"device_id,omitempty"`
	DeviceName string                  `json:"device_name"`
	Metrics    []services.HealthMetric `json:"metrics"`
}

type deviceHealthWidget struct {
	devices DeviceReader
	health  HealthFunc
}

func (deviceHealthWidget) Type() string { return "device_health" }

func (deviceHealthWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateDeviceConfig(raw)
}

func (deviceHealthWidget) Subjects(raw json.RawMessage) Subjects { return deviceSubjects(raw) }

func (deviceHealthWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w deviceHealthWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Devices) == 0 {
		return nil, ErrNoData
	}
	d, err := w.devices.Get(ctx, in.Visible.Devices[0])
	if err != nil {
		return nil, ErrNoData
	}
	h, err := w.health(ctx, d)
	if err != nil {
		return nil, err
	}
	if h == nil || len(h.Metrics) == 0 {
		return nil, ErrNoData
	}
	return DeviceHealthData{DeviceID: idFor(in.Viewer, d.ID), DeviceName: d.Name, Metrics: h.Metrics}, nil
}

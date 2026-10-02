package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// deviceConfig is the config of widgets about one device.
type deviceConfig struct {
	DeviceID uuid.UUID `json:"device_id"`
}

func validateDeviceConfig(raw json.RawMessage) (json.RawMessage, error) {
	var c deviceConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.DeviceID == uuid.Nil {
		return nil, fieldErr("device_id", "is required")
	}
	return json.Marshal(c)
}

func deviceSubjects(raw json.RawMessage) Subjects {
	var c deviceConfig
	_ = json.Unmarshal(raw, &c)
	if c.DeviceID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Devices: []uuid.UUID{c.DeviceID}}
}

// PortGridData is a device's faceplate with live port status.
type PortGridData struct {
	DeviceID   *uuid.UUID              `json:"device_id,omitempty"`
	DeviceName string                  `json:"device_name"`
	Model      string                  `json:"model"`
	Ports      []services.PortView     `json:"ports"`
	Faceplates []portmon.UnitFaceplate `json:"faceplates"`
}

type portGridWidget struct {
	devices DeviceReader
	ports   PortReader
}

func (portGridWidget) Type() string { return "port_grid" }

func (portGridWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateDeviceConfig(raw)
}

func (portGridWidget) Subjects(raw json.RawMessage) Subjects { return deviceSubjects(raw) }

func (portGridWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w portGridWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Devices) == 0 {
		return nil, ErrNoData
	}
	d, err := w.devices.Get(ctx, in.Visible.Devices[0])
	if errors.Is(err, services.ErrDeviceNotFound) {
		return nil, ErrNoData
	}
	if err != nil {
		return nil, err
	}
	view, err := w.ports.DevicePorts(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(view.Ports) == 0 {
		return nil, ErrNoData
	}
	if in.Viewer.Public {
		for i := range view.Ports {
			trimPort(&view.Ports[i])
		}
	}
	return PortGridData{DeviceID: idFor(in.Viewer, d.ID), DeviceName: d.Name, Model: d.EffectiveModel,
		Ports: view.Ports, Faceplates: view.Faceplates}, nil
}

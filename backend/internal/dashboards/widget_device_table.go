package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

var deviceStatuses = map[string]bool{"pending": true, "up": true, "down": true, "paused": true, "error": true}

type deviceTableConfig struct {
	SiteID   uuid.UUID `json:"site_id"`
	Types    []string  `json:"types,omitempty"`
	Statuses []string  `json:"statuses,omitempty"`
}

// DeviceRow is one device. Host is set only for logged-in viewers.
type DeviceRow struct {
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	Name            string     `json:"name"`
	Host            string     `json:"host,omitempty"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	VendorModel     string     `json:"vendor_model"`
	LastSeenAt      *time.Time `json:"last_seen_at"`
	Availability30d *float64   `json:"availability_30d"`
	OpenIncidents   int        `json:"open_incidents"`
}

// DeviceTableData is a site's devices, sorted as the devices list sorts them.
type DeviceTableData struct {
	Devices []DeviceRow `json:"devices"`
}

type deviceTableWidget struct {
	devices   DeviceReader
	incidents IncidentReader
}

func (deviceTableWidget) Type() string { return "device_table" }

func (deviceTableWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c deviceTableConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.SiteID == uuid.Nil {
		return nil, fieldErr("site_id", "is required")
	}
	for _, t := range c.Types {
		if !models.ValidDeviceTypes[t] {
			return nil, fieldErr("types", "include "+t+", which is not a device type")
		}
	}
	for _, s := range c.Statuses {
		if !deviceStatuses[s] {
			return nil, fieldErr("statuses", "include "+s+", which is not a device status")
		}
	}
	return json.Marshal(c)
}

func (deviceTableWidget) Subjects(raw json.RawMessage) Subjects {
	var c deviceTableConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Sites: []uuid.UUID{c.SiteID}}
}

func (deviceTableWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w deviceTableWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c deviceTableConfig
	_ = json.Unmarshal(raw, &c)
	if len(in.Visible.Sites) == 0 {
		return nil, ErrNoData
	}
	site := in.Visible.Sites[0]
	list, err := w.devices.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &site})
	if err != nil {
		return nil, err
	}
	keep := func(set []string, v string) bool {
		if len(set) == 0 {
			return true
		}
		for _, s := range set {
			if s == v {
				return true
			}
		}
		return false
	}
	var rows []DeviceRow
	var ids []uuid.UUID
	for _, d := range list {
		if !keep(c.Types, d.EffectiveType) || !keep(c.Statuses, d.Status) {
			continue
		}
		row := DeviceRow{DeviceID: idFor(in.Viewer, d.ID), Name: d.Name, Type: d.EffectiveType, Status: d.Status,
			VendorModel: strings.TrimSpace(d.EffectiveVendor + " " + d.EffectiveModel),
			LastSeenAt:  d.LastSeenAt, Availability30d: d.Availability30d}
		if !in.Viewer.Public {
			row.Host = d.Host
		}
		rows = append(rows, row)
		ids = append(ids, d.ID)
	}
	counts, err := w.incidents.OpenCountsByDevice(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].OpenIncidents = counts[ids[i]]
	}
	if rows == nil {
		rows = []DeviceRow{}
	}
	return DeviceTableData{Devices: rows}, nil
}

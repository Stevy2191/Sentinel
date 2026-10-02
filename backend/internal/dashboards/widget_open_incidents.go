package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type openIncidentsConfig struct {
	Scope    string      `json:"scope"`
	SiteID   *uuid.UUID  `json:"site_id,omitempty"`
	Devices  []uuid.UUID `json:"devices,omitempty"`
	Monitors []uuid.UUID `json:"monitors,omitempty"`
	Limit    int         `json:"limit"`
}

// IncidentRow is one open incident: who, what and since when. No root
// cause, notes, URLs or hosts.
type IncidentRow struct {
	IncidentID      *uuid.UUID `json:"incident_id,omitempty"`
	SubjectType     string     `json:"subject_type"`
	SubjectName     string     `json:"subject_name"`
	SiteName        string     `json:"site_name"`
	Condition       string     `json:"condition"`
	Severity        string     `json:"severity"`
	StartTime       time.Time  `json:"start_time"`
	DurationSeconds int        `json:"duration_seconds"`
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	MonitorID       *uuid.UUID `json:"monitor_id,omitempty"`
	PortIfIndex     *int       `json:"port_if_index,omitempty"`
}

// OpenIncidentsData is the newest open incidents and how many there are.
type OpenIncidentsData struct {
	Incidents []IncidentRow `json:"incidents"`
	Total     int64         `json:"total"`
}

type openIncidentsWidget struct {
	incidents IncidentReader
}

// incidentViewer is v for the incident list; a public viewer reads as an
// admin, since an admin published the dashboard.
func incidentViewer(v Viewer) *services.IncidentViewer {
	if v.Public {
		return &services.IncidentViewer{IsAdmin: true}
	}
	return &services.IncidentViewer{UserID: v.UserID, IsAdmin: v.IsAdmin}
}

func (openIncidentsWidget) Type() string { return "open_incidents" }

func (openIncidentsWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c openIncidentsConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Devices, c.Monitors = dedupe(c.Devices), dedupe(c.Monitors)
	switch c.Scope {
	case "site":
		if c.SiteID == nil {
			return nil, fieldErr("site_id", "is required")
		}
		c.Devices, c.Monitors = nil, nil
	case "devices":
		if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
			return nil, err
		}
		c.SiteID, c.Monitors = nil, nil
	case "monitors":
		if err := checkCount("monitors", len(c.Monitors), 1, maxMonitors); err != nil {
			return nil, err
		}
		c.SiteID, c.Devices = nil, nil
	case "all":
		c.SiteID, c.Devices, c.Monitors = nil, nil, nil
	default:
		return nil, fieldErr("scope", "must be site, devices, monitors or all")
	}
	if c.Limit == 0 {
		c.Limit = 20
	}
	if err := checkCount("limit", c.Limit, 5, 50); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (openIncidentsWidget) Subjects(raw json.RawMessage) Subjects {
	var c openIncidentsConfig
	_ = json.Unmarshal(raw, &c)
	switch c.Scope {
	case "site":
		if c.SiteID != nil {
			return Subjects{Sites: []uuid.UUID{*c.SiteID}}
		}
	case "devices":
		return Subjects{Devices: c.Devices}
	case "monitors":
		return Subjects{Monitors: c.Monitors}
	case "all":
		return Subjects{Broad: true}
	}
	return Subjects{}
}

func (openIncidentsWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w openIncidentsWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c openIncidentsConfig
	_ = json.Unmarshal(raw, &c)
	opts := services.IncidentListOptions{Viewer: incidentViewer(in.Viewer), Status: models.IncidentStatusOngoing,
		Page: 1, Limit: c.Limit, Desc: true}
	switch c.Scope {
	case "site":
		// An empty filter would list every incident, and a public viewer
		// reads as an admin, so an empty visible set is no data.
		if len(in.Visible.Sites) == 0 {
			return nil, ErrNoData
		}
		site := in.Visible.Sites[0]
		opts.SiteID = &site
	case "devices":
		if len(in.Visible.Devices) == 0 {
			return nil, ErrNoData
		}
		opts.DeviceIDs = in.Visible.Devices
	case "monitors":
		if len(in.Visible.Monitors) == 0 {
			return nil, ErrNoData
		}
		opts.MonitorIDs = in.Visible.Monitors
	}
	rows, total, err := w.incidents.ListIncidents(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := OpenIncidentsData{Incidents: make([]IncidentRow, 0, len(rows)), Total: total}
	for _, r := range rows {
		row := IncidentRow{IncidentID: idFor(in.Viewer, r.ID), SubjectType: r.SubjectType, SubjectName: r.SubjectName,
			SiteName: r.SiteName, Severity: r.Severity, StartTime: r.StartTime,
			DurationSeconds: int(in.Now.Sub(r.StartTime).Seconds()), PortIfIndex: r.PortIfIndex}
		if r.Condition != nil {
			row.Condition = *r.Condition
		}
		if r.DeviceID != nil {
			row.DeviceID = idFor(in.Viewer, *r.DeviceID)
		}
		if r.MonitorID != nil {
			row.MonitorID = idFor(in.Viewer, *r.MonitorID)
		}
		out.Incidents = append(out.Incidents, row)
	}
	return out, nil
}

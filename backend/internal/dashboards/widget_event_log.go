package dashboards

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type eventLogConfig struct {
	SiteID   *uuid.UUID `json:"site_id,omitempty"`
	DeviceID *uuid.UUID `json:"device_id,omitempty"`
	Limit    int        `json:"limit"`
}

// EventItem is one line of the log: a port event, or an incident opening or
// closing. No free text (root causes, notes) is ever included.
type EventItem struct {
	Time       time.Time  `json:"time"`
	Kind       string     `json:"kind"`
	DeviceID   *uuid.UUID `json:"device_id,omitempty"`
	DeviceName string     `json:"device_name"`
	IfIndex    *int       `json:"if_index,omitempty"`
	PortLabel  string     `json:"port_label"`
	PortName   string     `json:"port_name"`
	Subject    string     `json:"subject"`
	Condition  string     `json:"condition"`
	Severity   string     `json:"severity"`
	IncidentID *uuid.UUID `json:"incident_id,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// EventLogData is the newest events first.
type EventLogData struct {
	Items []EventItem `json:"items"`
}

type eventLogWidget struct {
	ports     PortReader
	incidents IncidentReader
}

func (eventLogWidget) Type() string { return "event_log" }

func (eventLogWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c eventLogConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	switch {
	case c.SiteID != nil && c.DeviceID != nil:
		return nil, fieldErr("device_id", "choose a site or a device, not both")
	case c.SiteID == nil && c.DeviceID == nil:
		return nil, fieldErr("site_id", "choose a site or a device")
	}
	if c.Limit == 0 {
		c.Limit = 20
	}
	if err := checkCount("limit", c.Limit, 10, 50); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (eventLogWidget) Subjects(raw json.RawMessage) Subjects {
	var c eventLogConfig
	_ = json.Unmarshal(raw, &c)
	switch {
	case c.SiteID != nil:
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	case c.DeviceID != nil:
		return Subjects{Devices: []uuid.UUID{*c.DeviceID}}
	}
	return Subjects{}
}

func (eventLogWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w eventLogWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c eventLogConfig
	_ = json.Unmarshal(raw, &c)
	// The subject comes from the access-filtered set, never the raw config.
	// Every incident of a visible site or device is visible to its viewer.
	filter := services.PortEventFilter{Page: 1, Limit: c.Limit}
	opts := services.IncidentListOptions{
		Viewer: &services.IncidentViewer{IsAdmin: true},
		Page:   1, Limit: c.Limit, Subject: "device", Desc: true,
	}
	switch {
	case c.SiteID != nil:
		if len(in.Visible.Sites) == 0 {
			return nil, ErrNoData
		}
		site := in.Visible.Sites[0]
		filter.SiteID, opts.SiteID = &site, &site
	case c.DeviceID != nil:
		if len(in.Visible.Devices) == 0 {
			return nil, ErrNoData
		}
		dev := in.Visible.Devices[0]
		filter.DeviceID, opts.DeviceID = &dev, &dev
	default:
		return nil, ErrNoData
	}
	events, _, err := w.ports.Events(ctx, filter)
	if err != nil {
		return nil, err
	}
	incs, _, err := w.incidents.ListIncidents(ctx, opts)
	if err != nil {
		return nil, err
	}
	items := make([]EventItem, 0, len(events)+2*len(incs))
	for _, e := range events {
		ifIndex := e.IfIndex
		items = append(items, EventItem{Time: e.StartedAt, Kind: e.Kind, DeviceID: idFor(in.Viewer, e.DeviceID),
			DeviceName: e.DeviceName, IfIndex: &ifIndex, PortLabel: e.PortLabel, PortName: e.PortName, EndedAt: e.EndedAt})
	}
	for _, inc := range incs {
		base := EventItem{Subject: inc.SubjectName, Severity: inc.Severity, IncidentID: idFor(in.Viewer, inc.ID),
			IfIndex: inc.PortIfIndex}
		if inc.DeviceID != nil {
			base.DeviceID = idFor(in.Viewer, *inc.DeviceID)
		}
		if inc.Condition != nil {
			base.Condition = *inc.Condition
		}
		opened := base
		opened.Time, opened.Kind = inc.StartTime, "incident_opened"
		items = append(items, opened)
		if inc.EndTime != nil {
			closed := base
			closed.Time, closed.Kind = *inc.EndTime, "incident_closed"
			items = append(items, closed)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	if len(items) > c.Limit {
		items = items[:c.Limit]
	}
	return EventLogData{Items: items}, nil
}

package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type monitorsConfig struct {
	Monitors []uuid.UUID `json:"monitors,omitempty"`
	Agents   []uuid.UUID `json:"agents,omitempty"`
	Style    string      `json:"style"`
	Window   string      `json:"window"`
}

// UptimeBucket is one bar: an hour (24h) or a day (90d).
type UptimeBucket struct {
	Label  string  `json:"label"`
	Status string  `json:"status"`
	Uptime float64 `json:"uptime"`
}

// MonitorRow is one monitor. Its URL and target are never included.
type MonitorRow struct {
	MonitorID      *uuid.UUID     `json:"monitor_id,omitempty"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Status         string         `json:"status"`
	ResponseTimeMs int            `json:"response_time_ms"`
	LastCheckAt    *time.Time     `json:"last_check_at"`
	Buckets        []UptimeBucket `json:"buckets,omitempty"`
}

// AgentRow is one server agent: its name and whether it is reporting.
type AgentRow struct {
	AgentID       *uuid.UUID `json:"agent_id,omitempty"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}

// MonitorsData is the chosen monitors and agents, in config order.
type MonitorsData struct {
	Style    string       `json:"style"`
	Window   string       `json:"window"`
	Monitors []MonitorRow `json:"monitors"`
	Agents   []AgentRow   `json:"agents"`
}

type monitorsWidget struct {
	monitors  MonitorReader
	checks    CheckReader
	incidents IncidentReader
	agents    AgentReader
}

func (monitorsWidget) Type() string { return "monitors" }

func (monitorsWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c monitorsConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Monitors, c.Agents = dedupe(c.Monitors), dedupe(c.Agents)
	if len(c.Monitors)+len(c.Agents) == 0 {
		return nil, fieldErr("monitors", "choose at least one monitor or server")
	}
	if err := checkCount("monitors", len(c.Monitors), 0, maxMonitors); err != nil {
		return nil, err
	}
	if err := checkCount("agents", len(c.Agents), 0, maxAgents); err != nil {
		return nil, err
	}
	switch c.Style {
	case "":
		c.Style = "list"
	case "list", "bars":
	default:
		return nil, fieldErr("style", "must be list or bars")
	}
	switch c.Window {
	case "":
		c.Window = "24h"
	case "24h", "90d":
	default:
		return nil, fieldErr("window", "must be 24h or 90d")
	}
	return json.Marshal(c)
}

func (monitorsWidget) Subjects(raw json.RawMessage) Subjects {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	return Subjects{Monitors: c.Monitors, Agents: c.Agents}
}

func (monitorsWidget) Refresh(raw json.RawMessage, _ string) time.Duration {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	if c.Style == "bars" && c.Window == "90d" {
		return 15 * time.Minute
	}
	return statusRefresh
}

func (w monitorsWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	out := MonitorsData{Style: c.Style, Window: c.Window, Monitors: []MonitorRow{}, Agents: []AgentRow{}}

	if len(in.Visible.Monitors) > 0 {
		list, err := w.monitors.MonitorsByIDs(ctx, in.Visible.Monitors)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]*models.Monitor, len(list))
		for i := range list {
			byID[list[i].ID] = &list[i]
		}
		for _, id := range in.Visible.Monitors {
			m := byID[id]
			if m == nil {
				continue
			}
			row := MonitorRow{MonitorID: idFor(in.Viewer, m.ID), Name: m.Name, Type: m.Type, Status: m.CurrentStatus,
				ResponseTimeMs: m.LastResponseTimeMs, LastCheckAt: m.LastCheckAt}
			if !m.Enabled {
				row.Status = "paused"
			}
			if c.Style == "bars" {
				b, err := w.buckets(ctx, m, c.Window, in.Now.UTC())
				if err != nil {
					return nil, err
				}
				row.Buckets = b
			}
			out.Monitors = append(out.Monitors, row)
		}
	}

	if len(in.Visible.Agents) > 0 {
		all, err := w.agents.List(ctx)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]models.Agent, len(all))
		for _, a := range all {
			byID[a.ID] = a
		}
		for _, id := range in.Visible.Agents {
			a, ok := byID[id]
			if !ok {
				continue
			}
			out.Agents = append(out.Agents, AgentRow{AgentID: idFor(in.Viewer, a.ID), Name: a.Name, Status: a.Status, LastHeartbeat: a.LastHeartbeat})
		}
	}
	return out, nil
}

// buckets is a monitor's uptime bars, from the same helpers the status page
// and the monitor page use.
func (w monitorsWidget) buckets(ctx context.Context, m *models.Monitor, window string, now time.Time) ([]UptimeBucket, error) {
	if window == "90d" {
		checks, err := w.checks.ChecksInRange(ctx, m.ID, now.AddDate(0, 0, -services.UptimeDailyDays), now, 0, 0)
		if err != nil {
			return nil, err
		}
		return toBuckets(services.DailyUptimeBuckets(checks, now), "date"), nil
	}
	start := now.Add(-24 * time.Hour)
	checks, err := w.checks.ChecksInRange(ctx, m.ID, start, now, 0, 0)
	if err != nil {
		return nil, err
	}
	incidents, err := w.incidents.GetOverlappingIncidents(ctx, m.ID, start, now)
	if err != nil {
		return nil, err
	}
	maint, err := w.monitors.GetMaintenanceHistory(ctx, m.ID, start, now)
	if err != nil {
		return nil, err
	}
	return toBuckets(services.HourlyUptimeBuckets(checks, incidents, maint, now, m.CreatedAt), "bucket_start"), nil
}

func toBuckets(entries []map[string]any, labelKey string) []UptimeBucket {
	out := make([]UptimeBucket, len(entries))
	for i, e := range entries {
		label, _ := e[labelKey].(string)
		status, _ := e["status"].(string)
		uptime, _ := e["uptime"].(float64)
		out[i] = UptimeBucket{Label: label, Status: status, Uptime: uptime}
	}
	return out
}

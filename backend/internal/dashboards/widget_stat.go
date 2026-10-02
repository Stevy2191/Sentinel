package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type statConfig struct {
	Metric    string     `json:"metric"`
	DeviceID  *uuid.UUID `json:"device_id,omitempty"`
	Instance  string     `json:"instance,omitempty"`
	SiteID    *uuid.UUID `json:"site_id,omitempty"`
	Mode      string     `json:"mode"`
	Warn      *float64   `json:"warn,omitempty"`
	Crit      *float64   `json:"crit,omitempty"`
	Direction string     `json:"direction"`
	Sparkline bool       `json:"sparkline"`
	Range     string     `json:"range"`
}

// StatData is one number and how it compares with its thresholds.
type StatData struct {
	Label string                 `json:"label"`
	Unit  string                 `json:"unit"`
	Value float64                `json:"value"`
	Level string                 `json:"level"`
	Mode  string                 `json:"mode"`
	Range string                 `json:"range"`
	Spark []services.MetricPoint `json:"spark,omitempty"`
}

type statWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (statWidget) Type() string { return "stat" }

// statLevel is ok, warn or crit: crossing a threshold in direction counts
// from the threshold itself.
func statLevel(v float64, warn, crit *float64, direction string) string {
	past := func(t *float64) bool {
		if t == nil {
			return false
		}
		if direction == "below" {
			return v <= *t
		}
		return v >= *t
	}
	switch {
	case past(crit):
		return "crit"
	case past(warn):
		return "warn"
	}
	return "ok"
}

func (statWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c statConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.Metric == "" {
		return nil, fieldErr("metric", "is required")
	}
	if !services.KnownMetric(c.Metric) {
		return nil, fieldErr("metric", c.Metric+" is not a known metric")
	}
	switch {
	case c.DeviceID != nil && c.SiteID != nil:
		return nil, fieldErr("device_id", "choose a device or a site total, not both")
	case c.DeviceID == nil && c.SiteID == nil:
		return nil, fieldErr("device_id", "choose a device or a site")
	}
	if c.SiteID != nil {
		c.Instance = ""
	}
	switch c.Mode {
	case "":
		c.Mode = "latest"
	case "latest", "average":
	default:
		return nil, fieldErr("mode", "must be latest or average")
	}
	switch c.Direction {
	case "":
		c.Direction = "above"
	case "above", "below":
	default:
		return nil, fieldErr("direction", "must be above or below")
	}
	if c.Warn != nil && c.Crit != nil {
		if (c.Direction == "above" && *c.Warn > *c.Crit) || (c.Direction == "below" && *c.Warn < *c.Crit) {
			return nil, fieldErr("warn", "must come before the critical threshold")
		}
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (statWidget) Subjects(raw json.RawMessage) Subjects {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	switch {
	case c.SiteID != nil:
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	case c.DeviceID != nil:
		return Subjects{Devices: []uuid.UUID{*c.DeviceID}}
	}
	return Subjects{}
}

func (statWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w statWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	if !services.KnownMetric(c.Metric) {
		return nil, ErrNoData
	}
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	q := services.MetricsQuery{Metrics: []string{c.Metric}, From: to.Add(-rangeSpan(rng)), To: to, Sum: true}
	var parts []string
	if c.SiteID != nil {
		ids, err := siteDeviceIDs(ctx, w.devices, *c.SiteID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs = ids
		if allPortMetrics(q.Metrics) {
			ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			if len(ifs) == 0 {
				return nil, ErrNoData
			}
			q.InterfaceIDs = ifs
		}
		parts = append(parts, "Site total")
	} else {
		if len(in.Visible.Devices) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs = in.Visible.Devices[:1]
		if c.Instance != "" {
			q.Instances = []string{c.Instance}
			labels, err := w.metrics.InstanceLabels(ctx, q.DeviceIDs, q.Metrics)
			if err != nil {
				return nil, err
			}
			if l := labels[services.InstanceKey{DeviceID: q.DeviceIDs[0], Metric: c.Metric, Instance: c.Instance}]; l != "" {
				parts = append(parts, l)
			}
		}
	}
	res, err := w.metrics.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(res.Series) == 0 || len(res.Series[0].Points) == 0 {
		return nil, ErrNoData
	}
	pts := res.Series[0].Points
	value := pts[len(pts)-1].Avg
	if c.Mode == "average" {
		var sum float64
		for _, p := range pts {
			sum += p.Avg
		}
		value = sum / float64(len(pts))
	}
	defs, err := w.metrics.Describe(ctx, q.Metrics)
	if err != nil {
		return nil, err
	}
	def := defs[c.Metric]
	label := def.Label
	if label == "" {
		label = c.Metric
	}
	out := StatData{Label: strings.Join(append(parts, label), " · "), Unit: def.Unit, Value: value,
		Level: statLevel(value, c.Warn, c.Crit, c.Direction), Mode: c.Mode, Range: rng}
	if c.Sparkline {
		out.Spark = pts
	}
	return out, nil
}

package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	sourceMetrics     = "metrics"
	sourceSiteTraffic = "site_traffic"
)

type timeseriesConfig struct {
	Source    string      `json:"source"`
	Metrics   []string    `json:"metrics,omitempty"`
	Devices   []uuid.UUID `json:"devices,omitempty"`
	Instances []string    `json:"instances,omitempty"`
	// SiteID: with source metrics, a site total; with site_traffic, the site.
	SiteID *uuid.UUID `json:"site_id,omitempty"`
	// View is site_traffic's chart: internet (north-south) or east_west.
	View  string `json:"view,omitempty"`
	Range string `json:"range"`
}

// SeriesLine is one drawn line. Key is unique within one response.
type SeriesLine struct {
	Key    string                 `json:"key"`
	Label  string                 `json:"label"`
	Metric string                 `json:"metric"`
	Unit   string                 `json:"unit"`
	Points []services.MetricPoint `json:"points"`
}

// TimeseriesData is a chart's response. It never carries subject ids.
type TimeseriesData struct {
	Range       string       `json:"range"`
	Resolution  string       `json:"resolution"`
	StepSeconds int          `json:"step_seconds"`
	Lines       []SeriesLine `json:"lines"`
}

type timeseriesWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (timeseriesWidget) Type() string { return "timeseries" }

func (timeseriesWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c timeseriesConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	switch c.Source {
	case "", sourceMetrics:
		c.Source, c.View = sourceMetrics, ""
		if c.Metrics, err = validateMetricKeys(c.Metrics, 1, maxMetrics); err != nil {
			return nil, err
		}
		c.Devices = dedupe(c.Devices)
		switch {
		case c.SiteID != nil && len(c.Devices) > 0:
			return nil, fieldErr("devices", "choose devices or a site total, not both")
		case c.SiteID == nil:
			if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
				return nil, err
			}
		}
		if err := checkCount("instances", len(c.Instances), 0, maxInstances); err != nil {
			return nil, err
		}
		if c.SiteID != nil {
			c.Instances = nil
		}
	case sourceSiteTraffic:
		if c.SiteID == nil {
			return nil, fieldErr("site_id", "is required")
		}
		switch c.View {
		case "":
			c.View = "internet"
		case "internet", "east_west":
		default:
			return nil, fieldErr("view", "must be internet or east_west")
		}
		c.Metrics, c.Devices, c.Instances = nil, nil, nil
	default:
		return nil, fieldErr("source", "must be metrics or site_traffic")
	}
	return json.Marshal(c)
}

func (timeseriesWidget) Subjects(raw json.RawMessage) Subjects {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID != nil {
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	}
	return Subjects{Devices: c.Devices}
}

func (timeseriesWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w timeseriesWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	from := to.Add(-rangeSpan(rng))
	if c.Source == sourceSiteTraffic {
		return w.siteTraffic(ctx, c, rng, from, to)
	}

	keys := knownOnly(c.Metrics)
	if len(keys) == 0 {
		return nil, ErrNoData
	}
	q := services.MetricsQuery{Metrics: keys, From: from, To: to}
	siteTotal := c.SiteID != nil
	if siteTotal {
		ids, err := siteDeviceIDs(ctx, w.devices, *c.SiteID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs, q.Sum = ids, true
		if allPortMetrics(keys) {
			ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			if len(ifs) == 0 {
				return nil, ErrNoData
			}
			q.InterfaceIDs = ifs
		}
	} else {
		q.DeviceIDs, q.Instances = in.Visible.Devices, c.Instances
	}
	res, err := w.metrics.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defs, err := w.metrics.Describe(ctx, keys)
	if err != nil {
		return nil, err
	}
	series := res.Series
	if !siteTotal && len(c.Instances) == 0 {
		series = sumPerDeviceMetric(series)
	}
	names := map[uuid.UUID]string{}
	labels := map[services.InstanceKey]string{}
	if !siteTotal {
		if names, err = deviceNames(ctx, w.devices, q.DeviceIDs); err != nil {
			return nil, err
		}
		if labels, err = w.metrics.InstanceLabels(ctx, q.DeviceIDs, keys); err != nil {
			return nil, err
		}
	}
	data := TimeseriesData{Range: rng, Resolution: res.Resolution, StepSeconds: res.StepSeconds, Lines: []SeriesLine{}}
	for _, s := range series {
		def := defs[s.Metric]
		metricLabel := def.Label
		if metricLabel == "" {
			metricLabel = s.Metric
		}
		var parts []string
		switch {
		case siteTotal:
			parts = append(parts, "Site total")
		default:
			if len(q.DeviceIDs) > 1 && s.DeviceID != nil {
				parts = append(parts, names[*s.DeviceID])
			}
			if s.Instance != "" && s.DeviceID != nil {
				label := labels[services.InstanceKey{DeviceID: *s.DeviceID, Metric: s.Metric, Instance: s.Instance}]
				if label == "" {
					label = s.Instance
				}
				parts = append(parts, label)
			}
		}
		parts = append(parts, metricLabel)
		data.Lines = append(data.Lines, SeriesLine{
			Key: fmt.Sprintf("s%d", len(data.Lines)), Label: strings.Join(parts, " · "),
			Metric: s.Metric, Unit: def.Unit, Points: s.Points,
		})
	}
	if noPoints(data.Lines) {
		return nil, ErrNoData
	}
	return data, nil
}

// siteTraffic is the site traffic chart: internet in/out, or east-west.
func (w timeseriesWidget) siteTraffic(ctx context.Context, c timeseriesConfig, rng string, from, to time.Time) (any, error) {
	st, err := w.ports.SiteTraffic(ctx, *c.SiteID, from, to)
	if err != nil {
		return nil, err
	}
	data := TimeseriesData{Range: rng, Resolution: st.Resolution, StepSeconds: st.StepSeconds, Lines: []SeriesLine{}}
	if c.View == "east_west" {
		pts := make([]services.MetricPoint, len(st.EastWest))
		for i, p := range st.EastWest {
			pts[i] = services.MetricPoint{Time: p.Time, Avg: p.Bps, Min: p.Bps, Max: p.Bps}
		}
		data.Lines = append(data.Lines, SeriesLine{Key: "s0", Label: "Inside the site", Metric: "east_west_bps", Unit: "bps", Points: pts})
	} else {
		if !st.WANConfigured {
			return nil, ErrNoData
		}
		down := make([]services.MetricPoint, len(st.NorthSouth))
		up := make([]services.MetricPoint, len(st.NorthSouth))
		for i, p := range st.NorthSouth {
			down[i] = services.MetricPoint{Time: p.Time, Avg: p.InBps, Min: p.InBps, Max: p.InBps}
			up[i] = services.MetricPoint{Time: p.Time, Avg: p.OutBps, Min: p.OutBps, Max: p.OutBps}
		}
		data.Lines = append(data.Lines,
			SeriesLine{Key: "s0", Label: "Download", Metric: "internet_in_bps", Unit: "bps", Points: down},
			SeriesLine{Key: "s1", Label: "Upload", Metric: "internet_out_bps", Unit: "bps", Points: up})
	}
	if noPoints(data.Lines) {
		return nil, ErrNoData
	}
	return data, nil
}

// sumPerDeviceMetric adds up each device's instances of a metric (a
// device's ports) into one line per device and metric, as the device page's
// traffic chart does. Min and Max then equal Avg.
func sumPerDeviceMetric(in []services.MetricSeries) []services.MetricSeries {
	type key struct {
		device uuid.UUID
		metric string
	}
	sums := map[key]map[time.Time]float64{}
	var order []key
	for _, s := range in {
		if s.DeviceID == nil {
			continue
		}
		k := key{*s.DeviceID, s.Metric}
		if sums[k] == nil {
			sums[k] = map[time.Time]float64{}
			order = append(order, k)
		}
		for _, p := range s.Points {
			sums[k][p.Time] += p.Avg
		}
	}
	out := make([]services.MetricSeries, 0, len(order))
	for _, k := range order {
		times := make([]time.Time, 0, len(sums[k]))
		for t := range sums[k] {
			times = append(times, t)
		}
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
		pts := make([]services.MetricPoint, len(times))
		for i, t := range times {
			v := sums[k][t]
			pts[i] = services.MetricPoint{Time: t, Avg: v, Min: v, Max: v}
		}
		dev := k.device
		out = append(out, services.MetricSeries{DeviceID: &dev, Metric: k.metric, Points: pts})
	}
	return out
}

func noPoints(lines []SeriesLine) bool {
	for _, l := range lines {
		if len(l.Points) > 0 {
			return false
		}
	}
	return true
}

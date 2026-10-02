package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type topNConfig struct {
	SiteID  *uuid.UUID  `json:"site_id,omitempty"`
	Devices []uuid.UUID `json:"devices,omitempty"`
	Measure string      `json:"measure"`
	N       int         `json:"n"`
	Range   string      `json:"range"`
}

// TopNItem is one ranked port.
type TopNItem struct {
	DeviceID   *uuid.UUID `json:"device_id,omitempty"`
	DeviceName string     `json:"device_name"`
	IfIndex    int        `json:"if_index"`
	PortName   string     `json:"port_name"`
	Alias      string     `json:"alias"`
	Value      float64    `json:"value"`
}

// TopNData is the ranking and the unit of its values.
type TopNData struct {
	Measure string     `json:"measure"`
	Unit    string     `json:"unit"`
	Range   string     `json:"range"`
	Items   []TopNItem `json:"items"`
}

var topNUnits = map[string]string{services.TopNTraffic: "bps", services.TopNUtilisation: "%", services.TopNErrors: "per_min"}

type topNWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (topNWidget) Type() string { return "top_n" }

func (topNWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c topNConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Devices = dedupe(c.Devices)
	switch {
	case c.SiteID != nil && len(c.Devices) > 0:
		return nil, fieldErr("devices", "choose devices or a site, not both")
	case c.SiteID == nil:
		if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
			return nil, err
		}
	}
	if c.Measure == "" {
		c.Measure = services.TopNTraffic
	}
	if _, ok := topNUnits[c.Measure]; !ok {
		return nil, fieldErr("measure", "must be traffic, utilisation or errors")
	}
	if c.N == 0 {
		c.N = 10
	}
	if err := checkCount("n", c.N, 5, 20); err != nil {
		return nil, err
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (topNWidget) Subjects(raw json.RawMessage) Subjects {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID != nil {
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	}
	return Subjects{Devices: c.Devices}
}

func (topNWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w topNWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	ids := in.Visible.Devices
	if c.SiteID != nil {
		if len(in.Visible.Sites) == 0 {
			return nil, ErrNoData
		}
		var err error
		if ids, err = siteDeviceIDs(ctx, w.devices, in.Visible.Sites[0]); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, ErrNoData
	}
	ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(ifs) == 0 {
		return nil, ErrNoData
	}
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	rows, err := w.metrics.TopN(ctx, services.TopNQuery{DeviceIDs: ids, InterfaceIDs: ifs, Measure: c.Measure,
		From: to.Add(-rangeSpan(rng)), To: to, N: c.N})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNoData
	}
	out := TopNData{Measure: c.Measure, Unit: topNUnits[c.Measure], Range: rng, Items: make([]TopNItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = TopNItem{DeviceID: idFor(in.Viewer, r.DeviceID), DeviceName: r.DeviceName, IfIndex: r.IfIndex,
			PortName: r.PortName, Alias: r.Alias, Value: r.Value}
	}
	return out, nil
}

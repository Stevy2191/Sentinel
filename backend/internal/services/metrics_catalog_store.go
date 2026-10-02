package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// InstanceKey names one series of one device.
type InstanceKey struct {
	DeviceID uuid.UUID
	Metric   string
	Instance string
}

// MetricInstance is one instance of a metric with its human name.
type MetricInstance struct {
	Instance string `json:"instance"`
	Label    string `json:"label"`
}

// DeviceMetric is one metric a device has stored series for, for the
// dashboard editor's pickers.
type DeviceMetric struct {
	Metric    string           `json:"metric"`
	Label     string           `json:"label"`
	Unit      string           `json:"unit"`
	Instances []MetricInstance `json:"instances"`
}

// instanceLabelSQL names a series for people: a custom row's label, else the
// port's name, else the raw instance.
const instanceLabelSQL = `COALESCE(NULLIF(s.label, ''), NULLIF(di.name, ''), s.instance)`

func builtinDef(key string) (MetricDef, bool) {
	for _, d := range MetricCatalogue {
		if d.Key == key {
			return d, true
		}
	}
	return MetricDef{}, false
}

// Describe returns the label and unit of each known key: the built-in
// catalogue, then custom metrics' names and units. Unknown keys are absent.
func (m *MetricsStore) Describe(ctx context.Context, keys []string) (map[string]MetricDef, error) {
	out := make(map[string]MetricDef, len(keys))
	var custom []string
	for _, k := range keys {
		if d, ok := builtinDef(k); ok {
			out[k] = d
		} else {
			custom = append(custom, k)
		}
	}
	if len(custom) == 0 {
		return out, nil
	}
	var rows []struct {
		Key   string
		Name  string
		Units string
	}
	if err := m.db.WithContext(ctx).Table("profile_metrics").Select("key, name, units").
		Where("key IN ?", custom).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("describing custom metrics: %w", err)
	}
	for _, r := range rows {
		out[r.Key] = MetricDef{Key: r.Key, Unit: r.Units, Label: r.Name}
	}
	return out, nil
}

// InstanceLabels returns the human name of every stored series of these
// devices and metrics.
func (m *MetricsStore) InstanceLabels(ctx context.Context, deviceIDs []uuid.UUID, metrics []string) (map[InstanceKey]string, error) {
	out := map[InstanceKey]string{}
	if len(deviceIDs) == 0 || len(metrics) == 0 {
		return out, nil
	}
	var rows []struct {
		DeviceID uuid.UUID
		Metric   string
		Instance string
		Label    string
	}
	err := m.db.WithContext(ctx).Table("metrics.series AS s").
		Select("s.device_id, s.metric, s.instance, "+instanceLabelSQL+" AS label").
		Joins("LEFT JOIN device_interfaces di ON di.id = s.interface_id").
		Where("s.device_id IN ? AND s.metric IN ?", deviceIDs, metrics).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("loading series labels: %w", err)
	}
	for _, r := range rows {
		out[InstanceKey{DeviceID: r.DeviceID, Metric: r.Metric, Instance: r.Instance}] = r.Label
	}
	return out, nil
}

// DeviceMetrics lists every known metric with stored series for a device,
// each with its instances in natural order (port 2 before port 10), sorted by
// label. A metric whose key is no longer known (a deleted custom metric) is
// left out: a widget could not be saved with it.
func (m *MetricsStore) DeviceMetrics(ctx context.Context, deviceID uuid.UUID) ([]DeviceMetric, error) {
	var rows []struct {
		Metric   string
		Instance string
		Label    string
	}
	err := m.db.WithContext(ctx).Table("metrics.series AS s").
		Select("s.metric, s.instance, "+instanceLabelSQL+" AS label").
		Joins("LEFT JOIN device_interfaces di ON di.id = s.interface_id").
		Where("s.device_id = ?", deviceID).
		Order("s.metric, length(s.instance), s.instance").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing device metrics: %w", err)
	}
	var keys []string
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.Metric] && KnownMetric(r.Metric) {
			seen[r.Metric] = true
			keys = append(keys, r.Metric)
		}
	}
	defs, err := m.Describe(ctx, keys)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*DeviceMetric{}
	out := make([]DeviceMetric, 0, len(keys))
	for _, k := range keys {
		def, ok := defs[k]
		if !ok {
			continue
		}
		out = append(out, DeviceMetric{Metric: k, Label: def.Label, Unit: def.Unit, Instances: []MetricInstance{}})
	}
	for i := range out {
		byKey[out[i].Metric] = &out[i]
	}
	for _, r := range rows {
		if d := byKey[r.Metric]; d != nil {
			d.Instances = append(d.Instances, MetricInstance{Instance: r.Instance, Label: r.Label})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

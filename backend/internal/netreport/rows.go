package netreport

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// seriesRow is one metrics.series row.
type seriesRow struct {
	ID          int64      `gorm:"column:id"`
	DeviceID    uuid.UUID  `gorm:"column:device_id"`
	Metric      string     `gorm:"column:metric"`
	Instance    string     `gorm:"column:instance"`
	InterfaceID *uuid.UUID `gorm:"column:interface_id"`
	Label       string     `gorm:"column:label"`
}

// seriesIndex finds a scope's series by port and by device.
type seriesIndex struct {
	byPort   map[uuid.UUID]map[string]int64
	byDevice map[uuid.UUID]map[string][]seriesRow
}

// loadSeries reads the scope's series of these metrics in one query. A
// device deleted since has none: DeviceService.Delete removes its series.
func (b *Builder) loadSeries(ctx context.Context, res *resolved, metrics []string) (*seriesIndex, error) {
	idx := &seriesIndex{byPort: map[uuid.UUID]map[string]int64{}, byDevice: map[uuid.UUID]map[string][]seriesRow{}}
	devices := res.deviceIDs()
	if len(devices) == 0 || len(metrics) == 0 {
		return idx, nil
	}
	var rows []seriesRow
	if err := b.db.WithContext(ctx).Raw(`SELECT id, device_id, metric, instance, interface_id, label
		FROM metrics.series WHERE device_id IN ? AND metric IN ?
		ORDER BY device_id, metric, length(instance), instance`, devices, metrics).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading the scope's series: %w", err)
	}
	for _, r := range rows {
		if r.InterfaceID != nil {
			if idx.byPort[*r.InterfaceID] == nil {
				idx.byPort[*r.InterfaceID] = map[string]int64{}
			}
			idx.byPort[*r.InterfaceID][r.Metric] = r.ID
		}
		if idx.byDevice[r.DeviceID] == nil {
			idx.byDevice[r.DeviceID] = map[string][]seriesRow{}
		}
		idx.byDevice[r.DeviceID][r.Metric] = append(idx.byDevice[r.DeviceID][r.Metric], r)
	}
	return idx, nil
}

// portSeries is the series of metric on each of these ports that has one.
func (x *seriesIndex) portSeries(ports []uuid.UUID, metric string) []int64 {
	var out []int64
	for _, p := range ports {
		if id, ok := x.byPort[p][metric]; ok {
			out = append(out, id)
		}
	}
	return out
}

// deviceSeries is every series of metric on these devices.
func (x *seriesIndex) deviceSeries(devices []uuid.UUID, metric string) []int64 {
	var out []int64
	for _, d := range devices {
		for _, s := range x.byDevice[d][metric] {
			out = append(out, s.ID)
		}
	}
	return out
}

// row is one line of a metric's table.
type row struct {
	// key names the line across a pair's two metrics: "p:<port>",
	// "d:<device>", "s:<site>" or "i:<device>:<instance>".
	key string
	// subject is what the 500 cap counts the line as: its port ("p:<id>")
	// or its device ("d:<id>"); "" for a site total, which is never cut.
	subject string
	name    string
	site    bool  // a site total: leads its table, never ranked
	port    *port // a port's line (running hot lists ports only)
	series  []int64
	// total: the line combines its series per bucket (a device or site total).
	total bool
}

// rowsFor lists one metric's lines in scope order. Site totals come first.
// Then a port metric has a line per port (on a devices scope, a total per
// device over its physical ports), and a device metric a line per instance
// of each device that has one (a stack member's CPU, a sensor). A port with
// no series still gets its line, which shows "No data"; so does a device
// chosen on a devices scope that has no series of a device metric (a sites
// scope lists only the devices that have it).
func rowsFor(res *resolved, m metricInfo, idx *seriesIndex) []row {
	var out []row
	for _, s := range res.sites {
		r := row{key: "s:" + s.ID.String(), name: s.Name + " (site total)", site: true, total: true}
		if m.port() {
			for _, d := range res.devices {
				if d.SiteID == s.ID {
					r.series = append(r.series, idx.portSeries(d.Physical, m.Key)...)
				}
			}
		} else {
			r.series = idx.deviceSeries(s.Devices, m.Key)
		}
		out = append(out, r)
	}
	switch {
	case m.port() && res.scopeType == models.ScopeTypeDevices:
		for _, d := range res.devices {
			out = append(out, row{key: "d:" + d.ID.String(), subject: "d:" + d.ID.String(), name: d.Name, total: true,
				series: idx.portSeries(d.Physical, m.Key)})
		}
	case m.port():
		for i := range res.ports {
			p := &res.ports[i]
			r := row{key: "p:" + p.ID.String(), subject: "p:" + p.ID.String(), name: p.Name, port: p}
			if id, ok := idx.byPort[p.ID][m.Key]; ok {
				r.series = []int64{id}
			}
			out = append(out, r)
		}
	default:
		for _, d := range res.devices {
			if len(idx.byDevice[d.ID][m.Key]) == 0 && res.scopeType == models.ScopeTypeDevices {
				// A chosen device without the metric keeps its line: "No data".
				out = append(out, row{key: "i:" + d.ID.String() + ":", subject: "d:" + d.ID.String(), name: d.Name})
				continue
			}
			for _, s := range idx.byDevice[d.ID][m.Key] {
				out = append(out, row{key: "i:" + d.ID.String() + ":" + s.Instance, subject: "d:" + d.ID.String(),
					name: instanceName(d.Name, s), series: []int64{s.ID}})
			}
		}
	}
	return out
}

// instanceName is "device · instance label"; just the device for a metric
// with one unnamed instance (a UPS reading).
func instanceName(deviceName string, s seriesRow) string {
	label := s.Label
	if label == "" {
		label = s.Instance
	}
	if label == "" {
		return deviceName
	}
	return deviceName + " · " + label
}

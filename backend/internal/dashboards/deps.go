package dashboards

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Per-widget limits (spec §3), matching the metrics query endpoint.
const (
	maxMetrics   = 10
	maxDevices   = 50
	maxInstances = 200
	maxMonitors  = 50
	maxAgents    = 50
)

// MetricsReader is the part of services.MetricsStore widgets use.
type MetricsReader interface {
	Query(ctx context.Context, q services.MetricsQuery) (*services.MetricsResult, error)
	TopN(ctx context.Context, q services.TopNQuery) ([]services.TopNRow, error)
	Describe(ctx context.Context, keys []string) (map[string]services.MetricDef, error)
	InstanceLabels(ctx context.Context, deviceIDs []uuid.UUID, metrics []string) (map[services.InstanceKey]string, error)
}

// PortReader is the part of services.PortService widgets use.
type PortReader interface {
	DevicePorts(ctx context.Context, d *services.DeviceView) (*services.DevicePortsView, error)
	Events(ctx context.Context, f services.PortEventFilter) ([]services.PortEventView, int64, error)
	SiteTraffic(ctx context.Context, siteID uuid.UUID, from, to time.Time) (*services.SiteTraffic, error)
	UPSStatus(ctx context.Context, d *services.DeviceView) (*services.UPSStatusView, error)
	PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error)
}

// DeviceReader is the part of services.DeviceService widgets use.
type DeviceReader interface {
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
	List(ctx context.Context, userID uuid.UUID, isAdmin bool, f services.DeviceFilter) ([]services.DeviceView, error)
}

// HealthFunc is a device's Health section (ProfileService.DeviceHealth with
// its metrics store and incident service bound).
type HealthFunc func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error)

// IncidentReader is the part of services.IncidentService widgets use.
type IncidentReader interface {
	ListIncidents(ctx context.Context, opts services.IncidentListOptions) ([]services.IncidentWithMonitor, int64, error)
	OpenCountsByDevice(ctx context.Context, deviceIDs []uuid.UUID) (map[uuid.UUID]int, error)
	GetOverlappingIncidents(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.Incident, error)
}

// MonitorReader is the part of services.MonitorService widgets use.
type MonitorReader interface {
	MonitorsByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Monitor, error)
	GetMaintenanceHistory(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.MaintenanceHistory, error)
}

// CheckReader is the part of services.CheckService widgets use.
type CheckReader interface {
	GetChecksInRange(ctx context.Context, monitorID uuid.UUID, start, end time.Time, limit, offset int) ([]models.Check, error)
}

// AgentReader is the part of services.AgentService widgets use.
type AgentReader interface {
	List(ctx context.Context) ([]models.Agent, error)
}

// Deps is what the widgets read their data from. Each widget takes only the
// parts it needs, so widgets are tested against the real services in DB
// tests and nothing else. main.go fills it in.
type Deps struct {
	Metrics   MetricsReader
	Ports     PortReader
	Devices   DeviceReader
	Health    HealthFunc
	Incidents IncidentReader
	Monitors  MonitorReader
	Checks    CheckReader
	Agents    AgentReader
}

// NewDefaultRegistry registers every widget type Sentinel ships, in the order
// the editor lists them.
func NewDefaultRegistry(d Deps) *Registry {
	return NewRegistry(
		labelWidget{},
		timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
		statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
	)
}

// deviceNames maps each id to its device's name; missing devices are absent.
func deviceNames(ctx context.Context, d DeviceReader, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		v, err := d.Get(ctx, id)
		if err != nil {
			continue // deleted between the access check and now: its line is dropped
		}
		out[id] = v.Name
	}
	return out, nil
}

// siteDeviceIDs lists a site's devices. The site was access-checked already.
func siteDeviceIDs(ctx context.Context, d DeviceReader, siteID uuid.UUID) ([]uuid.UUID, error) {
	list, err := d.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(list))
	for i, v := range list {
		ids[i] = v.ID
	}
	return ids, nil
}

// allPortMetrics reports whether every key is a built-in port metric.
func allPortMetrics(keys []string) bool {
	for _, k := range keys {
		if len(k) < 3 || k[:3] != "if_" {
			return false
		}
	}
	return len(keys) > 0
}

// validateMetricKeys dedupes keys and checks there are lo to hi known ones.
func validateMetricKeys(keys []string, lo, hi int) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		if !services.KnownMetric(k) {
			return nil, fieldErr("metrics", "include "+k+", which is not a known metric")
		}
		seen[k] = true
		out = append(out, k)
	}
	if err := checkCount("metrics", len(out), lo, hi); err != nil {
		return nil, err
	}
	return out, nil
}

// knownOnly drops keys that are no longer known (a custom metric deleted
// after the widget was saved). Viewing never re-validates; it just shows less.
func knownOnly(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if services.KnownMetric(k) {
			out = append(out, k)
		}
	}
	return out
}

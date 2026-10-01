package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

// UPSIncidents opens and closes UPS condition incidents (IncidentService).
type UPSIncidents interface {
	OpenDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)
	CloseDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)
	OpenDeviceConditionIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)
}

// UPSThresholdSource gives the instance-wide UPS thresholds (SettingsService).
type UPSThresholdSource interface {
	UPSThresholds(ctx context.Context) upsmon.Thresholds
}

// SiteNamer names a site for alerts (PortService).
type SiteNamer interface {
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
}

// UPSMonitor runs a UPS's poll: UPS-MIB readings to samples, and on battery,
// low battery and high load to incidents and alerts.
type UPSMonitor struct {
	metrics    MetricsWriter
	incidents  UPSIncidents
	notifier   Notifier
	client     snmp.Client
	thresholds UPSThresholdSource
	sites      SiteNamer

	now    func() time.Time
	logger *log.Logger

	// overLoad is each UPS's high-load clock (when load first went over the
	// threshold). It is the only state kept between polls: which conditions
	// are active is read from the open incidents every poll, so a pause, a
	// failed close or a restart can never leave the two disagreeing.
	mu       sync.Mutex
	overLoad map[uuid.UUID]*time.Time
}

func NewUPSMonitor(metrics MetricsWriter, incidents UPSIncidents, notifier Notifier, client snmp.Client, thresholds UPSThresholdSource, sites SiteNamer) *UPSMonitor {
	return &UPSMonitor{metrics: metrics, incidents: incidents, notifier: notifier, client: client, thresholds: thresholds,
		sites: sites, now: time.Now, logger: log.Default(), overLoad: map[uuid.UUID]*time.Time{}}
}

// IsUPS reports whether a device's effective type (the user's choice, else
// what inventory detected) is UPS.
func IsUPS(d models.Device) bool {
	if d.DeviceType != nil {
		return *d.DeviceType == models.DeviceTypeUPS
	}
	return d.DeviceTypeDetected == models.DeviceTypeUPS
}

var upsConditionLabel = map[upsmon.Condition]string{
	upsmon.OnBattery: "On battery", upsmon.LowBattery: "Low battery", upsmon.HighLoad: "High load",
}

// thresholdsFor applies a device's overrides to the defaults.
func thresholdsFor(d models.Device, def upsmon.Thresholds) upsmon.Thresholds {
	if d.UPSLowBatteryPct != nil {
		def.LowBatteryPct = float64(*d.UPSLowBatteryPct)
	}
	if d.UPSHighLoadPct != nil {
		def.HighLoadPct = float64(*d.UPSHighLoadPct)
	}
	return def
}

// openConditions reads the device's open UPS incidents: condition -> the
// incident's start.
func (m *UPSMonitor) openConditions(ctx context.Context, deviceID uuid.UUID) (map[upsmon.Condition]time.Time, error) {
	open, err := m.incidents.OpenDeviceConditionIncidents(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	out := map[upsmon.Condition]time.Time{}
	for _, inc := range open {
		if inc.Condition != nil {
			out[upsmon.Condition(*inc.Condition)] = inc.StartTime
		}
	}
	return out, nil
}

// PollUPS reads one UPS and applies the result. A failed read changes
// nothing: an unreachable UPS is the device-down incident's business.
//
// The conditions active before this poll are the device's open incidents,
// so an alert whose incident was closed elsewhere (a pause) starts afresh
// now, an open that failed is retried as a new start, and an incident whose
// condition is no longer active (a close that failed) is closed quietly.
func (m *UPSMonitor) PollUPS(ctx context.Context, d models.Device, t snmp.Target) {
	r, err := snmp.ReadUPS(ctx, m.client, t)
	if err != nil {
		m.logger.Printf("[ups] poll of %s: %v", d.Host, err)
		return
	}
	now := m.now().UTC()
	if r.Any() {
		if err := m.metrics.Write(ctx, d.ID, now, upsPoints(r)); err != nil {
			m.logger.Printf("[ups] writing samples for %s: %v", d.Host, err)
		}
	}
	open, err := m.openConditions(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[ups] listing open incidents for %s: %v", d.Host, err)
		return
	}
	prev := upsmon.Restore(open)
	m.mu.Lock()
	prev.OverLoadSince = m.overLoad[d.ID]
	m.mu.Unlock()
	next, changes := upsmon.Evaluate(prev, r, thresholdsFor(d, m.thresholds.UPSThresholds(ctx)), now)
	m.mu.Lock()
	m.overLoad[d.ID] = next.OverLoadSince
	m.mu.Unlock()

	site := ""
	for _, c := range changes {
		if c.Started {
			inc, opened, err := m.incidents.OpenDeviceConditionIncident(ctx, d.ID, string(c.Condition), c.At, upsProblem(d, c))
			if err != nil {
				m.logger.Printf("[ups] opening %s incident for %s: %v", c.Condition, d.Host, err)
				continue
			}
			if opened {
				m.notify(ctx, d, c, inc, &site)
			}
			continue
		}
		inc, err := m.incidents.CloseDeviceConditionIncident(ctx, d.ID, string(c.Condition), c.At, "")
		if err != nil {
			m.logger.Printf("[ups] closing %s incident for %s: %v", c.Condition, d.Host, err)
			continue
		}
		if inc != nil {
			m.notify(ctx, d, c, inc, &site)
		}
	}
}

// ReconcileUPS closes, without notifying, any UPS incident left open on a
// device that is no longer a UPS, and forgets its state.
func (m *UPSMonitor) ReconcileUPS(ctx context.Context, d models.Device) {
	m.mu.Lock()
	delete(m.overLoad, d.ID)
	m.mu.Unlock()
	open, err := m.incidents.OpenDeviceConditionIncidents(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[ups] listing open incidents for %s: %v", d.Host, err)
		return
	}
	for _, inc := range open {
		if inc.Condition == nil {
			continue
		}
		if _, err := m.incidents.CloseDeviceConditionIncident(ctx, d.ID, *inc.Condition, m.now().UTC(), "Closed: the device is no longer a UPS."); err != nil {
			m.logger.Printf("[ups] closing %s incident for %s: %v", *inc.Condition, d.Host, err)
		}
	}
}

// upsPoints are one poll's samples: every reading the UPS answered.
func upsPoints(r upsmon.Readings) []SamplePoint {
	var out []SamplePoint
	add := func(metric string, v *float64) {
		if v != nil {
			out = append(out, SamplePoint{Metric: metric, Value: *v})
		}
	}
	add(MetricUPSChargePct, r.ChargePct)
	add(MetricUPSRuntimeMin, r.RuntimeMin)
	add(MetricUPSLoadPct, r.LoadPct)
	add(MetricUPSInputV, r.InputV)
	add(MetricUPSOutputV, r.OutputV)
	add(MetricUPSBatteryTempC, r.BatteryTempC)
	if on, known := r.OnBattery(); known {
		v := 0.0
		if on {
			v = 1
		}
		out = append(out, SamplePoint{Metric: MetricUPSOnBattery, Value: v})
	}
	if r.BatteryStatus != 0 {
		out = append(out, SamplePoint{Metric: MetricUPSBatteryStatus, Value: float64(r.BatteryStatus)})
	}
	return out
}

func pct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " %" }

// figures renders "(charge 96 %, 41 min left)" from whichever details exist;
// "" when there are none.
func figures(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	out := " ("
	for i, p := range kept {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out + ")"
}

func detail(c upsmon.Change, key, format string) string {
	if v, ok := c.Detail[key]; ok {
		return fmt.Sprintf(format, strconv.FormatFloat(v, 'f', -1, 64))
	}
	return ""
}

// upsProblem is the alert sentence for a condition starting.
func upsProblem(d models.Device, c upsmon.Change) string {
	who := displayName(d)
	switch c.Condition {
	case upsmon.OnBattery:
		return who + " is on battery" + figures(detail(c, "charge_pct", "charge %s %%"), detail(c, "runtime_min", "%s min left"))
	case upsmon.LowBattery:
		return who + " battery is low" + figures(detail(c, "charge_pct", "charge %s %%"), detail(c, "runtime_min", "%s min left"))
	default:
		return who + " load is high" + figures(detail(c, "load_pct", "%s %%"), detail(c, "threshold_pct", "threshold %s %%"))
	}
}

// upsRecovered is the sentence for a condition ending.
func upsRecovered(d models.Device, c upsmon.Change) string {
	who := displayName(d)
	switch c.Condition {
	case upsmon.OnBattery:
		return who + " is back on mains power"
	case upsmon.LowBattery:
		return who + " battery has recovered" + figures(detail(c, "charge_pct", "charge %s %%"))
	default:
		return who + " load is back to normal" + figures(detail(c, "load_pct", "%s %%"))
	}
}

func (m *UPSMonitor) notify(ctx context.Context, d models.Device, c upsmon.Change, inc *models.Incident, site *string) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	if *site == "" {
		name, err := m.sites.SiteName(ctx, d.SiteID)
		if err != nil || name == "" {
			name = "its site"
		}
		*site = name
	}
	id := d.ID
	msg := &notifications.NotificationMessage{
		DeviceID: &id, SiteName: *site, MonitorName: displayName(d) + " · " + upsConditionLabel[c.Condition],
		MonitorURL: d.Host, Timestamp: c.At,
	}
	if d.NotifyChannels != nil {
		msg.Channels = []string(d.NotifyChannels)
	}
	if inc != nil {
		msg.IncidentID = &inc.ID
	}
	status := "warning"
	if c.Condition == upsmon.LowBattery {
		status = "down"
	}
	if c.Started {
		msg.Status, msg.PreviousStatus, msg.Message = status, "up", upsProblem(d, c)
	} else {
		dur := time.Duration(0)
		if inc != nil {
			dur = time.Duration(inc.DurationSeconds) * time.Second
		}
		msg.Status, msg.PreviousStatus, msg.DowntimeDuration = "recovered", status, dur
		msg.Message = fmt.Sprintf("%s after %s.", upsRecovered(d, c), humanDuration(dur))
	}
	if err := m.notifier.SendNotification(ctx, msg); err != nil {
		m.logger.Printf("[ups] sending %s notification for %s: %v", msg.Status, d.Host, err)
	}
}

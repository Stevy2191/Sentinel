package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// PortStore is the monitor's view of the database (PortService).
type PortStore interface {
	CollectedInterfaces(ctx context.Context, deviceID uuid.UUID) ([]models.DeviceInterface, error)
	SavePortState(ctx context.Context, interfaceID uuid.UUID, u PortStateUpdate) error
	RecordPortEvents(ctx context.Context, starts []models.PortEvent, ends []PortEventEnd) error
	SaveStatsRun(ctx context.Context, deviceID uuid.UUID, at time.Time, took time.Duration) error
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
}

// PortIncidents opens and closes port incidents (IncidentService). Open is
// idempotent per port and condition.
type PortIncidents interface {
	OpenPortIncident(ctx context.Context, deviceID, interfaceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)
	ClosePortIncident(ctx context.Context, interfaceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)
	// OpenPortIncidentsForDevice lists a device's open port incidents (every
	// port, every condition), for PollStats' reconciliation pass.
	OpenPortIncidentsForDevice(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)
}

// MetricsWriter stores samples (MetricsStore).
type MetricsWriter interface {
	Write(ctx context.Context, deviceID uuid.UUID, at time.Time, points []SamplePoint) error
}

// ThresholdSource gives the instance-wide port thresholds (SettingsService).
type ThresholdSource interface {
	PortThresholds(ctx context.Context) portmon.Thresholds
}

// PortMonitor runs each device's stats poll: counters to rates to samples,
// link state and conditions to events, and important ports' problems to
// incidents and alerts.
type PortMonitor struct {
	store      PortStore
	metrics    MetricsWriter
	incidents  PortIncidents
	notifier   Notifier
	client     snmp.Client
	thresholds ThresholdSource

	now    func() time.Time
	logger *log.Logger

	mu      sync.Mutex
	devices map[uuid.UUID]*deviceStats
}

// deviceStats is what the monitor remembers about a device between polls.
type deviceStats struct {
	// hc: nil until learned; false when the device has no ifHC counters.
	hc         *bool
	lastUptime int64
	ports      map[uuid.UUID]*portStats
}

type portStats struct {
	last    *portmon.Reading
	tracker *portmon.Tracker
}

func NewPortMonitor(store PortStore, metrics MetricsWriter, incidents PortIncidents, notifier Notifier, client snmp.Client, thresholds ThresholdSource) *PortMonitor {
	return &PortMonitor{
		store: store, metrics: metrics, incidents: incidents, notifier: notifier, client: client, thresholds: thresholds,
		now: time.Now, logger: log.Default(), devices: map[uuid.UUID]*deviceStats{},
	}
}

func (m *PortMonitor) state(deviceID uuid.UUID) *deviceStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	ds := m.devices[deviceID]
	if ds == nil {
		ds = &deviceStats{lastUptime: -1, ports: map[uuid.UUID]*portStats{}}
		m.devices[deviceID] = ds
	}
	return ds
}

// Forget drops what the monitor remembers about a device (it was deleted).
func (m *PortMonitor) Forget(deviceID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.devices, deviceID)
}

func (ds *deviceStats) port(row models.DeviceInterface) *portStats {
	ps := ds.ports[row.ID]
	if ps == nil {
		ps = &portStats{tracker: portmon.NewTracker(snapshotOf(row))}
		ds.ports[row.ID] = ps
	}
	return ps
}

// pruneStalePorts drops every ds.ports entry whose interface id is not among
// this poll's collected rows: the port stopped being collected (or was
// deleted, or is simply not present right now), so its tracker must not go
// on remembering conditions that are stale by the time collection resumes.
func pruneStalePorts(ds *deviceStats, rows []models.DeviceInterface) {
	if len(ds.ports) == 0 {
		return
	}
	keep := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		keep[r.ID] = true
	}
	for id := range ds.ports {
		if !keep[id] {
			delete(ds.ports, id)
		}
	}
}

// snapshotOf restores a tracker from the stored interface row.
func snapshotOf(row models.DeviceInterface) portmon.Snapshot {
	active := map[portmon.Condition]time.Time{}
	for _, c := range row.Conditions {
		at := row.ConditionsSince[c]
		if at.IsZero() {
			at = row.UpdatedAt
		}
		active[portmon.Condition(c)] = at
	}
	var changed time.Time
	if row.OperChangedAt != nil {
		changed = *row.OperChangedAt
	}
	return portmon.Snapshot{
		OperUp: row.OperStatus == "up", AdminUp: row.AdminStatus != "down", SpeedBps: row.SpeedBps,
		LastChangeSeconds: row.LastChangeSeconds, OperChangedAt: changed, Active: active,
	}
}

// portThresholds applies a port's overrides to the defaults.
func portThresholds(row models.DeviceInterface, def portmon.Thresholds) portmon.Thresholds {
	th := def
	if row.ErrorThresholdPerMin != nil {
		th.ErrorsPerMin = float64(*row.ErrorThresholdPerMin)
	}
	if row.UtilThresholdPct != nil {
		th.UtilPct = float64(*row.UtilThresholdPct)
	}
	if row.DownGraceSeconds != nil {
		th.DownGrace = time.Duration(*row.DownGraceSeconds) * time.Second
	}
	return th
}

// PollStats reads one device's collected interfaces and applies the results.
// uptimeSeconds is the device's sysUpTime from this poll (< 0 if unknown).
func (m *PortMonitor) PollStats(ctx context.Context, d models.Device, t snmp.Target, uptimeSeconds int64) {
	began := m.now()
	defer func() {
		if err := m.store.SaveStatsRun(ctx, d.ID, began, m.now().Sub(began)); err != nil {
			m.logger.Printf("[snmp] recording stats run for %s: %v", d.Host, err)
		}
	}()

	rows, err := m.store.CollectedInterfaces(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[snmp] listing ports of %s: %v", d.Host, err)
		return
	}
	ds := m.state(d.ID)
	pruneStalePorts(ds, rows)
	if len(rows) == 0 {
		return
	}
	hc := t.Credential.Version != models.SNMPVersion1 && (ds.hc == nil || *ds.hc)
	indexes := make([]int, len(rows))
	for i, r := range rows {
		indexes[i] = r.IfIndex
	}
	stats, err := snmp.ReadStats(ctx, m.client, t, indexes, hc)
	if err != nil {
		m.logger.Printf("[snmp] stats poll of %s: %v", d.Host, err)
	}
	if len(stats) == 0 {
		return
	}
	if hc && ds.hc == nil {
		learned := false
		for _, st := range stats {
			if st.HaveOctets {
				learned = true
				break
			}
		}
		ds.hc = &learned
		if !learned {
			return // no 64-bit counters: the next poll reads the 32-bit ones
		}
	}

	rebooted := uptimeSeconds >= 0 && ds.lastUptime >= 0 && uptimeSeconds < ds.lastUptime
	ds.lastUptime = uptimeSeconds
	now := m.now()
	def := m.thresholds.PortThresholds(ctx)
	interval := time.Duration(d.PollInterval) * time.Second
	var points []SamplePoint
	var starts []models.PortEvent
	var ends []PortEventEnd
	site := ""

	for _, row := range rows {
		st, ok := stats[row.IfIndex]
		if !ok || !st.HaveStatus {
			continue
		}
		ps := ds.port(row)
		speed := st.SpeedBps
		if speed <= 0 {
			speed = row.SpeedBps
		}

		var rates *portmon.Rates
		if st.HaveOctets {
			cur := portmon.Reading{
				At: now, UptimeSeconds: uptimeSeconds, HC: st.HC,
				InOctets: st.InOctets, OutOctets: st.OutOctets, InErrors: st.InErrors, OutErrors: st.OutErrors,
				InDiscards: st.InDiscards, OutDiscards: st.OutDiscards, SpeedBps: speed,
			}
			if st.OperUp {
				if r, skip := portmon.ComputeRates(ps.last, cur, interval); skip == portmon.SkipNone {
					rates = &r
				}
			}
			ps.last = &cur
		} else {
			ps.last = nil
		}
		points = append(points, portPoints(row, rates, st.OperUp, speed)...)

		obs := portmon.Observation{
			At: now, OperUp: st.OperUp, AdminUp: st.AdminUp, SpeedBps: speed,
			LastChangeSeconds: st.LastChangeSeconds, UtilPct: -1,
		}
		if rebooted {
			obs.LastChangeSeconds = -1
		}
		if rates != nil {
			obs.HaveRates = true
			obs.ErrorsPerMin = rates.InErrorsPM + rates.OutErrorsPM + rates.InDiscardsPM + rates.OutDiscardsPM
			if rates.InUtilPct != nil {
				obs.UtilPct = math.Max(*rates.InUtilPct, *rates.OutUtilPct)
			}
		}
		usual := int64(0)
		if row.UsualSpeedBps != nil {
			usual = *row.UsualSpeedBps
		}
		res := ps.tracker.Observe(obs, portThresholds(row, def), usual)

		if err := m.savePortState(ctx, row, st, speed, obs.LastChangeSeconds, ps.tracker); err != nil {
			m.logger.Printf("[snmp] saving port %d of %s: %v", row.IfIndex, d.Host, err)
		}
		s, e := portEvents(d.ID, row, res)
		starts, ends = append(starts, s...), append(ends, e...)
		if row.Important {
			m.alert(ctx, d, row, ps.tracker, res, &site)
		}
	}

	m.reconcileOpenIncidents(ctx, d, ds, rows)

	if err := m.store.RecordPortEvents(ctx, starts, ends); err != nil {
		m.logger.Printf("[snmp] recording port events of %s: %v", d.Host, err)
	}
	if err := m.metrics.Write(ctx, d.ID, now, points); err != nil {
		m.logger.Printf("[snmp] writing samples of %s: %v", d.Host, err)
	}
}

// reconcileOpenIncidents closes any open port incident this poll's tracker
// states say should not still be open: its port is no longer important, its
// condition is not in that port's tracker.Active(), or its port is not
// collected/present at all (not among this poll's rows). This catches what a
// changed-condition alert alone cannot: an incident left open by a failed
// close, an in-app config restore, or the ensure loop re-opening what
// UpdatePort just closed while a poll was in flight. These closes are
// reconciliation, not a real recovery, so they do not notify.
func (m *PortMonitor) reconcileOpenIncidents(ctx context.Context, d models.Device, ds *deviceStats, rows []models.DeviceInterface) {
	open, err := m.incidents.OpenPortIncidentsForDevice(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[snmp] listing open port incidents for %s: %v", d.Host, err)
		return
	}
	if len(open) == 0 {
		return
	}
	byID := make(map[uuid.UUID]models.DeviceInterface, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, inc := range open {
		if inc.InterfaceID == nil || inc.Condition == nil {
			continue
		}
		row, present := byID[*inc.InterfaceID]
		stale := false
		switch {
		case !present:
			stale = true
		case !row.Important:
			stale = true
		default:
			if ps := ds.ports[*inc.InterfaceID]; ps != nil {
				if _, active := ps.tracker.Active()[portmon.Condition(*inc.Condition)]; !active {
					stale = true
				}
			}
		}
		if !stale {
			continue
		}
		if _, err := m.incidents.ClosePortIncident(ctx, *inc.InterfaceID, *inc.Condition, m.now().UTC(),
			"Closed: the condition is no longer active."); err != nil {
			m.logger.Printf("[snmp] reconciling %s incident for %s port: %v", *inc.Condition, d.Host, err)
		}
	}
}

// portPoints are one port's samples for this poll: rates when there are any,
// and the link speed while the link is up.
func portPoints(row models.DeviceInterface, r *portmon.Rates, up bool, speed int64) []SamplePoint {
	id := row.ID
	inst := instanceKey(row.IfIndex)
	var out []SamplePoint
	add := func(metric string, v float64) {
		out = append(out, SamplePoint{Metric: metric, Instance: inst, InterfaceID: &id, Value: v})
	}
	if r != nil {
		add(MetricIfInBps, r.InBps)
		add(MetricIfOutBps, r.OutBps)
		if r.InUtilPct != nil {
			add(MetricIfInUtilPct, *r.InUtilPct)
			add(MetricIfOutUtilPct, *r.OutUtilPct)
		}
		add(MetricIfInErrorsPM, r.InErrorsPM)
		add(MetricIfOutErrorsPM, r.OutErrorsPM)
		add(MetricIfInDiscardsPM, r.InDiscardsPM)
		add(MetricIfOutDiscardsPM, r.OutDiscardsPM)
	}
	if up && speed > 0 {
		add(MetricIfSpeedBps, float64(speed))
	}
	return out
}

func (m *PortMonitor) savePortState(ctx context.Context, row models.DeviceInterface, st snmp.IfStats, speed, lastChange int64, tr *portmon.Tracker) error {
	oper, admin := "down", "down"
	if st.OperUp {
		oper = "up"
	}
	if st.AdminUp {
		admin = "up"
	}
	active := tr.Active()
	conds := make([]string, 0, len(active))
	since := make(map[string]time.Time, len(active))
	for c, at := range active {
		conds = append(conds, string(c))
		since[string(c)] = at
	}
	sort.Strings(conds)
	stored := append([]string(nil), row.Conditions...)
	sort.Strings(stored)
	operChanged := row.OperStatus != oper
	if !operChanged && row.AdminStatus == admin && row.SpeedBps == speed &&
		(lastChange < 0 || lastChange == row.LastChangeSeconds) && equalStrings(conds, stored) {
		return nil
	}
	u := PortStateUpdate{OperStatus: oper, AdminStatus: admin, SpeedBps: speed, LastChangeSeconds: lastChange,
		Conditions: conds, ConditionsSince: since}
	if at := tr.OperChangedAt(); operChanged && !at.IsZero() {
		u.OperChangedAt = &at
	}
	return m.store.SavePortState(ctx, row.ID, u)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spanKinds are the conditions logged as span events; LinkDown is already
// logged by its link_down event.
var spanKinds = map[portmon.Condition]string{
	portmon.Flapping:  models.PortEventFlapping,
	portmon.Errors:    models.PortEventErrors,
	portmon.Saturated: models.PortEventSaturated,
	portmon.SlowLink:  models.PortEventSlowLink,
}

func portEvents(deviceID uuid.UUID, row models.DeviceInterface, res portmon.Result) ([]models.PortEvent, []PortEventEnd) {
	var starts []models.PortEvent
	var ends []PortEventEnd
	for _, e := range res.Events {
		starts = append(starts, models.PortEvent{DeviceID: deviceID, InterfaceID: row.ID, IfIndex: row.IfIndex,
			Kind: e.Kind, StartedAt: e.At, Detail: e.Detail})
	}
	for _, c := range res.Changes {
		kind, ok := spanKinds[c.Condition]
		if !ok {
			continue
		}
		if c.Started {
			starts = append(starts, models.PortEvent{DeviceID: deviceID, InterfaceID: row.ID, IfIndex: row.IfIndex,
				Kind: kind, StartedAt: c.At, Detail: c.Detail})
		} else {
			ends = append(ends, PortEventEnd{InterfaceID: row.ID, Kind: kind, At: c.At, Detail: c.Detail})
		}
	}
	return starts, ends
}

// alert turns an important port's condition changes into incidents and
// notifications. Conditions active without a change this poll are made sure
// of too (idempotently): that retries an open that failed to save, and gives
// a port just marked important an incident for what is already wrong.
func (m *PortMonitor) alert(ctx context.Context, d models.Device, row models.DeviceInterface, tr *portmon.Tracker, res portmon.Result, site *string) {
	handled := map[portmon.Condition]bool{}
	for _, c := range res.Changes {
		handled[c.Condition] = true
		cond := string(c.Condition)
		if c.Started {
			inc, opened, err := m.incidents.OpenPortIncident(ctx, d.ID, row.ID, cond, c.At.UTC(), portProblem(d, row, c))
			if err != nil {
				m.logger.Printf("[snmp] opening %s incident for %s port %d: %v", cond, d.Host, row.IfIndex, err)
				continue
			}
			if opened {
				m.notifyPort(ctx, d, row, c, inc, site)
			}
			continue
		}
		inc, err := m.incidents.ClosePortIncident(ctx, row.ID, cond, c.At.UTC(), "")
		if err != nil {
			m.logger.Printf("[snmp] closing %s incident for %s port %d: %v", cond, d.Host, row.IfIndex, err)
			continue
		}
		if inc != nil {
			m.notifyPort(ctx, d, row, c, inc, site)
		}
	}
	for c, since := range tr.Active() {
		if handled[c] {
			continue
		}
		ch := portmon.Change{Condition: c, Started: true, At: since}
		inc, opened, err := m.incidents.OpenPortIncident(ctx, d.ID, row.ID, string(c), since.UTC(), portProblem(d, row, ch))
		if err != nil {
			m.logger.Printf("[snmp] ensuring %s incident for %s port %d: %v", c, d.Host, row.IfIndex, err)
			continue
		}
		if opened {
			m.notifyPort(ctx, d, row, ch, inc, site)
		}
	}
}

// portLabel names a port for people: "port 51 (Uplink To Quantum Gate)",
// "port 1/4" for a module port, or "switch 2 port 5 (Uplink)" on a stack
// member.
func portLabel(row models.DeviceInterface) string {
	n := portmon.PortLabel(row.Name, row.Descr, row.IfIndex)
	label := "port " + n
	if row.StackUnit > 0 {
		label = fmt.Sprintf("switch %d port %s", row.StackUnit, n)
	}
	if row.Alias != "" {
		return fmt.Sprintf("%s (%s)", label, row.Alias)
	}
	return label
}

var conditionNoun = map[portmon.Condition]string{
	portmon.LinkDown: "link down", portmon.Errors: "errors", portmon.Flapping: "flapping",
	portmon.SlowLink: "slow link", portmon.Saturated: "nearly full",
}

// portProblem is the alert sentence for a condition starting. Detail values
// come from the tracker; a retried open has none and gets the short form.
func portProblem(d models.Device, row models.DeviceInterface, c portmon.Change) string {
	who := displayName(d) + " " + portLabel(row)
	num := func(key string) (string, bool) {
		switch v := c.Detail[key].(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), true
		case int:
			return strconv.Itoa(v), true
		case int64:
			return strconv.FormatInt(v, 10), true
		}
		return "", false
	}
	speed := func(key string) (string, bool) {
		if v, ok := c.Detail[key].(int64); ok {
			return formatBitsPerSecond(float64(v)), true
		}
		return "", false
	}
	switch c.Condition {
	case portmon.LinkDown:
		return who + " is down."
	case portmon.Errors:
		if n, ok := num("per_minute"); ok {
			return fmt.Sprintf("%s is logging errors: %s per minute.", who, n)
		}
		return who + " is logging errors."
	case portmon.Flapping:
		if n, ok := num("transitions"); ok {
			return fmt.Sprintf("%s is flapping: %s link changes in 10 minutes.", who, n)
		}
		return who + " is flapping."
	case portmon.SlowLink:
		now, ok1 := speed("speed_bps")
		usual, ok2 := speed("usual_speed_bps")
		if ok1 && ok2 {
			return fmt.Sprintf("%s linked at %s instead of its usual %s.", who, now, usual)
		}
		return who + " linked slower than usual."
	case portmon.Saturated:
		if n, ok := num("util_pct"); ok {
			return fmt.Sprintf("%s is nearly full: %s%% busy.", who, n)
		}
		return who + " is nearly full."
	}
	return who + " has a problem."
}

func formatBitsPerSecond(bps float64) string {
	switch {
	case bps >= 1e9:
		return strconv.FormatFloat(bps/1e9, 'f', -1, 64) + " Gb/s"
	case bps >= 1e6:
		return strconv.FormatFloat(bps/1e6, 'f', -1, 64) + " Mb/s"
	default:
		return strconv.FormatFloat(bps/1e3, 'f', -1, 64) + " kb/s"
	}
}

func (m *PortMonitor) notifyPort(ctx context.Context, d models.Device, row models.DeviceInterface, c portmon.Change, inc *models.Incident, site *string) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	if *site == "" {
		name, err := m.store.SiteName(ctx, d.SiteID)
		if err != nil || name == "" {
			name = "its site"
		}
		*site = name
	}
	id, ifID, ifIndex := d.ID, row.ID, row.IfIndex
	label := portLabel(row)
	msg := &notifications.NotificationMessage{
		DeviceID: &id, InterfaceID: &ifID, PortIfIndex: &ifIndex, SiteName: *site,
		MonitorName: displayName(d) + " " + label, MonitorURL: d.Host, Timestamp: c.At,
	}
	if d.NotifyChannels != nil {
		msg.Channels = []string(d.NotifyChannels)
	}
	if inc != nil {
		msg.IncidentID = &inc.ID
	}
	status := "warning"
	if c.Condition == portmon.LinkDown {
		status = "down"
	}
	if c.Started {
		msg.Status, msg.PreviousStatus = status, "up"
		msg.Message = portProblem(d, row, c)
	} else {
		dur := time.Duration(0)
		if inc != nil {
			dur = time.Duration(inc.DurationSeconds) * time.Second
		}
		msg.Status, msg.PreviousStatus, msg.DowntimeDuration = "recovered", status, dur
		msg.Message = fmt.Sprintf("%s %s: %s cleared after %s.", displayName(d), label, conditionNoun[c.Condition], humanDuration(dur))
	}
	if err := m.notifier.SendNotification(ctx, msg); err != nil {
		m.logger.Printf("[snmp] sending %s notification for %s %s: %v", msg.Status, d.Host, label, err)
	}
}

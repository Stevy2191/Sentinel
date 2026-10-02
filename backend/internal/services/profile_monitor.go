package services

import (
	"context"
	"log"
	"maps"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ProfileSource gives a device's applicable profiles and records their runs
// (ProfileService).
type ProfileSource interface {
	ProfilesForDevice(ctx context.Context, d models.Device) ([]ProfileWithMetrics, error)
	SaveRun(ctx context.Context, deviceID, profileID uuid.UUID, at time.Time, ok bool, errText string) error
}

// MetricIncidents opens and closes metric-rule incidents (IncidentService).
type MetricIncidents interface {
	OpenMetricIncident(ctx context.Context, deviceID uuid.UUID, key, instance string, start time.Time, reason string) (*models.Incident, bool, error)
	CloseMetricIncident(ctx context.Context, deviceID uuid.UUID, key, instance string, end time.Time, note string) (*models.Incident, error)
	OpenMetricIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)
}

const (
	// profilePollTimeout bounds one device's SNMP reads for all its due
	// profiles together.
	profilePollTimeout = 30 * time.Second
	// profileDueSlack lets a profile run on a device poll that lands a little
	// early: the device poll's own timing jitters by the reachability Get's
	// latency, and a 1-minute profile must not slip to every other minute.
	profileDueSlack = 15 * time.Second
)

// profileState is what the monitor remembers about one device between polls.
// Which rule rows are active is not in here: that is read from the open
// incidents every poll, so a failed open or close, a pause or a restart can
// never leave the two disagreeing.
type profileState struct {
	lastRun map[uuid.UUID]time.Time
	// cache holds the inventory-like columns (labels, filters, precision),
	// re-read every inventoryInterval.
	cache   custommetric.Columns
	cacheAt time.Time
	// prev is each counter's previous raw samples, by key then instance.
	prev map[string]map[string]custommetric.Sample
	// holds is each rule's hold clocks (when a row started violating), by
	// key then instance.
	holds map[string]map[string]time.Time
	// lastState is each status row's state at the previous poll, by key then
	// instance, for "(was normal)".
	lastState map[string]map[string]string
}

// ProfileMonitor runs a device's metric profiles: due profiles are read,
// their rows stored as labelled samples, and rule violations turned into
// incidents and alerts.
type ProfileMonitor struct {
	profiles  ProfileSource
	metrics   MetricsWriter
	incidents MetricIncidents
	notifier  Notifier
	client    snmp.Client
	sites     SiteNamer

	now    func() time.Time
	logger *log.Logger

	mu      sync.Mutex
	devices map[uuid.UUID]*profileState
}

func NewProfileMonitor(profiles ProfileSource, metrics MetricsWriter, incidents MetricIncidents, notifier Notifier, client snmp.Client, sites SiteNamer) *ProfileMonitor {
	return &ProfileMonitor{profiles: profiles, metrics: metrics, incidents: incidents, notifier: notifier, client: client,
		sites: sites, now: time.Now, logger: log.Default(), devices: map[uuid.UUID]*profileState{}}
}

// with runs f on a device's state under the monitor's lock.
func (m *ProfileMonitor) with(deviceID uuid.UUID, f func(st *profileState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.devices[deviceID]
	if st == nil {
		st = &profileState{lastRun: map[uuid.UUID]time.Time{}, prev: map[string]map[string]custommetric.Sample{},
			holds: map[string]map[string]time.Time{}, lastState: map[string]map[string]string{}}
		m.devices[deviceID] = st
	}
	f(st)
}

// evaluatedMetric is one metric's rows from this poll.
type evaluatedMetric struct {
	def   custommetric.Definition
	units string
	rows  []custommetric.Row
}

func hasRule(m models.ProfileMetric) bool { return m.RuleEnabled && m.RuleKind != "" }

// PollProfiles runs a device's due profiles and applies their rules, then
// closes quietly any metric incident no applicable rule accounts for.
func (m *ProfileMonitor) PollProfiles(ctx context.Context, d models.Device, t snmp.Target) {
	profiles, err := m.profiles.ProfilesForDevice(ctx, d)
	if err != nil {
		m.logger.Printf("[profiles] loading profiles for %s: %v", d.Host, err)
		return
	}
	now := m.now().UTC()
	if len(profiles) == 0 {
		m.mu.Lock()
		delete(m.devices, d.ID)
		m.mu.Unlock()
		m.reconcile(ctx, d, nil, now, "Closed: no profile applies to this device any more.")
		return
	}
	var evaluated []evaluatedMetric
	if due := m.due(d.ID, profiles, now); len(due) > 0 {
		cols, errs := m.read(ctx, d, t, due, now)
		for _, p := range due {
			evaluated = append(evaluated, m.evaluateProfile(ctx, d, p, cols, errs, now)...)
		}
		m.write(ctx, d, now, evaluated)
	}
	open, err := m.incidents.OpenMetricIncidents(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[profiles] listing open metric incidents for %s: %v", d.Host, err)
		return
	}
	active := map[string]map[string]time.Time{}
	for _, inc := range open {
		if inc.MetricKey != nil && inc.MetricInstance != nil {
			if active[*inc.MetricKey] == nil {
				active[*inc.MetricKey] = map[string]time.Time{}
			}
			active[*inc.MetricKey][*inc.MetricInstance] = inc.StartTime
		}
	}
	site := ""
	for _, e := range evaluated {
		if e.def.Rule.Enabled && e.def.Rule.Kind != "" {
			m.applyRule(ctx, d, e, active[e.def.Key], now, &site)
		}
	}
	m.reconcileOpen(ctx, d, profiles, open, now, "Closed: the rule was turned off or removed.")
}

// due returns the profiles to run now, marking them as run.
func (m *ProfileMonitor) due(deviceID uuid.UUID, profiles []ProfileWithMetrics, now time.Time) []ProfileWithMetrics {
	var out []ProfileWithMetrics
	m.with(deviceID, func(st *profileState) {
		for _, p := range profiles {
			interval := time.Duration(max(p.Profile.PollIntervalMinutes, 1)) * time.Minute
			last, ran := st.lastRun[p.Profile.ID]
			if !ran || now.Before(last) || now.Sub(last) >= interval-profileDueSlack {
				st.lastRun[p.Profile.ID] = now
				out = append(out, p)
			}
		}
	})
	return out
}

// read walks the due profiles' OIDs in one ReadColumns call: values every
// time, inventory-like columns only when the device's cache is missing them
// or is older than inventoryInterval.
func (m *ProfileMonitor) read(ctx context.Context, d models.Device, t snmp.Target, due []ProfileWithMetrics, now time.Time) (custommetric.Columns, map[string]error) {
	var every, cached []string
	for _, p := range due {
		for _, pm := range p.Metrics {
			e, c := custommetric.Needed(ToDefinition(pm))
			for _, o := range e {
				if !slices.Contains(every, o) {
					every = append(every, o)
				}
			}
			for _, o := range c {
				if !slices.Contains(cached, o) {
					cached = append(cached, o)
				}
			}
		}
	}
	cached = slices.DeleteFunc(cached, func(o string) bool { return slices.Contains(every, o) })
	toRead := slices.Clone(every)
	fresh := false
	m.with(d.ID, func(st *profileState) {
		fresh = st.cache != nil && !now.Before(st.cacheAt) && now.Sub(st.cacheAt) < inventoryInterval
		for _, o := range cached {
			if _, ok := st.cache[o]; !fresh || !ok {
				toRead = append(toRead, o)
			}
		}
	})
	rctx, cancel := context.WithTimeout(ctx, profilePollTimeout)
	defer cancel()
	cols, errs := ReadColumns(rctx, m.client, t, toRead)
	m.with(d.ID, func(st *profileState) {
		if !fresh {
			st.cache, st.cacheAt = custommetric.Columns{}, now
		}
		for _, o := range cached {
			if col, ok := cols[o]; ok {
				st.cache[o] = col
			} else if col, ok := st.cache[o]; ok {
				cols[o] = col
			}
		}
	})
	return cols, errs
}

// evaluateProfile evaluates every metric whose OIDs all read and records the
// profile's run: OK unless one of its OIDs errored ("<first OID>: <error>").
func (m *ProfileMonitor) evaluateProfile(ctx context.Context, d models.Device, p ProfileWithMetrics, cols custommetric.Columns, errs map[string]error, now time.Time) []evaluatedMetric {
	var failed []string
	var out []evaluatedMetric
	for _, pm := range p.Metrics {
		def := ToDefinition(pm)
		every, cached := custommetric.Needed(def)
		skip := false
		for _, o := range append(every, cached...) {
			if errs[o] != nil {
				skip = true
				if !slices.Contains(failed, o) {
					failed = append(failed, o)
				}
			}
		}
		if skip {
			continue
		}
		rows := custommetric.Evaluate(def, cols)
		if def.Kind == "counter" {
			m.with(d.ID, func(st *profileState) { rows, st.prev[def.Key] = custommetric.Rates(st.prev[def.Key], rows, now) })
		}
		out = append(out, evaluatedMetric{def: def, units: pm.Units, rows: rows})
	}
	ok, errText := true, ""
	if len(failed) > 0 {
		slices.Sort(failed)
		ok, errText = false, failed[0]+": "+errs[failed[0]].Error()
	}
	if err := m.profiles.SaveRun(ctx, d.ID, p.Profile.ID, now, ok, errText); err != nil {
		m.logger.Printf("[profiles] saving run of %q for %s: %v", p.Profile.Name, d.Host, err)
	}
	return out
}

// write stores this poll's rows as labelled samples (a status metric stores
// its state code).
func (m *ProfileMonitor) write(ctx context.Context, d models.Device, now time.Time, evaluated []evaluatedMetric) {
	var points []SamplePoint
	for _, e := range evaluated {
		for _, r := range e.rows {
			points = append(points, SamplePoint{Metric: e.def.Key, Instance: r.Instance, Label: r.Label, Value: r.Value})
		}
	}
	if len(points) == 0 {
		return
	}
	if err := m.metrics.Write(ctx, d.ID, now, points); err != nil {
		m.logger.Printf("[profiles] writing samples for %s: %v", d.Host, err)
	}
}

// applyRule evaluates one metric's rule against this poll's rows. The rows
// active before this poll are the metric's open incidents (active), so an
// open or a close that failed earlier is simply retried now.
func (m *ProfileMonitor) applyRule(ctx context.Context, d models.Device, e evaluatedMetric, active map[string]time.Time, now time.Time, site *string) {
	key := e.def.Key
	var since map[string]time.Time
	var lastState map[string]string
	m.with(d.ID, func(st *profileState) { since, lastState = st.holds[key], maps.Clone(st.lastState[key]) })
	next, changes := custommetric.EvalRule(e.def.Rule, e.def.Kind, custommetric.RuleState{Active: active, Since: since}, e.rows, now)
	m.with(d.ID, func(st *profileState) {
		st.holds[key] = next.Since
		if e.def.Kind == "status" {
			states := make(map[string]string, len(e.rows))
			for _, r := range e.rows {
				states[r.Instance] = r.State
			}
			st.lastState[key] = states
		}
	})
	for _, c := range changes {
		if c.Started {
			problem := metricProblem(d, e, c, lastState[c.Instance])
			inc, opened, err := m.incidents.OpenMetricIncident(ctx, d.ID, key, c.Instance, c.At, problem)
			if err != nil {
				m.logger.Printf("[profiles] opening %s/%s incident for %s: %v", key, c.Instance, d.Host, err)
				continue
			}
			if opened {
				m.notify(ctx, d, e, c, inc, problem, site)
			}
			continue
		}
		inc, err := m.incidents.CloseMetricIncident(ctx, d.ID, key, c.Instance, c.At, "")
		if err != nil {
			m.logger.Printf("[profiles] closing %s/%s incident for %s: %v", key, c.Instance, d.Host, err)
			continue
		}
		if inc != nil {
			dur := time.Duration(inc.DurationSeconds) * time.Second
			m.notify(ctx, d, e, c, inc, metricRecovered(d, e, c, dur), site)
		}
	}
}

// reconcile lists the device's open metric incidents and closes, quietly,
// those no applicable rule accounts for.
func (m *ProfileMonitor) reconcile(ctx context.Context, d models.Device, profiles []ProfileWithMetrics, now time.Time, note string) {
	open, err := m.incidents.OpenMetricIncidents(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[profiles] listing open metric incidents for %s: %v", d.Host, err)
		return
	}
	m.reconcileOpen(ctx, d, profiles, open, now, note)
}

// reconcileOpen closes, without notifying, every open metric incident whose
// key is not an applicable metric with an enabled rule, and forgets the
// rule state of such keys (so a rule turned back on starts its hold afresh).
func (m *ProfileMonitor) reconcileOpen(ctx context.Context, d models.Device, profiles []ProfileWithMetrics, open []models.Incident, now time.Time, note string) {
	ruled := map[string]bool{}
	for _, p := range profiles {
		for _, pm := range p.Metrics {
			if hasRule(pm) {
				ruled[pm.Key] = true
			}
		}
	}
	if len(profiles) > 0 {
		m.with(d.ID, func(st *profileState) {
			maps.DeleteFunc(st.holds, func(k string, _ map[string]time.Time) bool { return !ruled[k] })
			maps.DeleteFunc(st.lastState, func(k string, _ map[string]string) bool { return !ruled[k] })
		})
	}
	for _, inc := range open {
		if inc.MetricKey == nil || inc.MetricInstance == nil || ruled[*inc.MetricKey] {
			continue
		}
		if _, err := m.incidents.CloseMetricIncident(ctx, d.ID, *inc.MetricKey, *inc.MetricInstance, now, note); err != nil {
			m.logger.Printf("[profiles] closing %s/%s incident for %s: %v", *inc.MetricKey, *inc.MetricInstance, d.Host, err)
		}
	}
}

// metricNumber renders a value for an alert: at most two decimals.
func metricNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// withUnits renders "95 %", or "95" when there are no units.
func withUnits(v float64, units string) string {
	if units == "" {
		return metricNumber(v)
	}
	return metricNumber(v) + " " + units
}

// metricProblem is the alert sentence for a rule row starting; previous is
// the row's state at the previous poll ("" when unknown).
func metricProblem(d models.Device, e evaluatedMetric, c custommetric.Change, previous string) string {
	who := displayName(d)
	if e.def.Rule.Kind == "not_ok" {
		msg := who + " " + c.Label + " is " + c.State
		if previous != "" && previous != c.State {
			msg += " (was " + previous + ")"
		}
		return msg
	}
	msg := who + " " + e.def.Name + ", " + c.Label + ": " + withUnits(c.Value, e.units) + " (" + e.def.Rule.Kind + " " + withUnits(e.def.Rule.Value, e.units)
	if hold := int(e.def.Rule.Hold.Minutes()); hold > 0 {
		msg += " for " + strconv.Itoa(hold) + " min"
	}
	return msg + ")"
}

// metricRecovered is the sentence for a rule row ending after dur.
func metricRecovered(d models.Device, e evaluatedMetric, c custommetric.Change, dur time.Duration) string {
	who := displayName(d)
	if e.def.Rule.Kind == "not_ok" {
		return who + " " + c.Label + " is " + c.State + " again after " + humanDuration(dur) + "."
	}
	return who + " " + e.def.Name + ", " + c.Label + ": back to " + withUnits(c.Value, e.units) + " after " + humanDuration(dur) + "."
}

// notify sends a rule row's alert or recovery, the way UPSMonitor.notify
// does: the device's channels (nil = every enabled channel, [] = none) and
// the site name looked up once per poll.
func (m *ProfileMonitor) notify(ctx context.Context, d models.Device, e evaluatedMetric, c custommetric.Change, inc *models.Incident, text string, site *string) {
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
		DeviceID: &id, SiteName: *site, MonitorName: displayName(d) + " · " + e.def.Name + ": " + c.Label,
		MonitorURL: d.Host, Timestamp: c.At, Message: text,
	}
	if d.NotifyChannels != nil {
		msg.Channels = []string(d.NotifyChannels)
	}
	if inc != nil {
		msg.IncidentID = &inc.ID
	}
	status := "warning"
	if e.def.Rule.Kind == "not_ok" {
		status = "down"
	}
	if c.Started {
		msg.Status, msg.PreviousStatus = status, "up"
	} else {
		msg.Status, msg.PreviousStatus = "recovered", status
		if inc != nil {
			msg.DowntimeDuration = time.Duration(inc.DurationSeconds) * time.Second
		}
	}
	if err := m.notifier.SendNotification(ctx, msg); err != nil {
		m.logger.Printf("[profiles] sending %s notification for %s: %v", msg.Status, d.Host, err)
	}
}

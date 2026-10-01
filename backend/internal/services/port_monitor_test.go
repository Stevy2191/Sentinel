package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// pmSNMP answers GETs from a table of OID -> value; missing OIDs come back
// nil (noSuchInstance), as a v2c agent answers.
type pmSNMP struct {
	mu        sync.Mutex
	values    map[string]any
	requested []string
}

func newPMSNMP() *pmSNMP { return &pmSNMP{values: map[string]any{}} }

func (f *pmSNMP) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]snmp.PDU, 0, len(oids))
	for _, o := range oids {
		f.requested = append(f.requested, o)
		out = append(out, snmp.PDU{OID: o, Value: f.values[o]})
	}
	return out, nil
}
func (f *pmSNMP) Walk(context.Context, snmp.Target, string) ([]snmp.PDU, error) { return nil, nil }

// port sets one interface's HC counters and state.
func (f *pmSNMP) port(idx int, in, out uint64, up bool, lastChangeTicks uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, x := "1.3.6.1.2.1.2.2.1", "1.3.6.1.2.1.31.1.1.1"
	oper := int64(1)
	if !up {
		oper = 2
	}
	set := func(base string, col int, v any) { f.values[fmt.Sprintf("%s.%d.%d", base, col, idx)] = v }
	set(x, 6, in)
	set(x, 10, out)
	set(x, 15, uint64(1000))
	set(e, 7, int64(1))
	set(e, 8, oper)
	set(e, 9, lastChangeTicks)
	for _, col := range []int{13, 14, 19, 20} {
		set(e, col, uint64(0))
	}
}

type pmStore struct {
	mu     sync.Mutex
	rows   []models.DeviceInterface
	starts []models.PortEvent
	ends   []PortEventEnd
	runs   int
}

func (s *pmStore) CollectedInterfaces(context.Context, uuid.UUID) ([]models.DeviceInterface, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.DeviceInterface(nil), s.rows...), nil
}
func (s *pmStore) SavePortState(_ context.Context, id uuid.UUID, u PortStateUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].OperStatus, s.rows[i].AdminStatus, s.rows[i].SpeedBps = u.OperStatus, u.AdminStatus, u.SpeedBps
			s.rows[i].Conditions, s.rows[i].ConditionsSince = u.Conditions, u.ConditionsSince
			if u.LastChangeSeconds >= 0 {
				s.rows[i].LastChangeSeconds = u.LastChangeSeconds
			}
			if u.OperChangedAt != nil {
				s.rows[i].OperChangedAt = u.OperChangedAt
			}
		}
	}
	return nil
}
func (s *pmStore) RecordPortEvents(_ context.Context, starts []models.PortEvent, ends []PortEventEnd) error {
	s.starts = append(s.starts, starts...)
	s.ends = append(s.ends, ends...)
	return nil
}
func (s *pmStore) SaveStatsRun(context.Context, uuid.UUID, time.Time, time.Duration) error {
	s.runs++
	return nil
}
func (s *pmStore) SiteName(context.Context, uuid.UUID) (string, error) { return "HQ", nil }

type pmMetrics struct{ writes [][]SamplePoint }

func (m *pmMetrics) Write(_ context.Context, _ uuid.UUID, _ time.Time, p []SamplePoint) error {
	m.writes = append(m.writes, p)
	return nil
}

func (m *pmMetrics) last(metric, instance string) (float64, bool) {
	if len(m.writes) == 0 {
		return 0, false
	}
	for _, p := range m.writes[len(m.writes)-1] {
		if p.Metric == metric && p.Instance == instance {
			return p.Value, true
		}
	}
	return 0, false
}

type pmIncidents struct {
	open          map[string]*models.Incident
	opens, closes int
}

func newPMIncidents() *pmIncidents { return &pmIncidents{open: map[string]*models.Incident{}} }

func (f *pmIncidents) OpenPortIncident(_ context.Context, dev, port uuid.UUID, cond string, start time.Time, _ string) (*models.Incident, bool, error) {
	f.opens++
	k := port.String() + cond
	if inc, ok := f.open[k]; ok {
		return inc, false, nil
	}
	inc := &models.Incident{ID: uuid.New(), DeviceID: &dev, InterfaceID: &port, Condition: &cond, StartTime: start}
	f.open[k] = inc
	return inc, true, nil
}
func (f *pmIncidents) ClosePortIncident(_ context.Context, port uuid.UUID, cond string, end time.Time, _ string) (*models.Incident, error) {
	f.closes++
	k := port.String() + cond
	inc, ok := f.open[k]
	if !ok {
		return nil, nil
	}
	delete(f.open, k)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}

// OpenPortIncidentsForDevice mirrors IncidentService.OpenPortIncidentsForDevice:
// every still-open entry whose DeviceID matches (an incident a test inserted
// directly into f.open without a DeviceID is returned regardless, since the
// real query only filters what it is given to filter on).
func (f *pmIncidents) OpenPortIncidentsForDevice(_ context.Context, dev uuid.UUID) ([]models.Incident, error) {
	var out []models.Incident
	for _, inc := range f.open {
		if inc.DeviceID != nil && *inc.DeviceID != dev {
			continue
		}
		out = append(out, *inc)
	}
	return out, nil
}

type pmThresholds struct{}

func (pmThresholds) PortThresholds(context.Context) portmon.Thresholds {
	return portmon.Thresholds{ErrorsPerMin: 10, UtilPct: 80, DownGrace: 2 * time.Minute}
}

type pmRig struct {
	m      *PortMonitor
	snmp   *pmSNMP
	store  *pmStore
	mets   *pmMetrics
	incs   *pmIncidents
	notif  *fakeNotifier
	dev    models.Device
	target snmp.Target
	clock  time.Time
}

func newPMRig(important bool) *pmRig {
	r := &pmRig{snmp: newPMSNMP(), store: &pmStore{}, mets: &pmMetrics{}, incs: newPMIncidents(), notif: &fakeNotifier{},
		clock: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	r.dev = models.Device{ID: uuid.New(), SiteID: uuid.New(), Name: "sw1", Host: "10.0.0.2", PollInterval: 60}
	r.target = snmp.Target{Credential: snmp.Credential{Version: "2c"}}
	r.store.rows = []models.DeviceInterface{{ID: uuid.New(), DeviceID: r.dev.ID, IfIndex: 1, Name: "0/1", Alias: "Uplink",
		OperStatus: "up", AdminStatus: "up", SpeedBps: 1e9, LastChangeSeconds: 10, Important: important, CollectDefault: true}}
	r.m = NewPortMonitor(r.store, r.mets, r.incs, r.notif, r.snmp, pmThresholds{})
	r.m.now = func() time.Time { return r.clock }
	return r
}

func (r *pmRig) poll(uptime int64) {
	r.m.PollStats(context.Background(), r.dev, r.target, uptime)
	r.clock = r.clock.Add(time.Minute)
}

func TestPortMonitorWritesRatesOnSecondPoll(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if _, ok := r.mets.last(MetricIfInBps, "1"); ok {
		t.Fatal("first poll wrote a rate")
	}
	if v, ok := r.mets.last(MetricIfSpeedBps, "1"); !ok || v != 1e9 {
		t.Fatalf("first poll speed %v %v", v, ok)
	}
	r.snmp.port(1, 750_000_000, 75_000_000, true, 1000)
	r.poll(160)
	if v, ok := r.mets.last(MetricIfInBps, "1"); !ok || v != 100e6 {
		t.Fatalf("in bps %v %v", v, ok)
	}
	if v, ok := r.mets.last(MetricIfOutUtilPct, "1"); !ok || v != 1 {
		t.Fatalf("out util %v %v", v, ok)
	}
	if r.store.runs != 2 {
		t.Errorf("stats runs recorded %d", r.store.runs)
	}
}

// Review Focus 2: a reboot between polls writes no rate at all.
func TestPortMonitorRebootWritesNoRates(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 5_000_000_000, 5_000_000_000, true, 1000)
	r.poll(100_000)
	r.snmp.port(1, 1_000, 1_000, true, 20)
	r.poll(30)
	if _, ok := r.mets.last(MetricIfInBps, "1"); ok {
		t.Fatal("rate written across a reboot")
	}
	if len(r.store.starts) != 0 {
		t.Fatalf("reboot logged port events: %+v", r.store.starts)
	}
}

// A v2c device without ifHC counters is learned once; the next poll asks for
// the 32-bit counters.
func TestPortMonitorLearnsNoHC(t *testing.T) {
	r := newPMRig(false)
	e := "1.3.6.1.2.1.2.2.1"
	r.snmp.values[e+".7.1"], r.snmp.values[e+".8.1"] = int64(1), int64(1)
	r.snmp.values[e+".10.1"], r.snmp.values[e+".16.1"] = uint64(10), uint64(10)
	r.poll(100)
	if len(r.mets.writes) != 0 {
		t.Fatalf("points written while learning: %+v", r.mets.writes)
	}
	r.snmp.requested = nil
	r.poll(160)
	if !strings.Contains(strings.Join(r.snmp.requested, " "), e+".10.1") {
		t.Fatalf("second poll did not read 32-bit counters: %v", r.snmp.requested)
	}
}

func TestPortMonitorImportantPortAlertsAfterGrace(t *testing.T) {
	r := newPMRig(true)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100) // t0 up
	r.snmp.port(1, 0, 0, false, 7000)
	r.poll(160) // t0+1m down
	r.poll(220) // t0+2m: 1 minute into the grace
	if r.incs.opens != 0 || len(r.notif.sent) != 0 {
		t.Fatalf("alerted inside the grace period: %d opens, %d sent", r.incs.opens, len(r.notif.sent))
	}
	r.poll(280) // t0+3m: grace over
	if len(r.incs.open) != 1 || len(r.notif.sent) != 1 {
		t.Fatalf("after grace: %d open, %d sent", len(r.incs.open), len(r.notif.sent))
	}
	m := r.notif.sent[0]
	if m.Status != "down" || m.PortIfIndex == nil || *m.PortIfIndex != 1 || m.InterfaceID == nil ||
		m.Message != "sw1 port 1 (Uplink) is down." || m.ViewPath() != fmt.Sprintf("/network/devices/%s/ports/1", r.dev.ID) {
		t.Errorf("message %+v", m)
	}
	r.poll(340) // still down: nothing new
	r.snmp.port(1, 0, 0, true, 9000)
	r.poll(400)
	if len(r.incs.open) != 0 || len(r.notif.sent) != 2 || r.notif.sent[1].Status != "recovered" {
		t.Fatalf("recovery: %d open, sent %+v", len(r.incs.open), r.notif.sent)
	}
}

func TestPortMonitorUnimportantPortLogsButNeverAlerts(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	r.snmp.port(1, 0, 0, false, 7000)
	for i := 0; i < 5; i++ {
		r.poll(int64(160 + 60*i))
	}
	if r.incs.opens != 0 || len(r.notif.sent) != 0 {
		t.Fatalf("unimportant port alerted: %d opens", r.incs.opens)
	}
	if len(r.store.starts) != 1 || r.store.starts[0].Kind != models.PortEventLinkDown {
		t.Fatalf("events %+v", r.store.starts)
	}
	if r.store.rows[0].OperStatus != "down" || r.store.rows[0].OperChangedAt == nil {
		t.Errorf("state not saved: %+v", r.store.rows[0])
	}
}

// Review Focus 5: restarted with an active condition and its incident open,
// one quiet poll neither closes it nor alerts again.
func TestPortMonitorRestartKeepsIncident(t *testing.T) {
	r := newPMRig(true)
	since := r.clock.Add(-time.Hour)
	r.store.rows[0].Conditions = models.ConditionSet{"errors"}
	r.store.rows[0].ConditionsSince = models.TimeMap{"errors": since}
	port := r.store.rows[0].ID
	r.incs.open[port.String()+"errors"] = &models.Incident{ID: uuid.New(), StartTime: since}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if r.incs.closes != 0 || len(r.incs.open) != 1 || len(r.notif.sent) != 0 {
		t.Fatalf("restart: closes %d open %d sent %d", r.incs.closes, len(r.incs.open), len(r.notif.sent))
	}
}

// A condition already active when a port becomes important gets its incident
// on the next poll.
func TestPortMonitorOpensIncidentForActiveConditionOnNewlyImportantPort(t *testing.T) {
	r := newPMRig(true)
	r.store.rows[0].Conditions = models.ConditionSet{"saturated"}
	r.store.rows[0].ConditionsSince = models.TimeMap{"saturated": r.clock.Add(-10 * time.Minute)}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if len(r.incs.open) != 1 || len(r.notif.sent) != 1 || r.notif.sent[0].Status != "warning" {
		t.Fatalf("open %d sent %+v", len(r.incs.open), r.notif.sent)
	}
}

func TestPortMonitorNoChannelsStillOpensIncident(t *testing.T) {
	r := newPMRig(true)
	r.dev.NotifyChannels = models.StringSlice{}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	r.snmp.port(1, 0, 0, false, 7000)
	for i := 0; i < 3; i++ {
		r.poll(int64(160 + 60*i))
	}
	if len(r.incs.open) != 1 || len(r.notif.sent) != 0 {
		t.Fatalf("open %d sent %d", len(r.incs.open), len(r.notif.sent))
	}
}

// I1: a port's tracker must not survive collection being turned off. A port
// down long enough opens an incident and leaves the tracker holding an
// active condition; the port then disappears from CollectedInterfaces
// (collection turned off clears its ds.ports entry too); when it reappears
// with no stored conditions (as UpdatePort left it) and reporting healthy,
// the old condition must not come back and nothing must reopen.
func TestPortMonitorPrunesTrackerWhenPortStopsBeingCollected(t *testing.T) {
	r := newPMRig(true)
	port := r.store.rows[0]
	since := r.clock.Add(-time.Hour)

	// Seed the live tracker with an active "errors" condition the stored row
	// does not have — as if a real problem had been found, then collection
	// was turned off (UpdatePort clears the stored row and its incidents,
	// but has no way to reach this process's in-memory tracker) and is now
	// back on. Errors needs 5 consecutive clean polls of real rate data to
	// clear on its own, so a single clean poll cannot be mistaken for the
	// fix: only pruning (a fresh tracker from a fresh snapshot) can explain
	// it going away immediately.
	ds := r.m.state(r.dev.ID)
	ds.ports[port.ID] = &portStats{tracker: portmon.NewTracker(portmon.Snapshot{
		OperUp: true, AdminUp: true, SpeedBps: 1e9,
		Active: map[portmon.Condition]time.Time{portmon.Errors: since},
	})}

	// Collection turned off: the port disappears from CollectedInterfaces.
	r.store.rows = nil
	r.poll(100) // nothing collected this poll

	// Collection turned back on; the row reappears (its stored conditions
	// were already empty) reporting cleanly.
	r.store.rows = []models.DeviceInterface{port}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(160)
	if r.incs.opens != 0 || len(r.incs.open) != 0 {
		t.Fatalf("stale condition came back: opens=%d open=%+v", r.incs.opens, r.incs.open)
	}
}

// I2: an open incident whose condition the tracker no longer has active is
// closed on the next poll (e.g. after a failed close, or an in-app config
// restore that left the incident row open), and this reconciliation close
// does not notify.
func TestPortMonitorReconcileClosesStaleIncident(t *testing.T) {
	r := newPMRig(true)
	port := r.store.rows[0].ID
	r.incs.open[port.String()+"errors"] = &models.Incident{ID: uuid.New(), DeviceID: &r.dev.ID, InterfaceID: &port,
		Condition: strPtr("errors"), StartTime: r.clock.Add(-time.Hour)}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if r.incs.closes != 1 || len(r.incs.open) != 0 {
		t.Fatalf("stale incident not closed: closes=%d open=%+v", r.incs.closes, r.incs.open)
	}
	if len(r.notif.sent) != 0 {
		t.Fatalf("reconciliation close notified: %+v", r.notif.sent)
	}
}

// I2: an open incident on a port that is no longer important is closed too,
// even though alert() itself never looks at an unimportant port.
func TestPortMonitorReconcileClosesIncidentOnUnimportantPort(t *testing.T) {
	r := newPMRig(false)
	port := r.store.rows[0].ID
	r.incs.open[port.String()+"link_down"] = &models.Incident{ID: uuid.New(), DeviceID: &r.dev.ID, InterfaceID: &port,
		Condition: strPtr("link_down"), StartTime: r.clock.Add(-time.Hour)}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if r.incs.closes != 1 || len(r.incs.open) != 0 {
		t.Fatalf("incident on unimportant port not closed: closes=%d open=%+v", r.incs.closes, r.incs.open)
	}
	if len(r.notif.sent) != 0 {
		t.Fatalf("reconciliation close notified: %+v", r.notif.sent)
	}
}

func strPtr(s string) *string { return &s }

// M3: a stacked switch's port label names its stack member.
func TestPortLabelIncludesStackUnit(t *testing.T) {
	row := models.DeviceInterface{IfIndex: 5, StackUnit: 2, Alias: "Uplink"}
	if got, want := portLabel(row), "switch 2 port 5 (Uplink)"; got != want {
		t.Errorf("portLabel = %q, want %q", got, want)
	}
	row.Alias = ""
	if got, want := portLabel(row), "switch 2 port 5"; got != want {
		t.Errorf("portLabel (no alias) = %q, want %q", got, want)
	}
	row.StackUnit = 0
	if got, want := portLabel(row), "port 5"; got != want {
		t.Errorf("portLabel (not stacked) = %q, want %q", got, want)
	}
	row.Name = "Te1/1/4"
	if got, want := portLabel(row), "port 1/4"; got != want {
		t.Errorf("portLabel (module port) = %q, want %q", got, want)
	}
}

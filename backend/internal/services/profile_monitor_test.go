package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	tFanDescr = "1.3.6.1.4.1.9.9.13.1.4.1.2"
	tFanState = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	tCPU      = "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
)

type fakeProfileSource struct {
	profiles []ProfileWithMetrics
	runs     []models.DeviceProfileRun
}

func (f *fakeProfileSource) ProfilesForDevice(context.Context, models.Device) ([]ProfileWithMetrics, error) {
	return f.profiles, nil
}
func (f *fakeProfileSource) SaveRun(_ context.Context, d, p uuid.UUID, at time.Time, ok bool, e string) error {
	f.runs = append(f.runs, models.DeviceProfileRun{DeviceID: d, ProfileID: p, RanAt: at, OK: ok, Error: e})
	return nil
}

type fakeMetricIncidents struct {
	open      map[string]*models.Incident
	opened    []string
	closed    []string
	notes     []string
	failClose bool
}

func (f *fakeMetricIncidents) OpenMetricIncident(_ context.Context, d uuid.UUID, key, inst string, start time.Time, _ string) (*models.Incident, bool, error) {
	k := key + "|" + inst
	if inc, ok := f.open[k]; ok {
		return inc, false, nil
	}
	cond := models.IncidentConditionMetric
	inc := &models.Incident{ID: uuid.New(), DeviceID: &d, Condition: &cond, MetricKey: &key, MetricInstance: &inst, StartTime: start}
	f.open[k] = inc
	f.opened = append(f.opened, k)
	return inc, true, nil
}
func (f *fakeMetricIncidents) CloseMetricIncident(_ context.Context, _ uuid.UUID, key, inst string, end time.Time, note string) (*models.Incident, error) {
	if f.failClose {
		return nil, errors.New("database unavailable")
	}
	k := key + "|" + inst
	inc, ok := f.open[k]
	if !ok {
		return nil, nil
	}
	delete(f.open, k)
	f.closed = append(f.closed, k)
	f.notes = append(f.notes, note)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}
func (f *fakeMetricIncidents) OpenMetricIncidents(context.Context, uuid.UUID) ([]models.Incident, error) {
	var out []models.Incident
	for _, inc := range f.open {
		out = append(out, *inc)
	}
	return out, nil
}

type profileRig struct {
	src   *fakeProfileSource
	agent *walkFake
	inc   *fakeMetricIncidents
	mets  *fakeUPSMetrics
	notif *fakeNotifier
	mon   *ProfileMonitor
	dev   models.Device
	now   time.Time
}

func newProfileRig(interval int) *profileRig {
	p, metrics := starterProfile()
	p.ID, p.PollIntervalMinutes = uuid.New(), interval
	var keep []models.ProfileMetric
	for _, m := range metrics {
		if m.Key == "cisco_fan_envmon" || m.Key == "cisco_cpu_5min" {
			keep = append(keep, m)
		}
	}
	// A CPU labelled by index keeps the rig small.
	for i := range keep {
		if keep[i].Key == "cisco_cpu_5min" {
			keep[i].LabelMode, keep[i].LabelPointerOID, keep[i].LabelTargetOID = "index", "", ""
		}
	}
	r := &profileRig{
		src: &fakeProfileSource{profiles: []ProfileWithMetrics{{Profile: p, Metrics: keep}}},
		agent: &walkFake{walks: map[string][]snmp.PDU{
			tFanState: {{OID: tFanState + ".1", Value: int64(1)}, {OID: tFanState + ".2", Value: int64(1)}},
			tFanDescr: {{OID: tFanDescr + ".1", Value: []byte("Fan 1")}, {OID: tFanDescr + ".2", Value: []byte("Fan 2")}},
			tCPU:      {{OID: tCPU + ".1", Value: uint64(20)}},
		}, fail: map[string]bool{}},
		inc:   &fakeMetricIncidents{open: map[string]*models.Incident{}},
		mets:  &fakeUPSMetrics{},
		notif: &fakeNotifier{},
		now:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	r.mon = NewProfileMonitor(r.src, r.mets, r.inc, r.notif, r.agent, fakeSiteNamer{})
	r.mon.now = func() time.Time { return r.now }
	r.dev = models.Device{ID: uuid.New(), Name: "core-3850", Host: "10.0.0.5", SysObjectID: "1.3.6.1.4.1.9.1.2066"}
	return r
}

func (r *profileRig) set(oid, idx string, v any) {
	pdus := r.agent.walks[oid]
	for i := range pdus {
		if pdus[i].OID == oid+"."+idx {
			pdus[i].Value = v
		}
	}
}

func (r *profileRig) poll() {
	r.mon.PollProfiles(context.Background(), r.dev, snmp.Target{})
	r.now = r.now.Add(time.Minute)
}

// metric returns the rig profile's metric with key, to change it in a test.
func (r *profileRig) metric(key string) *models.ProfileMetric {
	ms := r.src.profiles[0].Metrics
	for i := range ms {
		if ms[i].Key == key {
			return &ms[i]
		}
	}
	return nil
}

func (r *profileRig) points(key string) []SamplePoint {
	var out []SamplePoint
	for _, p := range r.mets.points {
		if p.Metric == key {
			out = append(out, p)
		}
	}
	return out
}

func walks(f *walkFake, root string) int {
	n := 0
	for _, w := range f.walked {
		if w == root {
			n++
		}
	}
	return n
}

func TestProfileMonitorFanAlertAndRecovery(t *testing.T) {
	r := newProfileRig(1)
	r.poll()
	r.set(tFanState, "2", int64(3)) // critical
	r.poll()
	r.poll() // still critical: nothing new
	if len(r.inc.opened) != 1 || r.inc.opened[0] != "cisco_fan_envmon|2" || len(r.notif.sent) != 1 {
		t.Fatalf("opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
	start := r.notif.sent[0]
	if start.Message != "core-3850 Fan 2 is critical (was normal)" || start.Status != "down" || start.MonitorName != "core-3850 · Fan: Fan 2" {
		t.Errorf("start %+v", start)
	}
	r.set(tFanState, "2", int64(1))
	r.poll()
	if len(r.inc.closed) != 1 || len(r.notif.sent) != 2 || !strings.HasPrefix(r.notif.sent[1].Message, "core-3850 Fan 2 is normal again") {
		t.Fatalf("recovery: closed %v sent %+v", r.inc.closed, r.notif.sent)
	}
	if rec := r.notif.sent[1]; rec.Message != "core-3850 Fan 2 is normal again after 2 minutes." || rec.Status != "recovered" || rec.PreviousStatus != "down" {
		t.Errorf("recovery %+v", rec)
	}
	for _, p := range r.mets.points {
		if p.Metric == "cisco_fan_envmon" && p.Instance == "2" && p.Label != "Fan 2" {
			t.Errorf("sample label %q", p.Label)
		}
	}
	// Status metrics store the state code.
	if fan := r.points("cisco_fan_envmon"); len(fan) != 8 || fan[3].Instance != "2" || fan[3].Value != 3 {
		t.Errorf("fan samples %+v", fan)
	}
}

func TestProfileMonitorCPUHold(t *testing.T) {
	r := newProfileRig(1)
	cpu := r.metric("cisco_cpu_5min")
	cpu.LabelMode, cpu.LabelPointerOID, cpu.LabelTargetOID = "pointer", oidCpmCPUTotalPhysIndex, oidEntPhysicalName
	r.agent.walks[oidCpmCPUTotalPhysIndex] = []snmp.PDU{{OID: oidCpmCPUTotalPhysIndex + ".1", Value: int64(1000)}}
	r.agent.walks[oidEntPhysicalName] = []snmp.PDU{{OID: oidEntPhysicalName + ".1000", Value: []byte("Switch 1")}}
	r.set(tCPU, "1", uint64(95))
	for i := 0; i < 10; i++ { // above from 12:00 to 12:09: 9 minutes, short of the hold
		r.poll()
	}
	if len(r.inc.opened) != 0 || len(r.notif.sent) != 0 {
		t.Fatalf("alerted before the hold: opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
	r.poll() // 12:10: above for 10 minutes
	if len(r.inc.opened) != 1 || r.inc.opened[0] != "cisco_cpu_5min|1" || len(r.notif.sent) != 1 {
		t.Fatalf("after the hold: opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
	start := r.notif.sent[0]
	if start.Message != "core-3850 CPU busy (5 min), Switch 1: 95 % (above 90 % for 10 min)" || start.Status != "warning" ||
		start.MonitorName != "core-3850 · CPU busy (5 min): Switch 1" {
		t.Errorf("start %+v", start)
	}
	r.set(tCPU, "1", uint64(40))
	r.poll()
	if len(r.notif.sent) != 2 || r.notif.sent[1].Message != "core-3850 CPU busy (5 min), Switch 1: back to 40 % after 1 minute." {
		t.Errorf("recovery %+v", r.notif.sent)
	}
}

// Review focus 3: a switch without the ENVMON tables answers its walks with
// nothing. That is a successful run, not an error.
func TestProfileMonitorUnsupportedTablesAreOK(t *testing.T) {
	r := newProfileRig(1)
	delete(r.agent.walks, tFanState)
	delete(r.agent.walks, tFanDescr)
	r.poll()
	if len(r.src.runs) != 1 || !r.src.runs[0].OK || r.src.runs[0].Error != "" {
		t.Fatalf("runs %+v", r.src.runs)
	}
	if len(r.points("cisco_fan_envmon")) != 0 || len(r.points("cisco_cpu_5min")) != 1 {
		t.Errorf("points %+v", r.mets.points)
	}
}

func TestProfileMonitorWalkErrorRecordsRun(t *testing.T) {
	r := newProfileRig(1)
	r.agent.fail[tCPU] = true
	r.set(tFanState, "2", int64(3))
	r.poll()
	if len(r.src.runs) != 1 || r.src.runs[0].OK || r.src.runs[0].Error != tCPU+": request timeout" {
		t.Fatalf("runs %+v", r.src.runs)
	}
	if len(r.points("cisco_cpu_5min")) != 0 || len(r.points("cisco_fan_envmon")) != 2 {
		t.Errorf("points %+v", r.mets.points)
	}
	if len(r.inc.opened) != 1 || r.inc.opened[0] != "cisco_fan_envmon|2" {
		t.Errorf("fan not evaluated: opened %v", r.inc.opened)
	}

	// A metric whose label column fails is skipped, not written unlabelled.
	r = newProfileRig(1)
	r.agent.fail[tFanDescr] = true
	r.poll()
	if len(r.src.runs) != 1 || r.src.runs[0].Error != tFanDescr+": request timeout" {
		t.Fatalf("label failure runs %+v", r.src.runs)
	}
	if len(r.points("cisco_fan_envmon")) != 0 || len(r.points("cisco_cpu_5min")) != 1 {
		t.Errorf("label failure points %+v", r.mets.points)
	}
	// It is retried on the next poll rather than waiting for the inventory.
	r.agent.fail[tFanDescr] = false
	r.poll()
	if fan := r.points("cisco_fan_envmon"); len(fan) != 2 || fan[1].Label != "Fan 2" || !r.src.runs[1].OK {
		t.Errorf("after the label column answered: %+v runs %+v", fan, r.src.runs)
	}
}

func TestProfileMonitorIntervalDue(t *testing.T) {
	r := newProfileRig(5)
	start := r.now
	r.poll()
	n := len(r.agent.walked)
	if n == 0 || len(r.src.runs) != 1 {
		t.Fatalf("first poll: walked %d runs %d", n, len(r.src.runs))
	}
	r.poll() // a minute later: not due
	if len(r.agent.walked) != n || len(r.src.runs) != 1 || len(r.mets.points) != 3 {
		t.Fatalf("not due: walked %v runs %d points %d", r.agent.walked[n:], len(r.src.runs), len(r.mets.points))
	}
	r.now = start.Add(5 * time.Minute)
	r.poll()
	if walks(r.agent, tCPU) != 2 || len(r.src.runs) != 2 {
		t.Errorf("due again: cpu walks %d runs %d", walks(r.agent, tCPU), len(r.src.runs))
	}
}

func TestProfileMonitorLabelsCachedUntilInventory(t *testing.T) {
	r := newProfileRig(1)
	start := r.now
	r.poll()
	r.poll()
	if walks(r.agent, tFanDescr) != 1 || walks(r.agent, tFanState) != 2 {
		t.Fatalf("labels walked %d, states %d", walks(r.agent, tFanDescr), walks(r.agent, tFanState))
	}
	// Labels still come from the cache.
	if fan := r.points("cisco_fan_envmon"); len(fan) != 4 || fan[3].Label != "Fan 2" {
		t.Errorf("cached labels %+v", fan)
	}
	r.now = start.Add(15 * time.Minute)
	r.poll()
	if walks(r.agent, tFanDescr) != 2 {
		t.Errorf("labels not refreshed after 15 min: %d", walks(r.agent, tFanDescr))
	}
}

func TestProfileMonitorRetriesFailedClose(t *testing.T) {
	r := newProfileRig(1)
	r.set(tFanState, "2", int64(3))
	r.poll()
	r.set(tFanState, "2", int64(1))
	r.inc.failClose = true
	r.poll()
	if len(r.inc.open) != 1 || len(r.notif.sent) != 1 {
		t.Fatalf("failed close: open %d sent %d", len(r.inc.open), len(r.notif.sent))
	}
	r.inc.failClose = false
	r.poll()
	r.poll()
	if len(r.inc.open) != 0 || len(r.inc.closed) != 1 || len(r.notif.sent) != 2 ||
		!strings.HasPrefix(r.notif.sent[1].Message, "core-3850 Fan 2 is normal again after ") {
		t.Errorf("retry: open %d closed %v sent %+v", len(r.inc.open), r.inc.closed, r.notif.sent)
	}
}

func TestProfileMonitorReconcilesRemovedRule(t *testing.T) {
	r := newProfileRig(1)
	r.set(tFanState, "2", int64(3))
	r.poll()
	sent := len(r.notif.sent)
	// The fan rule is turned off, and an incident is left for a metric that
	// is no longer in the profile at all.
	r.metric("cisco_fan_envmon").RuleEnabled = false
	_, _, _ = r.inc.OpenMetricIncident(context.Background(), r.dev.ID, "acme_gone", "1", r.now, "")
	r.poll()
	slices.Sort(r.inc.closed)
	if !slices.Equal(r.inc.closed, []string{"acme_gone|1", "cisco_fan_envmon|2"}) || len(r.inc.open) != 0 {
		t.Fatalf("closed %v open %d", r.inc.closed, len(r.inc.open))
	}
	if len(r.notif.sent) != sent {
		t.Errorf("reconcile notified: %+v", r.notif.sent[sent:])
	}
	for _, n := range r.inc.notes {
		if n != "Closed: the rule was turned off or removed." {
			t.Errorf("note %q", n)
		}
	}
}

func TestProfileMonitorNoProfilesClosesLeftovers(t *testing.T) {
	r := newProfileRig(1)
	r.src.profiles = nil
	_, _, _ = r.inc.OpenMetricIncident(context.Background(), r.dev.ID, "cisco_fan_envmon", "2", r.now, "")
	r.poll()
	if len(r.inc.closed) != 1 || len(r.inc.open) != 0 || len(r.notif.sent) != 0 || len(r.agent.walked) != 0 || len(r.src.runs) != 0 {
		t.Fatalf("closed %v open %d sent %d walked %v runs %d", r.inc.closed, len(r.inc.open), len(r.notif.sent), r.agent.walked, len(r.src.runs))
	}
	if r.inc.notes[0] != "Closed: no profile applies to this device any more." {
		t.Errorf("note %q", r.inc.notes[0])
	}
}

func TestProfileMonitorCounterRates(t *testing.T) {
	const tOctets = "1.3.6.1.4.1.99999.1.1.1.2"
	r := newProfileRig(1)
	r.src.profiles[0].Metrics = append(r.src.profiles[0].Metrics, models.ProfileMetric{Name: "Octets in", Key: "acme_octets",
		Source: "column", Kind: "counter", Units: "B/s", Scale: 1, OID: tOctets, LabelMode: "index"})
	r.agent.walks[tOctets] = []snmp.PDU{{OID: tOctets + ".1", Value: uint64(1000)}}
	r.poll()
	if got := r.points("acme_octets"); len(got) != 0 {
		t.Fatalf("first poll wrote a counter: %+v", got)
	}
	r.set(tOctets, "1", uint64(7000))
	r.poll()
	if got := r.points("acme_octets"); len(got) != 1 || got[0].Value != 100 || got[0].Instance != "1" {
		t.Errorf("rate %+v", got)
	}
}

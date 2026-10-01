package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

const upsPrefix = "1.3.6.1.2.1.33.1"

// fakeUPSAgent answers UPS GETs from vals; fail makes every GET time out.
type fakeUPSAgent struct {
	vals map[string]any
	fail bool
}

func (f *fakeUPSAgent) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	if f.fail {
		return nil, errors.New("timeout")
	}
	out := make([]snmp.PDU, len(oids))
	for i, o := range oids {
		out[i] = snmp.PDU{OID: o, Value: f.vals[o]}
	}
	return out, nil
}
func (f *fakeUPSAgent) Walk(context.Context, snmp.Target, string) ([]snmp.PDU, error) {
	return nil, nil
}

func onMains() map[string]any {
	return map[string]any{upsPrefix + ".2.1.0": int64(2), upsPrefix + ".2.3.0": int64(41), upsPrefix + ".2.4.0": int64(96),
		upsPrefix + ".4.1.0": int64(3), upsPrefix + ".4.4.1.5.1": int64(34)}
}

type fakeUPSIncidents struct {
	open    map[string]*models.Incident
	opened  []string
	closed  []string
	failOps bool
}

func newFakeUPSIncidents() *fakeUPSIncidents {
	return &fakeUPSIncidents{open: map[string]*models.Incident{}}
}

func (f *fakeUPSIncidents) OpenDeviceConditionIncident(_ context.Context, id uuid.UUID, c string, start time.Time, _ string) (*models.Incident, bool, error) {
	if inc, ok := f.open[c]; ok {
		return inc, false, nil
	}
	cond := c
	inc := &models.Incident{ID: uuid.New(), DeviceID: &id, Condition: &cond, StartTime: start}
	f.open[c] = inc
	f.opened = append(f.opened, c)
	return inc, true, nil
}
func (f *fakeUPSIncidents) CloseDeviceConditionIncident(_ context.Context, _ uuid.UUID, c string, end time.Time, _ string) (*models.Incident, error) {
	inc, ok := f.open[c]
	if !ok {
		return nil, nil
	}
	delete(f.open, c)
	f.closed = append(f.closed, c)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}
func (f *fakeUPSIncidents) OpenDeviceConditionIncidents(context.Context, uuid.UUID) ([]models.Incident, error) {
	var out []models.Incident
	for _, inc := range f.open {
		out = append(out, *inc)
	}
	return out, nil
}

type fakeUPSMetrics struct{ points []SamplePoint }

func (f *fakeUPSMetrics) Write(_ context.Context, _ uuid.UUID, _ time.Time, p []SamplePoint) error {
	f.points = append(f.points, p...)
	return nil
}

type fixedUPSThresholds struct{}

func (fixedUPSThresholds) UPSThresholds(context.Context) upsmon.Thresholds {
	return upsmon.Thresholds{LowBatteryPct: 25, HighLoadPct: 80}
}

type fakeSiteNamer struct{}

func (fakeSiteNamer) SiteName(context.Context, uuid.UUID) (string, error) { return "Expo", nil }

type upsRig struct {
	agent *fakeUPSAgent
	inc   *fakeUPSIncidents
	mets  *fakeUPSMetrics
	notif *fakeNotifier
	mon   *UPSMonitor
	dev   models.Device
	now   time.Time
}

func newUPSRig() *upsRig {
	r := &upsRig{agent: &fakeUPSAgent{vals: onMains()}, inc: newFakeUPSIncidents(), mets: &fakeUPSMetrics{}, notif: &fakeNotifier{},
		now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	r.mon = NewUPSMonitor(r.mets, r.inc, r.notif, r.agent, fixedUPSThresholds{}, fakeSiteNamer{})
	r.mon.now = func() time.Time { return r.now }
	ups := "ups"
	r.dev = models.Device{ID: uuid.New(), Name: "EXP-BEB-SMART1500", Host: "10.10.255.202", DeviceType: &ups}
	return r
}

func (r *upsRig) poll() {
	r.mon.PollUPS(context.Background(), r.dev, snmp.Target{})
	r.now = r.now.Add(time.Minute)
}

func TestUPSMonitorWritesSamples(t *testing.T) {
	r := newUPSRig()
	r.poll()
	got := map[string]float64{}
	for _, p := range r.mets.points {
		if p.Instance != "" || p.InterfaceID != nil {
			t.Errorf("point %+v has an instance or interface", p)
		}
		got[p.Metric] = p.Value
	}
	if got[MetricUPSChargePct] != 96 || got[MetricUPSLoadPct] != 34 || got[MetricUPSRuntimeMin] != 41 ||
		got[MetricUPSOnBattery] != 0 || got[MetricUPSBatteryStatus] != 2 {
		t.Errorf("samples %v", got)
	}
	if _, ok := got[MetricUPSInputV]; ok {
		t.Error("absent input voltage was written")
	}
}

func TestUPSMonitorOnBatteryOneIncidentOneAlertOneRecovery(t *testing.T) {
	r := newUPSRig()
	r.poll()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	r.poll() // still on battery: nothing new
	r.agent.vals[upsPrefix+".4.1.0"] = int64(3)
	r.poll()
	if len(r.inc.opened) != 1 || len(r.inc.closed) != 1 || r.inc.opened[0] != models.UPSConditionOnBattery {
		t.Fatalf("opened %v closed %v", r.inc.opened, r.inc.closed)
	}
	if len(r.notif.sent) != 2 {
		t.Fatalf("sent %d notifications", len(r.notif.sent))
	}
	start, end := r.notif.sent[0], r.notif.sent[1]
	if start.Message != "EXP-BEB-SMART1500 is on battery (charge 96 %, 41 min left)" || start.Status != "warning" ||
		start.MonitorName != "EXP-BEB-SMART1500 · On battery" || start.SiteName != "Expo" || start.IncidentID == nil {
		t.Errorf("start %+v", start)
	}
	if !strings.HasPrefix(end.Message, "EXP-BEB-SMART1500 is back on mains power") || end.Status != "recovered" {
		t.Errorf("end %+v", end)
	}
}

func TestUPSMonitorFailedGetChangesNothing(t *testing.T) {
	r := newUPSRig()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	r.agent.fail = true
	before := len(r.mets.points)
	r.poll()
	if len(r.mets.points) != before || len(r.inc.closed) != 0 || len(r.notif.sent) != 1 {
		t.Errorf("failed GET acted: points %d->%d closed %v sent %d", before, len(r.mets.points), r.inc.closed, len(r.notif.sent))
	}
}

// After a restart the monitor rebuilds from open incidents: no second alert,
// and the recovery still closes and notifies.
func TestUPSMonitorRestartMidOutage(t *testing.T) {
	r := newUPSRig()
	id := r.dev.ID
	cond := models.UPSConditionOnBattery
	r.inc.open[cond] = &models.Incident{ID: uuid.New(), DeviceID: &id, Condition: &cond, StartTime: r.now.Add(-time.Hour)}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	if len(r.notif.sent) != 0 || len(r.inc.opened) != 0 {
		t.Fatalf("re-alerted after restart: %d sent, opened %v", len(r.notif.sent), r.inc.opened)
	}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(3)
	r.poll()
	if len(r.inc.closed) != 1 || len(r.notif.sent) != 1 || r.notif.sent[0].Status != "recovered" {
		t.Errorf("recovery: closed %v sent %+v", r.inc.closed, r.notif.sent)
	}
}

// A device override replaces the instance threshold.
func TestUPSMonitorDeviceThresholdOverride(t *testing.T) {
	r := newUPSRig()
	fifty := 50
	r.dev.UPSLowBatteryPct = &fifty
	r.agent.vals[upsPrefix+".2.4.0"] = int64(40)
	r.poll()
	if len(r.inc.opened) != 1 || r.inc.opened[0] != models.UPSConditionLowBattery {
		t.Fatalf("override ignored: %v", r.inc.opened)
	}
	if !strings.Contains(r.notif.sent[0].Message, "battery is low (charge 40 %") {
		t.Errorf("message %q", r.notif.sent[0].Message)
	}
}

// A device that is no longer a UPS has its open UPS incidents closed quietly.
func TestUPSMonitorReconcileClosesQuietly(t *testing.T) {
	r := newUPSRig()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	sent := len(r.notif.sent)
	r.dev.DeviceType = nil
	r.dev.DeviceTypeDetected = "other"
	r.mon.ReconcileUPS(context.Background(), r.dev)
	if len(r.inc.open) != 0 || len(r.notif.sent) != sent {
		t.Errorf("open %v, notifications %d->%d", r.inc.open, sent, len(r.notif.sent))
	}
}

func TestUPSMonitorNotifyChannelsNone(t *testing.T) {
	r := newUPSRig()
	r.dev.NotifyChannels = models.StringSlice{}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	if len(r.inc.opened) != 1 || len(r.notif.sent) != 0 {
		t.Errorf("opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
}

func TestIsUPS(t *testing.T) {
	ups, sw := "ups", "switch"
	for _, c := range []struct {
		d    models.Device
		want bool
	}{
		{models.Device{DeviceTypeDetected: "ups"}, true},
		{models.Device{DeviceType: &ups, DeviceTypeDetected: "other"}, true},
		{models.Device{DeviceType: &sw, DeviceTypeDetected: "ups"}, false},
		{models.Device{DeviceTypeDetected: "switch"}, false},
	} {
		if got := IsUPS(c.d); got != c.want {
			t.Errorf("IsUPS(%+v) = %v", c.d, got)
		}
	}
}

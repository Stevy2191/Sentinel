package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// fakeCounters answers sysUpTime and the IF-MIB stats OIDs from a table of
// in/out octets per ifIndex, and counts requests.
type fakeCounters struct {
	mu sync.Mutex
	// block: requests to this host wait until their context ends.
	block    string
	blocked  int // requests to the blocked host
	in, out  map[int]uint64
	uptime   uint64
	gets     int
	maxBatch int
	asked    []int
	hcAsked  []bool
}

func newFakeCounters() *fakeCounters {
	return &fakeCounters{in: map[int]uint64{}, out: map[int]uint64{}, uptime: 100000}
}

func (f *fakeCounters) Walk(context.Context, snmp.Target, string) ([]snmp.PDU, error) {
	return nil, nil
}

func (f *fakeCounters) Get(ctx context.Context, t snmp.Target, oids []string) ([]snmp.PDU, error) {
	f.mu.Lock()
	if f.block != "" && t.Host == f.block {
		f.blocked++
		f.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer f.mu.Unlock()
	f.gets++
	var out []snmp.PDU
	for _, oid := range oids {
		if oid == snmp.OIDSysUpTime {
			out = append(out, snmp.PDU{OID: oid, Value: f.uptime})
			continue
		}
		k := strings.LastIndex(oid, ".")
		col, ix := oid[:k], oid[k+1:]
		n, _ := strconv.Atoi(ix)
		var v any = uint64(0)
		switch col {
		case "1.3.6.1.2.1.31.1.1.1.6", "1.3.6.1.2.1.2.2.1.10":
			v = f.in[n]
			f.asked = append(f.asked, n)
			f.hcAsked = append(f.hcAsked, col == "1.3.6.1.2.1.31.1.1.1.6")
		case "1.3.6.1.2.1.31.1.1.1.10", "1.3.6.1.2.1.2.2.1.16":
			v = f.out[n]
		case "1.3.6.1.2.1.2.2.1.7", "1.3.6.1.2.1.2.2.1.8":
			v = int64(1)
		case "1.3.6.1.2.1.31.1.1.1.15":
			v = uint64(1000)
		}
		out = append(out, snmp.PDU{OID: oid, Value: v})
	}
	return out, nil
}

type fakeLinkDeps struct {
	ports   map[uuid.UUID][]BoundPort
	devices map[uuid.UUID]*DeviceView
	hc      map[uuid.UUID][2]bool // hc, known
	live    map[uuid.UUID]LivePort
	extra   map[uuid.UUID][]uuid.UUID // map devices with no bound port
	mu      sync.Mutex
	pub     []MapTraffic
}

func (f *fakeLinkDeps) BoundPorts(_ context.Context, site uuid.UUID) ([]BoundPort, []uuid.UUID, error) {
	seen := map[uuid.UUID]bool{}
	var devs []uuid.UUID
	for _, p := range f.ports[site] {
		if !seen[p.DeviceID] {
			seen[p.DeviceID] = true
			devs = append(devs, p.DeviceID)
		}
	}
	return f.ports[site], append(devs, f.extra[site]...), nil
}
func (f *fakeLinkDeps) Get(_ context.Context, id uuid.UUID) (*DeviceView, error) {
	if d, ok := f.devices[id]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}
func (f *fakeLinkDeps) Target(_ context.Context, d models.Device) (snmp.Target, error) {
	return snmp.Target{Host: d.Host, Credential: snmp.Credential{Version: "2c"}}, nil
}
func (f *fakeLinkDeps) HC(id uuid.UUID) (bool, bool) { v := f.hc[id]; return v[0], v[1] }
func (f *fakeLinkDeps) LivePorts(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]LivePort, error) {
	out := map[uuid.UUID]LivePort{}
	for _, id := range ids {
		if p, ok := f.live[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}
func (f *fakeLinkDeps) publish(_ uuid.UUID, t MapTraffic) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pub = append(f.pub, t)
}

type linkRig struct {
	site, dev uuid.UUID
	deps      *fakeLinkDeps
	client    *fakeCounters
	poller    *LinkPoller
	clock     time.Time
}

func newLinkRig(t *testing.T, nPorts int) *linkRig {
	t.Helper()
	r := &linkRig{site: uuid.New(), dev: uuid.New(), client: newFakeCounters(), clock: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	r.deps = &fakeLinkDeps{ports: map[uuid.UUID][]BoundPort{}, devices: map[uuid.UUID]*DeviceView{}, hc: map[uuid.UUID][2]bool{}, live: map[uuid.UUID]LivePort{}, extra: map[uuid.UUID][]uuid.UUID{}}
	for i := 1; i <= nPorts; i++ {
		r.deps.ports[r.site] = append(r.deps.ports[r.site], BoundPort{DeviceID: r.dev, IfIndex: i, InterfaceID: uuid.New(), LinkKey: fmt.Sprintf("link-%d", i)})
	}
	r.deps.devices[r.dev] = &DeviceView{Device: models.Device{ID: r.dev, Host: "10.0.0.2", Enabled: true, Status: models.DeviceStatusUp}}
	r.deps.hc[r.dev] = [2]bool{true, true}
	r.poller = NewLinkPoller(LinkPollerDeps{Maps: r.deps, Devices: r.deps, Targets: r.deps, HC: r.deps, Live: r.deps, Client: r.client, Publish: r.deps.publish})
	r.poller.now = func() time.Time { return r.clock }
	return r
}

func (r *linkRig) tick(t *testing.T, advance time.Duration) {
	t.Helper()
	r.clock = r.clock.Add(advance)
	r.poller.Tick(context.Background())
}

func (r *linkRig) gets() int {
	r.client.mu.Lock()
	defer r.client.mu.Unlock()
	return r.client.gets
}

func TestLinkPollerOnlyWhileWatched(t *testing.T) {
	r := newLinkRig(t, 1)
	r.tick(t, 0)
	if r.gets() != 0 {
		t.Fatalf("polled with nobody watching: %d gets", r.gets())
	}
	release := r.poller.Watch(r.site)
	r.tick(t, 10*time.Second)
	if r.gets() == 0 {
		t.Fatal("no poll while watched")
	}
	release()
	r.tick(t, 30*time.Second) // still within 60 s of the last viewer
	n := r.gets()
	r.tick(t, 40*time.Second) // now 70 s after
	r.tick(t, 10*time.Second)
	if r.gets() != n {
		t.Errorf("still polling 60 s after the last viewer left: %d → %d", n, r.gets())
	}
}

func TestLinkPollerTouchCounts60s(t *testing.T) {
	r := newLinkRig(t, 1)
	r.poller.Touch(r.site)
	r.tick(t, 10*time.Second)
	if r.gets() == 0 {
		t.Fatal("no poll after a Touch")
	}
	r.tick(t, 55*time.Second)
	n := r.gets()
	r.tick(t, 10*time.Second)
	if r.gets() != n {
		t.Error("still polling 65 s after a Touch")
	}
}

func TestLinkPollerRates(t *testing.T) {
	r := newLinkRig(t, 1)
	defer r.poller.Watch(r.site)()
	r.client.in[1], r.client.out[1] = 1_000_000, 1_000_000
	r.tick(t, 0)
	r.client.in[1] += 12_500_000 // 100 Mb over 10 s
	r.client.uptime += 1000
	r.tick(t, 10*time.Second)
	got := r.poller.Latest(context.Background(), r.site).Links["link-1"]
	if got.InBps == nil || *got.InBps != 10_000_000 || got.OperUp == nil || !*got.OperUp || got.At == nil {
		t.Fatalf("rate = %+v, want 10 Mb/s in, up, timed", got)
	}
	if len(r.deps.pub) == 0 {
		t.Error("nothing published")
	}
}

func TestLinkPollerSkipsUnhealthy(t *testing.T) {
	for name, mod := range map[string]func(*models.Device){
		"down":     func(d *models.Device) { d.Status = models.DeviceStatusDown },
		"failing":  func(d *models.Device) { d.ConsecutiveFailures = 1 },
		"disabled": func(d *models.Device) { d.Enabled = false },
	} {
		r := newLinkRig(t, 1)
		mod(&r.deps.devices[r.dev].Device)
		release := r.poller.Watch(r.site)
		r.tick(t, 10*time.Second)
		release()
		if r.gets() != 0 {
			t.Errorf("%s device was polled", name)
		}
	}
}

// Review Focus 5.
func TestLinkPollerHCUnknownSkipped(t *testing.T) {
	r := newLinkRig(t, 1)
	r.deps.hc[r.dev] = [2]bool{false, false}
	defer r.poller.Watch(r.site)()
	r.tick(t, 10*time.Second)
	if r.gets() != 0 {
		t.Fatal("polled before 64-bit support was known")
	}
	r.deps.hc[r.dev] = [2]bool{false, true}
	r.tick(t, 10*time.Second)
	if len(r.client.hcAsked) == 0 || r.client.hcAsked[0] {
		t.Fatalf("hc asked = %v, want the 32-bit counters", r.client.hcAsked)
	}
}

func TestLinkPollerCaps64(t *testing.T) {
	r := newLinkRig(t, 70)
	defer r.poller.Watch(r.site)()
	r.tick(t, 10*time.Second)
	if len(r.client.asked) != 64 {
		t.Fatalf("asked for %d ports, want 64", len(r.client.asked))
	}
}

func TestLinkPollerLatestFallsBackToStored(t *testing.T) {
	r := newLinkRig(t, 1)
	in, out := 5e6, 1e6
	r.deps.live[r.deps.ports[r.site][0].InterfaceID] = LivePort{OperStatus: "up", InBps: &in, OutBps: &out}
	got := r.poller.Latest(context.Background(), r.site).Links["link-1"]
	if got.InBps == nil || *got.InBps != 5e6 || got.OperUp == nil || !*got.OperUp {
		t.Fatalf("fallback = %+v, want the stored 5 Mb/s", got)
	}
}

// Review: a device that stops answering must not stall every map's traffic.
func TestLinkPollerSlowDeviceDoesNotStall(t *testing.T) {
	r := newLinkRig(t, 1)
	other := uuid.New()
	dev2 := uuid.New()
	r.deps.ports[other] = []BoundPort{{DeviceID: dev2, IfIndex: 1, InterfaceID: uuid.New(), LinkKey: "other-1"}}
	r.deps.devices[dev2] = &DeviceView{Device: models.Device{ID: dev2, Host: "10.0.0.9", Enabled: true, Status: models.DeviceStatusUp}}
	r.deps.hc[dev2] = [2]bool{true, true}
	r.client.block = "10.0.0.2" // the first site's switch hangs
	r.poller.tickTimeout = 150 * time.Millisecond
	defer r.poller.Watch(r.site)()
	defer r.poller.Watch(other)()
	start := time.Now()
	r.tick(t, 10*time.Second)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("tick took %v with one device hanging", d)
	}
	if _, ok := r.poller.Latest(context.Background(), other).Links["other-1"]; !ok {
		t.Error("the other site's traffic wasn't published")
	}
	r.client.mu.Lock()
	hung := r.client.blocked
	r.client.mu.Unlock()
	if hung != 1 {
		t.Errorf("%d requests to the hanging device, want the uptime GET only", hung)
	}
}

// Review: every device on the map gets its status each tick, not only those
// whose port is measured.
func TestLinkPollerStatusesForAllMapDevices(t *testing.T) {
	r := newLinkRig(t, 1)
	leaf := uuid.New()
	r.deps.extra[r.site] = []uuid.UUID{leaf}
	r.deps.devices[leaf] = &DeviceView{Device: models.Device{ID: leaf, Status: models.DeviceStatusDown}}
	defer r.poller.Watch(r.site)()
	r.tick(t, 10*time.Second)
	got := r.poller.Latest(context.Background(), r.site).Devices
	if got[leaf] != models.DeviceStatusDown || got[r.dev] != models.DeviceStatusUp {
		t.Fatalf("statuses = %v, want the leaf down and the switch up", got)
	}
}

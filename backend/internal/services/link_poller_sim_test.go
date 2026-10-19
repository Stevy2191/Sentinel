package services

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type simTarget struct{ t snmp.Target }

func (s simTarget) Target(context.Context, models.Device) (snmp.Target, error) { return s.t, nil }

// Two fast polls of the simulated EdgeSwitch's ports 1 and 2 (both up) give each a
// state and a rate (zero: the simulator's counters don't move).
func TestLinkPollerSim(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "edgeswitch"}, Timeout: 2 * time.Second, Retries: 1}

	r := newLinkRig(t, 0)
	r.deps.ports[r.site] = []BoundPort{{DeviceID: r.dev, IfIndex: 1, InterfaceID: uuid.New(), LinkKey: "p1"}, {DeviceID: r.dev, IfIndex: 2, InterfaceID: uuid.New(), LinkKey: "p2"}}
	r.poller = NewLinkPoller(LinkPollerDeps{Maps: r.deps, Devices: r.deps, Targets: simTarget{target}, HC: r.deps, Live: r.deps, Client: snmp.GoSNMPClient{}, Publish: r.deps.publish})
	r.poller.now = func() time.Time { return r.clock }
	defer r.poller.Watch(r.site)()
	r.tick(t, 0)
	r.tick(t, 10*time.Second)
	for _, k := range []string{"p1", "p2"} {
		got := r.poller.Latest(context.Background(), r.site).Links[k]
		if got.OperUp == nil || !*got.OperUp || got.InBps == nil {
			t.Errorf("%s = %+v, want up with a rate", k, got)
		}
	}
}

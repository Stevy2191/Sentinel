package services

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The simulated EdgeSwitch has 3 MACs on 0/3, 12 on 0/48 (an LLDP switch)
// and 2 ARP rows.
func TestDBL2Simulator(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "edgeswitch"}, Timeout: 2 * time.Second, Retries: 1}
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 48)
	NewL2Monitor(db, snmp.GoSNMPClient{}).PollL2(context.Background(), d, target)
	if s, n, a := l2Count(t, db, "sightings"), l2Count(t, db, "neighbors"), l2Count(t, db, "arp"); s != 15 || n != 1 || a != 2 {
		t.Errorf("sightings %d, neighbours %d, arp %d; want 15, 1, 2", s, n, a)
	}
}

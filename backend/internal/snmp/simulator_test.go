package snmp

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// simTarget returns a target on the simulator, skipping unless
// SENTINEL_TEST_SNMPSIM (host:port) is set. See deploy/snmpsim.
func simTarget(t *testing.T, cred Credential) Target {
	t.Helper()
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return Target{Host: host, Port: uint16(port), Credential: cred, Timeout: 2 * time.Second, Retries: 0}
}

func TestSimV2cInventory(t *testing.T) {
	inv, err := ReadInventory(context.Background(), GoSNMPClient{}, simTarget(t, Credential{Version: "2c", Community: "edgeswitch"}))
	if err != nil {
		t.Fatal(err)
	}
	if inv.System.Name != "sim-edgeswitch" || inv.Vendor != "Ubiquiti (EdgeSwitch)" || inv.Model != "ES-48-500W" ||
		len(inv.Interfaces) != 52 || inv.Interfaces[0].Alias != "Uplink to MDF" || inv.Interfaces[0].SpeedBps != 1_000_000_000 {
		t.Errorf("inventory: %+v (interfaces %d)", inv.System, len(inv.Interfaces))
	}
}

// v1 has no GETBULK: the walk must use GETNEXT, and a device without ifXTable
// still lists its ports.
func TestSimV1WalkWithoutBulk(t *testing.T) {
	inv, err := ReadInventory(context.Background(), GoSNMPClient{}, simTarget(t, Credential{Version: "1", Community: "radio"}))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Vendor != "Cambium Networks" || len(inv.Interfaces) != 2 || inv.Interfaces[0].Name != "eth1" || inv.Model != "" {
		t.Errorf("v1 radio: %+v %+v", inv.System, inv.Interfaces)
	}
}

func TestSimV3(t *testing.T) {
	for _, cred := range []Credential{
		{Version: "3", Username: "sentinel-auth", AuthProtocol: "SHA", AuthPassword: "authpass123", PrivProtocol: "none"},
		{Version: "3", Username: "sentinel-priv", AuthProtocol: "SHA", AuthPassword: "authpass123", PrivProtocol: "AES", PrivPassword: "privpass123"},
	} {
		sys, err := Identify(context.Background(), GoSNMPClient{}, simTarget(t, cred))
		if err != nil || sys.Name != "sim-edgeswitch" {
			t.Errorf("%s: %+v %v", cred.Username, sys, err)
		}
	}
	bad := Credential{Version: "3", Username: "sentinel-priv", AuthProtocol: "SHA", AuthPassword: "wrongpass99", PrivProtocol: "AES", PrivPassword: "privpass123"}
	if _, err := Identify(context.Background(), GoSNMPClient{}, simTarget(t, bad)); err == nil {
		t.Error("wrong v3 password answered")
	}
}

// A wrong community and a closed port both fail, and within the timeout.
func TestSimFailures(t *testing.T) {
	tg := simTarget(t, Credential{Version: "2c", Community: "no-such-community"})
	start := time.Now()
	if _, err := Identify(context.Background(), GoSNMPClient{}, tg); err == nil {
		t.Error("wrong community answered")
	}
	tg.Port = 1199 // nothing listens there
	if _, err := Identify(context.Background(), GoSNMPClient{}, tg); err == nil {
		t.Error("closed port answered")
	}
	if time.Since(start) > 6*time.Second {
		t.Errorf("failures took %s; timeouts not honoured", time.Since(start))
	}
}

// Counters read through ReadStats move between two polls, and the profile's
// down ports (every third) read as down.
func TestSimStatsCountersIncrease(t *testing.T) {
	tg := simTarget(t, Credential{Version: "2c", Community: "edgeswitch"})
	a, err := ReadStats(context.Background(), GoSNMPClient{}, tg, []int{1, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	b, err := ReadStats(context.Background(), GoSNMPClient{}, tg, []int{1, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !a[1].HaveOctets || !a[1].OperUp || b[1].InOctets <= a[1].InOctets || a[1].SpeedBps != 1_000_000_000 {
		t.Errorf("port 1: %+v then %+v", a[1], b[1])
	}
	if a[3].OperUp {
		t.Errorf("port 3 should be down: %+v", a[3])
	}
}

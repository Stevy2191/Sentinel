package services

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestProberRefusesBlockedTargets(t *testing.T) {
	client := &fakeSNMP{}
	p := &Prober{client: client,
		resolve: func(context.Context, string) (net.IP, error) { return net.ParseIP("169.254.169.254"), nil },
		blocked: func(net.IP) bool { return true }}
	_, err := p.IdentifyWith(context.Background(), "metadata", 161, snmp.Credential{Version: "2c"}, time.Second, 0)
	if !errors.Is(err, ErrTargetBlocked) || client.gets != 0 {
		t.Fatalf("err %v, gets %d; want ErrTargetBlocked and no SNMP sent", err, client.gets)
	}

	p.blocked = func(net.IP) bool { return false }
	sys, err := p.IdentifyWith(context.Background(), "10.0.0.2", 161, snmp.Credential{Version: "2c"}, time.Second, 0)
	if err != nil || sys.Name != "core-sw-1" {
		t.Fatalf("allowed target: %+v %v", sys, err)
	}
}

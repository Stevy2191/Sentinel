package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestNextDriveBay(t *testing.T) {
	var prev *bayCounters
	steps := []struct {
		found         bool
		read, written uint64
		want          string
	}{
		{false, 0, 0, DriveBayAbsent},
		{true, 100, 200, DriveBayPresent},
		{true, 100, 200, DriveBayPresent},
		{true, 100, 4296, DriveBayActive},
		{true, 101, 4296, DriveBayActive},
		{true, 101, 10, DriveBayPresent}, // written went down: a reset
		{true, 101, 10, DriveBayPresent},
		{false, 0, 0, DriveBayAbsent},
	}
	for i, s := range steps {
		var got string
		got, prev = nextDriveBay(prev, s.found, s.read, s.written)
		if got != s.want {
			t.Fatalf("step %d: %s, want %s", i, got, s.want)
		}
	}
}

// fakeDiskSNMP answers the disk activity table: names and counters by disk.
type fakeDiskSNMP struct {
	disks   []string
	read    map[string]uint64
	written map[string]uint64
	err     error
	walks   int
}

func (f *fakeDiskSNMP) Get(context.Context, snmp.Target, []string) ([]snmp.PDU, error) {
	return nil, nil
}

func (f *fakeDiskSNMP) Walk(_ context.Context, _ snmp.Target, root string) ([]snmp.PDU, error) {
	f.walks++
	if f.err != nil {
		return nil, f.err
	}
	var out []snmp.PDU
	for i, name := range f.disks {
		oid := fmt.Sprintf("%s.%d", root, i+1)
		switch root {
		case oidDiskIODevice:
			out = append(out, snmp.PDU{OID: oid, Value: []byte(name)})
		case oidDiskIONReadX:
			out = append(out, snmp.PDU{OID: oid, Value: f.read[name]})
		case oidDiskIONWrittenX:
			out = append(out, snmp.PDU{OID: oid, Value: f.written[name]})
		}
	}
	return out, nil
}

func udmSEDevice() models.Device {
	return models.Device{ID: uuid.New(), Host: "10.0.0.1", Model: "UDM-SE"}
}

func TestDriveBayPoll(t *testing.T) {
	c := &fakeDiskSNMP{disks: []string{"sda", "sdb"}, read: map[string]uint64{"sda": 5, "sdb": 7}, written: map[string]uint64{"sda": 9, "sdb": 11}}
	m := NewDriveBayMonitor(c)
	d := udmSEDevice()
	ctx := context.Background()
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayPresent {
		t.Fatalf("first poll %s", got)
	}
	c.written["sdb"]++
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayActive {
		t.Fatalf("written +1: %s", got)
	}
	c.written["sda"] += 1000 // the built-in SSD is not the bay
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayPresent {
		t.Fatalf("only sda busy: %s", got)
	}
	c.disks = []string{"sda"}
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayAbsent {
		t.Fatalf("drive pulled: %s", got)
	}

	other := models.Device{ID: uuid.New(), Model: "USW-24"}
	walks := c.walks
	m.PollDriveBay(ctx, other, snmp.Target{})
	if c.walks != walks || m.State(other.ID) != DriveBayUnknown {
		t.Errorf("a model with no drive bay was walked (%d walks) or has state %s", c.walks-walks, m.State(other.ID))
	}
	m.Forget(d.ID)
	if got := m.State(d.ID); got != DriveBayUnknown {
		t.Errorf("after Forget: %s", got)
	}
}

// A walk that fails is unknown, not empty, and keeps the last counters.
func TestDriveBayWalkErrorIsUnknown(t *testing.T) {
	c := &fakeDiskSNMP{disks: []string{"sda", "sdb"}, read: map[string]uint64{"sdb": 7}, written: map[string]uint64{"sdb": 11}}
	m := NewDriveBayMonitor(c)
	d := udmSEDevice()
	ctx := context.Background()
	m.PollDriveBay(ctx, d, snmp.Target{})
	c.err = errors.New("request timeout")
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayUnknown {
		t.Fatalf("walk error: %s", got)
	}
	c.err = nil
	m.PollDriveBay(ctx, d, snmp.Target{})
	if got := m.State(d.ID); got != DriveBayPresent {
		t.Errorf("after the error, unchanged counters: %s", got)
	}
}

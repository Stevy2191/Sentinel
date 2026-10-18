package services

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// Drive bay states, for a realistic drawing's bay light.
const (
	DriveBayAbsent  = "absent"
	DriveBayPresent = "present"
	DriveBayActive  = "active"
	DriveBayUnknown = "unknown"
)

// UCD-DISKIO-MIB diskIOTable columns.
const (
	oidDiskIODevice    = "1.3.6.1.4.1.2021.13.15.1.1.2"
	oidDiskIONReadX    = "1.3.6.1.4.1.2021.13.15.1.1.12"
	oidDiskIONWrittenX = "1.3.6.1.4.1.2021.13.15.1.1.13"
)

type bayCounters struct{ read, written uint64 }

// nextDriveBay is one poll's drive bay state from the disk's counters (when
// found) and the previous poll's. A counter that went down (a restart or a
// reinserted drive) counts as present and becomes the new baseline.
func nextDriveBay(prev *bayCounters, found bool, read, written uint64) (string, *bayCounters) {
	if !found {
		return DriveBayAbsent, nil
	}
	next := &bayCounters{read: read, written: written}
	if prev == nil || read < prev.read || written < prev.written {
		return DriveBayPresent, next
	}
	if read > prev.read || written > prev.written {
		return DriveBayActive, next
	}
	return DriveBayPresent, next
}

type bayEntry struct {
	state    string
	counters *bayCounters
}

// DriveBayMonitor reads the bay disk's activity for devices whose drawing
// has a drive bay, each poll, and keeps the state in memory.
type DriveBayMonitor struct {
	client snmp.Client
	logger *log.Logger
	mu     sync.Mutex
	bays   map[uuid.UUID]bayEntry
}

func NewDriveBayMonitor(client snmp.Client) *DriveBayMonitor {
	return &DriveBayMonitor{client: client, logger: log.New(os.Stderr, "", log.LstdFlags), bays: map[uuid.UUID]bayEntry{}}
}

func effectiveModel(d models.Device) string {
	if d.ModelOverride != nil && strings.TrimSpace(*d.ModelOverride) != "" {
		return *d.ModelOverride
	}
	return d.Model
}

// PollDriveBay walks the disk activity table for a device whose drawing has
// a drive bay; any other device is left alone. A failed walk makes the
// state unknown but keeps the last counters.
func (m *DriveBayMonitor) PollDriveBay(ctx context.Context, d models.Device, t snmp.Target) {
	drawing := portmon.DrawingFor(effectiveModel(d))
	if drawing == nil || drawing.DriveBayDisk == "" {
		return
	}
	found, read, written, err := m.readDisk(ctx, t, drawing.DriveBayDisk)
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.bays[d.ID]
	if err != nil {
		if prev.state != DriveBayUnknown {
			m.logger.Printf("[snmp] drive bay of %s: %v", d.Host, err)
		}
		m.bays[d.ID] = bayEntry{state: DriveBayUnknown, counters: prev.counters}
		return
	}
	state, next := nextDriveBay(prev.counters, found, read, written)
	m.bays[d.ID] = bayEntry{state: state, counters: next}
}

// readDisk finds disk in the table and returns its read and written
// counters.
func (m *DriveBayMonitor) readDisk(ctx context.Context, t snmp.Target, disk string) (bool, uint64, uint64, error) {
	names, err := m.client.Walk(ctx, t, oidDiskIODevice)
	if err != nil {
		return false, 0, 0, err
	}
	row := ""
	for _, p := range names {
		if p.Text() == disk {
			row = p.OID[strings.LastIndex(p.OID, ".")+1:]
			break
		}
	}
	if row == "" {
		return false, 0, 0, nil
	}
	var counters [2]uint64
	for i, col := range []string{oidDiskIONReadX, oidDiskIONWrittenX} {
		pdus, err := m.client.Walk(ctx, t, col)
		if err != nil {
			return false, 0, 0, err
		}
		for _, p := range pdus {
			if strings.HasSuffix(p.OID, "."+row) {
				counters[i], _ = p.Number()
			}
		}
	}
	return true, counters[0], counters[1], nil
}

// Forget drops a device's state, as when it stops being up: its bay is
// unknown until it is polled again.
func (m *DriveBayMonitor) Forget(deviceID uuid.UUID) {
	m.mu.Lock()
	delete(m.bays, deviceID)
	m.mu.Unlock()
}

// State is the device's drive bay state; unknown when it has none.
func (m *DriveBayMonitor) State(deviceID uuid.UUID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.bays[deviceID]; ok {
		return e.state
	}
	return DriveBayUnknown
}

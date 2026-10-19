package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// The tables the L2 poll walks.
const (
	oidL2QFdbPort      = "1.3.6.1.2.1.17.7.1.2.2.1.2"
	oidL2QFdbStatus    = "1.3.6.1.2.1.17.7.1.2.2.1.3"
	oidL2Dot1dFdbPort  = "1.3.6.1.2.1.17.4.3.1.2"
	oidL2Dot1dFdbState = "1.3.6.1.2.1.17.4.3.1.3"
	oidL2BasePortIf    = "1.3.6.1.2.1.17.1.4.1.2"
	oidL2LLDPRem       = "1.0.8802.1.1.2.1.4.1.1"
	oidL2IPNetToPhys   = "1.3.6.1.2.1.4.35.1.4"
	oidL2IPNetToMedia  = "1.3.6.1.2.1.4.22.1.2"
)

// l2PollInterval is how often a device's MAC, LLDP and ARP tables are read:
// they change slowly and a busy switch's tables are large.
const l2PollInterval = 5 * time.Minute

// l2Concurrent is how many devices' tables are walked at once.
const l2Concurrent = 4

type l2Sighting struct {
	DeviceID  uuid.UUID `gorm:"column:device_id;primaryKey"`
	IfIndex   int       `gorm:"column:if_index;primaryKey"`
	VLAN      int       `gorm:"column:vlan;primaryKey"`
	MAC       string    `gorm:"column:mac;primaryKey"`
	FirstSeen time.Time `gorm:"column:first_seen"`
	LastSeen  time.Time `gorm:"column:last_seen"`
}

func (l2Sighting) TableName() string { return "l2.sightings" }

type l2Neighbor struct {
	DeviceID  uuid.UUID `gorm:"column:device_id;primaryKey"`
	IfIndex   int       `gorm:"column:if_index;primaryKey"`
	ChassisID string    `gorm:"column:chassis_id;primaryKey"`
	SysName   string    `gorm:"column:sys_name"`
	PortID    string    `gorm:"column:port_id"`
	PortDescr string    `gorm:"column:port_descr"`
	Caps      int       `gorm:"column:caps"`
	LastSeen  time.Time `gorm:"column:last_seen"`
}

func (l2Neighbor) TableName() string { return "l2.neighbors" }

type l2ARP struct {
	DeviceID uuid.UUID `gorm:"column:device_id;primaryKey"`
	IP       string    `gorm:"column:ip;primaryKey"`
	MAC      string    `gorm:"column:mac"`
	LastSeen time.Time `gorm:"column:last_seen"`
}

func (l2ARP) TableName() string { return "l2.arp" }

// L2Monitor reads each up device's MAC address table, LLDP neighbours and
// ARP table at most every five minutes, into the l2 schema.
type L2Monitor struct {
	db     *gorm.DB
	client snmp.Client
	now    func() time.Time
	logger *log.Logger

	sem chan struct{}

	mu      sync.Mutex
	last    map[uuid.UUID]time.Time
	running map[uuid.UUID]bool
}

func NewL2Monitor(db *gorm.DB, client snmp.Client) *L2Monitor {
	return &L2Monitor{db: db, client: client, now: time.Now, logger: log.New(os.Stderr, "", log.LstdFlags),
		sem: make(chan struct{}, l2Concurrent), last: map[uuid.UUID]time.Time{}, running: map[uuid.UUID]bool{}}
}

// start records a poll of the device now, unless it had one in the last
// five minutes or one is still running; done must follow a true.
func (m *L2Monitor) start(id uuid.UUID, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if last, ok := m.last[id]; m.running[id] || (ok && now.Sub(last) < l2PollInterval) {
		return false
	}
	m.last[id], m.running[id] = now, true
	return true
}

func (m *L2Monitor) done(id uuid.UUID) {
	m.mu.Lock()
	delete(m.running, id)
	m.mu.Unlock()
}

// walk reads one table; a failure is logged and leaves that table alone.
func (m *L2Monitor) walk(ctx context.Context, d models.Device, t snmp.Target, root string) ([]snmp.PDU, bool) {
	pdus, err := m.client.Walk(ctx, t, root)
	if err != nil {
		m.logger.Printf("[l2] %s: walking %s: %v", d.Host, root, err)
		return nil, false
	}
	return pdus, true
}

// PollL2 walks the device's tables and upserts what it finds with
// last_seen = now; rows not seen keep their old last_seen until the purge.
// Each table is written on its own, so one failing walk keeps the others.
// A UPS (a network card with no MAC table) is skipped. At most l2Concurrent
// devices are walked at once, and one poll per device.
func (m *L2Monitor) PollL2(ctx context.Context, d models.Device, t snmp.Target) {
	now := m.now().UTC()
	if IsUPS(d) || !m.start(d.ID, now) {
		return
	}
	defer m.done(d.ID)
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		return
	}
	var known []int
	if err := m.db.WithContext(ctx).Model(&models.DeviceInterface{}).Where("device_id = ?", d.ID).Pluck("if_index", &known).Error; err != nil {
		m.logger.Printf("[l2] %s: listing interfaces: %v", d.Host, err)
		return
	}
	ifIndexes := make(map[int]bool, len(known))
	for _, i := range known {
		ifIndexes[i] = true
	}
	basePDUs, _ := m.walk(ctx, d, t, oidL2BasePortIf)
	base := l2.ParseBasePorts(basePDUs)

	m.writeSightings(ctx, d, t, base, ifIndexes, now)
	m.writeNeighbors(ctx, d, t, base, ifIndexes, now)
	m.writeARP(ctx, d, t, now)
}

// bridgeToIfIndex maps a bridge port to its ifIndex: through
// dot1dBasePortIfIndex, else as an ifIndex the device has, else 0.
func bridgeToIfIndex(port int, base map[int]int, ifIndexes map[int]bool) int {
	if ix, ok := base[port]; ok {
		return ix
	}
	if ifIndexes[port] {
		return port
	}
	return 0
}

func (m *L2Monitor) writeSightings(ctx context.Context, d models.Device, t snmp.Target, base map[int]int, ifIndexes map[int]bool, now time.Time) {
	var entries []l2.FDBEntry
	if port, ok := m.walk(ctx, d, t, oidL2QFdbPort); ok && len(port) > 0 {
		status, _ := m.walk(ctx, d, t, oidL2QFdbStatus)
		entries = l2.ParseQBridgeFDB(port, status)
	} else if ok {
		if port, ok := m.walk(ctx, d, t, oidL2Dot1dFdbPort); ok && len(port) > 0 {
			status, _ := m.walk(ctx, d, t, oidL2Dot1dFdbState)
			entries = l2.ParseDot1dFDB(port, status)
		}
	}
	seen := map[l2Sighting]bool{}
	var rows []l2Sighting
	for _, e := range entries {
		ix := bridgeToIfIndex(e.BridgePort, base, ifIndexes)
		if ix == 0 {
			continue
		}
		key := l2Sighting{DeviceID: d.ID, IfIndex: ix, VLAN: e.VLAN, MAC: e.MAC}
		if seen[key] {
			continue
		}
		seen[key] = true
		key.FirstSeen, key.LastSeen = now, now
		rows = append(rows, key)
	}
	m.upsert(ctx, d, "sightings", rows, []string{"device_id", "if_index", "vlan", "mac"}, []string{"last_seen"})
}

func (m *L2Monitor) writeNeighbors(ctx context.Context, d models.Device, t snmp.Target, base map[int]int, ifIndexes map[int]bool, now time.Time) {
	cols := map[string][]snmp.PDU{}
	for _, col := range []string{"5", "7", "8", "9", "12"} {
		pdus, ok := m.walk(ctx, d, t, oidL2LLDPRem+"."+col)
		if !ok {
			return
		}
		cols[col] = pdus
	}
	seen := map[[2]any]bool{}
	var rows []l2Neighbor
	for _, n := range l2.ParseLLDP(cols) {
		if n.ChassisID == "" {
			continue
		}
		ix := bridgeToIfIndex(n.LocalPort, base, ifIndexes)
		if k := [2]any{ix, n.ChassisID}; !seen[k] {
			seen[k] = true
			rows = append(rows, l2Neighbor{DeviceID: d.ID, IfIndex: ix, ChassisID: n.ChassisID, SysName: n.SysName,
				PortID: n.PortID, PortDescr: n.PortDescr, Caps: int(n.Caps), LastSeen: now})
		}
	}
	m.upsert(ctx, d, "neighbors", rows, []string{"device_id", "if_index", "chassis_id"}, []string{"sys_name", "port_id", "port_descr", "caps", "last_seen"})
	// A complete walk is the whole truth: a neighbour it no longer lists
	// (an AP moved, a switch swapped for a PC) must stop classing its port.
	if err := m.db.WithContext(ctx).Exec(`DELETE FROM l2.neighbors WHERE device_id = ? AND last_seen < ?`, d.ID, now).Error; err != nil {
		m.logger.Printf("[l2] %s: dropping gone neighbours: %v", d.Host, err)
	}
}

func (m *L2Monitor) writeARP(ctx context.Context, d models.Device, t snmp.Target, now time.Time) {
	physical, _ := m.walk(ctx, d, t, oidL2IPNetToPhys)
	var media []snmp.PDU
	// The older table too when the newer gives no IPv4 (some fill it with
	// IPv6 only).
	if len(l2.ParseARP(physical, nil)) == 0 {
		media, _ = m.walk(ctx, d, t, oidL2IPNetToMedia)
	}
	seen := map[string]bool{}
	var rows []l2ARP
	for _, e := range l2.ParseARP(physical, media) {
		if !seen[e.IP] {
			seen[e.IP] = true
			rows = append(rows, l2ARP{DeviceID: d.ID, IP: e.IP, MAC: e.MAC, LastSeen: now})
		}
	}
	m.upsert(ctx, d, "arp", rows, []string{"device_id", "ip"}, []string{"mac", "last_seen"})
}

func upsertRows[T any](ctx context.Context, db *gorm.DB, rows []T, keys, update []string) error {
	if len(rows) == 0 {
		return nil
	}
	cols := make([]clause.Column, len(keys))
	for i, k := range keys {
		cols[i] = clause.Column{Name: k}
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: cols, DoUpdates: clause.AssignmentColumns(update)}).
		CreateInBatches(rows, 500).Error
}

func (m *L2Monitor) upsert(ctx context.Context, d models.Device, table string, rows any, keys, update []string) {
	var err error
	switch r := rows.(type) {
	case []l2Sighting:
		err = upsertRows(ctx, m.db, r, keys, update)
	case []l2Neighbor:
		err = upsertRows(ctx, m.db, r, keys, update)
	case []l2ARP:
		err = upsertRows(ctx, m.db, r, keys, update)
	}
	if err != nil {
		m.logger.Printf("[l2] %s: saving %s: %v", d.Host, table, err)
	}
}

// Purge deletes L2 rows (and cached DNS names) last seen before the given
// time, returning how many went.
func (m *L2Monitor) Purge(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, q := range []string{
			`DELETE FROM l2.sightings WHERE last_seen < ?`,
			`DELETE FROM l2.neighbors WHERE last_seen < ?`,
			`DELETE FROM l2.arp WHERE last_seen < ?`,
			`DELETE FROM l2.dns_names WHERE resolved_at < ?`,
		} {
			res := tx.Exec(q, before)
			if res.Error != nil {
				return fmt.Errorf("purging l2: %w", res.Error)
			}
			total += res.RowsAffected
		}
		return nil
	})
	return total, err
}

// deleteDeviceL2 removes a device's L2 rows, inside DeviceService.Delete's
// transaction (the l2 schema has no FKs to cascade them).
func deleteDeviceL2(tx *gorm.DB, id uuid.UUID) error {
	for _, table := range []string{"l2.sightings", "l2.neighbors", "l2.arp"} {
		if err := tx.Exec("DELETE FROM "+table+" WHERE device_id = ?", id).Error; err != nil {
			return fmt.Errorf("deleting %s rows: %w", table, err)
		}
	}
	return nil
}

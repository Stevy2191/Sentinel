package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// fakeL2SNMP answers walks from a table of roots; errs makes a root fail.
// A walk made in a context listed in contexts (the target's v3 context, else
// its community, such as "pub@20") answers from there and ctxErrs instead.
// walked records each walk as "<context> <root>".
type fakeL2SNMP struct {
	rows     map[string][]snmp.PDU
	errs     map[string]error
	contexts map[string]map[string][]snmp.PDU
	ctxErrs  map[string]map[string]error
	walks    int
	walked   []string
}

func (f *fakeL2SNMP) Get(context.Context, snmp.Target, []string) ([]snmp.PDU, error) {
	return nil, nil
}

func (f *fakeL2SNMP) Walk(_ context.Context, t snmp.Target, root string) ([]snmp.PDU, error) {
	f.walks++
	key := t.Credential.Community
	if t.Context != "" {
		key = t.Context
	}
	f.walked = append(f.walked, key+" "+root)
	if rows, ok := f.contexts[key]; ok {
		if err := f.ctxErrs[key][root]; err != nil {
			return nil, err
		}
		return rows[root], nil
	}
	if err := f.errs[root]; err != nil {
		return nil, err
	}
	return f.rows[root], nil
}

func p(oid string, v any) snmp.PDU { return snmp.PDU{OID: oid, Value: v} }

// switchWalk is a switch with two learned MACs (bridge ports 3 and 48, as
// ifIndex 3 and 48), an LLDP AP on local port 48 and two ARP rows.
func switchWalk() map[string][]snmp.PDU {
	lldp := oidL2LLDPRem
	return map[string][]snmp.PDU{
		oidL2QFdbPort: {
			p(oidL2QFdbPort+".1.0.17.34.51.68.85", int64(3)),
			p(oidL2QFdbPort+".1.36.90.76.1.2.3", int64(48)),
		},
		oidL2QFdbStatus: {
			p(oidL2QFdbStatus+".1.0.17.34.51.68.85", int64(3)),
			p(oidL2QFdbStatus+".1.36.90.76.1.2.3", int64(3)),
		},
		oidL2BasePortIf: {p(oidL2BasePortIf+".3", int64(3)), p(oidL2BasePortIf+".48", int64(48))},
		lldp + ".5":     {p(lldp+".5.0.48.1", []byte{0x24, 0x5a, 0x4c, 1, 2, 3})},
		lldp + ".9":     {p(lldp+".9.0.48.1", []byte("KitchenAP"))},
		lldp + ".12":    {p(lldp+".12.0.48.1", []byte{0x10})},
		oidL2IPNetToPhys: {
			p(oidL2IPNetToPhys+".10.1.4.10.1.10.60", []byte{0, 0x11, 0x22, 0x33, 0x44, 0x55}),
			p(oidL2IPNetToPhys+".10.1.4.10.1.10.61", []byte{0x24, 0x5a, 0x4c, 1, 2, 3}),
		},
	}
}

func l2Count(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	testdb.Must(t, db.Raw("SELECT count(*) FROM l2."+table).Scan(&n).Error)
	return n
}

func l2Device(t *testing.T, db *gorm.DB, ifIndexes ...int) models.Device {
	t.Helper()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	for _, i := range ifIndexes {
		seedPort(t, db, s.DeviceID, i, "", "")
	}
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	return d
}

func TestL2PollWritesTables(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	d := l2Device(t, db, 3, 48)
	c := &fakeL2SNMP{rows: switchWalk()}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }

	m.PollL2(ctx, d, snmp.Target{})
	if s, n, a := l2Count(t, db, "sightings"), l2Count(t, db, "neighbors"), l2Count(t, db, "arp"); s != 2 || n != 1 || a != 2 {
		t.Fatalf("sightings %d, neighbours %d, arp %d; want 2, 1, 2", s, n, a)
	}
	var ifIndex int
	testdb.Must(t, db.Raw(`SELECT if_index FROM l2.sightings WHERE mac = '00:11:22:33:44:55'`).Scan(&ifIndex).Error)
	if ifIndex != 3 {
		t.Errorf("sighting on ifIndex %d, want 3", ifIndex)
	}

	walks := c.walks
	now = now.Add(4 * time.Minute)
	m.PollL2(ctx, d, snmp.Target{})
	if c.walks != walks {
		t.Fatalf("a poll within 5 minutes walked %d more tables", c.walks-walks)
	}

	now = now.Add(2 * time.Minute)
	m.PollL2(ctx, d, snmp.Target{})
	var last time.Time
	testdb.Must(t, db.Raw(`SELECT max(last_seen) FROM l2.sightings`).Scan(&last).Error)
	if !last.Equal(now) {
		t.Errorf("last_seen %v, want %v", last, now)
	}
	var first time.Time
	testdb.Must(t, db.Raw(`SELECT min(first_seen) FROM l2.sightings`).Scan(&first).Error)
	if !first.Equal(now.Add(-6 * time.Minute)) {
		t.Errorf("first_seen moved to %v", first)
	}
}

func TestL2PollTableFailureKeepsOthers(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 48)
	c := &fakeL2SNMP{rows: switchWalk(), errs: map[string]error{}}
	for _, col := range []string{"5", "7", "8", "9", "12"} {
		c.errs[oidL2LLDPRem+"."+col] = errors.New("timeout")
	}
	NewL2Monitor(db, c).PollL2(context.Background(), d, snmp.Target{})
	if s, n, a := l2Count(t, db, "sightings"), l2Count(t, db, "neighbors"), l2Count(t, db, "arp"); s != 2 || n != 0 || a != 2 {
		t.Errorf("sightings %d, neighbours %d, arp %d; want 2, 0, 2", s, n, a)
	}
}

// QuantumLink's walk: LLDP local ports 78 (bridge port 78 is ifIndex 52),
// 34 (not a bridge port, but an ifIndex) and 1100026 (neither).
func TestL2LLDPPortMapping(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 34, 52)
	lldp := oidL2LLDPRem
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{
		oidL2BasePortIf: {p(oidL2BasePortIf+".78", int64(52))},
		lldp + ".5": {
			p(lldp+".5.0.78.1", []byte{2, 0, 0, 0, 0, 78}),
			p(lldp+".5.0.34.2", []byte{2, 0, 0, 0, 0, 34}),
			p(lldp+".5.0.1100026.3", []byte{2, 0, 0, 0, 0, 26}),
		},
		lldp + ".9": {
			p(lldp+".9.0.78.1", []byte("KitchenAP")),
			p(lldp+".9.0.34.2", []byte("TS5410R81E")),
			p(lldp+".9.0.1100026.3", []byte("Quantum-Gate")),
		},
	}}
	NewL2Monitor(db, c).PollL2(context.Background(), d, snmp.Target{})
	got := map[string]int{}
	rows, err := db.Raw(`SELECT sys_name, if_index FROM l2.neighbors`).Rows()
	testdb.Must(t, err)
	defer rows.Close()
	for rows.Next() {
		var name string
		var ix int
		testdb.Must(t, rows.Scan(&name, &ix))
		got[name] = ix
	}
	want := map[string]int{"KitchenAP": 52, "TS5410R81E": 34, "Quantum-Gate": 0}
	for name, ix := range want {
		if v, ok := got[name]; !ok || v != ix {
			t.Errorf("%s: if_index %d (present %v), want %d", name, v, ok, ix)
		}
	}
}

func TestDBL2DeviceDeleteRemovesRows(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	keep := seedDevice(t, db, "Branch", "10.0.0.3")
	d := l2Device(t, db, 3)
	for _, id := range []any{d.ID, keep.DeviceID} {
		testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, mac) VALUES (?, 3, 'aa:bb:cc:dd:ee:ff')`, id)
		testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id) VALUES (?, 3, 'x')`, id)
		testdb.Exec(t, db, `INSERT INTO l2.arp (device_id, ip, mac) VALUES (?, '10.0.0.9', 'aa:bb:cc:dd:ee:ff')`, id)
	}
	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	if _, err := devices.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sightings", "neighbors", "arp"} {
		if n := l2Count(t, db, table); n != 1 {
			t.Errorf("l2.%s: %d rows, want only the other device's", table, n)
		}
	}
}

func TestDBL2Purge(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db)
	now := time.Now().UTC()
	for i, age := range []int{31, 29} {
		seen := now.AddDate(0, 0, -age)
		mac := []string{"aa:aa:aa:aa:aa:aa", "bb:bb:bb:bb:bb:bb"}[i]
		testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, mac, first_seen, last_seen) VALUES (?, 3, ?, ?, ?)`, d.ID, mac, seen, seen)
		testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, last_seen) VALUES (?, 3, ?, ?)`, d.ID, mac, seen)
		testdb.Exec(t, db, `INSERT INTO l2.arp (device_id, ip, mac, last_seen) VALUES (?, ?, ?, ?)`, d.ID, mac, mac, seen)
		testdb.Exec(t, db, `INSERT INTO l2.dns_names (ip, name, resolved_at) VALUES (?, 'x', ?)`, mac, seen)
	}
	n, err := NewL2Monitor(db, nil).Purge(context.Background(), now.AddDate(0, 0, -30))
	testdb.Must(t, err)
	if n != 4 {
		t.Errorf("purged %d rows, want 4", n)
	}
	for _, table := range []string{"sightings", "neighbors", "arp", "dns_names"} {
		if c := l2Count(t, db, table); c != 1 {
			t.Errorf("l2.%s: %d rows left, want the 29-day-old one", table, c)
		}
	}
}

// The nightly maintenance runs the 30-day purge.
func TestDBNetworkMaintenancePurgesL2(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db)
	old := time.Now().UTC().AddDate(0, 0, -31)
	testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, mac, first_seen, last_seen) VALUES (?, 3, 'aa:aa:aa:aa:aa:aa', ?, ?)`, d.ID, old, old)
	n := NewNetworkMaintenance(NewMetricsStore(db), NewSettingsService(db))
	n.SetL2(NewL2Monitor(db, nil))
	_ = n.RunOnce(context.Background())
	if c := l2Count(t, db, "sightings"); c != 0 {
		t.Errorf("%d old sightings left", c)
	}
}

// Config backups dump public.* only: endpoint names go in, the L2 tables
// (regenerated by the next poll) stay out.
func TestDBBackupExcludesL2(t *testing.T) {
	b := &BackupService{db: DBConfig{Host: "h", Port: "5432", User: "u", Name: "n"}}
	args := strings.Join(b.dumpArgs(), " ")
	if !strings.Contains(args, "--table=public.*") || strings.Contains(args, "l2") {
		t.Fatalf("dump args %q", args)
	}
	container := os.Getenv("SENTINEL_TEST_DB_CONTAINER")
	if container == "" {
		t.Skip("SENTINEL_TEST_DB_CONTAINER not set; run `make test-db`")
	}
	db := testdb.Open(t)
	d := l2Device(t, db)
	testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, mac) VALUES (?, 3, 'aa:bb:cc:dd:ee:ff')`, d.ID)
	testdb.Exec(t, db, `INSERT INTO endpoint_names (mac, name) VALUES ('aa:bb:cc:dd:ee:ff', 'Front desk phone')`)
	var name string
	testdb.Must(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	b = &BackupService{db: DBConfig{Host: "127.0.0.1", Port: "5432", User: "sentinel", Name: name}}
	out, err := exec.Command("docker", append([]string{"exec", "-e", "PGPASSWORD=test", container, "pg_dump"}, b.dumpArgs()...)...).Output()
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	dump := string(out)
	if !strings.Contains(dump, "Front desk phone") {
		t.Error("the dump is missing endpoint_names")
	}
	if strings.Contains(dump, "CREATE TABLE l2.") || strings.Contains(dump, "COPY l2.") || strings.Contains(dump, "SCHEMA l2") {
		t.Error("the dump includes the l2 schema")
	}
}

// A UPS card has no MAC table: its poll walks nothing.
func TestL2SkipsUPS(t *testing.T) {
	c := &fakeL2SNMP{rows: switchWalk()}
	ups := models.Device{Host: "10.0.0.5", DeviceTypeDetected: models.DeviceTypeUPS}
	NewL2Monitor(nil, c).PollL2(context.Background(), ups, snmp.Target{})
	if c.walks != 0 {
		t.Errorf("walked %d tables on a UPS", c.walks)
	}
}

// Review: a neighbour that has gone (an AP moved to another port) is
// dropped by the next successful LLDP walk, so its old port is no longer
// classed by it.
func TestL2PollDropsGoneNeighbors(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 7, 9)
	lldp := oidL2LLDPRem
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{
		lldp + ".5": {p(lldp+".5.0.7.1", []byte{0x24, 0x5a, 0x4c, 1, 2, 3})},
		lldp + ".9": {p(lldp+".9.0.7.1", []byte("KitchenAP"))},
	}}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})
	c.rows[lldp+".5"] = []snmp.PDU{p(lldp+".5.0.9.1", []byte{0x24, 0x5a, 0x4c, 1, 2, 3})}
	c.rows[lldp+".9"] = []snmp.PDU{p(lldp+".9.0.9.1", []byte("KitchenAP"))}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	var ports []int
	testdb.Must(t, db.Raw(`SELECT if_index FROM l2.neighbors ORDER BY if_index`).Scan(&ports).Error)
	if len(ports) != 1 || ports[0] != 9 {
		t.Errorf("neighbour ports %v, want [9]", ports)
	}
}

// Review: a device whose ipNetToPhysical table holds only IPv6 rows still
// has its IPv4 ARP read from ipNetToMedia.
func TestL2PollARPMediaWhenPhysicalIPv6Only(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db)
	mac := []byte{0x3c, 0xef, 0x8c, 0x12, 0x9a, 0x41}
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{
		oidL2IPNetToPhys:  {p(oidL2IPNetToPhys+".13.2.16.254.128.0.0.0.0.0.0.42.37.95.255.254.204.73.241", mac)},
		oidL2IPNetToMedia: {p(oidL2IPNetToMedia+".59.10.1.10.61", mac)},
	}}
	NewL2Monitor(db, c).PollL2(context.Background(), d, snmp.Target{})
	if n := l2Count(t, db, "arp"); n != 1 {
		t.Errorf("arp rows %d, want 1", n)
	}
}

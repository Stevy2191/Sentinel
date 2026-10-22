package services

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

var ciscoTarget = snmp.Target{Credential: snmp.Credential{Version: "2c", Community: "pub"}}

// ciscoWalk is a Cisco's default context: no Q-BRIDGE rows, operational
// ethernet VLANs 1 and 20, and VLAN 1's dot1d table (MAC …:04 on bridge
// port 3), which is all a Cisco's default context reports.
func ciscoWalk() map[string][]snmp.PDU {
	s, k := l2.OIDVtpVlanState, l2.OIDVtpVlanType
	return map[string][]snmp.PDU{
		oidL2QFdbPort:     nil,
		s:                 {p(s+".1.1", int64(1)), p(s+".1.20", int64(1))},
		k:                 {p(k+".1.1", int64(1)), p(k+".1.20", int64(1))},
		oidL2Dot1dFdbPort: {p(oidL2Dot1dFdbPort+".2.0.0.0.0.4", int64(3))},
		oidL2BasePortIf:   {p(oidL2BasePortIf+".3", int64(3))},
	}
}

// ciscoVLANs is VLAN 1's and VLAN 20's tables in their contexts: …:01 on
// bridge port 3 (ifIndex 3) in VLAN 1; …:02 on bridge port 7 (ifIndex 5)
// and …:03 on bridge port 9 (ifIndex 78, a Port-channel) in VLAN 20.
func ciscoVLANs() map[string]map[string][]snmp.PDU {
	return map[string]map[string][]snmp.PDU{
		"pub@1": {
			oidL2Dot1dFdbPort: {p(oidL2Dot1dFdbPort+".2.0.0.0.0.1", int64(3))},
			oidL2BasePortIf:   {p(oidL2BasePortIf+".3", int64(3))},
		},
		"pub@20": {
			oidL2Dot1dFdbPort: {p(oidL2Dot1dFdbPort+".2.0.0.0.0.2", int64(7)), p(oidL2Dot1dFdbPort+".2.0.0.0.0.3", int64(9))},
			oidL2BasePortIf:   {p(oidL2BasePortIf+".7", int64(5)), p(oidL2BasePortIf+".9", int64(78))},
		},
	}
}

type sightingRow struct {
	IfIndex, VLAN int
	MAC           string
	LastSeen      time.Time
}

func sightingRows(t *testing.T, db *gorm.DB) []sightingRow {
	t.Helper()
	var rows []sightingRow
	testdb.Must(t, db.Raw(`SELECT if_index, vlan, mac, last_seen FROM l2.sightings ORDER BY mac`).Scan(&rows).Error)
	return rows
}

func withoutTimes(rows []sightingRow) []sightingRow {
	out := make([]sightingRow, len(rows))
	for i, r := range rows {
		out[i] = sightingRow{IfIndex: r.IfIndex, VLAN: r.VLAN, MAC: r.MAC}
	}
	return out
}

func TestL2PollPerVLANSightings(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	c := &fakeL2SNMP{rows: ciscoWalk(), contexts: ciscoVLANs()}
	NewL2Monitor(db, c).PollL2(context.Background(), d, ciscoTarget)
	want := []sightingRow{{3, 1, "02:00:00:00:00:01", time.Time{}}, {5, 20, "02:00:00:00:00:02", time.Time{}}, {78, 20, "02:00:00:00:00:03", time.Time{}}}
	if got := withoutTimes(sightingRows(t, db)); !reflect.DeepEqual(got, want) {
		t.Errorf("sightings = %v, want %v", got, want)
	}
	if slices.Contains(c.walked, "pub "+oidL2Dot1dFdbPort) {
		t.Error("the default context's dot1d table was walked too (it is VLAN 1, read in its own context)")
	}
}

func TestL2PollFailedVLANKeepsItsRows(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	c := &fakeL2SNMP{rows: ciscoWalk(), contexts: ciscoVLANs()}
	m := NewL2Monitor(db, c)
	first := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	now := first
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, ciscoTarget)
	now = first.Add(6 * time.Minute)
	c.ctxErrs = map[string]map[string]error{"pub@20": {oidL2Dot1dFdbPort: errors.New("timeout")}}
	m.PollL2(context.Background(), d, ciscoTarget)
	for _, r := range sightingRows(t, db) {
		want := first
		if r.VLAN == 1 {
			want = now
		}
		if !r.LastSeen.Equal(want) {
			t.Errorf("%s in VLAN %d last seen %v, want %v", r.MAC, r.VLAN, r.LastSeen, want)
		}
	}
}

// A community whose view has no VLAN contexts still reads VLAN 1 from the
// default context.
func TestL2PollAllVLANsFailReadsDefaultContext(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	fail := map[string]error{oidL2Dot1dFdbPort: errors.New("timeout")}
	c := &fakeL2SNMP{rows: ciscoWalk(), contexts: ciscoVLANs(), ctxErrs: map[string]map[string]error{"pub@1": fail, "pub@20": fail}}
	NewL2Monitor(db, c).PollL2(context.Background(), d, ciscoTarget)
	if got, want := withoutTimes(sightingRows(t, db)), []sightingRow{{IfIndex: 3, VLAN: 0, MAC: "02:00:00:00:00:04"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sightings = %v, want %v", got, want)
	}
}

// A view or v3 group without the VLAN contexts answers them empty
// (noSuchObject, authorizationError), not with an error: VLAN 1 still comes
// from the default context.
func TestL2PollEmptyVLANContextsReadDefaultContext(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	c := &fakeL2SNMP{rows: ciscoWalk(), contexts: map[string]map[string][]snmp.PDU{"pub@1": {}, "pub@20": {}}}
	NewL2Monitor(db, c).PollL2(context.Background(), d, ciscoTarget)
	if got, want := withoutTimes(sightingRows(t, db)), []sightingRow{{IfIndex: 3, VLAN: 0, MAC: "02:00:00:00:00:04"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sightings = %v, want %v", got, want)
	}
}

// A switch that refuses community@vlan times out on each VLAN: after two
// with none read, the rest aren't tried.
func TestL2PollGivesUpOnRefusedVLANContexts(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	rows := ciscoWalk()
	s, k := l2.OIDVtpVlanState, l2.OIDVtpVlanType
	rows[s] = append(rows[s], p(s+".1.30", int64(1)))
	rows[k] = append(rows[k], p(k+".1.30", int64(1)))
	fail := map[string]error{oidL2Dot1dFdbPort: errors.New("timeout")}
	ctx := map[string]map[string][]snmp.PDU{"pub@1": {}, "pub@20": {}, "pub@30": {}}
	c := &fakeL2SNMP{rows: rows, contexts: ctx, ctxErrs: map[string]map[string]error{"pub@1": fail, "pub@20": fail, "pub@30": fail}}
	NewL2Monitor(db, c).PollL2(context.Background(), d, ciscoTarget)
	if slices.Contains(c.walked, "pub@30 "+oidL2Dot1dFdbPort) {
		t.Error("tried VLAN 30 after VLANs 1 and 20 both failed")
	}
	if got := len(sightingRows(t, db)); got != 1 {
		t.Errorf("%d sightings, want the default context's one", got)
	}
}

func TestL2PollNoVLANTableReadsDefaultContext(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 5, 78)
	rows := ciscoWalk()
	delete(rows, l2.OIDVtpVlanState)
	delete(rows, l2.OIDVtpVlanType)
	NewL2Monitor(db, &fakeL2SNMP{rows: rows}).PollL2(context.Background(), d, ciscoTarget)
	if got, want := withoutTimes(sightingRows(t, db)), []sightingRow{{IfIndex: 3, VLAN: 0, MAC: "02:00:00:00:00:04"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sightings = %v, want %v", got, want)
	}
}

// A switch with a Q-BRIDGE table (UniFi, EdgeSwitch) never reads the
// Cisco VLAN table.
func TestL2PollQBridgeSkipsVLANTable(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 48)
	c := &fakeL2SNMP{rows: switchWalk()}
	NewL2Monitor(db, c).PollL2(context.Background(), d, ciscoTarget)
	for _, w := range c.walked {
		if w == "pub "+l2.OIDVtpVlanState {
			t.Fatal("walked the VLAN table on a Q-BRIDGE switch")
		}
	}
}

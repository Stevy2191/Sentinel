package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func lagOf(t *testing.T, db *gorm.DB, dev uuid.UUID) map[int]*int {
	t.Helper()
	var rows []struct {
		IfIndex    int
		LagIfIndex *int
	}
	testdb.Must(t, db.Raw(`SELECT if_index, lag_if_index FROM device_interfaces WHERE device_id = ?`, dev).Scan(&rows).Error)
	out := map[int]*int{}
	for _, r := range rows {
		out[r.IfIndex] = r.LagIfIndex
	}
	return out
}

func asRouter(t *testing.T, db *gorm.DB, d models.Device) models.Device {
	t.Helper()
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'router' WHERE id = ?`, d.ID)
	testdb.Must(t, db.First(&d, "id = ?", d.ID).Error)
	return d
}

func TestL2PollWritesLAGMembers(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 49, 50, 66)
	testdb.Exec(t, db, `UPDATE device_interfaces SET if_type = 161 WHERE device_id = ? AND if_index = 66`, d.ID)
	agg := l2.OIDDot3adAttachedAgg
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{agg: {p(agg+".49", int64(66)), p(agg+".50", int64(66))}}, errs: map[string]error{}}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})
	got := lagOf(t, db, d.ID)
	if got[49] == nil || *got[49] != 66 || got[50] == nil || *got[50] != 66 || got[66] != nil {
		t.Fatalf("lag_if_index = 49:%v 50:%v 66:%v, want 66, 66, NULL", got[49], got[50], got[66])
	}

	// A walk that errors leaves them.
	c.errs[agg] = errors.New("timeout")
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := lagOf(t, db, d.ID); got[49] == nil {
		t.Fatal("a failed LAG walk cleared the members")
	}

	// A walk that answers with no members clears them.
	delete(c.errs, agg)
	c.rows[agg] = []snmp.PDU{p(agg+".49", int64(0)), p(agg+".50", int64(0))}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := lagOf(t, db, d.ID); got[49] != nil || got[50] != nil {
		t.Fatalf("lag_if_index = 49:%v 50:%v after the LAG went, want NULL", got[49], got[50])
	}
}

func cdpRows(ifIndex string) map[string][]snmp.PDU {
	c := l2.OIDCDPCache
	return map[string][]snmp.PDU{
		c + ".3": {p(c+".3."+ifIndex+".1", int64(1))},
		c + ".4": {p(c+".4."+ifIndex+".1", []byte{10, 20, 0, 5})},
		c + ".6": {p(c+".6."+ifIndex+".1", []byte("LAB-C3850.corp.example"))},
		c + ".7": {p(c+".7."+ifIndex+".1", []byte("GigabitEthernet1/0/1"))},
		c + ".8": {p(c+".8."+ifIndex+".1", []byte("cisco WS-C3850-12S"))},
	}
}

func TestL2PollWritesCDP(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3)
	m := NewL2Monitor(db, &fakeL2SNMP{rows: cdpRows("3")})
	m.PollL2(context.Background(), d, snmp.Target{})
	var n struct {
		IfIndex                                                  int
		ChassisID, SysName, PortID, PortDescr, Protocol, Address string
	}
	testdb.Must(t, db.Raw(`SELECT if_index, chassis_id, sys_name, port_id, port_descr, protocol, address FROM l2.neighbors`).Scan(&n).Error)
	want := struct {
		IfIndex                                                  int
		ChassisID, SysName, PortID, PortDescr, Protocol, Address string
	}{3, "LAB-C3850.corp.example", "LAB-C3850", "GigabitEthernet1/0/1", "cisco WS-C3850-12S", "cdp", "10.20.0.5"}
	if n != want {
		t.Errorf("CDP neighbour = %+v, want %+v", n, want)
	}
}

// Review Focus 1: each protocol's complete walk drops only its own rows.
func TestL2PollKeepsCDPWhenLLDPRewrites(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3, 7)
	lldp := oidL2LLDPRem
	rows := cdpRows("3")
	rows[lldp+".5"] = []snmp.PDU{p(lldp+".5.0.7.1", []byte{0x24, 0x5a, 0x4c, 1, 2, 3})}
	rows[lldp+".9"] = []snmp.PDU{p(lldp+".9.0.7.1", []byte("core-sw"))}
	c := &fakeL2SNMP{rows: rows}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})
	protocols := func() []string {
		var out []string
		testdb.Must(t, db.Raw(`SELECT protocol FROM l2.neighbors ORDER BY protocol`).Scan(&out).Error)
		return out
	}
	if got := protocols(); len(got) != 2 {
		t.Fatalf("after the first poll, neighbours %v, want cdp and lldp", got)
	}

	// LLDP gone and the CDP walk timing out: the CDP row stays, since only
	// a complete CDP walk may drop CDP rows.
	delete(c.rows, lldp+".5")
	delete(c.rows, lldp+".9")
	c.errs = map[string]error{l2.OIDCDPCache + ".6": errors.New("timeout")}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := protocols(); len(got) != 1 || got[0] != "cdp" {
		t.Fatalf("LLDP gone, CDP walk failed: neighbours %v, want [cdp]", got)
	}
	c.errs = nil

	c.rows = map[string][]snmp.PDU{lldp + ".5": {p(lldp+".5.0.7.1", []byte{0x24, 0x5a, 0x4c, 1, 2, 3})}, lldp + ".9": {p(lldp+".9.0.7.1", []byte("core-sw"))}}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := protocols(); len(got) != 1 || got[0] != "lldp" {
		t.Fatalf("CDP gone: neighbours %v, want [lldp]", got)
	}
}

func wanEvidence(t *testing.T, db *gorm.DB, dev uuid.UUID) map[int]string {
	t.Helper()
	var rows []struct {
		IfIndex     int
		WANEvidence string `gorm:"column:wan_evidence"`
	}
	testdb.Must(t, db.Raw(`SELECT if_index, wan_evidence FROM device_interfaces WHERE device_id = ?`, dev).Scan(&rows).Error)
	out := map[int]string{}
	for _, r := range rows {
		out[r.IfIndex] = r.WANEvidence
	}
	return out
}

func TestL2PollWANEvidenceRouterOnly(t *testing.T) {
	db := testdb.Open(t)
	d := asRouter(t, db, l2Device(t, db, 13, 58))
	a := l2.OIDIPAdEntIfIndex
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{a: {p(a+".108.204.170.224", int64(13)), p(a+".10.99.1.1", int64(58))}}}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := wanEvidence(t, db, d.ID); got[13] != "public_address" || got[58] != "" {
		t.Fatalf("wan_evidence = %v, want 13 public_address, 58 ''", got)
	}

	cidr := l2.OIDCidrRouteIfIndexDefault
	c.rows[cidr] = []snmp.PDU{p(cidr+".0.0.0.0.0.10.1.1.254", int64(58))}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := wanEvidence(t, db, d.ID); got[13] != "public_address" || got[58] != "default_route" {
		t.Fatalf("wan_evidence = %v, want 13 public_address, 58 default_route", got)
	}

	ss := seedDevice(t, db, "Annex", "10.0.0.3")
	seedPort(t, db, ss.DeviceID, 13, "", "")
	var sw models.Device
	testdb.Must(t, db.First(&sw, "id = ?", ss.DeviceID).Error)
	m.PollL2(context.Background(), sw, snmp.Target{})
	if got := wanEvidence(t, db, sw.ID); got[13] != "" {
		t.Errorf("a switch got wan_evidence %q", got[13])
	}
}

func TestL2PollCallsAfterPoll(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 3)
	m := NewL2Monitor(db, &fakeL2SNMP{})
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	var calls []uuid.UUID
	m.SetAfterPoll(func(_ context.Context, id uuid.UUID) { calls = append(calls, id) })
	m.PollL2(context.Background(), d, snmp.Target{})
	now = now.Add(time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if len(calls) != 1 || calls[0] != d.ID {
		t.Errorf("after-poll calls %v, want one for %s", calls, d.ID)
	}
}

// A LAG member that goes down drops out of the LLDP table and the LAG; its
// neighbour and LAG membership stay, so the bundle keeps both cables and
// its role doesn't flap. A port that's up and gone from the walk still goes.
func TestL2PollKeepsDownPortsNeighbours(t *testing.T) {
	db := testdb.Open(t)
	d := l2Device(t, db, 49, 50, 51, 66)
	testdb.Exec(t, db, `UPDATE device_interfaces SET if_type = 161 WHERE device_id = ? AND if_index = 66`, d.ID)
	testdb.Exec(t, db, `UPDATE device_interfaces SET oper_status = 'up' WHERE device_id = ?`, d.ID)
	lldp, agg := oidL2LLDPRem, l2.OIDDot3adAttachedAgg
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{
		lldp + ".5": {p(lldp+".5.0.49.1", []byte{2, 0, 0, 0, 0, 1}), p(lldp+".5.0.50.2", []byte{2, 0, 0, 0, 0, 1}), p(lldp+".5.0.51.3", []byte{2, 0, 0, 0, 0, 2})},
		lldp + ".9": {p(lldp+".9.0.49.1", []byte("core-sw")), p(lldp+".9.0.50.2", []byte("core-sw")), p(lldp+".9.0.51.3", []byte("KitchenAP"))},
		agg:         {p(agg+".49", int64(66)), p(agg+".50", int64(66))},
	}, errs: map[string]error{}}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})

	// 50 goes down; 51 is up but its AP moved away.
	testdb.Exec(t, db, `UPDATE device_interfaces SET oper_status = 'down' WHERE device_id = ? AND if_index = 50`, d.ID)
	c.rows[lldp+".5"] = c.rows[lldp+".5"][:1]
	c.rows[lldp+".9"] = c.rows[lldp+".9"][:1]
	c.rows[agg] = []snmp.PDU{p(agg+".49", int64(66)), p(agg+".50", int64(0))}
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})

	var ports []int
	testdb.Must(t, db.Raw(`SELECT if_index FROM l2.neighbors WHERE device_id = ? ORDER BY if_index`, d.ID).Scan(&ports).Error)
	if len(ports) != 2 || ports[0] != 49 || ports[1] != 50 {
		t.Errorf("neighbours on ports %v, want [49 50]: the down member kept, the up port with nothing on it dropped", ports)
	}
	if got := lagOf(t, db, d.ID); got[50] == nil || *got[50] != 66 {
		t.Errorf("lag_if_index of the down member = %v, want 66", got[50])
	}
}

// Route walks that time out say nothing about the default route: the port
// it went out of keeps its evidence. Walks that answer with none clear it.
func TestL2PollWANKeepsRouteWhenWalksFail(t *testing.T) {
	db := testdb.Open(t)
	d := asRouter(t, db, l2Device(t, db, 13, 58))
	a, cidr, route := l2.OIDIPAdEntIfIndex, l2.OIDCidrRouteIfIndexDefault, l2.OIDRouteIfIndex
	c := &fakeL2SNMP{rows: map[string][]snmp.PDU{
		a:    {p(a+".10.99.1.1", int64(58))},
		cidr: {p(cidr+".0.0.0.0.0.10.1.1.254", int64(13))},
	}, errs: map[string]error{}}
	m := NewL2Monitor(db, c)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := wanEvidence(t, db, d.ID); got[13] != "default_route" {
		t.Fatalf("wan_evidence = %v, want 13 default_route", got)
	}

	c.errs[cidr] = errors.New("timeout")
	c.errs[route] = errors.New("timeout")
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := wanEvidence(t, db, d.ID); got[13] != "default_route" {
		t.Errorf("both route walks timed out: wan_evidence = %v, want 13 still default_route", got)
	}

	c.errs = map[string]error{}
	delete(c.rows, cidr)
	now = now.Add(6 * time.Minute)
	m.PollL2(context.Background(), d, snmp.Target{})
	if got := wanEvidence(t, db, d.ID); got[13] != "" {
		t.Errorf("no default route any more: wan_evidence = %v, want 13 ''", got)
	}
}

package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// New ports start automatic, and the new columns only take their own values.
func TestDBPortRoleColumns(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	seedPort(t, db, s.DeviceID, 1, "0/1", "")
	var row struct {
		RoleSource, RoleReason, WANEvidence string
		LagIfIndex                          *int
	}
	testdb.Must(t, db.Raw(`SELECT role_source, role_reason, wan_evidence, lag_if_index FROM device_interfaces
		WHERE device_id = ? AND if_index = 1`, s.DeviceID).Scan(&row).Error)
	if row.RoleSource != "default" || row.RoleReason != "" || row.WANEvidence != "" || row.LagIfIndex != nil {
		t.Fatalf("new port = %+v, want default, '', '', NULL", row)
	}
	if err := db.Exec(`UPDATE device_interfaces SET role_source = 'bogus' WHERE device_id = ?`, s.DeviceID).Error; err == nil {
		t.Error("role_source 'bogus' was accepted")
	}
	if err := db.Exec(`UPDATE device_interfaces SET wan_evidence = 'x' WHERE device_id = ?`, s.DeviceID).Error; err == nil {
		t.Error("wan_evidence 'x' was accepted")
	}
	for _, proto := range []string{"lldp", "cdp"} {
		testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, protocol) VALUES (?, 1, 'core-sw', ?)`, s.DeviceID, proto)
	}
	if err := db.Exec(`INSERT INTO l2.neighbors (device_id, if_index, chassis_id, protocol) VALUES (?, 1, 'x', 'isdp')`, s.DeviceID).Error; err == nil {
		t.Error("protocol 'isdp' was accepted")
	}
}

// An inventory rewrites a port's SNMP facts, never what the L2 poll and the
// role rules keep on it.
func TestDBSaveInventoryKeepsRoleSource(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ctx := context.Background()
	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'uplink', role_source = 'auto', role_reason = 'x', lag_if_index = 66,
		wan_evidence = 'public_address' WHERE device_id = ? AND if_index = 49`, s.DeviceID)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	var row struct {
		Role, RoleSource, RoleReason, WANEvidence string
		LagIfIndex                                *int
	}
	testdb.Must(t, db.Raw(`SELECT role, role_source, role_reason, wan_evidence, lag_if_index FROM device_interfaces
		WHERE device_id = ? AND if_index = 49`, s.DeviceID).Scan(&row).Error)
	if row.Role != "uplink" || row.RoleSource != "auto" || row.RoleReason != "x" || row.WANEvidence != "public_address" ||
		row.LagIfIndex == nil || *row.LagIfIndex != 66 {
		t.Fatalf("after inventory = %+v, want uplink/auto/x/public_address/66", row)
	}
}

// rolesSite is switches A and B at one site, each with physical ports 49 and
// 50 (named 0/49, 0/50), up; B is known by its sys_name "B".
func rolesSite(t *testing.T, db *gorm.DB) (a, b uuid.UUID) {
	t.Helper()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	a, b = s.DeviceID, seedDeviceInSite(t, db, s.SiteID, "10.0.0.3")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'switch' WHERE id IN (?, ?)`, a, b)
	testdb.Exec(t, db, `UPDATE devices SET name = 'B', sys_name = 'B' WHERE id = ?`, b)
	for _, d := range []uuid.UUID{a, b} {
		for _, ix := range []int{49, 50} {
			seedPort(t, db, d, ix, fmt.Sprintf("0/%d", ix), "")
		}
	}
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = true, oper_status = 'up', if_type = 6`)
	return a, b
}

func roleOf(t *testing.T, db *gorm.DB, dev uuid.UUID, ifIndex int) [3]string {
	t.Helper()
	var r struct{ Role, RoleSource, RoleReason string }
	testdb.Must(t, db.Raw(`SELECT role, role_source, role_reason FROM device_interfaces WHERE device_id = ? AND if_index = ?`, dev, ifIndex).Scan(&r).Error)
	return [3]string{r.Role, r.RoleSource, r.RoleReason}
}

func neighbour(t *testing.T, db *gorm.DB, dev uuid.UUID, ifIndex int, sysName, portID string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, sys_name, port_id, last_seen) VALUES (?, ?, ?, ?, ?, now())`,
		dev, ifIndex, "chassis-"+sysName, sysName, portID)
}

func TestDBPortRolesFromLLDP(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	neighbour(t, db, a, 49, "B", "0/49")
	NewPortRoleService(db).Apply(context.Background(), a)
	if got, want := roleOf(t, db, a, 49), [3]string{"uplink", "auto", "Found by LLDP: B port 49"}; got != want {
		t.Errorf("A port 49 = %v, want %v", got, want)
	}
	if got := roleOf(t, db, a, 50); got != [3]string{"access", "default", ""} {
		t.Errorf("A port 50 = %v, want untouched", got)
	}
}

// Review Focus 2: a port the user set stays as they set it.
func TestDBPortRolesSkipUserPorts(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	testdb.Exec(t, db, `UPDATE device_interfaces SET role_source = 'user' WHERE device_id = ? AND if_index = 49`, a)
	neighbour(t, db, a, 49, "B", "0/49")
	NewPortRoleService(db).Apply(context.Background(), a)
	if got := roleOf(t, db, a, 49); got != [3]string{"access", "user", ""} {
		t.Errorf("A port 49 = %v, want access set by the user", got)
	}
}

func TestDBPortRolesAPIsDontCount(t *testing.T) {
	db := testdb.Open(t)
	a, b := rolesSite(t, db)
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'access_point' WHERE id = ?`, b)
	neighbour(t, db, a, 49, "B", "0/49")
	NewPortRoleService(db).Apply(context.Background(), a)
	if got := roleOf(t, db, a, 49); got != [3]string{"access", "default", ""} {
		t.Errorf("A port 49 facing an AP = %v, want an endpoint", got)
	}
}

func TestDBPortRolesWAN(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'router' WHERE id = ?`, a)
	testdb.Exec(t, db, `UPDATE device_interfaces SET wan_evidence = 'public_address' WHERE device_id = ? AND if_index = 50`, a)
	NewPortRoleService(db).Apply(context.Background(), a)
	if got, want := roleOf(t, db, a, 50), [3]string{"wan", "auto", "Has a public address"}; got != want {
		t.Errorf("router port 50 = %v, want %v", got, want)
	}
}

func TestDBPortRolesClear(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	neighbour(t, db, a, 49, "B", "0/49")
	svc := NewPortRoleService(db)
	svc.Apply(context.Background(), a)
	testdb.Exec(t, db, `DELETE FROM l2.neighbors`)
	svc.Apply(context.Background(), a)
	if got := roleOf(t, db, a, 49); got != [3]string{"access", "default", ""} {
		t.Errorf("A port 49 with its neighbour gone = %v, want back to the default", got)
	}
}

// CDP sends B's name without the domain Sentinel knows it by, and its port
// in long form; the reason shows B's own short name for the port.
func TestDBPortRolesCDPWithoutDomain(t *testing.T) {
	db := testdb.Open(t)
	a, b := rolesSite(t, db)
	testdb.Exec(t, db, `UPDATE devices SET name = 'B.corp.example', sys_name = 'B.corp.example' WHERE id = ?`, b)
	testdb.Exec(t, db, `UPDATE device_interfaces SET descr = 'GigabitEthernet1/0/50' WHERE device_id = ? AND if_index = 50`, b)
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, protocol, sys_name, port_id, last_seen)
		VALUES (?, 49, 'B.corp.example', 'cdp', 'B', 'GigabitEthernet1/0/50', now())`, a)
	NewPortRoleService(db).Apply(context.Background(), a)
	if got, want := roleOf(t, db, a, 49), [3]string{"uplink", "auto", "Found by CDP: B.corp.example port 0/50"}; got != want {
		t.Errorf("A port 49 = %v, want %v", got, want)
	}
}

// A Cisco Port-channel (ifType 53) names its LAG as "lag 1" does.
func TestDBPortRolesPortChannelName(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	seedPort(t, db, a, 78, "Po1", "")
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = true, if_type = 53, descr = 'Port-channel1' WHERE device_id = ? AND if_index = 78`, a)
	testdb.Exec(t, db, `UPDATE device_interfaces SET lag_if_index = 78 WHERE device_id = ? AND if_index IN (49, 50)`, a)
	neighbour(t, db, a, 49, "B", "0/49")
	NewPortRoleService(db).Apply(context.Background(), a)
	if got, want := roleOf(t, db, a, 50), [3]string{"uplink", "auto", "Member of Po1 to B"}; got != want {
		t.Errorf("A port 50 = %v, want %v", got, want)
	}
}

func TestDBPortRolesLAGMember(t *testing.T) {
	db := testdb.Open(t)
	a, _ := rolesSite(t, db)
	seedPort(t, db, a, 66, "lag 1", "")
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = true, if_type = 161 WHERE device_id = ? AND if_index = 66`, a)
	testdb.Exec(t, db, `UPDATE device_interfaces SET lag_if_index = 66 WHERE device_id = ? AND if_index IN (49, 50)`, a)
	neighbour(t, db, a, 49, "B", "0/49")
	NewPortRoleService(db).Apply(context.Background(), a)
	if got, want := roleOf(t, db, a, 50), [3]string{"uplink", "auto", "Member of lag 1 to B"}; got != want {
		t.Errorf("A port 50 = %v, want %v", got, want)
	}
}

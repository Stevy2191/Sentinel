package services

import (
	"context"
	"testing"
	"time"

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

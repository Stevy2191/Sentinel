package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func switchInventory(withIfX bool) snmp.Inventory {
	yes := true
	inv := snmp.Inventory{System: snmp.System{Name: "sw1"}, Vendor: "Ubiquiti (EdgeSwitch)", Model: "USW-Pro-48-PoE"}
	for i := 1; i <= 52; i++ {
		it := snmp.Interface{Index: i, Descr: fmt.Sprintf("Slot: 0 Port: %d Gigabit - Level", i), Type: 6,
			SpeedBps: 100_000_000, OperStatus: "up", AdminStatus: "up"}
		if withIfX {
			it.Name, it.Alias, it.SpeedBps, it.HasIfX, it.ConnectorPresent = fmt.Sprintf("0/%d", i), "desk", 1_000_000_000, true, &yes
		} else {
			it.Name = it.Descr
		}
		inv.Interfaces = append(inv.Interfaces, it)
	}
	inv.Interfaces = append(inv.Interfaces, snmp.Interface{Index: 65, Name: "CPU Interface:  5/1", Type: 1, HasIfX: withIfX})
	return inv
}

func TestDBSaveInventoryKeepsIfXValuesAndClassifies(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	ctx := context.Background()

	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	// The next walk's ifXTable fails: names, aliases and speeds must survive.
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(false), time.Now()))

	var p1, cpu models.DeviceInterface
	testdb.Must(t, db.First(&p1, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	testdb.Must(t, db.First(&cpu, "device_id = ? AND if_index = 65", s.DeviceID).Error)
	if p1.Name != "0/1" || p1.Alias != "desk" || p1.SpeedBps != 1_000_000_000 {
		t.Errorf("ifX values lost: name %q alias %q speed %d", p1.Name, p1.Alias, p1.SpeedBps)
	}
	if p1.Descr != "Slot: 0 Port: 1 Gigabit - Level" {
		t.Errorf("ifTable values should still update: descr %q", p1.Descr)
	}
	if !p1.CollectDefault || cpu.CollectDefault {
		t.Errorf("collect_default: port %v cpu %v", p1.CollectDefault, cpu.CollectDefault)
	}
	if p1.ConnectorPresent == nil || !*p1.ConnectorPresent {
		t.Errorf("connector_present lost on the ifX-less walk: %v", p1.ConnectorPresent)
	}

	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	if d.DeviceTypeDetected != models.DeviceTypeSwitch {
		t.Errorf("device_type_detected = %q", d.DeviceTypeDetected)
	}
}

// Overrides are never written by inventory.
func TestDBSaveInventoryLeavesOverrides(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET model_override = 'UNVR (4-bay)', device_type = 'nvr' WHERE id = ?`, s.DeviceID)
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	testdb.Must(t, svc.SaveInventory(context.Background(), s.DeviceID, switchInventory(true), time.Now()))
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	if d.ModelOverride == nil || *d.ModelOverride != "UNVR (4-bay)" || d.DeviceType == nil || *d.DeviceType != "nvr" || d.Model != "USW-Pro-48-PoE" {
		t.Errorf("overrides touched: %+v", d)
	}
}

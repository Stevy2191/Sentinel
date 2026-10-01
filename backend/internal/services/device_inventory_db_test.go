package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

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

// Once a port stops being reported, any open incident on it must be closed
// too — not just marked absent. Review finding: Important 2.
func TestDBSaveInventoryClosesIncidentsOfPortsThatDisappear(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	incidents := NewIncidentService(db)
	svc := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	ctx := context.Background()

	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	var port3 models.DeviceInterface
	testdb.Must(t, db.First(&port3, "device_id = ? AND if_index = 3", s.DeviceID).Error)

	start := time.Now().UTC().Add(-time.Hour)
	inc, opened, err := incidents.OpenPortIncident(ctx, s.DeviceID, port3.ID, "link_down", start, "down")
	if err != nil || !opened {
		t.Fatalf("open incident: opened %v err %v", opened, err)
	}

	without3 := switchInventory(true)
	for i, it := range without3.Interfaces {
		if it.Index == 3 {
			without3.Interfaces = append(without3.Interfaces[:i], without3.Interfaces[i+1:]...)
			break
		}
	}
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, without3, time.Now()))

	testdb.Must(t, db.First(&port3, "id = ?", port3.ID).Error)
	if port3.Present {
		t.Fatal("port 3 should be marked absent")
	}
	var closed models.Incident
	testdb.Must(t, db.First(&closed, "id = ?", inc.ID).Error)
	if closed.EndTime == nil {
		t.Fatal("incident not closed")
	}
	if closed.ResolutionNotes != "The port is no longer reported by the device." {
		t.Errorf("resolution notes: %q", closed.ResolutionNotes)
	}
}

// Inventory must never reset a port's role: the upsert's DoUpdates column set
// must not include "role".
func TestDBSaveInventoryKeepsPortRole(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	ctx := context.Background()

	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'wan' WHERE device_id = ? AND if_index = 1`, s.DeviceID)

	// Walk again: role must survive even though every other ifTable-sourced
	// column is refreshed.
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))

	var p1 models.DeviceInterface
	testdb.Must(t, db.First(&p1, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	if p1.Role != models.PortRoleWAN {
		t.Errorf("role = %q, want wan", p1.Role)
	}

	// A newly seen interface still defaults to access.
	var p2 models.DeviceInterface
	testdb.Must(t, db.First(&p2, "device_id = ? AND if_index = 2", s.DeviceID).Error)
	if p2.Role != models.PortRoleAccess {
		t.Errorf("new interface role = %q, want access", p2.Role)
	}
}

// stackInventory is a two-member stack: unit/slot/port names, 24 physical
// ports per member.
func stackInventory() snmp.Inventory {
	inv := snmp.Inventory{System: snmp.System{Name: "stack1"}, Vendor: "Cisco", Model: "WS-C2960X-24TS-L"}
	idx := 1
	for _, unit := range []int{1, 2} {
		for i := 1; i <= 24; i++ {
			inv.Interfaces = append(inv.Interfaces, snmp.Interface{
				Index: idx, Name: fmt.Sprintf("%d/0/%d", unit, i), Type: 6, HasIfX: true,
				OperStatus: "up", AdminStatus: "up", SpeedBps: 1_000_000_000,
			})
			idx++
		}
	}
	return inv
}

// A stack's physical ports store their stack member (1, 2); a non-stacked
// device's ports store 0. Inventory must never touch role or the neighbor
// columns a user has set.
func TestDBSaveInventoryStoresStackUnits(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	ctx := context.Background()

	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, stackInventory(), time.Now()))
	var unit1, unit2 models.DeviceInterface
	testdb.Must(t, db.First(&unit1, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	testdb.Must(t, db.First(&unit2, "device_id = ? AND if_index = 25", s.DeviceID).Error)
	if unit1.StackUnit != 1 {
		t.Errorf("unit 1 port: stack_unit %d, want 1", unit1.StackUnit)
	}
	if unit2.StackUnit != 2 {
		t.Errorf("unit 2 port: stack_unit %d, want 2", unit2.StackUnit)
	}

	other := seedDevice(t, db, "Branch", "10.0.0.3")
	testdb.Must(t, svc.SaveInventory(ctx, other.DeviceID, switchInventory(true), time.Now()))
	var usw models.DeviceInterface
	testdb.Must(t, db.First(&usw, "device_id = ? AND if_index = 1", other.DeviceID).Error)
	if usw.StackUnit != 0 {
		t.Errorf("non-stacked port: stack_unit %d, want 0", usw.StackUnit)
	}

	// Role and the neighbor columns are untouched by a re-inventory.
	neighbor := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'nbr', '10.0.0.9')`,
		neighbor, s.SiteID, s.CredentialID)
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'uplink', neighbor_device_id = ?, neighbor_if_index = 10
		WHERE device_id = ? AND if_index = 1`, neighbor, s.DeviceID)
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, stackInventory(), time.Now()))
	testdb.Must(t, db.First(&unit1, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	if unit1.Role != models.PortRoleUplink || unit1.NeighborDeviceID == nil || *unit1.NeighborDeviceID != neighbor ||
		unit1.NeighborIfIndex == nil || *unit1.NeighborIfIndex != 10 {
		t.Errorf("inventory touched role/neighbor: %+v", unit1)
	}
	if unit1.StackUnit != 1 {
		t.Errorf("stack_unit lost on re-inventory: %d", unit1.StackUnit)
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

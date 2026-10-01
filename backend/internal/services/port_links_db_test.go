package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// seedDeviceInSite adds a second device (and credential) to an existing site,
// for neighbor tests that need two devices in the same site.
func seedDeviceInSite(t *testing.T, db *gorm.DB, siteID uuid.UUID, host string) uuid.UUID {
	t.Helper()
	credID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, site_id, version, community) VALUES (?, ?, ?, '2c', 'x')`,
		credID, "cred-"+credID.String()[:8], siteID)
	devID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		devID, siteID, credID, "dev-"+host, host)
	return devID
}

func interfaceRow(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int) models.DeviceInterface {
	t.Helper()
	var row models.DeviceInterface
	testdb.Must(t, db.First(&row, "device_id = ? AND if_index = ?", deviceID, ifIndex).Error)
	return row
}

func TestDBUpdatePortLinksBothSidesAndClearsOnReversal(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	ten := 10
	_, after, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)
	if after.Role != models.PortRoleUplink || after.NeighborDeviceID == nil || *after.NeighborDeviceID != router ||
		after.NeighborIfIndex == nil || *after.NeighborIfIndex != 10 {
		t.Fatalf("switch port 51 after link: %+v", after)
	}
	routerPort := interfaceRow(t, db, router, 10)
	if routerPort.Role != models.PortRoleUplink || routerPort.NeighborDeviceID == nil || *routerPort.NeighborDeviceID != sw.DeviceID ||
		routerPort.NeighborIfIndex == nil || *routerPort.NeighborIfIndex != 51 {
		t.Fatalf("router port 10 should point back: %+v", routerPort)
	}

	// Changing the switch port back to an endpoint clears both sides, and the
	// far port's role resets to access.
	accessRole := models.PortRoleAccess
	_, after, err = svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{Role: models.Opt[string]{Set: true, Value: &accessRole}})
	testdb.Must(t, err)
	if after.Role != models.PortRoleAccess || after.NeighborDeviceID != nil || after.NeighborIfIndex != nil {
		t.Errorf("switch port 51 after un-linking: %+v", after)
	}
	routerPort = interfaceRow(t, db, router, 10)
	if routerPort.Role != models.PortRoleAccess || routerPort.NeighborDeviceID != nil || routerPort.NeighborIfIndex != nil {
		t.Errorf("router port 10 should have cleared too: %+v", routerPort)
	}
}

func TestDBUpdatePortNeighborValidation(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	other := seedDevice(t, db, "Branch", "10.0.0.5") // a different site
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	foreignDevice := other.DeviceID
	ten := 10
	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &foreignDevice},
	})
	if !errors.Is(err, ErrPortNeighborInvalid) {
		t.Errorf("neighbor in another site: %v, want ErrPortNeighborInvalid", err)
	}
	unchanged := interfaceRow(t, db, sw.DeviceID, 51)
	if unchanged.NeighborDeviceID != nil || unchanged.Role != models.PortRoleAccess {
		t.Errorf("nothing should have changed: %+v", unchanged)
	}

	missing := 999
	_, _, err = svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &missing},
	})
	if !errors.Is(err, ErrPortNeighborInvalid) {
		t.Errorf("missing neighbor port: %v, want ErrPortNeighborInvalid", err)
	}

	selfID := sw.DeviceID
	_, _, err = svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &selfID},
	})
	if !errors.Is(err, ErrPortNeighborInvalid) {
		t.Errorf("link to self: %v, want ErrPortNeighborInvalid", err)
	}

	// Valid at last, with if_index now present.
	_, _, err = svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)
}

// A patch that gives neighbor_if_index alone, with no neighbor device (and
// none already stored), is rejected rather than stored as a dangling if_index.
func TestDBUpdatePortRejectsIfIndexWithoutADevice(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	ten := 10
	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborIfIndex: models.Opt[int]{Set: true, Value: &ten},
	})
	if !errors.Is(err, ErrPortNeighborInvalid) {
		t.Errorf("if_index with no device: %v, want ErrPortNeighborInvalid", err)
	}
	row := interfaceRow(t, db, sw.DeviceID, 51)
	if row.NeighborIfIndex != nil {
		t.Errorf("neighbor_if_index should not have been stored: %+v", row)
	}
}

// A far port already linked elsewhere must not be overwritten.
func TestDBUpdatePortDoesNotOverwriteAnExistingReverseLink(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	thirdSw := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.3")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	seedPort(t, db, thirdSw, 1, "0/1", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	// Router port 10 already points at the third switch.
	one := 1
	_, _, err := svc.UpdatePort(ctx, router, 10, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &thirdSw},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &one},
	})
	testdb.Must(t, err)

	// Now the switch claims the same router port as its own neighbor.
	ten := 10
	_, after, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)
	if after.Role != models.PortRoleUplink || after.NeighborDeviceID == nil || *after.NeighborDeviceID != router {
		t.Fatalf("switch port 51: %+v", after)
	}
	// Router port 10 must still point at the third switch, untouched.
	routerPort := interfaceRow(t, db, router, 10)
	if routerPort.NeighborDeviceID == nil || *routerPort.NeighborDeviceID != thirdSw || routerPort.NeighborIfIndex == nil || *routerPort.NeighborIfIndex != 1 {
		t.Errorf("router port 10 should still point at the third switch: %+v", routerPort)
	}
}

// Deleting the neighbor device nulls this port's neighbor_device_id (FK ON
// DELETE SET NULL) without the CHECK constraint rejecting the cascade. The
// link is by device only (no neighbor_if_index), which is the only shape
// that survives the cascade: see the migration's comment.
func TestDBDeletingNeighborDeviceClearsNeighborDeviceID(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
	})
	testdb.Must(t, err)

	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, router)

	row := interfaceRow(t, db, sw.DeviceID, 51)
	if row.NeighborDeviceID != nil {
		t.Errorf("neighbor_device_id survived the neighbor's deletion: %v", row.NeighborDeviceID)
	}
}

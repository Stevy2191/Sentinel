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
// DELETE SET NULL). That FK action only touches neighbor_device_id, leaving
// neighbor_if_index stale (non-null) with no further write — the link here
// is by device AND port (neighbor_if_index set), which is exactly the shape
// that broke the design doc's original CHECK (verified against Postgres 16
// before narrowing it to "neighbor_device_id IS NULL OR role = 'uplink'");
// if the DELETE itself failed, this test would fail right here, covering
// that narrowing as a regression test.
//
// Fix round 1, Important 1: the stale neighbor_if_index must not then block
// an unrelated later patch (it used to hit ErrPortNeighborInvalid), and
// must self-heal to NULL once anything next touches the port.
func TestDBDeletingNeighborDeviceLeavesStaleIfIndexThatSelfHeals(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	ten := 10
	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)

	// Deleting the neighbor device must not fail (the narrowed CHECK).
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, router)

	stale := interfaceRow(t, db, sw.DeviceID, 51)
	if stale.NeighborDeviceID != nil {
		t.Fatalf("neighbor_device_id survived the neighbor's deletion: %v", stale.NeighborDeviceID)
	}
	if stale.NeighborIfIndex == nil || *stale.NeighborIfIndex != 10 {
		t.Fatalf("expected the stale neighbor_if_index to still be 10 right after deletion: %+v", stale)
	}

	// An ordinary, unrelated patch must still work (not ErrPortNeighborInvalid)
	// and must self-heal the stale if_index to NULL.
	yes := true
	_, after, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &yes}})
	testdb.Must(t, err)
	if !after.Important {
		t.Errorf("important not set: %+v", after)
	}
	if after.NeighborIfIndex != nil {
		t.Errorf("stale neighbor_if_index did not self-heal: %+v", after)
	}
}

// Fix round 1, Important 2: clearing a link must find the far port by what
// it points at, not by trusting this port's own (possibly unknown) stored
// neighbor_if_index for the far device. Here the switch links to the router
// by device only (no port chosen); the router separately links its own
// port 10 back at the switch, which leaves the switch port unchanged
// because it already has a device. Un-linking the switch port must still
// find and clear router port 10.
func TestDBUpdatePortClearingFindsFarPortEvenWithoutAStoredIfIndex(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	// sw/51 -> (router, no port).
	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
	})
	testdb.Must(t, err)

	// router/10 -> (sw, 51). sw/51 already has a device, so the reverse-link
	// guard (neighbor_device_id IS NULL) leaves it unchanged.
	fiftyOne := 51
	_, _, err = svc.UpdatePort(ctx, router, 10, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &sw.DeviceID},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &fiftyOne},
	})
	testdb.Must(t, err)
	unchanged := interfaceRow(t, db, sw.DeviceID, 51)
	if unchanged.NeighborIfIndex != nil {
		t.Fatalf("sw/51 should still have no stored neighbor port: %+v", unchanged)
	}

	// sw/51 -> access. Even though sw/51 never knew router/10's if_index,
	// router/10 (which points back here) must be cleared.
	accessRole := models.PortRoleAccess
	_, _, err = svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{Role: models.Opt[string]{Set: true, Value: &accessRole}})
	testdb.Must(t, err)

	routerPort := interfaceRow(t, db, router, 10)
	if routerPort.Role != models.PortRoleAccess || routerPort.NeighborDeviceID != nil || routerPort.NeighborIfIndex != nil {
		t.Errorf("router port 10 should have cleared: %+v", routerPort)
	}
}

// Fix round 1, Important 2: changing a link (not just removing it) must
// clear the old far port and reverse-link the new one.
func TestDBUpdatePortChangingLinkClearsOldFarPortAndLinksNew(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	seedPort(t, db, router, 11, "0/11", "")
	svc := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	ten := 10
	_, _, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)
	port10 := interfaceRow(t, db, router, 10)
	if port10.NeighborDeviceID == nil || *port10.NeighborDeviceID != sw.DeviceID {
		t.Fatalf("router port 10 should point back at first: %+v", port10)
	}

	// sw/51 moves from (router,10) to (router,11).
	eleven := 11
	_, after, err := svc.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &eleven},
	})
	testdb.Must(t, err)
	if after.NeighborIfIndex == nil || *after.NeighborIfIndex != 11 {
		t.Fatalf("switch port 51 should now point at port 11: %+v", after)
	}

	port10 = interfaceRow(t, db, router, 10)
	if port10.Role != models.PortRoleAccess || port10.NeighborDeviceID != nil || port10.NeighborIfIndex != nil {
		t.Errorf("router port 10 should have cleared: %+v", port10)
	}
	port11 := interfaceRow(t, db, router, 11)
	if port11.Role != models.PortRoleUplink || port11.NeighborDeviceID == nil || *port11.NeighborDeviceID != sw.DeviceID ||
		port11.NeighborIfIndex == nil || *port11.NeighborIfIndex != 51 {
		t.Errorf("router port 11 should now point back at sw/51: %+v", port11)
	}
}

// Fix round 1, Important 3: moving a device to another site must clear its
// port links in both directions, so two devices in different sites never
// keep pointing at each other.
func TestDBUpdateDeviceSiteMoveClearsNeighborLinks(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	seedPort(t, db, sw.DeviceID, 51, "0/51", "")
	seedPort(t, db, router, 10, "0/10", "")
	ports := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))

	ten := 10
	_, _, err := ports.UpdatePort(ctx, sw.DeviceID, 51, models.PortPatch{
		NeighborDeviceID: models.Opt[uuid.UUID]{Set: true, Value: &router},
		NeighborIfIndex:  models.Opt[int]{Set: true, Value: &ten},
	})
	testdb.Must(t, err)

	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	swDevice, err := devices.Get(ctx, sw.DeviceID)
	testdb.Must(t, err)

	newSite := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, newSite)
	newCred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'global-new', '2c', 'x')`, newCred)

	_, _, err = devices.Update(ctx, sw.DeviceID, models.DeviceInput{
		SiteID: newSite, CredentialID: newCred, Name: swDevice.Name, Host: swDevice.Host, Port: swDevice.Port,
		Enabled: &swDevice.Enabled, PollInterval: swDevice.PollInterval, TimeoutMs: swDevice.TimeoutMs,
		Retries: swDevice.Retries, NotifyChannels: swDevice.NotifyChannels,
	})
	testdb.Must(t, err)

	swPort := interfaceRow(t, db, sw.DeviceID, 51)
	if swPort.Role != models.PortRoleAccess || swPort.NeighborDeviceID != nil || swPort.NeighborIfIndex != nil {
		t.Errorf("the moved device's own port should have cleared: %+v", swPort)
	}
	routerPort := interfaceRow(t, db, router, 10)
	if routerPort.Role != models.PortRoleAccess || routerPort.NeighborDeviceID != nil || routerPort.NeighborIfIndex != nil {
		t.Errorf("the port pointing at the moved device should have cleared: %+v", routerPort)
	}
}

package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// trafficDevice inserts a device of the given effective type (device_type_detected)
// into an existing site.
func trafficDevice(t *testing.T, db *gorm.DB, siteID, credentialID uuid.UUID, host, deviceType string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host, device_type_detected)
		VALUES (?, ?, ?, ?, ?, ?)`, id, siteID, credentialID, "dev-"+host, host, deviceType)
	return id
}

// trafficPort inserts a present, physical (ifType 6, no connector report),
// collected device_interfaces row with the given role.
func trafficPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, role string) uuid.UUID {
	t.Helper()
	return trafficPortFull(t, db, deviceID, ifIndex, role, 6, true)
}

// trafficPortFull is trafficPort with the ifType and collect flag spelled
// out, for cases that need a link aggregate (ifType 161) or collect: false.
func trafficPortFull(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, role string, ifType int, collect bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, if_type, present, collect, role)
		VALUES (?, ?, ?, ?, ?, true, ?, ?)`, id, deviceID, ifIndex, "eth", ifType, collect, role)
	return id
}

// writeBps writes one poll's in/out bps for one port. Each port on a device
// needs its own instance (its ifIndex, as the real poller uses): metrics.series
// identifies a series by (device_id, metric, instance), not interface_id, so
// two ports sharing an instance on the same device would collide into one
// series.
func writeBps(t *testing.T, metrics *MetricsStore, deviceID, ifaceID uuid.UUID, ifIndex int, at time.Time, in, out float64) {
	t.Helper()
	instance := strconv.Itoa(ifIndex)
	testdb.Must(t, metrics.Write(context.Background(), deviceID, at, []SamplePoint{
		{Metric: MetricIfInBps, Instance: instance, InterfaceID: &ifaceID, Value: in},
		{Metric: MetricIfOutBps, Instance: instance, InterfaceID: &ifaceID, Value: out},
	}))
}

// TestDBSiteTraffic covers the main scenario from the brief: a router's WAN
// and uplink ports, a router and switch's access ports (counted), an access
// point's access port (excluded), and an uplink port (excluded).
func TestDBSiteTraffic(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2") // gives a site + credential; its own device is unused here
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.0.10", models.DeviceTypeRouter)
	routerWAN := trafficPort(t, db, router, 1, models.PortRoleWAN)
	_ = trafficPort(t, db, router, 2, models.PortRoleUplink)
	routerAccess := trafficPort(t, db, router, 3, models.PortRoleAccess)

	sw := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.0.11", models.DeviceTypeSwitch)
	_ = trafficPort(t, db, sw, 1, models.PortRoleUplink)
	swAccess2 := trafficPort(t, db, sw, 2, models.PortRoleAccess)
	swAccess3 := trafficPort(t, db, sw, 3, models.PortRoleAccess)

	ap := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.0.12", models.DeviceTypeAccessPoint)
	apAccess := trafficPort(t, db, ap, 1, models.PortRoleAccess)

	now := time.Now().UTC()
	t1 := now.Add(-20 * time.Minute).Truncate(time.Minute)
	t2 := now.Add(-10 * time.Minute).Truncate(time.Minute)

	// WAN: download (in) 300/320, upload (out) 50/60.
	writeBps(t, metrics, router, routerWAN, 1, t1, 300, 50)
	writeBps(t, metrics, router, routerWAN, 1, t2, 320, 60)
	// Access ports (counted): sums to in 400/420, out 900/950.
	writeBps(t, metrics, router, routerAccess, 3, t1, 100, 200)
	writeBps(t, metrics, router, routerAccess, 3, t2, 110, 210)
	writeBps(t, metrics, sw, swAccess2, 2, t1, 150, 300)
	writeBps(t, metrics, sw, swAccess2, 2, t2, 155, 310)
	writeBps(t, metrics, sw, swAccess3, 3, t1, 150, 400)
	writeBps(t, metrics, sw, swAccess3, 3, t2, 155, 430)
	// The AP's access port must never be counted: huge values that would
	// obviously change the result if it leaked in.
	writeBps(t, metrics, ap, apAccess, 1, t1, 9999, 9999)

	from, to := t1.Add(-time.Minute), now
	v, err := ports.SiteTraffic(ctx, s.SiteID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if !v.WANConfigured {
		t.Fatal("wan_configured should be true")
	}
	if len(v.NorthSouth) != 2 {
		t.Fatalf("north_south points = %d: %+v", len(v.NorthSouth), v.NorthSouth)
	}
	if v.NorthSouth[0].InBps != 300 || v.NorthSouth[0].OutBps != 50 || !v.NorthSouth[0].Time.Equal(t1) {
		t.Errorf("north_south[0] = %+v, want {t1, 300, 50}", v.NorthSouth[0])
	}
	if v.NorthSouth[1].InBps != 320 || v.NorthSouth[1].OutBps != 60 || !v.NorthSouth[1].Time.Equal(t2) {
		t.Errorf("north_south[1] = %+v, want {t2, 320, 60}", v.NorthSouth[1])
	}
	if len(v.EastWest) != 2 {
		t.Fatalf("east_west points = %d: %+v", len(v.EastWest), v.EastWest)
	}
	// ew(t1) = ((400-50)+(900-300))/2 = (350+600)/2 = 475
	// ew(t2) = ((420-60)+(950-320))/2 = (360+630)/2 = 495
	if v.EastWest[0].Bps != 475 || !v.EastWest[0].Time.Equal(t1) {
		t.Errorf("east_west[0] = %+v, want {t1, 475}", v.EastWest[0])
	}
	if v.EastWest[1].Bps != 495 || !v.EastWest[1].Time.Equal(t2) {
		t.Errorf("east_west[1] = %+v, want {t2, 495}", v.EastWest[1])
	}
}

// A site with no WAN-role port reports wan_configured: false and empty series,
// regardless of how much access traffic exists.
func TestDBSiteTrafficNoWANPort(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "Branch", "10.0.1.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	sw := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.1.10", models.DeviceTypeSwitch)
	access := trafficPort(t, db, sw, 1, models.PortRoleAccess)
	now := time.Now().UTC()
	writeBps(t, metrics, sw, access, 1, now.Truncate(time.Minute), 100, 100)

	v, err := ports.SiteTraffic(ctx, s.SiteID, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if v.WANConfigured {
		t.Error("wan_configured should be false")
	}
	if len(v.NorthSouth) != 0 || len(v.EastWest) != 0 {
		t.Errorf("expected empty series, got north_south %+v east_west %+v", v.NorthSouth, v.EastWest)
	}
}

// East-west is clamped at 0: a bucket where the WAN ports carry far more
// traffic than the access ports (e.g. right after the WAN port is marked,
// before access ports are also collected) must not go negative.
func TestDBSiteTrafficClampsNegativeEastWest(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ2", "10.0.2.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.2.10", models.DeviceTypeRouter)
	wan := trafficPort(t, db, router, 1, models.PortRoleWAN)
	access := trafficPort(t, db, router, 3, models.PortRoleAccess)

	at := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Minute)
	writeBps(t, metrics, router, wan, 1, at, 1000, 1000)
	writeBps(t, metrics, router, access, 3, at, 10, 10)

	v, err := ports.SiteTraffic(ctx, s.SiteID, at.Add(-time.Minute), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.EastWest) != 1 || v.EastWest[0].Bps != 0 {
		t.Errorf("east_west = %+v, want one clamped-to-zero point", v.EastWest)
	}
}

// A WAN-role port that is not collected must not count as WAN: it has no
// samples, so counting it would leave north-south empty while east-west
// quietly absorbed all internet traffic as if it were internal. Review
// finding, fix round 1, item 2.
func TestDBSiteTrafficUncollectedWANDoesNotCount(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ3", "10.0.3.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.3.10", models.DeviceTypeRouter)
	wan := trafficPortFull(t, db, router, 1, models.PortRoleWAN, 6, false)
	access := trafficPort(t, db, router, 3, models.PortRoleAccess)

	now := time.Now().UTC().Truncate(time.Minute)
	// Written directly (bypassing the poller, which would not collect this
	// port): proves SiteTraffic itself excludes it, not just that no samples
	// exist in practice.
	writeBps(t, metrics, router, wan, 1, now, 500, 50)
	writeBps(t, metrics, router, access, 3, now, 100, 200)

	v, err := ports.SiteTraffic(ctx, s.SiteID, now.Add(-time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if v.WANConfigured {
		t.Error("wan_configured should be false: the only WAN-role port is not collected")
	}
	if len(v.NorthSouth) != 0 || len(v.EastWest) != 0 {
		t.Errorf("expected empty series, got north_south %+v east_west %+v", v.NorthSouth, v.EastWest)
	}
}

// With WAN configured, a bucket that has access samples but no WAN sample at
// all still yields an east-west point, treating the missing WAN sums as 0.
// Review finding, fix round 1, item 4.
func TestDBSiteTrafficMissingWANBucketDefaultsToZero(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ4", "10.0.4.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.4.10", models.DeviceTypeRouter)
	wan := trafficPort(t, db, router, 1, models.PortRoleWAN)
	access := trafficPort(t, db, router, 3, models.PortRoleAccess)

	now := time.Now().UTC()
	t1 := now.Add(-20 * time.Minute).Truncate(time.Minute)
	t2 := now.Add(-10 * time.Minute).Truncate(time.Minute)

	// WAN has a sample only at t1; access has samples at both t1 and t2.
	writeBps(t, metrics, router, wan, 1, t1, 300, 50)
	writeBps(t, metrics, router, access, 3, t1, 400, 900)
	writeBps(t, metrics, router, access, 3, t2, 420, 950)

	v, err := ports.SiteTraffic(ctx, s.SiteID, t1.Add(-time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.WANConfigured {
		t.Fatal("wan_configured should be true")
	}
	if len(v.NorthSouth) != 1 {
		t.Fatalf("north_south points = %d: %+v, want just t1 (no fabricated WAN point)", len(v.NorthSouth), v.NorthSouth)
	}
	if len(v.EastWest) != 2 {
		t.Fatalf("east_west points = %d: %+v, want one per access bucket even with no WAN sample at t2", len(v.EastWest), v.EastWest)
	}
	// t1: wanIn=300, wanOut=50 -> ew = ((400-50)+(900-300))/2 = (350+600)/2 = 475
	if v.EastWest[0].Bps != 475 || !v.EastWest[0].Time.Equal(t1) {
		t.Errorf("east_west[0] = %+v, want {t1, 475}", v.EastWest[0])
	}
	// t2: no WAN sample at all -> wanIn = wanOut = 0 -> ew = ((420-0)+(950-0))/2 = 685
	if v.EastWest[1].Bps != 685 || !v.EastWest[1].Time.Equal(t2) {
		t.Errorf("east_west[1] = %+v, want {t2, 685} (missing WAN sample treated as 0)", v.EastWest[1])
	}
}

// Access-role ports that are not collected, and access-role link aggregates,
// are excluded from east-west. Review finding, fix round 1, item 4.
func TestDBSiteTrafficExcludesUncollectedAndLAGAccessPorts(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ5", "10.0.5.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.5.10", models.DeviceTypeRouter)
	wan := trafficPort(t, db, router, 1, models.PortRoleWAN)
	access := trafficPort(t, db, router, 3, models.PortRoleAccess)
	uncollected := trafficPortFull(t, db, router, 4, models.PortRoleAccess, 6, false)
	lag := trafficPortFull(t, db, router, 5, models.PortRoleAccess, 161, true)

	// More than one step before `to` (below), so it is not treated as the
	// newest, possibly-partial bucket and dropped from east-west.
	at := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Minute)
	writeBps(t, metrics, router, wan, 1, at, 100, 20)
	writeBps(t, metrics, router, access, 3, at, 200, 300)
	// Huge, distinct values: if either leaked into the access sum, ew would
	// obviously change from the expected 190.
	writeBps(t, metrics, router, uncollected, 4, at, 9999, 9999)
	writeBps(t, metrics, router, lag, 5, at, 8888, 8888)

	v, err := ports.SiteTraffic(ctx, s.SiteID, at.Add(-time.Minute), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.EastWest) != 1 {
		t.Fatalf("east_west points = %d: %+v", len(v.EastWest), v.EastWest)
	}
	// ew = ((200-20)+(300-100))/2 = (180+200)/2 = 190
	if v.EastWest[0].Bps != 190 {
		t.Errorf("east_west[0].Bps = %v, want 190 (uncollected port and LAG excluded)", v.EastWest[0].Bps)
	}
}

// M2: `to` rarely lands exactly on a bucket boundary, so the newest bucket
// can be partial — its WAN and access sums may not cover the same span,
// which reads as a dip that is not real traffic. North-south (summed
// independently per side) keeps it; east-west, which combines both sides,
// drops it.
func TestDBSiteTrafficDropsPartialNewestEastWestBucket(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ6", "10.0.6.2")
	ctx := context.Background()
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))

	router := trafficDevice(t, db, s.SiteID, s.CredentialID, "10.0.6.10", models.DeviceTypeRouter)
	wan := trafficPort(t, db, router, 1, models.PortRoleWAN)
	access := trafficPort(t, db, router, 3, models.PortRoleAccess)

	now := time.Now().UTC()
	t1 := now.Add(-2 * time.Minute).Truncate(time.Minute)
	t2 := now.Truncate(time.Minute) // within one step of `to` (now): the partial newest bucket
	writeBps(t, metrics, router, wan, 1, t1, 300, 50)
	writeBps(t, metrics, router, access, 3, t1, 400, 900)
	writeBps(t, metrics, router, wan, 1, t2, 320, 60)
	writeBps(t, metrics, router, access, 3, t2, 420, 950)

	v, err := ports.SiteTraffic(ctx, s.SiteID, t1.Add(-time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.NorthSouth) != 2 {
		t.Fatalf("north_south should keep the newest bucket: %+v", v.NorthSouth)
	}
	if len(v.EastWest) != 1 || !v.EastWest[0].Time.Equal(t1) {
		t.Fatalf("east_west should drop the partial newest bucket: %+v", v.EastWest)
	}
}

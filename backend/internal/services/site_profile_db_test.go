package services

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func profileFixture(t *testing.T) (*gorm.DB, *SiteProfileService, *MetricsStore) {
	t.Helper()
	db := testdb.Open(t)
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, NewIncidentService(db), NewSettingsService(db))
	return db, NewSiteProfileService(db, ports), metrics
}

func newProfileSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

func network(t *testing.T, name, cidr string) models.SiteNetworkInput {
	t.Helper()
	in, err := models.NormalizeSiteNetworkInput(models.SiteNetworkInput{Name: name, CIDR: cidr})
	testdb.Must(t, err)
	return in
}

func circuit(t *testing.T, in models.SiteCircuitInput) models.SiteCircuitInput {
	t.Helper()
	out, err := models.NormalizeSiteCircuitInput(in)
	testdb.Must(t, err)
	return out
}

// Networks come back by subnet with IPv4 first; the same subnet typed as a
// host address is still a duplicate; another site may reuse it; an edit may
// keep its own subnet but not take another network's; another site's id is
// not found.
func TestDBSiteNetworksOrderAndDuplicates(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	court, annex := newProfileSite(t, db, "Courthouse"), newProfileSite(t, db, "Annex")
	for _, cidr := range []string{"2001:db8::/64", "10.20.8.0/24", "10.20.0.0/24"} {
		_, err := svc.CreateNetwork(ctx, court, network(t, cidr, cidr))
		testdb.Must(t, err)
	}
	p, err := svc.Profile(ctx, court)
	testdb.Must(t, err)
	var got []string
	for _, n := range p.Networks {
		got = append(got, n.CIDR)
	}
	if len(got) != 3 || got[0] != "10.20.0.0/24" || got[1] != "10.20.8.0/24" || got[2] != "2001:db8::/64" {
		t.Fatalf("order = %v", got)
	}

	_, err = svc.CreateNetwork(ctx, court, network(t, "Again", "10.20.0.5/24"))
	var taken *SubnetTakenError
	if !errors.As(err, &taken) || err.Error() != "Courthouse already has 10.20.0.0/24" {
		t.Errorf("duplicate typed as a host address: err = %v", err)
	}
	if _, err := svc.CreateNetwork(ctx, annex, network(t, "Staff", "10.20.0.0/24")); err != nil {
		t.Errorf("another site reusing the subnet: %v", err)
	}

	first := p.Networks[0]
	if _, _, err := svc.UpdateNetwork(ctx, court, first.ID, network(t, "Staff", "10.20.0.0/24")); err != nil {
		t.Errorf("keeping its own subnet: %v", err)
	}
	if _, _, err := svc.UpdateNetwork(ctx, court, first.ID, network(t, "Staff", "10.20.8.0/24")); !errors.As(err, &taken) {
		t.Errorf("taking another network's subnet: err = %v", err)
	}
	if _, _, err := svc.UpdateNetwork(ctx, annex, first.ID, network(t, "Staff", "10.30.0.0/24")); !errors.Is(err, ErrSiteNetworkNotFound) {
		t.Errorf("another site's network id: err = %v", err)
	}
	if _, err := svc.DeleteNetwork(ctx, annex, first.ID); !errors.Is(err, ErrSiteNetworkNotFound) {
		t.Errorf("deleting another site's network: err = %v", err)
	}
}

// A circuit can only be tied to a port of a device at the same site, and
// another site's circuit id is not found.
func TestDBSiteCircuitPortMustBeAtSite(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	annex := newProfileSite(t, db, "Annex")

	if _, err := svc.CreateCircuit(ctx, annex, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port})); !errors.Is(err, ErrCircuitPortNotAtSite) {
		t.Errorf("port at another site: err = %v", err)
	}
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)
	if c.InterfaceID == nil || *c.InterfaceID != port || c.Kind != "other" {
		t.Errorf("created %+v", c)
	}
	if _, _, err := svc.UpdateCircuit(ctx, annex, c.ID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum"})); !errors.Is(err, ErrSiteCircuitNotFound) {
		t.Errorf("another site's circuit id: err = %v", err)
	}
	if _, err := svc.DeleteCircuit(ctx, annex, c.ID); !errors.Is(err, ErrSiteCircuitNotFound) {
		t.Errorf("deleting another site's circuit: err = %v", err)
	}
}

// Deleting the port clears the circuit's link; the circuit stays.
func TestDBSiteCircuitLinkClearsWhenPortDeleted(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	_, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)

	testdb.Exec(t, db, `DELETE FROM device_interfaces WHERE id = ?`, port)

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if len(p.Circuits) != 1 || p.Circuits[0].InterfaceID != nil || p.Circuits[0].Port != nil {
		t.Errorf("after the port was deleted: %+v", p.Circuits)
	}
}

// The profile carries the linked port's device, status and current rates. A
// port whose device moved to another site is not shown, and the circuit can
// then be saved without a port (what the form sends).
func TestDBSiteProfileCarriesLivePort(t *testing.T) {
	db, svc, metrics := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 7, "ge-0/0/7", "WAN")
	testdb.Exec(t, db, `UPDATE device_interfaces SET oper_status = 'up' WHERE id = ?`, port)
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInBps, Instance: "7", Value: 2.12e8},
		{Metric: MetricIfOutBps, Instance: "7", Value: 3e7},
	}))
	down := 500.0
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", Kind: "fiber", DownloadMbps: &down, InterfaceID: &port}))
	testdb.Must(t, err)

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	lp := p.Circuits[0].Port
	if lp == nil || lp.DeviceID != s.DeviceID || lp.DeviceName != "dev-10.0.0.2" || lp.IfIndex != 7 || lp.Alias != "WAN" ||
		lp.OperStatus != "up" || lp.InBps == nil || *lp.InBps != 2.12e8 || lp.OutBps == nil || *lp.OutBps != 3e7 {
		t.Fatalf("port = %+v", lp)
	}

	elsewhere := newProfileSite(t, db, "Annex")
	testdb.Exec(t, db, `UPDATE devices SET site_id = ? WHERE id = ?`, elsewhere, s.DeviceID)
	p, err = svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if p.Circuits[0].Port != nil {
		t.Errorf("port of a device now at another site: %+v", p.Circuits[0].Port)
	}
	if p.Circuits[0].InterfaceID != nil {
		t.Errorf("stale interface_id kept next to no port: %v", p.Circuits[0].InterfaceID)
	}
	if _, _, err := svc.UpdateCircuit(ctx, s.SiteID, c.ID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", Kind: "fiber"})); err != nil {
		t.Errorf("saving the circuit without its moved port: %v", err)
	}
}

func TestDBSiteNotesSetAndClear(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	site := newProfileSite(t, db, "Courthouse")
	text := "Closet: room 104\nKey at the front desk"

	before, after, err := svc.SetNotes(ctx, site, &text)
	testdb.Must(t, err)
	if before != nil || after == nil || *after != text {
		t.Errorf("set: before %v after %v", before, after)
	}
	p, err := svc.Profile(ctx, site)
	testdb.Must(t, err)
	if p.Notes == nil || *p.Notes != text {
		t.Errorf("profile notes = %v", p.Notes)
	}

	before, _, err = svc.SetNotes(ctx, site, nil)
	testdb.Must(t, err)
	if before == nil || *before != text {
		t.Errorf("clear: before %v", before)
	}
	if p, _ = svc.Profile(ctx, site); p.Notes != nil {
		t.Errorf("after clearing: %v", p.Notes)
	}
	if _, _, err := svc.SetNotes(ctx, uuid.New(), nil); !errors.Is(err, ErrSiteNotFound) {
		t.Errorf("unknown site: err = %v", err)
	}
}

// A site with a profile but no devices is deleted with its profile; one with
// devices is still refused.
func TestDBSiteProfileGoesWithTheSite(t *testing.T) {
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	site := newProfileSite(t, db, "Annex")
	text := "notes"
	_, err := svc.CreateNetwork(ctx, site, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	_, err = svc.CreateCircuit(ctx, site, circuit(t, models.SiteCircuitInput{Provider: "Spectrum"}))
	testdb.Must(t, err)
	_, _, err = svc.SetNotes(ctx, site, &text)
	testdb.Must(t, err)

	if _, err := NewSiteService(db).Delete(ctx, site); err != nil {
		t.Fatalf("deleting a site with only a profile: %v", err)
	}
	var left int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM site_networks WHERE site_id = ?) + (SELECT count(*) FROM site_circuits WHERE site_id = ?)`, site, site).Scan(&left).Error)
	if left != 0 {
		t.Errorf("%d profile rows outlived their site", left)
	}

	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	_, err = svc.CreateNetwork(ctx, s.SiteID, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	if _, err := NewSiteService(db).Delete(ctx, s.SiteID); !errors.Is(err, ErrSiteNotEmpty) {
		t.Errorf("site with a device: err = %v, want ErrSiteNotEmpty", err)
	}
}

// A backup restored over a database holding a site profile keeps its
// networks, its circuit's port link and its notes. Runs pg_dump and psql
// inside the test container with the backup's own arguments.
func TestDBRestoreKeepsSiteProfiles(t *testing.T) {
	container := os.Getenv("SENTINEL_TEST_DB_CONTAINER")
	if container == "" {
		t.Skip("SENTINEL_TEST_DB_CONTAINER not set; run `make test-db`")
	}
	db, svc, _ := profileFixture(t)
	ctx := context.Background()
	s := seedDevice(t, db, "Courthouse", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 1, "ge-0/0/1", "WAN")
	text := "Closet: room 104"
	_, err := svc.CreateNetwork(ctx, s.SiteID, network(t, "Staff", "10.20.0.0/24"))
	testdb.Must(t, err)
	c, err := svc.CreateCircuit(ctx, s.SiteID, circuit(t, models.SiteCircuitInput{Provider: "Spectrum", InterfaceID: &port}))
	testdb.Must(t, err)
	_, _, err = svc.SetNotes(ctx, s.SiteID, &text)
	testdb.Must(t, err)

	var name string
	testdb.Must(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	b := &BackupService{db: DBConfig{Host: "127.0.0.1", Port: "5432", User: "sentinel", Name: name}}
	dump, err := exec.Command("docker", append([]string{"exec", "-e", "PGPASSWORD=test", container, "pg_dump"}, b.dumpArgs()...)...).Output()
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	// Empty the profile, so what comes back can only have come from the dump.
	testdb.Exec(t, db, `DELETE FROM site_circuits`)
	testdb.Exec(t, db, `DELETE FROM site_networks`)
	testdb.Exec(t, db, `UPDATE sites SET notes = NULL`)
	restore := exec.Command("docker", append([]string{"exec", "-i", "-e", "PGPASSWORD=test", container, "psql"}, b.restoreArgs()...)...)
	restore.Stdin = bytes.NewReader(dump)
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	}

	p, err := svc.Profile(ctx, s.SiteID)
	testdb.Must(t, err)
	if len(p.Networks) != 1 || p.Networks[0].CIDR != "10.20.0.0/24" || p.Notes == nil || *p.Notes != text ||
		len(p.Circuits) != 1 || p.Circuits[0].ID != c.ID || p.Circuits[0].InterfaceID == nil || *p.Circuits[0].InterfaceID != port {
		t.Errorf("after restore: %+v", p)
	}
}

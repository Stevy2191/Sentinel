package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type fixedThresholds struct{}

func (fixedThresholds) PortThresholds(context.Context) portmon.Thresholds {
	return portmon.Thresholds{UtilPct: 80, ErrorsPerMin: 10}
}

type homeMapIDs struct {
	site, gw, sw, ap, ups uuid.UUID
	admin                 Viewer
}

// seedHomeMap is the owner's home site as the sandbox records it: the
// router and the switch with their real ports and LLDP row, plus an AP and
// a UPS that must stay off the map.
func seedHomeMap(t *testing.T, db *gorm.DB) homeMapIDs {
	t.Helper()
	s := seedDevice(t, db, "S6 Node", "10.1.255.1")
	ids := homeMapIDs{site: s.SiteID, gw: s.DeviceID}
	ids.sw = seedDeviceInSite(t, db, s.SiteID, "10.1.255.10")
	ids.ap = seedDeviceInSite(t, db, s.SiteID, "10.1.255.12")
	ids.ups = seedDeviceInSite(t, db, s.SiteID, "10.1.60.30")
	set := func(id uuid.UUID, name, typ, descr string) {
		testdb.Exec(t, db, `UPDATE devices SET name = ?, sys_name = ?, device_type = ?, sys_descr = ?, status = 'up' WHERE id = ?`, name, name, typ, descr, id)
	}
	set(ids.gw, "Quantum-Gate", "router", "Ubiquiti UniFi UDM-SE 5.1.33 Linux")
	set(ids.sw, "QuantumLink", "switch", "USW-Pro-48-PoE, 7.2.123")
	set(ids.ap, "KitchenAP", "access_point", "U7-Pro")
	set(ids.ups, "Rack UPS", "ups", "")
	port := func(dev uuid.UUID, ix int, name, mac string, typ int, speed int64) {
		testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac, if_type, speed_bps, present) VALUES (?, ?, ?, ?, ?, ?, true)`,
			dev, ix, name, mac, typ, speed)
	}
	port(ids.gw, 4, "eth10", "e4:38:83:20:f5:d0", 6, 10_000_000_000)
	port(ids.gw, 24, "eth10.10", "e4:38:83:20:f5:d0", 135, 10_000_000_000)
	port(ids.gw, 16, "eth0", "e4:38:83:20:f5:c6", 6, 1_000_000_000)
	for _, ix := range []int{4, 5, 48, 49, 51, 52} {
		port(ids.sw, ix, "0/"+itoaTest(ix), "f4:e2:c6:58:0c:b8", 6, 10_000_000_000)
	}
	port(ids.ap, 1, "eth0", "84:78:48:fe:d0:e8", 6, 1_000_000_000)
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, sys_name, port_id, port_descr) VALUES (?, 51, 'e4:38:83:20:f5:c6', 'Quantum-Gate', 'e4:38:83:20:f5:d0', 'eth10')`, ids.sw)
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, sys_name, port_id, port_descr) VALUES (?, 48, '84:78:48:fe:d0:e8', 'KitchenAP', '84:78:48:fe:d0:e8', 'eth0')`, ids.sw)
	ids.admin = Viewer{UserID: testdb.NewUser(t, db, true), IsAdmin: true}
	return ids
}

func itoaTest(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoaTest(n/10) + string(rune('0'+n%10))
}

func newMapService(db *gorm.DB) *SiteMapService {
	return NewSiteMapService(db, NewSiteService(db), NewEndpointService(db, NewSiteService(db), &fakeResolver{}), fixedThresholds{})
}

func mapDeviceIDs(ds []MapDevice) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, d := range ds {
		out[d.ID] = true
	}
	return out
}

func TestSnapshotDevicesAreRoutersAndSwitches(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	m, err := newMapService(db).Snapshot(context.Background(), h.admin, h.site)
	testdb.Must(t, err)
	got := mapDeviceIDs(m.Devices)
	if len(got) != 2 || !got[h.gw] || !got[h.sw] {
		t.Fatalf("devices = %+v, want the router and the switch", m.Devices)
	}
	cand := mapDeviceIDs(m.Candidates)
	if !cand[h.ap] || !cand[h.ups] || !m.CanEdit || !m.IsAdmin || m.SiteName != "S6 Node" {
		t.Fatalf("snapshot = %+v", m)
	}
}

func TestSnapshotHomeLink(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	m, err := newMapService(db).Snapshot(context.Background(), h.admin, h.site)
	testdb.Must(t, err)
	if len(m.Links) != 1 {
		t.Fatalf("links = %+v, want 1", m.Links)
	}
	l := m.Links[0]
	label := map[uuid.UUID]string{l.A.DeviceID: l.A.Label, l.B.DeviceID: l.B.Label}
	if label[h.sw] != "51" || label[h.gw] != "11" {
		t.Errorf("labels = %v, want QuantumLink 51 and Quantum-Gate 11", label)
	}
	rate := l.A
	if l.RateEnd == "b" {
		rate = l.B
	}
	if rate.DeviceID != h.sw || l.ThresholdPct != 80 || l.SpeedBps != 10_000_000_000 || len(l.Sources) != 1 || l.Sources[0] != "lldp" {
		t.Errorf("link = %+v, want rate end on QuantumLink, threshold 80, 10 Gb, from LLDP", l)
	}
}

func TestSnapshotReadonlyCannotEdit(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	user := testdb.NewUser(t, db, false)
	_, err := NewSiteService(db).UpsertShare(ctx, h.site, user, h.admin.UserID, models.PermissionReadonly)
	testdb.Must(t, err)
	svc := newMapService(db)
	v := Viewer{UserID: user}
	m, err := svc.Snapshot(ctx, v, h.site)
	testdb.Must(t, err)
	if m.CanEdit || len(m.Candidates) != 0 {
		t.Errorf("read-only snapshot = %+v, want no editing", m)
	}
	if err := svc.SetPositions(ctx, v, h.site, []PositionInput{{DeviceID: h.sw, X: 8, Y: 8}}); !errors.Is(err, ErrSiteMapForbidden) {
		t.Errorf("read-only SetPositions = %v, want ErrSiteMapForbidden", err)
	}
}

func TestSnapshotNoAccess(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	_, err := newMapService(db).Snapshot(context.Background(), Viewer{UserID: testdb.NewUser(t, db, false)}, h.site)
	if !errors.Is(err, ErrSiteMapNotFound) {
		t.Fatalf("no share = %v, want ErrSiteMapNotFound", err)
	}
}

func TestSetPositionsSnaps(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	testdb.Must(t, svc.SetPositions(ctx, h.admin, h.site, []PositionInput{{DeviceID: h.sw, X: 13, Y: 21}}))
	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	for _, d := range m.Devices {
		if d.ID == h.sw && (d.X == nil || *d.X != 16 || *d.Y != 24) {
			t.Fatalf("position = %v,%v, want 16,24", d.X, d.Y)
		}
	}
	if err := svc.SetPositions(ctx, h.admin, h.site, []PositionInput{{DeviceID: h.ap, X: 0, Y: 0}}); !errors.Is(err, ErrSiteMapInvalid) {
		t.Errorf("placing an AP = %v, want ErrSiteMapInvalid", err)
	}
}

func TestAddDeviceOfOtherType(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	fw := seedDeviceInSite(t, db, h.site, "10.0.9.2")
	testdb.Exec(t, db, `UPDATE devices SET name = 'Edge-FW', device_type = 'other' WHERE id = ?`, fw)
	svc := newMapService(db)
	yes := true
	testdb.Must(t, svc.SetDevice(ctx, h.admin, h.site, fw, nil, &yes))
	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	if !mapDeviceIDs(m.Devices)[fw] {
		t.Fatalf("added firewall missing: %+v", m.Devices)
	}
	if err := svc.SetDevice(ctx, h.admin, h.site, h.gw, nil, &yes); !errors.Is(err, ErrSiteMapInvalid) {
		t.Errorf("adding a router = %v, want ErrSiteMapInvalid", err)
	}
}

func TestManualLinkLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	link, err := svc.AddLink(ctx, h.admin, h.site, sitemap.End{DeviceID: h.sw, IfIndex: 52}, sitemap.End{DeviceID: h.gw, IfIndex: 16})
	testdb.Must(t, err)
	has := func(key string) bool {
		m, err := svc.Snapshot(ctx, h.admin, h.site)
		testdb.Must(t, err)
		// The manual cable and the LLDP one join the same two devices, so
		// they're drawn as a bundle: look among its members too.
		for _, l := range m.Links {
			if l.Key == key {
				return true
			}
			for _, mem := range l.Members {
				if mem.Key == key {
					return true
				}
			}
		}
		return false
	}
	k1 := sitemap.KeyOf(sitemap.End{DeviceID: h.sw, IfIndex: 52}, sitemap.End{DeviceID: h.gw, IfIndex: 16})
	if !has(k1) {
		t.Fatal("manual link missing")
	}
	testdb.Must(t, svc.UpdateLink(ctx, h.admin, h.site, link.ID, sitemap.End{DeviceID: h.sw, IfIndex: 49}, sitemap.End{DeviceID: h.gw, IfIndex: 16}))
	if has(k1) || !has(sitemap.KeyOf(sitemap.End{DeviceID: h.sw, IfIndex: 49}, sitemap.End{DeviceID: h.gw, IfIndex: 16})) {
		t.Fatal("update didn't move the link")
	}
	testdb.Must(t, svc.DeleteLink(ctx, h.admin, h.site, link.ID))
	if has(sitemap.KeyOf(sitemap.End{DeviceID: h.sw, IfIndex: 49}, sitemap.End{DeviceID: h.gw, IfIndex: 16})) {
		t.Fatal("deleted link still there")
	}
	if _, err := svc.AddLink(ctx, h.admin, h.site, sitemap.End{DeviceID: h.sw, IfIndex: 99}, sitemap.End{DeviceID: h.gw}); !errors.Is(err, ErrSiteMapInvalid) {
		t.Errorf("a port that doesn't exist = %v, want ErrSiteMapInvalid", err)
	}
	if _, err := svc.AddLink(ctx, h.admin, h.site, sitemap.End{DeviceID: h.sw}, sitemap.End{DeviceID: h.ap}); !errors.Is(err, ErrSiteMapInvalid) {
		t.Errorf("a link to an AP = %v, want ErrSiteMapInvalid", err)
	}
	if err := svc.DeleteLink(ctx, h.admin, h.site, uuid.New()); !errors.Is(err, ErrSiteMapInvalid) {
		t.Errorf("deleting no link = %v, want ErrSiteMapInvalid", err)
	}
}

func TestHideAndKeep(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	// A switch with no LLDP, its MAC learnt on QuantumLink port 49.
	acc := seedDeviceInSite(t, db, h.site, "10.0.9.20")
	testdb.Exec(t, db, `UPDATE devices SET name = 'Office-1', device_type = 'switch' WHERE id = ?`, acc)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac, if_type, present) VALUES (?, 1, 'Port 1', '02:00:00:00:0a:01', 6, true)`, acc)
	testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, vlan, mac) VALUES (?, 49, 1, '02:00:00:00:0a:01')`, h.sw)
	svc := newMapService(db)

	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	var guess, lldp *MapLink
	for i := range m.Links {
		if m.Links[i].Guess {
			guess = &m.Links[i]
		} else {
			lldp = &m.Links[i]
		}
	}
	if guess == nil || lldp == nil {
		t.Fatalf("links = %+v, want the LLDP link and a guess", m.Links)
	}
	testdb.Must(t, svc.SetLinkHidden(ctx, h.admin, h.site, lldp.Key, true))
	_, err = svc.KeepGuess(ctx, h.admin, h.site, guess.Key)
	testdb.Must(t, err)
	m, err = svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	if len(m.HiddenLinks) != 1 || m.HiddenLinks[0].Key != lldp.Key {
		t.Errorf("hidden = %+v, want the LLDP link", m.HiddenLinks)
	}
	if len(m.Links) != 1 || m.Links[0].Guess || m.Links[0].ManualID == nil {
		t.Errorf("links = %+v, want the kept guess as a manual link", m.Links)
	}
	testdb.Must(t, svc.SetLinkHidden(ctx, h.admin, h.site, lldp.Key, false))
	m, _ = svc.Snapshot(ctx, h.admin, h.site)
	if len(m.HiddenLinks) != 0 || len(m.Links) != 2 {
		t.Errorf("after showing again: links %d, hidden %d", len(m.Links), len(m.HiddenLinks))
	}
}

func TestResetLayoutKeepsHidden(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	yes := true
	testdb.Must(t, svc.SetPositions(ctx, h.admin, h.site, []PositionInput{{DeviceID: h.sw, X: 40, Y: 40}, {DeviceID: h.gw, X: 80, Y: 0}}))
	testdb.Must(t, svc.SetDevice(ctx, h.admin, h.site, h.gw, &yes, nil))
	testdb.Must(t, svc.ResetLayout(ctx, h.admin, h.site))
	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	for _, d := range m.Devices {
		if d.X != nil {
			t.Errorf("%s still has a position after reset", d.Name)
		}
	}
	if len(m.HiddenDevices) != 1 || m.HiddenDevices[0].ID != h.gw {
		t.Errorf("hidden devices = %+v, want Quantum-Gate", m.HiddenDevices)
	}
}

// Review Focus 1.
func TestDeviceMovedOrRetyped(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	testdb.Must(t, svc.SetPositions(ctx, h.admin, h.site, []PositionInput{{DeviceID: h.sw, X: 40, Y: 40}}))
	_, err := svc.AddLink(ctx, h.admin, h.site, sitemap.End{DeviceID: h.sw, IfIndex: 52}, sitemap.End{DeviceID: h.gw, IfIndex: 16})
	testdb.Must(t, err)

	other := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Elsewhere')`, other)
	devs := NewDeviceService(db, NewSNMPCredentialService(db), nil)
	d, err := devs.Get(ctx, h.sw)
	testdb.Must(t, err)
	enabled := true
	testdb.Exec(t, db, `UPDATE snmp_credentials SET site_id = NULL WHERE id = ?`, d.CredentialID)
	_, _, err = devs.Update(ctx, h.sw, models.DeviceInput{SiteID: other, CredentialID: d.CredentialID, Name: d.Name, Host: d.Host, Port: 161, Enabled: &enabled})
	testdb.Must(t, err)
	if n := countRows(t, db, `SELECT count(*) FROM site_map_positions WHERE device_id = ?`, h.sw); n != 0 {
		t.Errorf("positions after the move = %d, want 0", n)
	}
	if n := countRows(t, db, `SELECT count(*) FROM site_map_links WHERE a_device_id = ? OR b_device_id = ?`, h.sw, h.sw); n != 0 {
		t.Errorf("links after the move = %d, want 0", n)
	}
	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	if mapDeviceIDs(m.Devices)[h.sw] || len(m.Links) != 0 {
		t.Errorf("moved switch still on the old map: %+v", m)
	}
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'access_point' WHERE id = ?`, h.gw)
	m, _ = svc.Snapshot(ctx, h.admin, h.site)
	if len(m.Devices) != 0 {
		t.Errorf("retyped router still on the map: %+v", m.Devices)
	}
}

func TestPublicSnapshotTrimmed(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	testdb.Exec(t, db, `UPDATE devices SET status_detail = 'timeout from 10.1.255.1' WHERE id = ?`, h.gw)
	m, err := newMapService(db).PublicSnapshot(context.Background(), h.site)
	testdb.Must(t, err)
	if m.CanEdit || len(m.Candidates) != 0 || len(m.HiddenLinks) != 0 {
		t.Errorf("public snapshot offers editing: %+v", m)
	}
	real := map[uuid.UUID]bool{h.gw: true, h.sw: true, h.site: true}
	for _, d := range m.Devices {
		if d.Host != "" || d.StatusDetail != "" || d.Connected != nil || real[d.ID] {
			t.Errorf("public device leaks: %+v", d)
		}
	}
	if m.SiteID != uuid.Nil || len(m.Links) != 1 {
		t.Fatalf("public map = %+v", m)
	}
	l := m.Links[0]
	if real[l.A.DeviceID] || real[l.B.DeviceID] || strings.Contains(l.Key, h.gw.String()) || strings.Contains(l.Key, h.sw.String()) {
		t.Errorf("public link leaks ids: %+v", l)
	}
	ids := mapDeviceIDs(m.Devices)
	if !ids[l.A.DeviceID] || !ids[l.B.DeviceID] {
		t.Errorf("public link ends %v %v aren't the public devices", l.A.DeviceID, l.B.DeviceID)
	}
	// The same stand-ins every time, and traffic keyed to match.
	again, _ := newMapService(db).PublicSnapshot(context.Background(), h.site)
	if again.Links[0].Key != l.Key {
		t.Errorf("stand-in ids changed between snapshots")
	}
	full, _ := newMapService(db).Snapshot(context.Background(), h.admin, h.site)
	up := true
	pt := PublicTraffic(h.site, MapTraffic{SiteID: h.site, Links: map[string]LinkRate{full.Links[0].Key: {OperUp: &up}}, Devices: map[uuid.UUID]string{h.sw: "up"}})
	if _, ok := pt.Links[l.Key]; !ok || pt.SiteID != uuid.Nil || pt.Devices[h.sw] != "" || len(pt.Devices) != 1 {
		t.Errorf("public traffic = %+v, want keyed by the public link key", pt)
	}
	dev, ok, err := newMapService(db).PublicDevice(context.Background(), h.site, l.A.DeviceID)
	if err != nil || !ok || (dev != h.gw && dev != h.sw) {
		t.Errorf("PublicDevice = %v %v %v", dev, ok, err)
	}
	if _, ok, _ := newMapService(db).PublicDevice(context.Background(), h.site, h.gw); ok {
		t.Error("a real device id opened a public device")
	}
}

func TestPublicLinkTokenRules(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	link, _, err := svc.CreatePublicLink(ctx, h.admin, h.site)
	testdb.Must(t, err)
	if len(link.Token) != 43 {
		t.Fatalf("token length = %d, want 43", len(link.Token))
	}
	if site, err := svc.ResolveToken(ctx, link.Token); err != nil || site != h.site {
		t.Fatalf("resolve = %v %v", site, err)
	}
	testdb.Exec(t, db, `UPDATE users SET is_admin = false WHERE id = ?`, h.admin.UserID)
	if _, err := svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrSiteMapNotFound) {
		t.Errorf("creator no longer admin = %v, want ErrSiteMapNotFound", err)
	}
	testdb.Exec(t, db, `UPDATE users SET is_admin = true WHERE id = ?`, h.admin.UserID)
	testdb.Must(t, svc.RevokePublicLink(ctx, h.admin, h.site))
	if _, err := svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrSiteMapNotFound) {
		t.Errorf("revoked = %v, want ErrSiteMapNotFound", err)
	}
	if _, err := svc.ResolveToken(ctx, "short"); !errors.Is(err, ErrSiteMapNotFound) {
		t.Errorf("short token = %v", err)
	}
	editor := testdb.NewUser(t, db, false)
	_, err = NewSiteService(db).UpsertShare(ctx, h.site, editor, h.admin.UserID, models.PermissionEditable)
	testdb.Must(t, err)
	if _, _, err := svc.CreatePublicLink(ctx, Viewer{UserID: editor}, h.site); !errors.Is(err, ErrSiteMapForbidden) {
		t.Errorf("non-admin create = %v, want ErrSiteMapForbidden", err)
	}
}

func TestOnChangeCalled(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	svc := newMapService(db)
	var got []uuid.UUID
	svc.OnChange(func(site uuid.UUID) { got = append(got, site) })
	testdb.Must(t, svc.SetPositions(ctx, h.admin, h.site, []PositionInput{{DeviceID: h.sw, X: 8, Y: 8}}))
	testdb.Must(t, svc.ResetLayout(ctx, h.admin, h.site))
	if len(got) != 2 || got[0] != h.site {
		t.Errorf("OnChange calls = %v, want 2 for the site", got)
	}
}

func TestBoundPortsAreRateEnds(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	ps, _, err := newMapService(db).BoundPorts(context.Background(), h.site)
	testdb.Must(t, err)
	if len(ps) != 1 || ps[0].DeviceID != h.sw || ps[0].IfIndex != 51 {
		t.Fatalf("bound ports = %+v, want QuantumLink 51", ps)
	}
}

var _ = time.Second

type countingPlacements struct {
	inner mapPlacements
	calls int
}

func (c *countingPlacements) Placements(ctx context.Context, macs []string) (map[string]l2.Placement, error) {
	c.calls++
	return c.inner.Placements(ctx, macs)
}

// Review: the org-wide L2 placements are read at most once per few seconds,
// and not at all to resolve a public stand-in id.
func TestSnapshotPlacementsCached(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeMap(t, db)
	count := &countingPlacements{inner: NewEndpointService(db, NewSiteService(db), &fakeResolver{})}
	svc := NewSiteMapService(db, NewSiteService(db), count, fixedThresholds{})
	for i := 0; i < 3; i++ {
		_, err := svc.Snapshot(ctx, h.admin, h.site)
		testdb.Must(t, err)
	}
	_, _, err := svc.BoundPorts(ctx, h.site)
	testdb.Must(t, err)
	if count.calls != 1 {
		t.Errorf("placements read %d times, want 1", count.calls)
	}
	pub, err := svc.PublicSnapshot(ctx, h.site)
	testdb.Must(t, err)
	before := count.calls
	if _, ok, _ := svc.PublicDevice(ctx, h.site, pub.Devices[0].ID); !ok || count.calls != before {
		t.Errorf("PublicDevice: ok %v, placements +%d", ok, count.calls-before)
	}
}

// seedHomeBundle adds a second cable to the home map: QuantumLink 52 to
// Quantum-Gate eth11, with QuantumLink's 51 and 52 in aggregator 3/1.
func seedHomeBundle(t *testing.T, db *gorm.DB) homeMapIDs {
	t.Helper()
	h := seedHomeMap(t, db)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac, if_type, speed_bps, present) VALUES (?, 5, 'eth11', 'e4:38:83:20:f5:d1', 6, 10000000000, true)`, h.gw)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac, if_type, speed_bps, present) VALUES (?, 66, '3/1', 'f4:e2:c6:58:0c:b8', 161, 20000000000, true)`, h.sw)
	testdb.Exec(t, db, `UPDATE device_interfaces SET lag_if_index = 66 WHERE device_id = ? AND if_index IN (51, 52)`, h.sw)
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, sys_name, port_id, port_descr) VALUES (?, 52, 'e4:38:83:20:f5:c6', 'Quantum-Gate', 'e4:38:83:20:f5:d1', 'eth11')`, h.sw)
	return h
}

func TestDBSiteMapBundleSnapshot(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeBundle(t, db)
	svc := newMapService(db)
	m, err := svc.Snapshot(context.Background(), h.admin, h.site)
	testdb.Must(t, err)
	if len(m.Links) != 1 || len(m.Links[0].Members) != 2 {
		t.Fatalf("links = %+v, want one bundle of two", m.Links)
	}
	b := m.Links[0]
	if !isBundleKey(b.Key) || b.SpeedBps != 20_000_000_000 || b.Lag == nil {
		t.Fatalf("bundle = %+v", b)
	}
	lagAt := map[uuid.UUID]string{b.A.DeviceID: b.Lag.A, b.B.DeviceID: b.Lag.B}
	if lagAt[h.sw] != "3/1" || lagAt[h.gw] != "" {
		t.Errorf("lag = %+v, want 3/1 on QuantumLink only", lagAt)
	}
	ps, _, err := svc.BoundPorts(context.Background(), h.site)
	testdb.Must(t, err)
	got := map[int]string{}
	for _, p := range ps {
		if p.DeviceID != h.sw {
			t.Errorf("bound port %+v, want QuantumLink's", p)
		}
		got[p.IfIndex] = p.LinkKey
	}
	keys := bundleMembers(b.Key)
	if len(ps) != 2 || got[51] == "" || got[52] == "" || got[51] == got[52] || !(got[51] == keys[0] || got[51] == keys[1]) || !(got[52] == keys[0] || got[52] == keys[1]) {
		t.Errorf("bound ports = %+v, want QuantumLink 51 and 52 under the member keys %v", ps, keys)
	}
}

// Review Focus 4: hiding a bundle hides each cable in it.
func TestDBSiteMapHideBundleHidesMembers(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h := seedHomeBundle(t, db)
	svc := newMapService(db)
	m, err := svc.Snapshot(ctx, h.admin, h.site)
	testdb.Must(t, err)
	key := m.Links[0].Key
	hiddenRows := func() int {
		var n int
		testdb.Must(t, db.Raw(`SELECT count(*) FROM site_map_links WHERE site_id = ? AND kind = 'hidden'`, h.site).Scan(&n).Error)
		return n
	}

	if err := svc.SetLinkHidden(ctx, h.admin, h.site, key+",nonsense", true); !errors.Is(err, ErrSiteMapInvalid) || hiddenRows() != 0 {
		t.Fatalf("a bundle key with a bad member: %v, %d rows; want invalid and nothing hidden", err, hiddenRows())
	}
	testdb.Must(t, svc.SetLinkHidden(ctx, h.admin, h.site, key, true))
	if n := hiddenRows(); n != 2 {
		t.Errorf("hidden rows = %d, want one per cable", n)
	}
	m, _ = svc.Snapshot(ctx, h.admin, h.site)
	if len(m.Links) != 0 || len(m.HiddenLinks) != 1 || m.HiddenLinks[0].Key != key {
		t.Fatalf("links %+v, hidden %+v; want the bundle hidden as one", m.Links, m.HiddenLinks)
	}
	if ps, _, _ := svc.BoundPorts(ctx, h.site); len(ps) != 0 {
		t.Errorf("bound ports %+v for a hidden bundle", ps)
	}
	testdb.Must(t, svc.SetLinkHidden(ctx, h.admin, h.site, key, false))
	m, _ = svc.Snapshot(ctx, h.admin, h.site)
	if len(m.Links) != 1 || len(m.HiddenLinks) != 0 || hiddenRows() != 0 {
		t.Errorf("after showing: links %d, hidden %d, rows %d", len(m.Links), len(m.HiddenLinks), hiddenRows())
	}
}

// Review Focus 5: a bundle's members carry no real ids onto a public map.
func TestPublicSnapshotAliasesBundleMembers(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeBundle(t, db)
	svc := newMapService(db)
	m, err := svc.PublicSnapshot(context.Background(), h.site)
	testdb.Must(t, err)
	raw, err := json.Marshal(m)
	testdb.Must(t, err)
	for _, id := range []uuid.UUID{h.gw, h.sw, h.site} {
		if strings.Contains(string(raw), id.String()) {
			t.Errorf("public map carries %s: %s", id, raw)
		}
	}
	if len(m.Links) != 1 || len(m.Links[0].Members) != 2 {
		t.Fatalf("public links = %+v", m.Links)
	}
	b := m.Links[0]
	var memberKeys []string
	for _, mem := range b.Members {
		memberKeys = append(memberKeys, mem.Key)
	}
	if b.Key != "bundle:"+strings.Join(memberKeys, ",") {
		t.Errorf("public bundle key %q isn't built from its members' public keys %v", b.Key, memberKeys)
	}
	// Traffic, keyed by the real member keys, arrives under the public ones.
	full, _ := svc.Snapshot(context.Background(), h.admin, h.site)
	up := true
	rates := map[string]LinkRate{}
	for _, mem := range full.Links[0].Members {
		rates[mem.Key] = LinkRate{OperUp: &up}
	}
	pt := PublicTraffic(h.site, MapTraffic{Links: rates})
	for _, k := range memberKeys {
		if _, ok := pt.Links[k]; !ok {
			t.Errorf("public traffic %v has no rate for member %s", pt.Links, k)
		}
	}
}

func TestDBSiteMapRouterWAN(t *testing.T) {
	db := testdb.Open(t)
	h := seedHomeMap(t, db)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac, if_type, speed_bps, present, role, role_source, role_reason)
		VALUES (?, 13, 'eth8', 'e4:38:83:20:f5:ce', 6, 1000000000, true, 'wan', 'auto', 'Has a public address')`, h.gw)
	// A switch port marked WAN isn't the internet on the map: only routers'.
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'wan' WHERE device_id = ? AND if_index = 5`, h.sw)
	svc := newMapService(db)
	m, err := svc.Snapshot(context.Background(), h.admin, h.site)
	testdb.Must(t, err)
	wan := map[uuid.UUID][]MapWAN{}
	for _, d := range m.Devices {
		wan[d.ID] = d.WAN
	}
	want := []MapWAN{{IfIndex: 13, Name: "eth8", SpeedBps: 1_000_000_000, Reason: "Has a public address"}}
	if !reflect.DeepEqual(wan[h.gw], want) || wan[h.sw] != nil {
		t.Errorf("WAN = router %+v, switch %+v; want %+v on the router only", wan[h.gw], wan[h.sw], want)
	}
	ps, _, err := svc.BoundPorts(context.Background(), h.site)
	testdb.Must(t, err)
	found := false
	for _, p := range ps {
		if p.DeviceID == h.gw && p.IfIndex == 13 {
			found = p.LinkKey == wanKey(h.gw, 13) && p.LinkKey == "wan:"+h.gw.String()+":13" && p.InterfaceID != uuid.Nil
		}
	}
	if !found {
		t.Errorf("bound ports %+v, want Quantum-Gate 13 under wan:<id>:13", ps)
	}
	pub, err := svc.PublicSnapshot(context.Background(), h.site)
	testdb.Must(t, err)
	for _, d := range pub.Devices {
		if len(d.WAN) > 0 && d.WAN[0] != (MapWAN{IfIndex: 13, SpeedBps: 1_000_000_000}) {
			t.Errorf("public WAN = %+v, want no name or reason", d.WAN)
		}
	}
}

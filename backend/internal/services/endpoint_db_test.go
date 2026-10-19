package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// fakeResolver answers reverse lookups from names and forward ones from
// hosts, counting the reverse lookups.
type fakeResolver struct {
	mu      sync.Mutex
	names   map[string]string
	hosts   map[string][]string
	reverse int
}

func (f *fakeResolver) LookupAddr(_ context.Context, ip string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reverse++
	if n, ok := f.names[ip]; ok {
		return []string{n + "."}, nil
	}
	return nil, errors.New("no such host")
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if ips, ok := f.hosts[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host")
}

func (f *fakeResolver) reverseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reverse
}

const (
	camMAC   = "44:19:b6:01:02:03" // Hikvision
	phoneMAC = "80:5e:c0:aa:bb:cc" // Yealink
)

func sight(t *testing.T, db *gorm.DB, dev uuid.UUID, ifIndex int, mac string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO l2.sightings (device_id, if_index, vlan, mac) VALUES (?, ?, 10, ?)`, dev, ifIndex, mac)
}

func arp(t *testing.T, db *gorm.DB, dev uuid.UUID, ip, mac string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO l2.arp (device_id, ip, mac) VALUES (?, ?, ?)`, dev, ip, mac)
}

func neighbor(t *testing.T, db *gorm.DB, dev uuid.UUID, ifIndex int, chassis, name string, caps int) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO l2.neighbors (device_id, if_index, chassis_id, sys_name, caps) VALUES (?, ?, ?, ?, ?)`, dev, ifIndex, chassis, name, caps)
}

func admin(t *testing.T, db *gorm.DB) Viewer {
	return Viewer{UserID: testdb.NewUser(t, db, true), IsAdmin: true}
}

func newEndpoints(db *gorm.DB, r Resolver) *EndpointService {
	return NewEndpointService(db, NewSiteService(db), r)
}

// one takes a Search result that must hold exactly one record.
func one(t *testing.T) func([]Endpoint, error) Endpoint {
	return func(got []Endpoint, err error) Endpoint {
		t.Helper()
		testdb.Must(t, err)
		if len(got) != 1 {
			t.Fatalf("got %d results: %+v", len(got), got)
		}
		return got[0]
	}
}

func TestDBEndpointRecord(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	seedPort(t, db, sw.DeviceID, 5, "0/5", "")
	seedPort(t, db, sw.DeviceID, 7, "0/7", "")
	router := seedDevice(t, db, "Branch", "10.0.0.1")
	neighbor(t, db, sw.DeviceID, 7, "24:5a:4c:0a:0b:0c", "KitchenAP", 0x10|0x20)
	sight(t, db, sw.DeviceID, 5, camMAC)
	sight(t, db, sw.DeviceID, 7, camMAC) // also learned through the AP's port
	sight(t, db, sw.DeviceID, 7, phoneMAC)
	arp(t, db, router.DeviceID, "10.1.10.60", camMAC)
	arp(t, db, router.DeviceID, "10.1.10.99", "3c:ef:8c:12:9a:41")
	s := newEndpoints(db, &fakeResolver{})
	v := admin(t, db)

	cam := one(t)(s.Search(ctx, v, camMAC))
	if cam.How != "wired" || cam.DeviceID == nil || *cam.DeviceID != sw.DeviceID || cam.IfIndex == nil || *cam.IfIndex != 5 ||
		cam.PortLabel != "5" || cam.VLAN != 10 || cam.Kind != "camera" || len(cam.IPs) != 1 || cam.IPs[0] != "10.1.10.60" ||
		cam.SiteID != sw.SiteID || cam.Maker == "" {
		t.Errorf("camera: %+v", cam)
	}
	phone := one(t)(s.Search(ctx, v, phoneMAC))
	if phone.How != "wireless" || phone.Via != "KitchenAP" || *phone.IfIndex != 7 || phone.Kind != "phone" {
		t.Errorf("wireless phone: %+v", phone)
	}
	only := one(t)(s.Search(ctx, v, "10.1.10.99"))
	if only.How != "router" || only.DeviceID == nil || *only.DeviceID != router.DeviceID || only.IfIndex != nil || only.SiteID != router.SiteID {
		t.Errorf("ARP only: %+v", only)
	}
}

func TestDBEndpointSearchSiteAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	sight(t, db, sw.DeviceID, 5, camMAC)
	arp(t, db, sw.DeviceID, "10.1.10.60", camMAC)
	s := newEndpoints(db, &fakeResolver{hosts: map[string][]string{"cam1.home.arpa": {"10.1.10.60"}}})

	outsider := Viewer{UserID: testdb.NewUser(t, db, false)}
	for _, q := range []string{"10.1.10.60", camMAC, "cam1.home.arpa"} {
		got, err := s.Search(ctx, outsider, q)
		testdb.Must(t, err)
		if len(got) != 0 {
			t.Errorf("outsider searching %q got %+v", q, got)
		}
	}
	reader := Viewer{UserID: testdb.NewUser(t, db, false)}
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, sw.SiteID, reader.UserID)
	for _, q := range []string{"10.1.10.60", camMAC, "cam1.home.arpa"} {
		got, err := s.Search(ctx, reader, q)
		testdb.Must(t, err)
		if len(got) != 1 {
			t.Errorf("reader searching %q got %d results", q, len(got))
		}
	}
}

func TestDBEndpointSearchMACForms(t *testing.T) {
	db := testdb.Open(t)
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	sight(t, db, sw.DeviceID, 5, "aa:bb:cc:dd:ee:ff")
	s := newEndpoints(db, &fakeResolver{})
	v := admin(t, db)
	for _, q := range []string{"AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff", "aa:bb:cc", "AABBCC"} {
		got, err := s.Search(context.Background(), v, q)
		testdb.Must(t, err)
		if len(got) != 1 {
			t.Errorf("%q found %d", q, len(got))
		}
	}
}

func TestDBEndpointNames(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	sight(t, db, sw.DeviceID, 5, phoneMAC)
	arp(t, db, sw.DeviceID, "10.1.10.61", phoneMAC)
	testdb.Exec(t, db, `INSERT INTO l2.dns_names (ip, name) VALUES ('10.1.10.61', 'sip-t54w.home.arpa')`)
	s := newEndpoints(db, &fakeResolver{})
	v := admin(t, db)

	if e := one(t)(s.Search(ctx, v, "sip-t54w")); e.Name != "sip-t54w.home.arpa" || e.NameSource != "dns" {
		t.Fatalf("DNS name: %+v", e)
	}
	reader := Viewer{UserID: testdb.NewUser(t, db, false)}
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, sw.SiteID, reader.UserID)
	if _, _, err := s.SetName(ctx, reader, phoneMAC, "Nope"); !errors.Is(err, ErrEndpointForbidden) {
		t.Errorf("readonly set name: %v", err)
	}
	if _, _, err := s.SetName(ctx, v, "02:00:00:00:00:99", "Ghost"); !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("unknown MAC: %v", err)
	}
	before, after, err := s.SetName(ctx, v, "80-5E-C0-AA-BB-CC", " Front desk phone ")
	testdb.Must(t, err)
	if before != "" || after != "Front desk phone" {
		t.Errorf("before %q after %q", before, after)
	}
	if e := one(t)(s.Search(ctx, v, "front desk")); e.Name != "Front desk phone" || e.NameSource != "user" {
		t.Errorf("user name: %+v", e)
	}
	before, after, err = s.SetName(ctx, v, phoneMAC, "")
	testdb.Must(t, err)
	if before != "Front desk phone" || after != "" {
		t.Errorf("clear: before %q after %q", before, after)
	}
	if e := one(t)(s.Search(ctx, v, phoneMAC)); e.NameSource != "dns" {
		t.Errorf("after clearing: %+v", e)
	}
}

func TestDBEndpointDNSCache(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	sight(t, db, sw.DeviceID, 5, camMAC)
	arp(t, db, sw.DeviceID, "10.1.10.60", camMAC)
	r := &fakeResolver{names: map[string]string{"10.1.10.60": "cam1.home.arpa"}}
	s := newEndpoints(db, r)
	v := admin(t, db)

	if e := one(t)(s.Search(ctx, v, camMAC)); e.Name != "" {
		t.Errorf("first read blocked on DNS: %+v", e)
	}
	s.waitDNS()
	if r.reverseCalls() != 1 {
		t.Fatalf("reverse lookups %d, want 1", r.reverseCalls())
	}
	if e := one(t)(s.Search(ctx, v, camMAC)); e.Name != "cam1.home.arpa" || e.NameSource != "dns" {
		t.Errorf("after the lookup: %+v", e)
	}
	s.waitDNS()
	if r.reverseCalls() != 1 {
		t.Errorf("looked up again within 24 h: %d", r.reverseCalls())
	}
	if e := one(t)(s.Search(ctx, v, "cam1")); e.MAC != camMAC {
		t.Errorf("search by DNS name: %+v", e)
	}
}

func TestDBConnectedView(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	for _, i := range []int{3, 7, 12, 49} {
		seedPort(t, db, sw.DeviceID, i, fmt.Sprintf("0/%d", i), "")
	}
	for i := 1; i <= 3; i++ {
		sight(t, db, sw.DeviceID, 3, fmt.Sprintf("00:11:22:33:44:%02x", i))
	}
	neighbor(t, db, sw.DeviceID, 7, "24:5a:4c:0a:0b:0c", "KitchenAP", 0x10)
	sight(t, db, sw.DeviceID, 7, "24:5a:4c:0a:0b:0c") // the AP itself
	sight(t, db, sw.DeviceID, 7, camMAC)
	sight(t, db, sw.DeviceID, 7, phoneMAC)
	for i := 1; i <= 12; i++ {
		sight(t, db, sw.DeviceID, 12, fmt.Sprintf("02:00:00:00:12:%02x", i))
	}
	neighbor(t, db, sw.DeviceID, 49, "78:8a:20:00:00:01", "Core", 0x20)
	sight(t, db, sw.DeviceID, 49, "02:00:00:00:49:01")
	s := newEndpoints(db, &fakeResolver{})

	view, err := s.ForDevice(ctx, admin(t, db), sw.DeviceID)
	testdb.Must(t, err)
	if len(view.Endpoints) != 3 {
		t.Errorf("edge endpoints %d, want 3: %+v", len(view.Endpoints), view.Endpoints)
	}
	if len(view.APs) != 1 || view.APs[0].Name != "KitchenAP" || view.APs[0].IfIndex != 7 || len(view.APs[0].Clients) != 2 {
		t.Errorf("APs %+v", view.APs)
	}
	if len(view.Behind) != 1 || view.Behind[0].IfIndex != 12 || view.Behind[0].Count != 12 || len(view.Behind[0].Endpoints) != 12 {
		t.Errorf("behind %+v", view.Behind)
	}
	for _, e := range view.Endpoints {
		if *e.IfIndex == 49 {
			t.Errorf("link port endpoint %+v", e)
		}
	}
	other := Viewer{UserID: testdb.NewUser(t, db, false)}
	if _, err := s.ForDevice(ctx, other, sw.DeviceID); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("unshared device: %v", err)
	}
}

// A monitored access point is an AP port even when its LLDP says only
// "bridge", and the group links to the AP's own page.
func TestDBConnectedViewMonitoredAP(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	ap := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.40")
	testdb.Exec(t, db, `UPDATE devices SET name = 'OfficeAP', device_type = 'access_point' WHERE id = ?`, ap)
	neighbor(t, db, sw.DeviceID, 8, "24:5a:4c:0a:0b:0d", "officeap", 0x20)
	sight(t, db, sw.DeviceID, 8, phoneMAC)
	view, err := newEndpoints(db, &fakeResolver{}).ForDevice(ctx, admin(t, db), sw.DeviceID)
	testdb.Must(t, err)
	if len(view.APs) != 1 || view.APs[0].APDeviceID == nil || *view.APs[0].APDeviceID != ap || view.APs[0].Name != "OfficeAP" || len(view.APs[0].Clients) != 1 {
		t.Errorf("APs %+v", view.APs)
	}
}

// Review: a monitored switch uplinked without LLDP or a port link: the
// core's port that learns the switch's own MAC is a link, so the devices
// behind it are placed on the switch, not flipping between the two.
func TestDBConnectedViewMonitoredSwitchUplink(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	core := seedDevice(t, db, "HQ", "10.0.0.2")
	edge := seedDeviceInSite(t, db, core.SiteID, "10.0.0.3")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'switch' WHERE id = ?`, edge)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, mac) VALUES (?, 1, '0/1', '78:8a:20:00:00:99')`, edge)
	sight(t, db, core.DeviceID, 10, "78:8a:20:00:00:99")
	for i := 1; i <= 3; i++ {
		m := fmt.Sprintf("00:11:22:33:44:%02x", i)
		sight(t, db, core.DeviceID, 10, m)
		sight(t, db, edge, 3+i, m)
	}
	s := newEndpoints(db, &fakeResolver{})
	v := admin(t, db)
	view, err := s.ForDevice(ctx, v, core.DeviceID)
	testdb.Must(t, err)
	if len(view.Endpoints) != 0 || len(view.Behind) != 0 {
		t.Errorf("core shows the edge switch's devices: %+v", view)
	}
	e := one(t)(s.Search(ctx, v, "00:11:22:33:44:01"))
	if e.How != "wired" || *e.DeviceID != edge || *e.IfIndex != 4 {
		t.Errorf("placed %+v", e)
	}
}

// Review: an AP monitored on a site the viewer can't see lends its name
// and id to no one: the group shows the LLDP name only.
func TestDBEndpointAPNameHiddenAcrossSites(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	other := seedDevice(t, db, "Secret", "10.0.0.40")
	testdb.Exec(t, db, `UPDATE devices SET name = 'Secret-AP', sys_name = 'u7-lobby', device_type = 'access_point' WHERE id = ?`, other.DeviceID)
	neighbor(t, db, sw.DeviceID, 8, "24:5a:4c:0a:0b:0d", "u7-lobby", 0x10)
	sight(t, db, sw.DeviceID, 8, phoneMAC)
	reader := Viewer{UserID: testdb.NewUser(t, db, false)}
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, sw.SiteID, reader.UserID)
	s := newEndpoints(db, &fakeResolver{})
	view, err := s.ForDevice(ctx, reader, sw.DeviceID)
	testdb.Must(t, err)
	if len(view.APs) != 1 || view.APs[0].Name != "u7-lobby" || view.APs[0].APDeviceID != nil {
		t.Errorf("APs %+v", view.APs)
	}
	if e := one(t)(s.Search(ctx, reader, phoneMAC)); e.Via != "u7-lobby" {
		t.Errorf("via %q", e.Via)
	}
}

// Searching an AP's (or any LLDP neighbour's) name finds it, named from
// LLDP when neither the user nor DNS names it, and lists its clients too.
func TestDBEndpointLLDPName(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	neighbor(t, db, sw.DeviceID, 48, "84:78:48:fe:d0:e8", "KitchenAP", 0x10|0x20)
	sight(t, db, sw.DeviceID, 48, "84:78:48:fe:d0:e8")
	sight(t, db, sw.DeviceID, 48, phoneMAC)
	got, err := newEndpoints(db, &fakeResolver{}).Search(ctx, admin(t, db), "kitchenap")
	testdb.Must(t, err)
	if len(got) != 2 || got[0].MAC != "84:78:48:fe:d0:e8" || got[0].Name != "KitchenAP" || got[0].NameSource != "lldp" || got[1].Via != "KitchenAP" {
		t.Errorf("%+v", got)
	}
}

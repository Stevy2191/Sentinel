package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrEndpointNotFound    = errors.New("endpoint not found")
	ErrEndpointForbidden   = errors.New("you can't rename devices on this site")
	ErrEndpointNameTooLong = errors.New("name must be at most 64 characters")
)

const (
	endpointSearchLimit  = 50
	endpointSightingsMax = 20
	endpointNameMax      = 64
)

// Viewer is who is asking, for site access.
type Viewer struct {
	UserID  uuid.UUID
	IsAdmin bool
}

// BehindGroup is a link port with an unmanaged switch (most likely) on it,
// and how many endpoints are placed behind it. Endpoints lists them in a
// switch's connected view only.
type BehindGroup struct {
	DeviceID  uuid.UUID  `json:"device_id"`
	IfIndex   int        `json:"if_index"`
	PortLabel string     `json:"port_label"`
	Count     int        `json:"count"`
	Endpoints []Endpoint `json:"endpoints,omitempty"`
}

// Endpoint is one MAC's record: what it is, its addresses and names, and
// where it is plugged in (How: wired, wireless through Via, behind a link
// port, or only in a router's ARP table).
type Endpoint struct {
	MAC        string       `json:"mac"`
	Maker      string       `json:"maker"`
	Kind       string       `json:"kind"`
	IPs        []string     `json:"ips"`
	Name       string       `json:"name"`
	NameSource string       `json:"name_source"`
	How        string       `json:"how"`
	Via        string       `json:"via"`
	DeviceID   *uuid.UUID   `json:"device_id"`
	DeviceName string       `json:"device_name"`
	IfIndex    *int         `json:"if_index"`
	PortLabel  string       `json:"port_label"`
	VLAN       int          `json:"vlan"`
	SiteID     uuid.UUID    `json:"site_id"`
	FirstSeen  time.Time    `json:"first_seen"`
	LastSeen   time.Time    `json:"last_seen"`
	Behind     *BehindGroup `json:"behind,omitempty"`
}

// APGroup is an access point on one of a switch's ports, with its wireless
// clients. APDeviceID is set when Sentinel monitors the AP.
type APGroup struct {
	IfIndex    int        `json:"if_index"`
	PortLabel  string     `json:"port_label"`
	Name       string     `json:"name"`
	MAC        string     `json:"mac"`
	APDeviceID *uuid.UUID `json:"ap_device_id"`
	Clients    []Endpoint `json:"clients"`
}

// ConnectedView is what a switch's (or router's) Connected section shows:
// the endpoints on its edge ports (and, for a router, those only its ARP
// table knows), its APs with their clients, and its "behind" groups. Link
// ports are left out.
type ConnectedView struct {
	Endpoints []Endpoint    `json:"endpoints"`
	APs       []APGroup     `json:"aps"`
	Behind    []BehindGroup `json:"behind"`
}

// SightingView is one place a MAC was learned, for its history.
type SightingView struct {
	DeviceID   uuid.UUID `json:"device_id"`
	DeviceName string    `json:"device_name"`
	IfIndex    int       `json:"if_index"`
	PortLabel  string    `json:"port_label"`
	VLAN       int       `json:"vlan"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

// EndpointService builds endpoint records from the L2 tables, placing each
// MAC when it is read (nothing about placement is stored).
type EndpointService struct {
	db       *gorm.DB
	sites    *SiteService
	resolver Resolver
	dns      *dnsCache
	now      func() time.Time
}

// NewEndpointService builds the service; a nil resolver is the system's.
func NewEndpointService(db *gorm.DB, sites *SiteService, resolver Resolver) *EndpointService {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &EndpointService{db: db, sites: sites, resolver: resolver, dns: newDNSCache(db, resolver), now: time.Now}
}

type epDevice struct {
	ID      uuid.UUID
	SiteID  uuid.UUID
	Name    string
	Host    string
	SysName string
	Type    string
}

type epPort struct {
	DeviceID         uuid.UUID
	IfIndex          int
	Name             string
	Descr            string
	Alias            string
	MAC              string
	Role             string
	NeighborDeviceID *uuid.UUID
}

type epDNS struct {
	IP         string
	Name       string
	ResolvedAt time.Time
}

// world is everything the records are built from, loaded per request.
type world struct {
	visible   map[uuid.UUID]bool // sites
	devices   map[uuid.UUID]epDevice
	ports     map[l2.PortKey]epPort
	sightings []l2Sighting
	neighbors map[l2.PortKey]l2Neighbor
	caps      map[string]l2.Caps // by chassis MAC
	arp       []l2ARP
	dns       map[string]epDNS
	names     map[string]string

	byMAC  map[string]uuid.UUID // monitored devices by interface MAC
	byName map[string]uuid.UUID // and by lower-case name and sysName

	info     map[l2.PortKey]l2.PortInfo
	apNames  map[l2.PortKey]string
	apDevice map[l2.PortKey]uuid.UUID
	records  map[string]*Endpoint // every placed MAC, visible or not
}

func (s *EndpointService) load(ctx context.Context, v Viewer) (*world, error) {
	db := s.db.WithContext(ctx)
	w := &world{visible: map[uuid.UUID]bool{}, devices: map[uuid.UUID]epDevice{}, ports: map[l2.PortKey]epPort{},
		neighbors: map[l2.PortKey]l2Neighbor{}, caps: map[string]l2.Caps{}, dns: map[string]epDNS{}, names: map[string]string{}}
	sites, err := s.sites.List(ctx, v.UserID, v.IsAdmin)
	if err != nil {
		return nil, err
	}
	for _, site := range sites {
		w.visible[site.ID] = true
	}

	var devices []epDevice
	if err := db.Raw(`SELECT id, site_id, name, host, COALESCE(sys_name, '') AS sys_name,
		COALESCE(device_type, device_type_detected) AS type FROM devices`).Scan(&devices).Error; err != nil {
		return nil, fmt.Errorf("loading devices: %w", err)
	}
	for _, d := range devices {
		w.devices[d.ID] = d
	}
	var ports []epPort
	if err := db.Raw(`SELECT device_id, if_index, COALESCE(name, '') AS name, COALESCE(descr, '') AS descr,
		COALESCE(alias, '') AS alias, COALESCE(mac, '') AS mac, role, neighbor_device_id FROM device_interfaces`).Scan(&ports).Error; err != nil {
		return nil, fmt.Errorf("loading interfaces: %w", err)
	}
	for _, p := range ports {
		w.ports[l2.PortKey{Device: p.DeviceID, IfIndex: p.IfIndex}] = p
	}
	if err := db.Find(&w.sightings).Error; err != nil {
		return nil, fmt.Errorf("loading sightings: %w", err)
	}
	var neighbors []l2Neighbor
	if err := db.Order("last_seen").Find(&neighbors).Error; err != nil {
		return nil, fmt.Errorf("loading neighbours: %w", err)
	}
	for _, n := range neighbors {
		w.caps[n.ChassisID] = l2.Caps(n.Caps)
		if n.IfIndex > 0 {
			w.neighbors[l2.PortKey{Device: n.DeviceID, IfIndex: n.IfIndex}] = n // the latest wins
		}
	}
	if err := db.Order("last_seen DESC").Find(&w.arp).Error; err != nil {
		return nil, fmt.Errorf("loading ARP: %w", err)
	}
	var dns []epDNS
	if err := db.Raw(`SELECT ip, name, resolved_at FROM l2.dns_names`).Scan(&dns).Error; err != nil {
		return nil, fmt.Errorf("loading DNS names: %w", err)
	}
	for _, d := range dns {
		w.dns[d.IP] = d
	}
	var names []struct{ MAC, Name string }
	if err := db.Raw(`SELECT mac, name FROM endpoint_names`).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("loading endpoint names: %w", err)
	}
	for _, n := range names {
		w.names[n.MAC] = n.Name
	}
	w.place()
	return w, nil
}

func (w *world) portLabel(k l2.PortKey) string {
	p, ok := w.ports[k]
	switch {
	case ok && p.Name != "":
		return p.Name
	case ok && p.Descr != "":
		return p.Descr
	default:
		return fmt.Sprintf("port %d", k.IfIndex)
	}
}

// monitored finds the monitored device an LLDP neighbour is, by its chassis
// MAC or its system name.
func (w *world) monitored(n l2Neighbor, self uuid.UUID) (epDevice, bool) {
	if id, ok := w.byMAC[n.ChassisID]; ok && id != self {
		return w.devices[id], true
	}
	if id, ok := w.byName[strings.ToLower(strings.TrimSpace(n.SysName))]; ok && id != self {
		return w.devices[id], true
	}
	return epDevice{}, false
}

// place classes the ports and builds a record for every MAC seen.
func (w *world) place() {
	w.info, w.apNames, w.apDevice = map[l2.PortKey]l2.PortInfo{}, map[l2.PortKey]string{}, map[l2.PortKey]uuid.UUID{}
	w.byMAC, w.byName = map[string]uuid.UUID{}, map[string]uuid.UUID{}
	for _, p := range w.ports {
		if m, ok := l2.NormalizeMAC(p.MAC); ok {
			w.byMAC[m] = p.DeviceID
		}
	}
	for _, d := range w.devices {
		for _, n := range []string{d.Name, d.SysName} {
			if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
				w.byName[n] = d.ID
			}
		}
	}
	keys := map[l2.PortKey]bool{}
	sightings := make([]l2.Sighting, 0, len(w.sightings))
	firstSeen := map[string]time.Time{}
	for _, s := range w.sightings {
		k := l2.PortKey{Device: s.DeviceID, IfIndex: s.IfIndex}
		keys[k] = true
		sightings = append(sightings, l2.Sighting{Port: k, VLAN: s.VLAN, MAC: s.MAC, LastSeen: s.LastSeen})
		if f, ok := firstSeen[s.MAC]; !ok || s.FirstSeen.Before(f) {
			firstSeen[s.MAC] = s.FirstSeen
		}
	}
	for k := range w.neighbors {
		keys[k] = true
	}
	for k := range keys {
		var info l2.PortInfo
		if p, ok := w.ports[k]; ok {
			info.LinkedTo = p.NeighborDeviceID != nil
			info.Uplink = p.Role == models.PortRoleUplink || p.Role == models.PortRoleWAN
		}
		if n, ok := w.neighbors[k]; ok {
			info.Neighbor = &l2.Neighbor{LocalPort: n.IfIndex, ChassisID: n.ChassisID, PortID: n.PortID, PortDescr: n.PortDescr, SysName: n.SysName, Caps: l2.Caps(n.Caps)}
			w.apNames[k] = n.SysName
			if d, ok := w.monitored(n, k.Device); ok {
				switch d.Type {
				case models.DeviceTypeAccessPoint:
					info.MonitoredAP = true
					w.apNames[k], w.apDevice[k] = d.Name, d.ID
				case models.DeviceTypeSwitch, models.DeviceTypeRouter:
					info.MonitoredNeighbor = true
				}
			}
		}
		w.info[k] = info
	}

	w.records = map[string]*Endpoint{}
	behind := map[l2.PortKey]int{}
	for mac, p := range l2.Place(sightings, w.info, w.apNames) {
		d, ok := w.devices[p.Port.Device]
		if !ok {
			continue
		}
		id, ix := d.ID, p.Port.IfIndex
		w.records[mac] = &Endpoint{MAC: mac, How: p.How, Via: p.Via, DeviceID: &id, DeviceName: d.Name, IfIndex: &ix,
			PortLabel: w.portLabel(*p.Port), VLAN: p.VLAN, SiteID: d.SiteID, FirstSeen: firstSeen[mac], LastSeen: p.LastSeen}
		if p.How == "behind" {
			behind[*p.Port]++
		}
	}
	for _, e := range w.records {
		if e.How == "behind" {
			k := l2.PortKey{Device: *e.DeviceID, IfIndex: *e.IfIndex}
			e.Behind = &BehindGroup{DeviceID: k.Device, IfIndex: k.IfIndex, PortLabel: e.PortLabel, Count: behind[k]}
		}
	}
	// On no switch, only in an ARP table: seen by that router, port
	// unknown. Rows are newest first, so the first router wins.
	for _, a := range w.arp {
		if _, ok := w.records[a.MAC]; ok {
			continue
		}
		d, ok := w.devices[a.DeviceID]
		if !ok {
			continue
		}
		id := d.ID
		w.records[a.MAC] = &Endpoint{MAC: a.MAC, How: "router", DeviceID: &id, DeviceName: d.Name, SiteID: d.SiteID, FirstSeen: a.LastSeen, LastSeen: a.LastSeen}
	}
}

// finish fills in what depends on the viewer (IPs from routers they can
// see) and the names, makers and kinds.
func (w *world) finish(e Endpoint) Endpoint {
	e.IPs = []string{}
	seen := map[string]bool{}
	for _, a := range w.arp {
		if a.MAC == e.MAC && !seen[a.IP] && w.visible[w.devices[a.DeviceID].SiteID] {
			seen[a.IP] = true
			e.IPs = append(e.IPs, a.IP)
		}
	}
	e.Maker = l2.Maker(e.MAC)
	e.Kind = l2.GuessKind(e.Maker, w.caps[e.MAC])
	e.Name, e.NameSource = "", ""
	if n := w.names[e.MAC]; n != "" {
		e.Name, e.NameSource = n, "user"
	} else {
		for _, ip := range e.IPs {
			if d := w.dns[ip]; d.Name != "" {
				e.Name, e.NameSource = d.Name, "dns"
				break
			}
		}
	}
	return e
}

// visibleRecords is every record on a site the viewer can see.
func (w *world) visibleRecords() []Endpoint {
	out := make([]Endpoint, 0, len(w.records))
	for _, e := range w.records {
		if w.visible[e.SiteID] {
			out = append(out, w.finish(*e))
		}
	}
	return out
}

// resolveNames queues reverse lookups for the IPs with no fresh name.
func (s *EndpointService) resolveNames(w *world, eps []Endpoint) {
	cutoff := s.now().Add(-dnsCacheTTL)
	var ips []string
	for _, e := range eps {
		for _, ip := range e.IPs {
			if d, ok := w.dns[ip]; !ok || d.ResolvedAt.Before(cutoff) {
				ips = append(ips, ip)
			}
		}
	}
	if len(ips) > 0 {
		s.dns.want(ips)
	}
}

func looksLikeHostname(q string) bool {
	if len(q) < 3 || net.ParseIP(q) != nil {
		return false
	}
	letter := false
	for _, r := range q {
		switch {
		case r >= 'a' && r <= 'z':
			letter = true
		case (r >= '0' && r <= '9') || r == '-' || r == '.':
		default:
			return false
		}
	}
	return letter
}

// matchRank is 0 for an exact match, 1 for a partial one, -1 for none.
func matchRank(e Endpoint, q, mac, prefix string) int {
	if (mac != "" && e.MAC == mac) || strings.ToLower(e.Name) == q {
		return 0
	}
	for _, ip := range e.IPs {
		if ip == q {
			return 0
		}
	}
	if (prefix != "" && strings.HasPrefix(e.MAC, prefix)) || strings.Contains(e.MAC, q) ||
		strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(strings.ToLower(e.Maker), q) {
		return 1
	}
	for _, ip := range e.IPs {
		if strings.HasPrefix(ip, q) {
			return 1
		}
	}
	return -1
}

// Search finds up to 50 records by name, DNS name, IP, MAC (any notation,
// or its start) or maker. When nothing matches a hostname-like query, its
// forward DNS answer is searched by IP.
func (s *EndpointService) Search(ctx context.Context, v Viewer, q string) ([]Endpoint, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return []Endpoint{}, nil
	}
	w, err := s.load(ctx, v)
	if err != nil {
		return nil, err
	}
	all := w.visibleRecords()
	mac, _ := l2.NormalizeMAC(q)
	prefix := l2.MACPrefixForSearch(q)
	type hit struct {
		e    Endpoint
		rank int
	}
	var hits []hit
	for _, e := range all {
		// A user's name replaces the DNS name on the record, but both
		// are searched.
		r := matchRank(e, q, mac, prefix)
		if r < 0 && e.NameSource == "user" {
			for _, ip := range e.IPs {
				if n := strings.ToLower(w.dns[ip].Name); n != "" && strings.Contains(n, q) {
					r = 1
				}
			}
		}
		if r >= 0 {
			hits = append(hits, hit{e, r})
		}
	}
	if len(hits) == 0 && looksLikeHostname(q) {
		lctx, cancel := context.WithTimeout(ctx, dnsTimeout)
		addrs, _ := s.resolver.LookupHost(lctx, q)
		cancel()
		want := map[string]bool{}
		for _, a := range addrs {
			if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
				want[ip.String()] = true
			}
		}
		for _, e := range all {
			for _, ip := range e.IPs {
				if want[ip] {
					hits = append(hits, hit{e, 1})
					break
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		if !hits[i].e.LastSeen.Equal(hits[j].e.LastSeen) {
			return hits[i].e.LastSeen.After(hits[j].e.LastSeen)
		}
		return hits[i].e.MAC < hits[j].e.MAC
	})
	if len(hits) > endpointSearchLimit {
		hits = hits[:endpointSearchLimit]
	}
	out := make([]Endpoint, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	s.resolveNames(w, out)
	return out, nil
}

func sortEndpoints(eps []Endpoint) {
	sort.Slice(eps, func(i, j int) bool {
		a, b := eps[i], eps[j]
		ai, bi := -1, -1
		if a.IfIndex != nil {
			ai = *a.IfIndex
		}
		if b.IfIndex != nil {
			bi = *b.IfIndex
		}
		if ai != bi {
			return ai < bi
		}
		return a.MAC < b.MAC
	})
}

// ForDevice is a switch's (or router's) connected view. A device the
// viewer cannot see is ErrDeviceNotFound.
func (s *EndpointService) ForDevice(ctx context.Context, v Viewer, deviceID uuid.UUID) (*ConnectedView, error) {
	w, err := s.load(ctx, v)
	if err != nil {
		return nil, err
	}
	d, ok := w.devices[deviceID]
	if !ok || !w.visible[d.SiteID] {
		return nil, ErrDeviceNotFound
	}
	view := &ConnectedView{Endpoints: []Endpoint{}, APs: []APGroup{}, Behind: []BehindGroup{}}
	aps := map[int]*APGroup{}
	for k, info := range w.info {
		if k.Device != deviceID || !(info.MonitoredAP || (info.Neighbor != nil && info.Neighbor.Caps.AP())) {
			continue
		}
		g := &APGroup{IfIndex: k.IfIndex, PortLabel: w.portLabel(k), Name: w.apNames[k], Clients: []Endpoint{}}
		if info.Neighbor != nil {
			g.MAC = info.Neighbor.ChassisID
		}
		if id, ok := w.apDevice[k]; ok {
			g.APDeviceID = &id
		}
		aps[k.IfIndex] = g
	}
	behind := map[int]*BehindGroup{}
	for _, e := range w.records {
		if e.DeviceID == nil || *e.DeviceID != deviceID {
			continue
		}
		rec := w.finish(*e)
		switch {
		case e.How == "router":
			view.Endpoints = append(view.Endpoints, rec)
		case e.How == "wireless" && aps[*e.IfIndex] != nil:
			aps[*e.IfIndex].Clients = append(aps[*e.IfIndex].Clients, rec)
		case e.How == "behind" && w.info[l2.PortKey{Device: deviceID, IfIndex: *e.IfIndex}] == (l2.PortInfo{}):
			// Only a port classed by the MAC count alone (nothing known on
			// its far end) is a group on the map; MACs behind a real link
			// belong to the switch beyond it.
			g := behind[*e.IfIndex]
			if g == nil {
				g = &BehindGroup{DeviceID: deviceID, IfIndex: *e.IfIndex, PortLabel: e.PortLabel, Count: e.Behind.Count}
				behind[*e.IfIndex] = g
			}
			g.Endpoints = append(g.Endpoints, rec)
		case e.How == "wired" && aps[*e.IfIndex] == nil:
			// (A wired record on an AP port is the AP itself.)
			view.Endpoints = append(view.Endpoints, rec)
		}
	}
	sortEndpoints(view.Endpoints)
	for _, g := range aps {
		sortEndpoints(g.Clients)
		view.APs = append(view.APs, *g)
	}
	sort.Slice(view.APs, func(i, j int) bool { return view.APs[i].IfIndex < view.APs[j].IfIndex })
	for _, g := range behind {
		sortEndpoints(g.Endpoints)
		view.Behind = append(view.Behind, *g)
	}
	sort.Slice(view.Behind, func(i, j int) bool { return view.Behind[i].IfIndex < view.Behind[j].IfIndex })

	var all []Endpoint
	all = append(all, view.Endpoints...)
	for _, g := range view.APs {
		all = append(all, g.Clients...)
	}
	for _, g := range view.Behind {
		all = append(all, g.Endpoints...)
	}
	s.resolveNames(w, all)
	return view, nil
}

// record is the viewer's view of one MAC, or ErrEndpointNotFound.
func (s *EndpointService) record(ctx context.Context, v Viewer, mac string) (*world, Endpoint, error) {
	norm, ok := l2.NormalizeMAC(mac)
	if !ok {
		return nil, Endpoint{}, ErrEndpointNotFound
	}
	w, err := s.load(ctx, v)
	if err != nil {
		return nil, Endpoint{}, err
	}
	e, ok := w.records[norm]
	if !ok || !w.visible[e.SiteID] {
		return nil, Endpoint{}, ErrEndpointNotFound
	}
	return w, w.finish(*e), nil
}

// Get is one MAC's record and the last 20 places it was learned, on
// devices the viewer can see.
func (s *EndpointService) Get(ctx context.Context, v Viewer, mac string) (*Endpoint, []SightingView, error) {
	w, e, err := s.record(ctx, v, mac)
	if err != nil {
		return nil, nil, err
	}
	history := []SightingView{}
	for _, row := range w.sightings {
		d, ok := w.devices[row.DeviceID]
		if row.MAC != e.MAC || !ok || !w.visible[d.SiteID] {
			continue
		}
		history = append(history, SightingView{DeviceID: d.ID, DeviceName: d.Name, IfIndex: row.IfIndex,
			PortLabel: w.portLabel(l2.PortKey{Device: d.ID, IfIndex: row.IfIndex}), VLAN: row.VLAN, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].LastSeen.After(history[j].LastSeen) })
	if len(history) > endpointSightingsMax {
		history = history[:endpointSightingsMax]
	}
	s.resolveNames(w, []Endpoint{e})
	return &e, history, nil
}

// SetName sets (or, when empty, clears) the user's name for a MAC. It
// needs editable access to the site the record is placed on.
func (s *EndpointService) SetName(ctx context.Context, v Viewer, mac, name string) (before, after string, err error) {
	w, e, err := s.record(ctx, v, mac)
	if err != nil {
		return "", "", err
	}
	access, err := s.sites.SiteAccess(ctx, v.UserID, v.IsAdmin, e.SiteID)
	if err != nil {
		return "", "", err
	}
	if access < SiteAccessEditable {
		return "", "", ErrEndpointForbidden
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > endpointNameMax {
		return "", "", ErrEndpointNameTooLong
	}
	before = w.names[e.MAC]
	db := s.db.WithContext(ctx)
	if name == "" {
		err = db.Exec(`DELETE FROM endpoint_names WHERE mac = ?`, e.MAC).Error
	} else {
		err = db.Exec(`INSERT INTO endpoint_names (mac, name, updated_by, updated_at) VALUES (?, ?, ?, now())
			ON CONFLICT (mac) DO UPDATE SET name = EXCLUDED.name, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			e.MAC, name, v.UserID).Error
	}
	if err != nil {
		return "", "", fmt.Errorf("saving the name: %w", err)
	}
	return before, name, nil
}

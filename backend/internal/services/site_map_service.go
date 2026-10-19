package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
)

var (
	ErrSiteMapNotFound  = errors.New("site map not found")
	ErrSiteMapForbidden = errors.New("you can't edit this site's map")
	// ErrSiteMapInvalid is every edit that doesn't fit the map: a device
	// that isn't on it, a port that doesn't exist, a link that isn't hand-drawn.
	ErrSiteMapInvalid = errors.New("that doesn't fit this site's map")
)

// MapEnd is one end of a link as the map shows it. A nil IfIndex is the
// device itself, port unknown.
type MapEnd struct {
	DeviceID uuid.UUID `json:"device_id"`
	IfIndex  *int      `json:"if_index"`
	Label    string    `json:"label"`
	Name     string    `json:"name"`
}

// MapDevice is a router or switch on the map. Host, StatusDetail and
// Connected are left out of a public map.
type MapDevice struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Model         string     `json:"model"`
	Host          string     `json:"host,omitempty"`
	Status        string     `json:"status"`
	StatusDetail  string     `json:"status_detail,omitempty"`
	DownSince     *time.Time `json:"down_since"`
	UptimeSeconds *int64     `json:"uptime_seconds"`
	X             *float64   `json:"x"`
	Y             *float64   `json:"y"`
	Added         bool       `json:"added"`
	Connected     *int       `json:"connected,omitempty"`
}

// MapLink is one cable. RateEnd says which end's port the traffic figures
// come from ("a", "b", or "" when neither port is known); SpeedBps and
// ThresholdPct are that port's.
type MapLink struct {
	Key          string     `json:"key"`
	A            MapEnd     `json:"a"`
	B            MapEnd     `json:"b"`
	Sources      []string   `json:"sources"`
	Guess        bool       `json:"guess"`
	Stale        bool       `json:"stale"`
	LastSeen     *time.Time `json:"last_seen"`
	ManualID     *uuid.UUID `json:"manual_id"`
	SpeedBps     int64      `json:"speed_bps"`
	ThresholdPct int        `json:"threshold_pct"`
	RateEnd      string     `json:"rate_end"`
}

// MapSnapshot is a site's whole map for one viewer.
type MapSnapshot struct {
	SiteID        uuid.UUID   `json:"site_id"`
	SiteName      string      `json:"site_name"`
	CanEdit       bool        `json:"can_edit"`
	IsAdmin       bool        `json:"is_admin"`
	Devices       []MapDevice `json:"devices"`
	Links         []MapLink   `json:"links"`
	HiddenDevices []MapDevice `json:"hidden_devices"`
	HiddenLinks   []MapLink   `json:"hidden_links"`
	// Candidates are the site's devices of other types an editor may add.
	Candidates  []MapDevice `json:"candidates"`
	GeneratedAt time.Time   `json:"generated_at"`
}

// BoundPort is a port at a link's rate end: what the fast poll reads.
type BoundPort struct {
	DeviceID    uuid.UUID
	IfIndex     int
	InterfaceID uuid.UUID
	LinkKey     string
}

// PositionInput is one device's new place on the map.
type PositionInput struct {
	DeviceID uuid.UUID `json:"device_id"`
	X        float64   `json:"x"`
	Y        float64   `json:"y"`
}

type mapPlacements interface {
	Placements(ctx context.Context, macs []string) (map[string]l2.Placement, error)
}

// SiteMapService builds site maps and saves their edits.
type SiteMapService struct {
	db        *gorm.DB
	sites     *SiteService
	endpoints mapPlacements
	settings  ThresholdSource
	now       func() time.Time
	onChange  []func(uuid.UUID)

	// Piece B's placements cover every site and are costly to work out, so
	// one copy serves every map for placementsTTL.
	placedMu sync.Mutex
	placedAt time.Time
	placed   map[string]l2.Placement
}

// placementsTTL is how long one reading of piece B's placements is reused.
const placementsTTL = 10 * time.Second

func (s *SiteMapService) placements(ctx context.Context) (map[string]l2.Placement, error) {
	s.placedMu.Lock()
	defer s.placedMu.Unlock()
	if s.placed != nil && s.now().Sub(s.placedAt) < placementsTTL {
		return s.placed, nil
	}
	p, err := s.endpoints.Placements(ctx, nil)
	if err != nil {
		return nil, err
	}
	s.placed, s.placedAt = p, s.now()
	return p, nil
}

func NewSiteMapService(db *gorm.DB, sites *SiteService, endpoints mapPlacements, settings ThresholdSource) *SiteMapService {
	return &SiteMapService{db: db, sites: sites, endpoints: endpoints, settings: settings, now: time.Now}
}

// OnChange registers f to be called with the site after every edit.
func (s *SiteMapService) OnChange(f func(siteID uuid.UUID)) { s.onChange = append(s.onChange, f) }

func (s *SiteMapService) changed(siteID uuid.UUID) {
	for _, f := range s.onChange {
		f(siteID)
	}
}

type mapDeviceRow struct {
	ID            uuid.UUID
	Name          string
	Type          string
	Model         string
	Host          string
	SysName       string
	SysDescr      string
	Status        string
	StatusDetail  string
	LastSeenAt    *time.Time
	UptimeSeconds *int64
}

type mapPortRow struct {
	ID               uuid.UUID
	DeviceID         uuid.UUID
	IfIndex          int
	Name             string
	Descr            string
	Alias            string
	MAC              string
	IfType           int
	ConnectorPresent *bool
	SpeedBps         int64
	UtilThresholdPct *int
	NeighborDeviceID *uuid.UUID
	NeighborIfIndex  *int
}

// mapWorld is everything one site's map is built from.
type mapWorld struct {
	site      models.Site
	devices   map[uuid.UUID]mapDeviceRow // every device at the site
	onMap     map[uuid.UUID]bool         // routers, switches and added devices
	positions map[uuid.UUID]models.SiteMapPosition
	ports     map[sitemap.End]mapPortRow
	links     []sitemap.Link
	placed    map[string]l2.Placement // every MAC piece B placed
	ownMACs   map[string]bool         // the map devices' own interface MACs
}

func onMapType(t string) bool { return t == models.DeviceTypeRouter || t == models.DeviceTypeSwitch }

func (s *SiteMapService) load(ctx context.Context, siteID uuid.UUID) (*mapWorld, error) {
	return s.loadWith(ctx, siteID, true)
}

// loadWith reads a site's map; without links it reads only the devices and
// their places (enough to say which devices are drawn).
func (s *SiteMapService) loadWith(ctx context.Context, siteID uuid.UUID, links bool) (*mapWorld, error) {
	db := s.db.WithContext(ctx)
	site, err := s.sites.Get(ctx, siteID)
	if errors.Is(err, ErrSiteNotFound) {
		return nil, ErrSiteMapNotFound
	}
	if err != nil {
		return nil, err
	}
	w := &mapWorld{site: *site, devices: map[uuid.UUID]mapDeviceRow{}, onMap: map[uuid.UUID]bool{},
		positions: map[uuid.UUID]models.SiteMapPosition{}, ports: map[sitemap.End]mapPortRow{}, ownMACs: map[string]bool{}}

	var devices []mapDeviceRow
	if err := db.Raw(`SELECT id, name, COALESCE(device_type, device_type_detected) AS type,
		COALESCE(NULLIF(model_override, ''), model, '') AS model, host, COALESCE(sys_name, '') AS sys_name,
		COALESCE(sys_descr, '') AS sys_descr, status, COALESCE(status_detail, '') AS status_detail,
		last_seen_at, sys_uptime_seconds AS uptime_seconds
		FROM devices WHERE site_id = ? ORDER BY name`, siteID).Scan(&devices).Error; err != nil {
		return nil, fmt.Errorf("loading the site's devices: %w", err)
	}
	var positions []models.SiteMapPosition
	if err := db.Where("site_id = ?", siteID).Find(&positions).Error; err != nil {
		return nil, fmt.Errorf("loading map positions: %w", err)
	}
	for _, p := range positions {
		w.positions[p.DeviceID] = p
	}
	var ids []uuid.UUID
	for _, d := range devices {
		w.devices[d.ID] = d
		if onMapType(d.Type) || w.positions[d.ID].Added {
			w.onMap[d.ID] = true
			ids = append(ids, d.ID)
		}
	}
	in := sitemap.Input{Now: s.now()}
	for _, id := range ids {
		d := w.devices[id]
		in.Devices = append(in.Devices, sitemap.Device{ID: id, Name: d.Name, SysName: d.SysName, SysDescr: d.SysDescr})
	}
	if len(ids) == 0 || !links {
		return w, nil
	}

	var ports []mapPortRow
	if err := db.Raw(`SELECT id, device_id, if_index, COALESCE(name, '') AS name, COALESCE(descr, '') AS descr,
		COALESCE(alias, '') AS alias, COALESCE(mac, '') AS mac, COALESCE(if_type, 0) AS if_type, connector_present,
		COALESCE(speed_bps, 0) AS speed_bps, util_threshold_pct, neighbor_device_id, neighbor_if_index
		FROM device_interfaces WHERE device_id IN ? AND present`, ids).Scan(&ports).Error; err != nil {
		return nil, fmt.Errorf("loading map ports: %w", err)
	}
	var macs []string
	owner := map[string]uuid.UUID{}
	for _, p := range ports {
		w.ports[sitemap.End{DeviceID: p.DeviceID, IfIndex: p.IfIndex}] = p
		if mac, ok := l2.NormalizeMAC(p.MAC); ok {
			w.ownMACs[mac] = true
		}
		in.Ports = append(in.Ports, sitemap.Port{DeviceID: p.DeviceID, IfIndex: p.IfIndex, Name: p.Name, Descr: p.Descr,
			Alias: p.Alias, MAC: p.MAC, IfType: p.IfType, ConnectorPresent: p.ConnectorPresent})
		if p.NeighborDeviceID != nil {
			to := sitemap.End{DeviceID: *p.NeighborDeviceID}
			if p.NeighborIfIndex != nil {
				to.IfIndex = *p.NeighborIfIndex
			}
			in.PortLinks = append(in.PortLinks, sitemap.PortLink{From: sitemap.End{DeviceID: p.DeviceID, IfIndex: p.IfIndex}, To: to})
		}
		if mac, ok := l2.NormalizeMAC(p.MAC); ok && portmon.IsPhysical(portmon.IfInfo{Name: p.Name, Descr: p.Descr, Type: p.IfType, ConnectorPresent: p.ConnectorPresent}) {
			if _, seen := owner[mac]; !seen {
				owner[mac] = p.DeviceID
				macs = append(macs, mac)
			}
		}
	}

	var neighbors []struct {
		DeviceID                              uuid.UUID
		IfIndex                               int
		ChassisID, SysName, PortID, PortDescr string
		LastSeen                              time.Time
	}
	if err := db.Raw(`SELECT device_id, if_index, chassis_id, sys_name, port_id, port_descr, last_seen
		FROM l2.neighbors WHERE device_id IN ?`, ids).Scan(&neighbors).Error; err != nil {
		return nil, fmt.Errorf("loading LLDP neighbours: %w", err)
	}
	for _, n := range neighbors {
		in.Neighbors = append(in.Neighbors, sitemap.Neighbor{DeviceID: n.DeviceID, IfIndex: n.IfIndex, ChassisID: n.ChassisID,
			SysName: n.SysName, PortID: n.PortID, PortDescr: n.PortDescr, LastSeen: n.LastSeen})
	}

	// Every placement at once: the guesses, and each switch's count of the
	// end devices plugged into it.
	w.placed, err = s.placements(ctx)
	if err != nil {
		return nil, fmt.Errorf("placing MACs: %w", err)
	}
	best := map[uuid.UUID]sitemap.Guess{}
	for _, mac := range macs {
		p, ok := w.placed[mac]
		x := owner[mac]
		if !ok || p.Port == nil || p.Port.Device == x || !w.onMap[p.Port.Device] || (p.How != "wired" && p.How != "behind") {
			continue
		}
		if g, ok := best[x]; !ok || p.LastSeen.After(g.LastSeen) {
			best[x] = sitemap.Guess{Device: x, At: sitemap.End{DeviceID: p.Port.Device, IfIndex: p.Port.IfIndex}, LastSeen: p.LastSeen}
		}
	}
	for _, g := range best {
		in.Guesses = append(in.Guesses, g)
	}
	sort.Slice(in.Guesses, func(i, j int) bool { return in.Guesses[i].Device.String() < in.Guesses[j].Device.String() })

	var rows []models.SiteMapLink
	if err := db.Where("site_id = ?", siteID).Order("created_at").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading map links: %w", err)
	}
	for _, r := range rows {
		in.Overrides = append(in.Overrides, sitemap.Override{ID: r.ID, Kind: r.Kind, A: endOf(r.ADeviceID, r.AIfIndex), B: endOf(r.BDeviceID, r.BIfIndex)})
	}
	w.links = sitemap.Find(in)
	return w, nil
}

func endOf(id uuid.UUID, ix *int) sitemap.End {
	e := sitemap.End{DeviceID: id}
	if ix != nil {
		e.IfIndex = *ix
	}
	return e
}

func (w *mapWorld) end(e sitemap.End) MapEnd {
	out := MapEnd{DeviceID: e.DeviceID}
	if e.IfIndex == 0 {
		return out
	}
	ix := e.IfIndex
	out.IfIndex = &ix
	p := w.ports[e]
	out.Name = p.Name
	out.Label = portmon.NumberingFor(w.devices[e.DeviceID].SysDescr).Label(p.Name, p.Descr, ix)
	return out
}

// rateEnd picks the end whose port the traffic figures come from: one with
// a port, a switch before a router, then one with a known speed.
func (w *mapWorld) rateEnd(l sitemap.Link) string {
	score := func(e sitemap.End) int {
		if e.IfIndex == 0 {
			return -1
		}
		n := 0
		if w.devices[e.DeviceID].Type == models.DeviceTypeSwitch {
			n += 2
		}
		if w.ports[e].SpeedBps > 0 {
			n++
		}
		return n
	}
	a, b := score(l.A), score(l.B)
	switch {
	case a < 0 && b < 0:
		return ""
	case b > a:
		return "b"
	default:
		return "a"
	}
}

func (w *mapWorld) mapLink(l sitemap.Link, defaultPct int) MapLink {
	out := MapLink{Key: l.Key, A: w.end(l.A), B: w.end(l.B), Guess: l.Guess, Stale: l.Stale, LastSeen: l.LastSeen, ManualID: l.ManualID,
		RateEnd: w.rateEnd(l), ThresholdPct: defaultPct}
	for _, src := range l.Sources {
		out.Sources = append(out.Sources, string(src))
	}
	var e sitemap.End
	switch out.RateEnd {
	case "a":
		e = l.A
	case "b":
		e = l.B
	default:
		return out
	}
	p := w.ports[e]
	out.SpeedBps = p.SpeedBps
	if p.UtilThresholdPct != nil {
		out.ThresholdPct = *p.UtilThresholdPct
	}
	return out
}

func (w *mapWorld) mapDevice(id uuid.UUID, public bool) MapDevice {
	d := w.devices[id]
	out := MapDevice{ID: id, Name: d.Name, Type: d.Type, Model: d.Model, Status: d.Status, UptimeSeconds: d.UptimeSeconds,
		Added: w.positions[id].Added}
	if d.Status == models.DeviceStatusDown {
		out.DownSince = d.LastSeenAt
	}
	if p, ok := w.positions[id]; ok && p.HasPosition {
		x, y := p.X, p.Y
		out.X, out.Y = &x, &y
	}
	if !public {
		out.Host, out.StatusDetail = d.Host, d.StatusDetail
		if onMapType(d.Type) {
			n := 0
			for mac, p := range w.placed {
				if p.Port != nil && p.Port.Device == id && p.How != "router" && !w.isMapMAC(mac) {
					n++
				}
			}
			out.Connected = &n
		}
	}
	return out
}

// isMapMAC reports a MAC belonging to one of the map's own devices.
func (w *mapWorld) isMapMAC(mac string) bool { return w.ownMACs[mac] }

func (s *SiteMapService) build(ctx context.Context, w *mapWorld, level SiteAccessLevel, public bool) *MapSnapshot {
	out := &MapSnapshot{SiteID: w.site.ID, SiteName: w.site.Name, CanEdit: !public && level >= SiteAccessEditable,
		IsAdmin: !public && level >= SiteAccessAdmin, Devices: []MapDevice{}, Links: []MapLink{}, HiddenDevices: []MapDevice{},
		HiddenLinks: []MapLink{}, Candidates: []MapDevice{}, GeneratedAt: s.now().UTC()}
	pct := int(s.settings.PortThresholds(ctx).UtilPct)
	visible := map[uuid.UUID]bool{}
	for id, d := range w.devices {
		switch {
		case w.onMap[id] && !w.positions[id].Hidden:
			visible[id] = true
			out.Devices = append(out.Devices, w.mapDevice(id, public))
		case w.onMap[id]:
			if out.CanEdit {
				out.HiddenDevices = append(out.HiddenDevices, w.mapDevice(id, public))
			}
		case out.CanEdit && !onMapType(d.Type):
			out.Candidates = append(out.Candidates, w.mapDevice(id, public))
		}
	}
	for _, l := range w.links {
		if !visible[l.A.DeviceID] || !visible[l.B.DeviceID] {
			continue
		}
		switch {
		case !l.Hidden:
			out.Links = append(out.Links, w.mapLink(l, pct))
		case out.CanEdit:
			out.HiddenLinks = append(out.HiddenLinks, w.mapLink(l, pct))
		}
	}
	byName := func(ds []MapDevice) {
		sort.Slice(ds, func(i, j int) bool { return strings.ToLower(ds[i].Name) < strings.ToLower(ds[j].Name) })
	}
	byName(out.Devices)
	byName(out.HiddenDevices)
	byName(out.Candidates)
	return out
}

// access is the viewer's level on the site, ErrSiteMapNotFound for none.
func (s *SiteMapService) access(ctx context.Context, v Viewer, siteID uuid.UUID) (SiteAccessLevel, error) {
	level, err := s.sites.SiteAccess(ctx, v.UserID, v.IsAdmin, siteID)
	if errors.Is(err, ErrSiteNotFound) || (err == nil && level == SiteAccessNone) {
		return SiteAccessNone, ErrSiteMapNotFound
	}
	return level, err
}

// Snapshot is the site's map as v may see it.
func (s *SiteMapService) Snapshot(ctx context.Context, v Viewer, siteID uuid.UUID) (*MapSnapshot, error) {
	level, err := s.access(ctx, v, siteID)
	if err != nil {
		return nil, err
	}
	w, err := s.load(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return s.build(ctx, w, level, false), nil
}

// PublicSnapshot is the map a public link shows: names, statuses and links,
// with no addresses, status details or end-device counts, nothing to edit,
// and stand-in ids in place of the devices' and site's own.
func (s *SiteMapService) PublicSnapshot(ctx context.Context, siteID uuid.UUID) (*MapSnapshot, error) {
	w, err := s.load(ctx, siteID)
	if err != nil {
		return nil, err
	}
	m := s.build(ctx, w, SiteAccessReadonly, true)
	aliasSnapshot(m)
	return m, nil
}

// BoundPorts is the rate-end port of each visible link (what the fast poll
// reads while the map is watched) and every device drawn on the map (whose
// statuses each tick carries).
func (s *SiteMapService) BoundPorts(ctx context.Context, siteID uuid.UUID) ([]BoundPort, []uuid.UUID, error) {
	w, err := s.load(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	snap := s.build(ctx, w, SiteAccessReadonly, true)
	devices := make([]uuid.UUID, 0, len(snap.Devices))
	for _, d := range snap.Devices {
		devices = append(devices, d.ID)
	}
	var out []BoundPort
	for _, l := range snap.Links {
		e := l.A
		if l.RateEnd == "b" {
			e = l.B
		}
		if l.RateEnd == "" || e.IfIndex == nil {
			continue
		}
		port := w.ports[sitemap.End{DeviceID: e.DeviceID, IfIndex: *e.IfIndex}]
		out = append(out, BoundPort{DeviceID: e.DeviceID, IfIndex: *e.IfIndex, InterfaceID: port.ID, LinkKey: l.Key})
	}
	return out, devices, nil
}

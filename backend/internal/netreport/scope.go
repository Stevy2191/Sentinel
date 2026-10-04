package netreport

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// port is one port of a resolved scope: a line of its own in every port
// metric's table.
type port struct {
	ID       uuid.UUID
	DeviceID uuid.UUID
	// Name is "device · port (alias)".
	Name string
	// SpeedBps is the link speed, 0 when unknown (no busy % is recorded then).
	SpeedBps int64
}

// device is one device of a resolved scope.
type device struct {
	ID     uuid.UUID
	SiteID uuid.UUID
	Name   string
	// Physical lists its present physical ports. A device or site total of a
	// port metric adds up these only (phase 4's rule), so a VLAN interface or
	// a port-channel never counts the same traffic twice.
	Physical []uuid.UUID
}

// site is one site total of a sites scope.
type site struct {
	ID      uuid.UUID
	Name    string
	Devices []uuid.UUID
}

// resolved is a network scope as one user may see it now.
type resolved struct {
	scopeType string
	// ports: the ports scope's ports, port_roles' matches, a sites scope's
	// collected ports.
	ports []port
	// devices: the devices scope's devices; a sites scope's devices (their
	// instances are the lines of device metrics, their physical ports make
	// the site totals).
	devices []device
	// sites: a sites scope's sites, one total line each.
	sites []site
	// siteNames are the chosen sites the user may see, in the order chosen.
	siteNames []string
	// unavailable counts chosen ids that are hidden from the user or gone.
	unavailable int
}

// deviceIDs lists every device the scope reads series from, once each.
func (r *resolved) deviceIDs() []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, p := range r.ports {
		add(p.DeviceID)
	}
	for _, d := range r.devices {
		add(d.ID)
	}
	return out
}

// portIDs lists the scope's ports.
func (r *resolved) portIDs() []uuid.UUID {
	out := make([]uuid.UUID, len(r.ports))
	for i, p := range r.ports {
		out[i] = p.ID
	}
	return out
}

// physicalPorts counts the physical ports of the scope's devices: what the
// device totals of a devices scope add up.
func (r *resolved) physicalPorts() int {
	n := 0
	for _, d := range r.devices {
		n += len(d.Physical)
	}
	return n
}

// empty reports a scope whose every chosen subject is unavailable.
func (r *resolved) empty() bool {
	return r.unavailable > 0 && len(r.ports) == 0 && len(r.devices) == 0 && len(r.siteNames) == 0
}

// gate answers "may the user see this site?" once per site, by the site
// access rules (services.SiteService.SiteAccess, readonly or better). A site
// that no longer exists is not visible.
type gate struct {
	sites   *services.SiteService
	user    uuid.UUID
	isAdmin bool
	seen    map[uuid.UUID]bool
}

// gateFor loads the user's admin flag. A user that no longer exists is an
// error, not an empty report: reports are deleted with their owner.
func (b *Builder) gateFor(ctx context.Context, user uuid.UUID) (*gate, error) {
	var u struct{ IsAdmin bool }
	res := b.db.WithContext(ctx).Raw(`SELECT is_admin FROM users WHERE id = ?`, user).Scan(&u)
	if res.Error != nil {
		return nil, fmt.Errorf("loading the report owner: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("loading the report owner: no such user")
	}
	return &gate{sites: b.sites, user: user, isAdmin: u.IsAdmin, seen: map[uuid.UUID]bool{}}, nil
}

func (g *gate) visible(ctx context.Context, siteID uuid.UUID) (bool, error) {
	if v, ok := g.seen[siteID]; ok {
		return v, nil
	}
	level, err := g.sites.SiteAccess(ctx, g.user, g.isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) {
		g.seen[siteID] = false
		return false, nil
	}
	if err != nil {
		return false, err
	}
	g.seen[siteID] = level >= services.SiteAccessReadonly
	return g.seen[siteID], nil
}

// resolve works out a network scope as user sees it now. Chosen ports,
// devices or sites that are hidden from the user, or that no longer exist,
// are counted in unavailable and otherwise left out. port_roles and sites
// are expanded from the ports and devices there are now, so a role set after
// the report was made is included.
func (b *Builder) resolve(ctx context.Context, user uuid.UUID, scopeType string, scope models.ReportScope) (*resolved, error) {
	g, err := b.gateFor(ctx, user)
	if err != nil {
		return nil, err
	}
	res := &resolved{scopeType: scopeType}
	switch scopeType {
	case models.ScopeTypePorts:
		err = b.resolvePorts(ctx, g, scope.PortIDs, res)
	case models.ScopeTypeDevices:
		err = b.resolveDevices(ctx, g, scope.DeviceIDs, res)
	case models.ScopeTypePortRoles:
		err = b.resolvePortRoles(ctx, g, scope.SiteIDs, scope.Roles, res)
	case models.ScopeTypeSites:
		err = b.resolveSites(ctx, g, scope.SiteIDs, res)
	default:
		err = fmt.Errorf("%q is not a network scope type", scopeType)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ifaceRow is a device_interfaces row with its device, as the scope reads it.
type ifaceRow struct {
	ID               uuid.UUID `gorm:"column:id"`
	DeviceID         uuid.UUID `gorm:"column:device_id"`
	SiteID           uuid.UUID `gorm:"column:site_id"`
	Device           string    `gorm:"column:device"`
	IfIndex          int       `gorm:"column:if_index"`
	Name             string    `gorm:"column:name"`
	Descr            string    `gorm:"column:descr"`
	Alias            string    `gorm:"column:alias"`
	IfType           int       `gorm:"column:if_type"`
	SpeedBps         int64     `gorm:"column:speed_bps"`
	ConnectorPresent *bool     `gorm:"column:connector_present"`
	Present          bool      `gorm:"column:present"`
}

// ifaceSelect reads ifaceRows; the nullable text and number columns come back
// as zero values.
const ifaceSelect = `SELECT di.id, di.device_id, d.site_id, COALESCE(NULLIF(d.name, ''), d.host) AS device,
	di.if_index, COALESCE(di.name, '') AS name, COALESCE(di.descr, '') AS descr,
	COALESCE(di.alias, '') AS alias, COALESCE(di.if_type, 0) AS if_type,
	COALESCE(di.speed_bps, 0) AS speed_bps, di.connector_present, di.present
	FROM device_interfaces di JOIN devices d ON d.id = di.device_id`

// ifaceOrder lists ports by device name, then index.
const ifaceOrder = ` ORDER BY lower(COALESCE(NULLIF(d.name, ''), d.host)), d.id, di.if_index`

// collected keeps the ports the stats poll reads: only they have traffic.
const collected = ` AND di.present AND COALESCE(di.collect, di.collect_default)`

// physical reports a present port a cable plugs into (portmon's rule, as
// PortService.PhysicalInterfaceIDs uses it).
func (r ifaceRow) physical() bool {
	return r.Present && portmon.IsPhysical(portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent})
}

// port names the row "device · port (alias)": the port's name, else its
// description, else its index, with the alias when it says something more.
func (r ifaceRow) port() port {
	name := r.Name
	if name == "" {
		name = r.Descr
	}
	if name == "" {
		name = "ifIndex " + strconv.Itoa(r.IfIndex)
	}
	if r.Alias != "" && r.Alias != name {
		name += " (" + r.Alias + ")"
	}
	return port{ID: r.ID, DeviceID: r.DeviceID, Name: r.Device + " · " + name, SpeedBps: r.SpeedBps}
}

func (b *Builder) resolvePorts(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE di.id IN ?`+ifaceOrder, ids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the chosen ports: %w", err)
	}
	found := map[uuid.UUID]bool{}
	for _, r := range rows {
		ok, err := g.visible(ctx, r.SiteID)
		if err != nil {
			return err
		}
		if ok {
			found[r.ID] = true
			res.ports = append(res.ports, r.port())
		}
	}
	res.unavailable = countMissing(ids, found)
	return nil
}

// deviceRow is a devices row as the scope reads it.
type deviceRow struct {
	ID     uuid.UUID `gorm:"column:id"`
	SiteID uuid.UUID `gorm:"column:site_id"`
	Name   string    `gorm:"column:name"`
}

const deviceSelect = `SELECT id, site_id, COALESCE(NULLIF(name, ''), host) AS name FROM devices`

const deviceOrder = ` ORDER BY lower(COALESCE(NULLIF(name, ''), host)), id`

func (b *Builder) resolveDevices(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	var rows []deviceRow
	if err := b.db.WithContext(ctx).Raw(deviceSelect+` WHERE id IN ?`+deviceOrder, ids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the chosen devices: %w", err)
	}
	found := map[uuid.UUID]bool{}
	var visible []deviceRow
	for _, r := range rows {
		ok, err := g.visible(ctx, r.SiteID)
		if err != nil {
			return err
		}
		if ok {
			found[r.ID] = true
			visible = append(visible, r)
		}
	}
	res.unavailable = countMissing(ids, found)
	devices, err := b.withPhysical(ctx, visible)
	if err != nil {
		return err
	}
	res.devices = devices
	return nil
}

// withPhysical turns device rows into devices with their physical ports.
func (b *Builder) withPhysical(ctx context.Context, rows []deviceRow) ([]device, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var ifs []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE di.device_id IN ? AND di.present`+ifaceOrder, ids).Scan(&ifs).Error; err != nil {
		return nil, fmt.Errorf("loading the devices' ports: %w", err)
	}
	physical := map[uuid.UUID][]uuid.UUID{}
	for _, r := range ifs {
		if r.physical() {
			physical[r.DeviceID] = append(physical[r.DeviceID], r.ID)
		}
	}
	out := make([]device, len(rows))
	for i, r := range rows {
		out[i] = device{ID: r.ID, SiteID: r.SiteID, Name: r.Name, Physical: physical[r.ID]}
	}
	return out, nil
}

// siteRow is a sites row as the scope reads it.
type siteRow struct {
	ID   uuid.UUID `gorm:"column:id"`
	Name string    `gorm:"column:name"`
}

// visibleSites returns the chosen sites the user may see, in the order
// chosen, records their names, and counts the rest as unavailable.
func (b *Builder) visibleSites(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) ([]siteRow, error) {
	var rows []siteRow
	if err := b.db.WithContext(ctx).Raw(`SELECT id, name FROM sites WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading the chosen sites: %w", err)
	}
	byID := make(map[uuid.UUID]siteRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	found := map[uuid.UUID]bool{}
	var out []siteRow
	for _, id := range ids {
		r, ok := byID[id]
		if !ok || found[id] {
			continue
		}
		vis, err := g.visible(ctx, id)
		if err != nil {
			return nil, err
		}
		if vis {
			found[id] = true
			out = append(out, r)
			res.siteNames = append(res.siteNames, r.Name)
		}
	}
	res.unavailable += countMissing(ids, found)
	return out, nil
}

func (b *Builder) resolvePortRoles(ctx context.Context, g *gate, siteIDs []uuid.UUID, roles []string, res *resolved) error {
	sites, err := b.visibleSites(ctx, g, siteIDs, res)
	if err != nil || len(sites) == 0 {
		return err
	}
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE d.site_id IN ? AND di.role IN ?`+collected+ifaceOrder,
		siteIDsOf(sites), roles).Scan(&rows).Error; err != nil {
		return fmt.Errorf("finding the ports with those roles: %w", err)
	}
	for _, r := range rows {
		res.ports = append(res.ports, r.port())
	}
	return nil
}

func (b *Builder) resolveSites(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	sites, err := b.visibleSites(ctx, g, ids, res)
	if err != nil || len(sites) == 0 {
		return err
	}
	visible := siteIDsOf(sites)
	var devs []deviceRow
	if err := b.db.WithContext(ctx).Raw(deviceSelect+` WHERE site_id IN ?`+deviceOrder, visible).Scan(&devs).Error; err != nil {
		return fmt.Errorf("loading the sites' devices: %w", err)
	}
	if res.devices, err = b.withPhysical(ctx, devs); err != nil {
		return err
	}
	for _, s := range sites {
		st := site{ID: s.ID, Name: s.Name}
		for _, d := range res.devices {
			if d.SiteID == s.ID {
				st.Devices = append(st.Devices, d.ID)
			}
		}
		res.sites = append(res.sites, st)
	}
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE d.site_id IN ?`+collected+ifaceOrder, visible).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the sites' ports: %w", err)
	}
	for _, r := range rows {
		res.ports = append(res.ports, r.port())
	}
	return nil
}

func siteIDsOf(rows []siteRow) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// countMissing counts the distinct ids not in found.
func countMissing(ids []uuid.UUID, found map[uuid.UUID]bool) int {
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !found[id] && !seen[id] {
			n++
		}
		seen[id] = true
	}
	return n
}

package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/portroles"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
)

// PortRoleService marks a device's ports as network links or internet (WAN)
// ports from what the L2 poll found (portroles.Decide), leaving every port a
// person set alone. It runs after each L2 poll and when a port goes back to
// automatic.
type PortRoleService struct {
	db     *gorm.DB
	now    func() time.Time
	logger *log.Logger
}

func NewPortRoleService(db *gorm.DB) *PortRoleService {
	return &PortRoleService{db: db, now: time.Now, logger: log.New(os.Stderr, "", log.LstdFlags)}
}

// Apply is ApplyErr for the L2 poll's hook: errors are logged.
func (s *PortRoleService) Apply(ctx context.Context, deviceID uuid.UUID) {
	if err := s.ApplyErr(ctx, deviceID); err != nil {
		s.logger.Printf("[port roles] %s: %v", deviceID, err)
	}
}

type roleDeviceRow struct {
	ID       uuid.UUID
	SiteID   *uuid.UUID
	Name     string
	SysName  string
	SysDescr string
	Host     string
	Type     string
}

type rolePortRow struct {
	DeviceID         uuid.UUID
	IfIndex          int
	Name             string
	Descr            string
	Alias            string
	MAC              string
	IfType           int
	ConnectorPresent *bool
	OperStatus       string
	Role             string
	RoleSource       string
	RoleReason       string
	LagIfIndex       *int
	WANEvidence      string
}

const rolePortColumns = `device_id, if_index, COALESCE(name, '') AS name, COALESCE(descr, '') AS descr, COALESCE(alias, '') AS alias,
	COALESCE(mac, '') AS mac, COALESCE(if_type, 0) AS if_type, connector_present, COALESCE(oper_status, '') AS oper_status,
	role, role_source, role_reason, lag_if_index, wan_evidence`

func (p rolePortRow) sitemapPort() sitemap.Port {
	return sitemap.Port{DeviceID: p.DeviceID, IfIndex: p.IfIndex, Name: p.Name, Descr: p.Descr, Alias: p.Alias, MAC: p.MAC,
		IfType: p.IfType, ConnectorPresent: p.ConnectorPresent}
}

// ApplyErr works out the device's port roles and writes the ones that
// changed, never over a port whose role a person set.
func (s *PortRoleService) ApplyErr(ctx context.Context, deviceID uuid.UUID) error {
	db := s.db.WithContext(ctx)
	const devCols = `id, site_id, name, COALESCE(sys_name, '') AS sys_name, COALESCE(sys_descr, '') AS sys_descr, host,
		COALESCE(device_type, device_type_detected) AS type`
	var dev roleDeviceRow
	if err := db.Raw(`SELECT `+devCols+` FROM devices WHERE id = ?`, deviceID).Scan(&dev).Error; err != nil {
		return fmt.Errorf("loading the device: %w", err)
	}
	if dev.ID == uuid.Nil {
		return nil
	}
	var ports []rolePortRow
	if err := db.Raw(`SELECT `+rolePortColumns+` FROM device_interfaces WHERE device_id = ? AND present`, deviceID).Scan(&ports).Error; err != nil {
		return fmt.Errorf("loading ports: %w", err)
	}
	var neighbors []struct {
		IfIndex                                                  int
		Protocol, ChassisID, SysName, PortID, PortDescr, Address string
		LastSeen                                                 time.Time
	}
	if err := db.Raw(`SELECT if_index, protocol, chassis_id, sys_name, port_id, port_descr, address, last_seen
		FROM l2.neighbors WHERE device_id = ?`, deviceID).Scan(&neighbors).Error; err != nil {
		return fmt.Errorf("loading neighbours: %w", err)
	}

	in := portroles.Input{IsRouter: dev.Type == models.DeviceTypeRouter, LagNames: map[int]string{}, LagPeer: map[int]string{},
		Manual: map[int]bool{}, Now: s.now()}
	if len(neighbors) > 0 {
		peers, err := s.peers(ctx, deviceID, neighbors)
		if err != nil {
			return err
		}
		in.Peers = peers
	}
	peerAt := map[int]string{}
	for _, p := range in.Peers {
		if in.Now.Sub(p.LastSeen) <= portroles.Fresh {
			peerAt[p.IfIndex] = p.DeviceName
		}
	}
	for _, p := range ports {
		if portmon.IsAggregator(portmon.IfInfo{Name: p.Name, Descr: p.Descr, Type: p.IfType}) {
			in.LagNames[p.IfIndex] = p.Name
		}
		lag := 0
		if p.LagIfIndex != nil {
			lag = *p.LagIfIndex
			if name := peerAt[p.IfIndex]; name != "" && in.LagPeer[lag] == "" {
				in.LagPeer[lag] = name
			}
		}
		in.Ports = append(in.Ports, portroles.Port{IfIndex: p.IfIndex, Name: p.Name, Label: p.Name,
			Physical: portmon.IsPhysical(portmon.IfInfo{Name: p.Name, Descr: p.Descr, Type: p.IfType, ConnectorPresent: p.ConnectorPresent}),
			Up:       p.OperStatus == "up", Role: p.Role, RoleSource: p.RoleSource, RoleReason: p.RoleReason, LagIfIndex: lag, WANEvidence: p.WANEvidence})
	}
	if dev.SiteID != nil {
		var ends []int
		if err := db.Raw(`SELECT a_if_index FROM site_map_links WHERE site_id = ? AND kind = 'manual' AND a_device_id = ? AND a_if_index IS NOT NULL
			UNION SELECT b_if_index FROM site_map_links WHERE site_id = ? AND kind = 'manual' AND b_device_id = ? AND b_if_index IS NOT NULL`,
			*dev.SiteID, deviceID, *dev.SiteID, deviceID).Scan(&ends).Error; err != nil {
			return fmt.Errorf("loading map links: %w", err)
		}
		for _, ix := range ends {
			in.Manual[ix] = true
		}
	}

	changes := portroles.Decide(in)
	if len(changes) == 0 {
		return nil
	}
	now := s.now().UTC()
	return db.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := tx.Model(&models.DeviceInterface{}).
				Where("device_id = ? AND if_index = ? AND role_source <> ?", deviceID, c.IfIndex, models.PortRoleSourceUser).
				Updates(map[string]any{"role": c.Role, "role_source": c.Source, "role_reason": c.Reason, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("saving port %d's role: %w", c.IfIndex, err)
			}
		}
		return nil
	})
}

// peers matches the device's neighbours to the user's routers and
// switches, anywhere in the instance (a cable can join two sites), with the
// far port as a reason shows it: its faceplate label for LLDP, its ifName
// for CDP (which sends the long form, "TenGigabitEthernet1/1/6"); the port
// as sent when it isn't found.
func (s *PortRoleService) peers(ctx context.Context, self uuid.UUID, neighbors []struct {
	IfIndex                                                  int
	Protocol, ChassisID, SysName, PortID, PortDescr, Address string
	LastSeen                                                 time.Time
}) ([]portroles.Peer, error) {
	db := s.db.WithContext(ctx)
	var devs []roleDeviceRow
	if err := db.Raw(`SELECT id, site_id, name, COALESCE(sys_name, '') AS sys_name, COALESCE(sys_descr, '') AS sys_descr, host,
		COALESCE(device_type, device_type_detected) AS type FROM devices
		WHERE id <> ? AND COALESCE(device_type, device_type_detected) IN ?`, self,
		[]string{models.DeviceTypeRouter, models.DeviceTypeSwitch}).Scan(&devs).Error; err != nil {
		return nil, fmt.Errorf("loading routers and switches: %w", err)
	}
	if len(devs) == 0 {
		return nil, nil
	}
	byID := map[uuid.UUID]roleDeviceRow{}
	var cands []sitemap.Device
	var ids []uuid.UUID
	for _, d := range devs {
		byID[d.ID] = d
		ids = append(ids, d.ID)
		cands = append(cands, sitemap.Device{ID: d.ID, Name: d.Name, SysName: d.SysName, SysDescr: d.SysDescr, Host: d.Host})
	}
	var macPorts []rolePortRow
	if err := db.Raw(`SELECT `+rolePortColumns+` FROM device_interfaces WHERE device_id IN ? AND present AND COALESCE(mac, '') <> ''`, ids).
		Scan(&macPorts).Error; err != nil {
		return nil, fmt.Errorf("loading MACs: %w", err)
	}
	var macs []sitemap.Port
	for _, p := range macPorts {
		macs = append(macs, p.sitemapPort())
	}
	farPorts := map[uuid.UUID][]sitemap.Port{}
	var out []portroles.Peer
	for _, n := range neighbors {
		sn := sitemap.Neighbor{DeviceID: self, IfIndex: n.IfIndex, ChassisID: n.ChassisID, SysName: n.SysName, PortID: n.PortID,
			PortDescr: n.PortDescr, Address: n.Address, LastSeen: n.LastSeen}
		far, ok := sitemap.MatchNeighborDevice(sn, cands, macs)
		if !ok {
			continue
		}
		text := n.PortID
		if _, loaded := farPorts[far]; !loaded {
			var rows []rolePortRow
			if err := db.Raw(`SELECT `+rolePortColumns+` FROM device_interfaces WHERE device_id = ? AND present`, far).Scan(&rows).Error; err != nil {
				return nil, fmt.Errorf("loading the neighbour's ports: %w", err)
			}
			list := make([]sitemap.Port, 0, len(rows))
			for _, r := range rows {
				list = append(list, r.sitemapPort())
			}
			farPorts[far] = list
		}
		numbering := portmon.NumberingFor(byID[far].SysDescr)
		if ix := sitemap.MatchNeighborPort(sn, far, farPorts[far], numbering); ix > 0 {
			for _, fp := range farPorts[far] {
				switch {
				case fp.IfIndex != ix:
				case n.Protocol != "cdp":
					text = numbering.Label(fp.Name, fp.Descr, ix)
				case fp.Name != "":
					text = fp.Name
				}
			}
		}
		out = append(out, portroles.Peer{IfIndex: n.IfIndex, Protocol: n.Protocol, DeviceName: byID[far].Name, PortText: text, LastSeen: n.LastSeen})
	}
	return out, nil
}

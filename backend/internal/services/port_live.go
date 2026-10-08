package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// LivePort is one port as a circuit card shows it: its device, its name, its
// link state and its latest rates (nil when there is no recent sample).
// SiteID is the device's site, so a caller can drop a port whose device has
// moved elsewhere.
type LivePort struct {
	InterfaceID uuid.UUID `json:"interface_id"`
	DeviceID    uuid.UUID `json:"device_id"`
	DeviceName  string    `json:"device_name"`
	SiteID      uuid.UUID `json:"-"`
	IfIndex     int       `json:"if_index"`
	Number      int       `json:"number"`
	Label       string    `json:"label"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	StackUnit   int       `json:"stack_unit"`
	OperStatus  string    `json:"oper_status"`
	InBps       *float64  `json:"in_bps"`
	OutBps      *float64  `json:"out_bps"`
}

// LivePorts reads the given ports with their devices and latest rates, keyed
// by interface id. Ports that no longer exist are left out. Rates use each
// device's own freshness window, as the port pages do.
func (s *PortService) LivePorts(ctx context.Context, interfaceIDs []uuid.UUID) (map[uuid.UUID]LivePort, error) {
	out := map[uuid.UUID]LivePort{}
	if len(interfaceIDs) == 0 {
		return out, nil
	}
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("id IN ?", interfaceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading ports: %w", err)
	}
	if len(rows) == 0 {
		return out, nil
	}
	seen := map[uuid.UUID]bool{}
	var deviceIDs []uuid.UUID
	for _, r := range rows {
		if !seen[r.DeviceID] {
			seen[r.DeviceID] = true
			deviceIDs = append(deviceIDs, r.DeviceID)
		}
	}
	var devices []struct {
		ID           uuid.UUID
		Name         string
		SiteID       uuid.UUID
		PollInterval int
	}
	if err := s.db.WithContext(ctx).Table("devices").Select("id, name, site_id, poll_interval").
		Where("id IN ?", deviceIDs).Scan(&devices).Error; err != nil {
		return nil, fmt.Errorf("loading port devices: %w", err)
	}
	type deviceLive struct {
		name string
		site uuid.UUID
		live map[string]map[string]float64
	}
	now := time.Now()
	byDevice := make(map[uuid.UUID]deviceLive, len(devices))
	for _, d := range devices {
		latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(now, d.PollInterval))
		if err != nil {
			return nil, err
		}
		byDevice[d.ID] = deviceLive{name: d.Name, site: d.SiteID, live: latest[d.ID]}
	}
	for _, r := range rows {
		d, ok := byDevice[r.DeviceID]
		if !ok {
			continue
		}
		v := toPortView(r, d.live)
		out[r.ID] = LivePort{InterfaceID: r.ID, DeviceID: r.DeviceID, DeviceName: d.name, SiteID: d.site,
			IfIndex: r.IfIndex, Number: v.Number, Label: v.Label, Name: r.Name, Alias: r.Alias, StackUnit: r.StackUnit,
			OperStatus: r.OperStatus, InBps: v.InBps, OutBps: v.OutBps}
	}
	return out, nil
}

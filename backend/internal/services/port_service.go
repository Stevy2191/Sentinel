package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// PortService stores port state and events for the PortMonitor, and (Task 9)
// serves ports, events and summaries to the API.
type PortService struct {
	db        *gorm.DB
	metrics   *MetricsStore
	incidents *IncidentService
	settings  *SettingsService
}

func NewPortService(db *gorm.DB, metrics *MetricsStore, incidents *IncidentService, settings *SettingsService) *PortService {
	return &PortService{db: db, metrics: metrics, incidents: incidents, settings: settings}
}

// PortStateUpdate is what one stats poll writes back to an interface.
type PortStateUpdate struct {
	OperStatus, AdminStatus string
	SpeedBps                int64
	// LastChangeSeconds < 0 leaves the stored value.
	LastChangeSeconds int64
	// OperChangedAt, when set, records when the link last changed state.
	OperChangedAt   *time.Time
	Conditions      []string
	ConditionsSince map[string]time.Time
}

// PortEventEnd closes an open span event (flapping, errors, saturated,
// slow_link).
type PortEventEnd struct {
	InterfaceID uuid.UUID
	Kind        string
	At          time.Time
	Detail      map[string]any
}

// CollectedInterfaces are the present interfaces the stats poll reads.
func (s *PortService) CollectedInterfaces(ctx context.Context, deviceID uuid.UUID) ([]models.DeviceInterface, error) {
	var out []models.DeviceInterface
	err := s.db.WithContext(ctx).
		Where("device_id = ? AND present AND COALESCE(collect, collect_default)", deviceID).
		Order("if_index").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing collected interfaces: %w", err)
	}
	return out, nil
}

// SavePortState writes an interface's live state and active conditions.
func (s *PortService) SavePortState(ctx context.Context, interfaceID uuid.UUID, u PortStateUpdate) error {
	updates := map[string]any{
		"oper_status":      u.OperStatus,
		"admin_status":     u.AdminStatus,
		"speed_bps":        u.SpeedBps,
		"conditions":       models.ConditionSet(u.Conditions),
		"conditions_since": models.TimeMap(u.ConditionsSince),
		"updated_at":       time.Now(),
	}
	if u.LastChangeSeconds >= 0 {
		updates["last_change_seconds"] = u.LastChangeSeconds
	}
	if u.OperChangedAt != nil {
		updates["oper_changed_at"] = *u.OperChangedAt
	}
	if err := s.db.WithContext(ctx).Model(&models.DeviceInterface{}).Where("id = ?", interfaceID).Updates(updates).Error; err != nil {
		return fmt.Errorf("saving port state: %w", err)
	}
	return nil
}

// RecordPortEvents inserts new events (span events with no end) and ends
// open span events, in one transaction. The tracker uses the same detail
// keys at start and end (e.g. "per_minute", "util_pct", "speed_bps"), so
// ending an event nests its end detail under an "end" key instead of merging
// it in at the top level: the start figures stay readable, and the clearing
// figures live at detail.end.*. When there is no end detail (e.g. the link
// went down while a traffic condition was active) the merge is skipped
// entirely — ended_at is still set, but no "end": {} is written.
func (s *PortService) RecordPortEvents(ctx context.Context, starts []models.PortEvent, ends []PortEventEnd) error {
	if len(starts) == 0 && len(ends) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(starts) > 0 {
			if err := tx.Create(&starts).Error; err != nil {
				return fmt.Errorf("recording port events: %w", err)
			}
		}
		for _, e := range ends {
			if len(e.Detail) == 0 {
				if err := tx.Exec(`UPDATE port_events SET ended_at = ?
					WHERE interface_id = ? AND kind = ? AND ended_at IS NULL`,
					e.At, e.InterfaceID, e.Kind).Error; err != nil {
					return fmt.Errorf("ending %s event: %w", e.Kind, err)
				}
				continue
			}
			detail, err := json.Marshal(e.Detail)
			if err != nil {
				return fmt.Errorf("marshalling end detail for %s event: %w", e.Kind, err)
			}
			if err := tx.Exec(`UPDATE port_events SET ended_at = ?, detail = detail || jsonb_build_object('end', ?::jsonb)
				WHERE interface_id = ? AND kind = ? AND ended_at IS NULL`,
				e.At, string(detail), e.InterfaceID, e.Kind).Error; err != nil {
				return fmt.Errorf("ending %s event: %w", e.Kind, err)
			}
		}
		return nil
	})
}

// SaveStatsRun records when a device's stats poll ran and how long it took.
func (s *PortService) SaveStatsRun(ctx context.Context, deviceID uuid.UUID, at time.Time, took time.Duration) error {
	return s.db.WithContext(ctx).Exec(`UPDATE devices SET last_stats_at = ?, last_stats_duration_ms = ? WHERE id = ?`,
		at, int(took.Milliseconds()), deviceID).Error
}

// SiteName names a site for alert text.
func (s *PortService) SiteName(ctx context.Context, siteID uuid.UUID) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM sites WHERE id = ?", siteID).Scan(&name).Error
	return name, err
}

var ErrPortNotFound = errors.New("port not found")

// PortView is one interface as the device and port pages show it: stored
// state plus its latest rates (nil when not collected or not fresh).
type PortView struct {
	models.DeviceInterface
	Number       int      `json:"number"`
	Collected    bool     `json:"collected"`
	Physical     bool     `json:"physical"`
	InBps        *float64 `json:"in_bps"`
	OutBps       *float64 `json:"out_bps"`
	InUtilPct    *float64 `json:"in_util_pct"`
	OutUtilPct   *float64 `json:"out_util_pct"`
	ErrorsPerMin *float64 `json:"errors_per_min"`
}

// PortDefaults are the instance thresholds a port uses when it has no
// overrides of its own.
type PortDefaults struct {
	ErrorThresholdPerMin int `json:"error_threshold_per_min"`
	UtilThresholdPct     int `json:"util_threshold_pct"`
	DownGraceSeconds     int `json:"down_grace_seconds"`
}

type DevicePortsView struct {
	Ports     []PortView        `json:"ports"`
	Faceplate portmon.Faceplate `json:"faceplate"`
	Defaults  PortDefaults      `json:"defaults"`
}

type PortDetailView struct {
	PortView
	Defaults      PortDefaults      `json:"defaults"`
	OpenIncidents []models.Incident `json:"open_incidents"`
}

var liveMetrics = []string{MetricIfInBps, MetricIfOutBps, MetricIfInUtilPct, MetricIfOutUtilPct,
	MetricIfInErrorsPM, MetricIfOutErrorsPM, MetricIfInDiscardsPM, MetricIfOutDiscardsPM}

// liveSince is how old a sample may be and still count as "now": three
// polls, and never less than five minutes.
func liveSince(now time.Time, pollInterval int) time.Time {
	w := 3 * time.Duration(pollInterval) * time.Second
	if w < 5*time.Minute {
		w = 5 * time.Minute
	}
	return now.Add(-w)
}

func (s *PortService) defaults(ctx context.Context) PortDefaults {
	th := s.settings.PortThresholds(ctx)
	return PortDefaults{ErrorThresholdPerMin: int(th.ErrorsPerMin), UtilThresholdPct: int(th.UtilPct),
		DownGraceSeconds: int(th.DownGrace.Seconds())}
}

func toPortView(row models.DeviceInterface, live map[string]map[string]float64) PortView {
	info := portmon.IfInfo{Name: row.Name, Descr: row.Descr, Type: row.IfType, ConnectorPresent: row.ConnectorPresent}
	v := PortView{DeviceInterface: row, Number: portmon.PortNumber(row.Name, row.Descr, row.IfIndex),
		Collected: row.CollectEffective(), Physical: portmon.IsPhysical(info)}
	inst := instanceKey(row.IfIndex)
	get := func(metric string) *float64 {
		if x, ok := live[metric][inst]; ok {
			return &x
		}
		return nil
	}
	v.InBps, v.OutBps = get(MetricIfInBps), get(MetricIfOutBps)
	v.InUtilPct, v.OutUtilPct = get(MetricIfInUtilPct), get(MetricIfOutUtilPct)
	sum, found := 0.0, false
	for _, k := range []string{MetricIfInErrorsPM, MetricIfOutErrorsPM, MetricIfInDiscardsPM, MetricIfOutDiscardsPM} {
		if p := get(k); p != nil {
			sum, found = sum+*p, true
		}
	}
	if found {
		v.ErrorsPerMin = &sum
	}
	return v
}

// DevicePorts lists a device's present interfaces with live figures, and the
// faceplate of its physical ports.
func (s *PortService) DevicePorts(ctx context.Context, d *DeviceView) (*DevicePortsView, error) {
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id = ? AND present", d.ID).Order("if_index").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing ports: %w", err)
	}
	latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(time.Now(), d.PollInterval))
	if err != nil {
		return nil, err
	}
	out := &DevicePortsView{Ports: make([]PortView, 0, len(rows)), Defaults: s.defaults(ctx)}
	var layout []portmon.LayoutPort
	for _, r := range rows {
		v := toPortView(r, latest[d.ID])
		out.Ports = append(out.Ports, v)
		if v.Physical {
			layout = append(layout, portmon.LayoutPort{IfIndex: r.IfIndex, Name: r.Name, Descr: r.Descr})
		}
	}
	out.Faceplate = portmon.Layout(layout, d.FaceplateRows, d.FaceplateSFPPorts)
	return out, nil
}

// Port is one interface with live figures and its open incidents.
func (s *PortService) Port(ctx context.Context, d *DeviceView, ifIndex int) (*PortDetailView, error) {
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id = ? AND if_index = ?", d.ID, ifIndex).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading port: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrPortNotFound
	}
	latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(time.Now(), d.PollInterval))
	if err != nil {
		return nil, err
	}
	open, err := s.incidents.OpenPortIncidents(ctx, rows[0].ID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		open = []models.Incident{}
	}
	return &PortDetailView{PortView: toPortView(rows[0], latest[d.ID]), Defaults: s.defaults(ctx), OpenIncidents: open}, nil
}

// UpdatePort applies a port patch, in one transaction: if closing an
// incident (or clearing stale state) fails, the column change is rolled back
// with it rather than left committed on its own.
//
// Un-marking a port as important closes its open port incidents. Turning off
// statistics collection for a port that was being collected does the same,
// and also clears its conditions (and conditions_since) and ends any open
// event spans: the port is no longer polled, so nothing else will ever
// notice those conditions clear or those spans end.
func (s *PortService) UpdatePort(ctx context.Context, deviceID uuid.UUID, ifIndex int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	var before models.DeviceInterface
	err := s.db.WithContext(ctx).Where("device_id = ? AND if_index = ?", deviceID, ifIndex).First(&before).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrPortNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("loading port: %w", err)
	}

	// Derived from the patch, not a reload: Collect is the only column this
	// patch can change that CollectEffective depends on.
	afterCollect := before
	if p.Collect.Set {
		afterCollect.Collect = p.Collect.Value
	}
	collectionStopped := before.CollectEffective() && !afterCollect.CollectEffective()
	now := time.Now().UTC()

	var after models.DeviceInterface
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.DeviceInterface{}).Where("id = ?", before.ID).Updates(p.Updates()).Error; err != nil {
			return fmt.Errorf("saving port: %w", err)
		}
		if before.Important && p.Important.Value != nil && !*p.Important.Value {
			if _, err := s.incidents.ClosePortIncidentsTx(tx, before.ID, now, "The port is no longer marked important."); err != nil {
				return err
			}
		}
		if collectionStopped {
			if err := tx.Model(&models.DeviceInterface{}).Where("id = ?", before.ID).
				Updates(map[string]any{"conditions": models.ConditionSet{}, "conditions_since": models.TimeMap{}}).Error; err != nil {
				return fmt.Errorf("clearing port conditions: %w", err)
			}
			if err := tx.Exec(`UPDATE port_events SET ended_at = ? WHERE interface_id = ? AND ended_at IS NULL`,
				now, before.ID).Error; err != nil {
				return fmt.Errorf("ending open port events: %w", err)
			}
			if _, err := s.incidents.ClosePortIncidentsTx(tx, before.ID, now, "Statistics are no longer collected for this port."); err != nil {
				return err
			}
		}
		if err := tx.First(&after, "id = ?", before.ID).Error; err != nil {
			return fmt.Errorf("reloading port: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &before, &after, nil
}

// PortEventFilter narrows the event log. Exactly one of DeviceID and SiteID
// is expected (the handlers set one).
type PortEventFilter struct {
	DeviceID *uuid.UUID
	SiteID   *uuid.UUID
	IfIndex  *int
	Page     int
	Limit    int
}

type PortEventView struct {
	models.PortEvent
	DeviceName string `json:"device_name" gorm:"column:device_name"`
	PortName   string `json:"port_name" gorm:"column:port_name"`
	PortAlias  string `json:"port_alias" gorm:"column:port_alias"`
	PortDescr  string `json:"-" gorm:"column:port_descr"`
	PortNumber int    `json:"port_number" gorm:"-"`
}

// Events returns a page of the port event log, newest first, and the total.
func (s *PortService) Events(ctx context.Context, f PortEventFilter) ([]PortEventView, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
	q := s.db.WithContext(ctx).Table("port_events AS e").
		Joins("JOIN devices d ON d.id = e.device_id").
		Joins("JOIN device_interfaces i ON i.id = e.interface_id")
	if f.DeviceID != nil {
		q = q.Where("e.device_id = ?", *f.DeviceID)
	}
	if f.SiteID != nil {
		q = q.Where("d.site_id = ?", *f.SiteID)
	}
	if f.IfIndex != nil {
		q = q.Where("e.if_index = ?", *f.IfIndex)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting port events: %w", err)
	}
	var rows []PortEventView
	err := q.Session(&gorm.Session{}).
		Select("e.*, d.name AS device_name, COALESCE(i.name, '') AS port_name, COALESCE(i.alias, '') AS port_alias, COALESCE(i.descr, '') AS port_descr").
		Order("e.started_at DESC, e.id DESC").Limit(f.Limit).Offset((f.Page - 1) * f.Limit).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("listing port events: %w", err)
	}
	for i := range rows {
		rows[i].PortNumber = portmon.PortNumber(rows[i].PortName, rows[i].PortDescr, rows[i].IfIndex)
	}
	if rows == nil {
		rows = []PortEventView{}
	}
	return rows, total, nil
}

// PortRef is a port in a site summary.
type PortRef struct {
	DeviceID   uuid.UUID `json:"device_id"`
	DeviceName string    `json:"device_name"`
	IfIndex    int       `json:"if_index"`
	Number     int       `json:"number"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	OperStatus string    `json:"oper_status"`
	Important  bool      `json:"important"`
	Conditions []string  `json:"conditions"`
	InBps      *float64  `json:"in_bps"`
	OutBps     *float64  `json:"out_bps"`
	UtilPct    *float64  `json:"util_pct"`
}

type SitePortSummary struct {
	Busiest  []PortRef `json:"busiest"`
	Problems []PortRef `json:"problems"`
}

// SiteSummary lists a site's five busiest physical ports and every port with
// a problem: a warning condition, or an important port whose link is down.
func (s *PortService) SiteSummary(ctx context.Context, siteID uuid.UUID) (*SitePortSummary, error) {
	var rows []struct {
		models.DeviceInterface
		DeviceName   string `gorm:"column:device_name"`
		PollInterval int    `gorm:"column:poll_interval"`
	}
	err := s.db.WithContext(ctx).Raw(`SELECT i.*, d.name AS device_name, d.poll_interval
		FROM device_interfaces i JOIN devices d ON d.id = i.device_id
		WHERE d.site_id = ? AND i.present AND COALESCE(i.collect, i.collect_default)`, siteID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing site ports: %w", err)
	}
	ids, seen, maxPoll := []uuid.UUID{}, map[uuid.UUID]bool{}, 60
	for _, r := range rows {
		if !seen[r.DeviceID] {
			seen[r.DeviceID] = true
			ids = append(ids, r.DeviceID)
		}
		maxPoll = max(maxPoll, r.PollInterval)
	}
	latest, err := s.metrics.LatestMany(ctx, ids, []string{MetricIfInBps, MetricIfOutBps, MetricIfInUtilPct, MetricIfOutUtilPct},
		liveSince(time.Now(), maxPoll))
	if err != nil {
		return nil, err
	}
	out := &SitePortSummary{Busiest: []PortRef{}, Problems: []PortRef{}}
	var busy []PortRef
	for _, r := range rows {
		v := toPortView(r.DeviceInterface, latest[r.DeviceID])
		conds := []string{}
		for _, c := range r.Conditions {
			if c != models.PortConditionLinkDown {
				conds = append(conds, c)
			}
		}
		ref := PortRef{DeviceID: r.DeviceID, DeviceName: r.DeviceName, IfIndex: r.IfIndex, Number: v.Number,
			Name: r.Name, Alias: r.Alias, OperStatus: r.OperStatus, Important: r.Important, Conditions: conds,
			InBps: v.InBps, OutBps: v.OutBps}
		if v.InUtilPct != nil || v.OutUtilPct != nil {
			u := 0.0
			if v.InUtilPct != nil {
				u = *v.InUtilPct
			}
			if v.OutUtilPct != nil {
				u = max(u, *v.OutUtilPct)
			}
			ref.UtilPct = &u
		}
		importantDown := r.Important && r.OperStatus != "up" && r.AdminStatus != "down"
		if len(conds) > 0 || importantDown {
			out.Problems = append(out.Problems, ref)
		}
		if v.Physical && ref.UtilPct != nil {
			busy = append(busy, ref)
		}
	}
	sort.Slice(busy, func(i, j int) bool { return *busy[i].UtilPct > *busy[j].UtilPct })
	if len(busy) > 5 {
		busy = busy[:5]
	}
	out.Busiest = append(out.Busiest, busy...)
	sort.Slice(out.Problems, func(i, j int) bool {
		a, b := out.Problems[i], out.Problems[j]
		if a.DeviceName != b.DeviceName {
			return a.DeviceName < b.DeviceName
		}
		return a.Number < b.Number
	})
	return out, nil
}

// NorthSouthPoint is one bucket of a site's internet traffic.
type NorthSouthPoint struct {
	Time   time.Time `json:"t"`
	InBps  float64   `json:"in_bps"`
	OutBps float64   `json:"out_bps"`
}

// EastWestPoint is one bucket of a site's traffic that stays inside the site.
type EastWestPoint struct {
	Time time.Time `json:"t"`
	Bps  float64   `json:"bps"`
}

// SiteTraffic is the response of GET /sites/:id/traffic: the site's
// north-south (internet) and east-west (internal) traffic over a range.
type SiteTraffic struct {
	Resolution    string            `json:"resolution"`
	StepSeconds   int               `json:"step_seconds"`
	WANConfigured bool              `json:"wan_configured"`
	NorthSouth    []NorthSouthPoint `json:"north_south"`
	EastWest      []EastWestPoint   `json:"east_west"`
}

// bucketValues is one metric's Sum series as a map keyed by bucket time, for
// easy lookup when combining several metrics' buckets.
func bucketValues(res *MetricsResult, metric string) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, s := range res.Series {
		if s.Metric != metric {
			continue
		}
		for _, p := range s.Points {
			out[p.Time] = p.Avg
		}
	}
	return out
}

// sortedTimes returns a's keys, b's keys, or both (deduplicated) in order.
func sortedTimes(maps ...map[time.Time]float64) []time.Time {
	seen := map[time.Time]bool{}
	var out []time.Time
	for _, m := range maps {
		for t := range m {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// SiteTraffic splits a site's port traffic into north-south (the sum of the
// site's WAN-role ports) and east-west (an estimate derived from the site's
// switch/router access ports and its WAN ports: see the design doc for the
// formula). With no WAN-role port in the site, WANConfigured is false and
// both series are empty: there is nothing to tell north-south from east-west.
func (s *PortService) SiteTraffic(ctx context.Context, siteID uuid.UUID, from, to time.Time) (*SiteTraffic, error) {
	var rows []struct {
		models.DeviceInterface
		EffectiveType string `gorm:"column:effective_type"`
	}
	err := s.db.WithContext(ctx).Raw(`SELECT i.*, COALESCE(d.device_type, d.device_type_detected) AS effective_type
		FROM device_interfaces i JOIN devices d ON d.id = i.device_id
		WHERE d.site_id = ? AND i.present`, siteID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing site ports for traffic: %w", err)
	}

	var wanIfaceIDs, wanDeviceIDs, accessIfaceIDs, accessDeviceIDs []uuid.UUID
	wanDevSeen, accessDevSeen := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, r := range rows {
		switch r.Role {
		case models.PortRoleWAN:
			wanIfaceIDs = append(wanIfaceIDs, r.ID)
			if !wanDevSeen[r.DeviceID] {
				wanDevSeen[r.DeviceID] = true
				wanDeviceIDs = append(wanDeviceIDs, r.DeviceID)
			}
		case models.PortRoleAccess:
			if r.EffectiveType != models.DeviceTypeSwitch && r.EffectiveType != models.DeviceTypeRouter {
				continue
			}
			if !r.CollectEffective() {
				continue
			}
			info := portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent}
			if !portmon.IsPhysical(info) {
				continue
			}
			accessIfaceIDs = append(accessIfaceIDs, r.ID)
			if !accessDevSeen[r.DeviceID] {
				accessDevSeen[r.DeviceID] = true
				accessDeviceIDs = append(accessDeviceIDs, r.DeviceID)
			}
		}
	}

	bpsMetrics := []string{MetricIfInBps, MetricIfOutBps}
	wanRes, err := s.metrics.Query(ctx, MetricsQuery{DeviceIDs: wanDeviceIDs, Metrics: bpsMetrics,
		InterfaceIDs: wanIfaceIDs, From: from, To: to, Sum: true})
	if err != nil {
		return nil, err
	}
	out := &SiteTraffic{Resolution: wanRes.Resolution, StepSeconds: wanRes.StepSeconds,
		WANConfigured: len(wanIfaceIDs) > 0, NorthSouth: []NorthSouthPoint{}, EastWest: []EastWestPoint{}}
	if !out.WANConfigured {
		return out, nil
	}

	accessRes, err := s.metrics.Query(ctx, MetricsQuery{DeviceIDs: accessDeviceIDs, Metrics: bpsMetrics,
		InterfaceIDs: accessIfaceIDs, From: from, To: to, Sum: true})
	if err != nil {
		return nil, err
	}

	wanIn, wanOut := bucketValues(wanRes, MetricIfInBps), bucketValues(wanRes, MetricIfOutBps)
	accIn, accOut := bucketValues(accessRes, MetricIfInBps), bucketValues(accessRes, MetricIfOutBps)

	for _, t := range sortedTimes(wanIn, wanOut) {
		out.NorthSouth = append(out.NorthSouth, NorthSouthPoint{Time: t, InBps: wanIn[t], OutBps: wanOut[t]})
	}

	// A bucket needs both access sums to mean anything; missing WAN sums at a
	// bucket default to 0 (map lookups of an absent key), so a quiet WAN still
	// yields an east-west point.
	for _, t := range sortedTimes(accIn, accOut) {
		accInV, inOK := accIn[t]
		accOutV, outOK := accOut[t]
		if !inOK || !outOK {
			continue
		}
		ew := ((accInV - wanOut[t]) + (accOutV - wanIn[t])) / 2
		if ew < 0 {
			ew = 0
		}
		out.EastWest = append(out.EastWest, EastWestPoint{Time: t, Bps: ew})
	}
	return out, nil
}

// PhysicalInterfaceIDs returns the ids of the physical ports of the given
// devices, for totals that must not count a link aggregate and its members
// twice.
func (s *PortService) PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id IN ? AND present", deviceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing physical ports: %w", err)
	}
	var ids []uuid.UUID
	for _, r := range rows {
		if portmon.IsPhysical(portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent}) {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

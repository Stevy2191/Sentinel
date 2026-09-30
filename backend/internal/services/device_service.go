package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrDeviceNotFound      = errors.New("device not found")
	ErrDeviceHostTaken     = errors.New("this site already has a device at that address and port")
	ErrCredentialNotUsable = errors.New("that credential profile cannot be used in this site")
)

// DeviceView is a device with the names and figures its pages show.
type DeviceView struct {
	models.Device
	SiteName        string   `json:"site_name" gorm:"column:site_name"`
	CredentialName  string   `json:"credential_name" gorm:"column:credential_name"`
	Availability30d *float64 `json:"availability_30d" gorm:"column:availability_30d"`
}

// DeviceFilter narrows the device list.
type DeviceFilter struct {
	SiteID *uuid.UUID
	Status string
}

// BulkAddError explains why one address of a bulk add was not added.
type BulkAddError struct {
	Host  string `json:"host"`
	Error string `json:"error"`
}

// DeviceService stores devices and is the poller's store.
type DeviceService struct {
	db        *gorm.DB
	creds     *SNMPCredentialService
	incidents *IncidentService
}

var _ PollerStore = (*DeviceService)(nil)

func NewDeviceService(db *gorm.DB, creds *SNMPCredentialService, incidents *IncidentService) *DeviceService {
	return &DeviceService{db: db, creds: creds, incidents: incidents}
}

// availabilitySQL is the share of the last 30 days (or of the device's life,
// if shorter) not covered by its incidents. NULL for a device younger than a
// minute, where a percentage would mean nothing.
const availabilitySQL = `(
	SELECT CASE WHEN win.secs < 60 THEN NULL ELSE
		GREATEST(0, 100 - 100 * COALESCE(SUM(EXTRACT(EPOCH FROM (
			LEAST(COALESCE(i.end_time, now()), now()) - GREATEST(i.start_time, win.since)))), 0) / win.secs)
	END
	FROM (SELECT GREATEST(d.created_at, now() - interval '30 days') AS since,
	             EXTRACT(EPOCH FROM (now() - GREATEST(d.created_at, now() - interval '30 days'))) AS secs) win
	LEFT JOIN incidents i ON i.device_id = d.id AND i.interface_id IS NULL
		AND COALESCE(i.end_time, now()) > win.since
	GROUP BY win.secs
) AS availability_30d`

func (s *DeviceService) viewQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("devices AS d").
		Select("d.*, st.name AS site_name, c.name AS credential_name, " + availabilitySQL).
		Joins("JOIN sites st ON st.id = d.site_id").
		Joins("JOIN snmp_credentials c ON c.id = d.credential_id")
}

// List returns the devices in sites the caller can see.
func (s *DeviceService) List(ctx context.Context, userID uuid.UUID, isAdmin bool, f DeviceFilter) ([]DeviceView, error) {
	q := s.viewQuery(ctx)
	if !isAdmin {
		q = q.Where("d.site_id IN (SELECT site_id FROM site_sharing WHERE shared_with_user_id = ?)", userID)
	}
	if f.SiteID != nil {
		q = q.Where("d.site_id = ?", *f.SiteID)
	}
	if f.Status != "" {
		q = q.Where("d.status = ?", f.Status)
	}
	var out []DeviceView
	if err := q.Order("lower(st.name), lower(d.name)").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	return out, nil
}

// Get returns one device, or ErrDeviceNotFound.
func (s *DeviceService) Get(ctx context.Context, id uuid.UUID) (*DeviceView, error) {
	var v DeviceView
	if err := s.viewQuery(ctx).Where("d.id = ?", id).Scan(&v).Error; err != nil {
		return nil, fmt.Errorf("loading device: %w", err)
	}
	if v.ID == uuid.Nil {
		return nil, ErrDeviceNotFound
	}
	return &v, nil
}

func (s *DeviceService) getRaw(ctx context.Context, id uuid.UUID) (*models.Device, error) {
	var d models.Device
	err := s.db.WithContext(ctx).First(&d, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading device: %w", err)
	}
	return &d, nil
}

func mapDeviceWriteError(err error) error {
	switch {
	case isDuplicateKey(err):
		return ErrDeviceHostTaken
	case isForeignKeyViolation(err):
		return ErrSiteNotFound
	default:
		return fmt.Errorf("saving device: %w", err)
	}
}

func (s *DeviceService) checkCredential(ctx context.Context, credentialID, siteID uuid.UUID) error {
	ok, err := s.creds.UsableAt(ctx, credentialID, siteID)
	if err != nil {
		return fmt.Errorf("checking credential profile: %w", err)
	}
	if !ok {
		return ErrCredentialNotUsable
	}
	return nil
}

// Create adds a device. An empty name becomes the host until the first
// inventory names it from sysName.
func (s *DeviceService) Create(ctx context.Context, raw models.DeviceInput, by uuid.UUID) (*models.Device, error) {
	in, err := models.NormalizeDeviceInput(raw)
	if err != nil {
		return nil, err
	}
	if err := s.checkCredential(ctx, in.CredentialID, in.SiteID); err != nil {
		return nil, err
	}
	d := models.Device{
		SiteID: in.SiteID, CredentialID: in.CredentialID, Name: in.Name, Host: in.Host, Port: in.Port,
		Enabled: *in.Enabled, PollInterval: in.PollInterval, TimeoutMs: in.TimeoutMs, Retries: in.Retries,
		NotifyChannels: in.NotifyChannels, Status: models.DeviceStatusPending,
	}
	if d.Name == "" {
		d.Name = d.Host
	}
	if !d.Enabled {
		d.Status = models.DeviceStatusPaused
	}
	if by != uuid.Nil {
		d.CreatedBy = &by
	}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return nil, mapDeviceWriteError(err)
	}
	return &d, nil
}

// CreateMany adds several devices to one site, reporting each failure
// instead of stopping at the first.
func (s *DeviceService) CreateMany(ctx context.Context, siteID uuid.UUID, in []models.DeviceInput, by uuid.UUID) ([]models.Device, []BulkAddError) {
	var added []models.Device
	var failed []BulkAddError
	for _, one := range in {
		one.SiteID = siteID
		d, err := s.Create(ctx, one, by)
		if err != nil {
			failed = append(failed, BulkAddError{Host: one.Host, Error: err.Error()})
			continue
		}
		added = append(added, *d)
	}
	return added, failed
}

// Update changes a device. Disabling pauses it (closing any open incident);
// enabling a paused device starts it again from pending.
func (s *DeviceService) Update(ctx context.Context, id uuid.UUID, raw models.DeviceInput) (*models.Device, *models.Device, error) {
	before, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	in, err := models.NormalizeDeviceInput(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkCredential(ctx, in.CredentialID, in.SiteID); err != nil {
		return nil, nil, err
	}
	updates := map[string]any{
		"site_id": in.SiteID, "credential_id": in.CredentialID, "host": in.Host, "port": in.Port,
		"enabled": *in.Enabled, "poll_interval": in.PollInterval, "timeout_ms": in.TimeoutMs,
		"retries": in.Retries, "notify_channels": in.NotifyChannels, "updated_at": gorm.Expr("now()"),
	}
	switch {
	case in.Name != "" && in.Name != before.Host:
		// A real, typed name (not just the pre-filled old host coming back
		// unchanged): it is the user's now, and is never auto-renamed again.
		updates["name"] = in.Name
	case before.Name == before.Host:
		// Still auto-named after its old host: keep following the device
		// onto its new host, so inventory can still rename it from sysName
		// (SaveInventory only renames while name == host).
		updates["name"] = in.Host
	}

	pausing := before.Enabled && !*in.Enabled
	var pauseTransition DeviceTransition
	switch {
	case pausing:
		pauseTransition = NextDeviceState(before.Status, before.ConsecutiveFailures, PollPaused)
		updates["status"], updates["consecutive_failures"], updates["status_detail"] =
			pauseTransition.Status, pauseTransition.Failures, ""
	case !before.Enabled && *in.Enabled:
		updates["status"], updates["consecutive_failures"], updates["last_polled_at"] = models.DeviceStatusPending, 0, nil
	case in.Host != before.Host || in.Port != before.Port || in.CredentialID != before.CredentialID:
		// A different target: poll and re-inventory it now.
		updates["last_polled_at"], updates["last_inventory_at"] = nil, nil
	}

	// The device's own update and (when pausing) closing its incident must
	// commit or roll back together: a failed update (e.g. a host+port
	// collision) must never leave an incident closed for a device that is
	// still down and still enabled.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return mapDeviceWriteError(err)
		}
		if pausing {
			for _, a := range pauseTransition.Actions {
				if a == ActionCloseIncident {
					if _, err := s.incidents.CloseDeviceIncidentTx(tx, id, time.Now().UTC(), "Monitoring was paused."); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	after, err := s.getRaw(ctx, id)
	return before, after, err
}

// Delete removes a device; its interfaces, incidents and notifications go
// with it (ON DELETE CASCADE).
func (s *DeviceService) Delete(ctx context.Context, id uuid.UUID) (*models.Device, error) {
	d, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	// The device's metric series go in the same transaction (metrics has no
	// FKs to cascade them); their samples are removed by the nightly cleanup.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.Device{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("deleting device: %w", err)
		}
		return deleteDeviceSeries(tx, id)
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Interfaces lists a device's interfaces by index.
func (s *DeviceService) Interfaces(ctx context.Context, id uuid.UUID, includeAbsent bool) ([]models.DeviceInterface, error) {
	q := s.db.WithContext(ctx).Where("device_id = ?", id)
	if !includeAbsent {
		q = q.Where("present")
	}
	var out []models.DeviceInterface
	if err := q.Order("if_index").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}
	return out, nil
}

// RequestRefresh makes the poller poll and re-inventory the device on its
// next tick.
func (s *DeviceService) RequestRefresh(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&models.Device{}).Where("id = ? AND enabled", id).
		Updates(map[string]any{"last_polled_at": nil, "last_inventory_at": nil})
	if res.Error != nil {
		return fmt.Errorf("requesting refresh: %w", res.Error)
	}
	return nil
}

// HostsInSite returns "host:port" for every device in a site, for marking scan
// results that are already added.
func (s *DeviceService) HostsInSite(ctx context.Context, siteID uuid.UUID) (map[string]bool, error) {
	var rows []struct {
		Host string
		Port int
	}
	if err := s.db.WithContext(ctx).Model(&models.Device{}).Select("host, port").
		Where("site_id = ?", siteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing site hosts: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[fmt.Sprintf("%s:%d", r.Host, r.Port)] = true
	}
	return out, nil
}

// ---- PollerStore ------------------------------------------------------------

// DueDevices returns enabled devices never polled or whose interval has
// passed, longest-waiting first.
func (s *DeviceService) DueDevices(ctx context.Context, now time.Time, limit int) ([]models.Device, error) {
	var out []models.Device
	err := s.db.WithContext(ctx).
		Where("enabled AND (last_polled_at IS NULL OR last_polled_at + make_interval(secs => poll_interval) <= ?)", now).
		Order("last_polled_at ASC NULLS FIRST").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing due devices: %w", err)
	}
	return out, nil
}

// DeviceCredential returns a device's decrypted credential profile. A
// missing profile (ErrCredentialNotFound) or one that cannot be decrypted
// (ErrCredentialDecryptFailed) is a configuration problem, not a lookup
// failure, so both are wrapped as ErrCredentialUnusable: PollOnce treats
// that — and only that — as PollBlocked. Any other error (a database
// hiccup, a cancelled context) says nothing about whether the credential is
// usable, so it is returned as-is.
func (s *DeviceService) DeviceCredential(ctx context.Context, credentialID uuid.UUID) (snmp.Credential, error) {
	cred, err := s.creds.Decrypted(ctx, credentialID)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) || errors.Is(err, ErrCredentialDecryptFailed) {
			return snmp.Credential{}, fmt.Errorf("%w: %v", ErrCredentialUnusable, err)
		}
		return snmp.Credential{}, err
	}
	return cred, nil
}

func (s *DeviceService) SiteName(ctx context.Context, siteID uuid.UUID) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM sites WHERE id = ?", siteID).Scan(&name).Error
	return name, err
}

// SaveReachability stores a poll's outcome. "AND enabled" keeps a poll that
// finished after the device was paused from overwriting the pause; when that
// happens the UPDATE touches 0 rows and ErrDeviceNotEnabled is returned so
// PollOnce knows the computed state no longer applies.
func (s *DeviceService) SaveReachability(ctx context.Context, id uuid.UUID, u ReachabilityUpdate) error {
	res := s.db.WithContext(ctx).Exec(`UPDATE devices SET
		status = ?, consecutive_failures = ?, status_detail = ?, last_polled_at = ?,
		last_seen_at = COALESCE(?, last_seen_at),
		sys_uptime_seconds = COALESCE(?, sys_uptime_seconds),
		updated_at = now()
		WHERE id = ? AND enabled`,
		u.Status, u.Failures, u.Detail, u.PolledAt, u.SeenAt, u.UptimeSeconds, id)
	if res.Error != nil {
		return fmt.Errorf("saving reachability: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDeviceNotEnabled
	}
	return nil
}

// SaveInventory stores identity and interfaces in one transaction.
// Interfaces are upserted by index; any not in this walk become absent. When
// this walk's ifXTable did not answer for an interface that it answered
// before, the stored name, alias and speed (which came from ifXTable) are
// kept rather than replaced by ifTable's fallbacks. Overrides (vendor_override
// and friends, device_type) are never written here.
func (s *DeviceService) SaveInventory(ctx context.Context, id uuid.UUID, inv snmp.Inventory, at time.Time) error {
	physical := 0
	for _, it := range inv.Interfaces {
		if portmon.IsPhysical(ifInfo(it)) {
			physical++
		}
	}
	detected := portmon.DetectDeviceType(inv.Model, physical)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sys := inv.System
		if err := tx.Exec(`UPDATE devices SET
			sys_name = ?, sys_descr = ?, sys_object_id = ?, sys_location = ?, sys_contact = ?,
			vendor = ?, model = ?, serial = ?, device_type_detected = ?, last_inventory_at = ?,
			name = CASE WHEN name = host AND ? <> '' THEN ? ELSE name END,
			status_detail = CASE WHEN status_detail LIKE 'inventory:%' THEN '' ELSE status_detail END,
			updated_at = now()
			WHERE id = ?`,
			sys.Name, sys.Descr, sys.ObjectID, sys.Location, sys.Contact,
			inv.Vendor, inv.Model, inv.Serial, detected, at, sys.Name, sys.Name, id).Error; err != nil {
			return fmt.Errorf("saving device identity: %w", err)
		}

		// ifX-sourced columns keep their stored value when this walk has no
		// ifX for the interface but an earlier one did.
		keepIfX := func(col string) clause.Assignment {
			return clause.Assignment{Column: clause.Column{Name: col}, Value: gorm.Expr(
				"CASE WHEN device_interfaces.has_ifx AND NOT EXCLUDED.has_ifx THEN device_interfaces." + col +
					" ELSE EXCLUDED." + col + " END")}
		}
		set := clause.AssignmentColumns([]string{"descr", "if_type", "mac", "admin_status", "oper_status",
			"last_change_seconds", "present", "updated_at", "collect_default"})
		set = append(set,
			keepIfX("name"), keepIfX("alias"), keepIfX("speed_bps"),
			clause.Assignment{Column: clause.Column{Name: "has_ifx"}, Value: gorm.Expr("device_interfaces.has_ifx OR EXCLUDED.has_ifx")},
			clause.Assignment{Column: clause.Column{Name: "connector_present"},
				Value: gorm.Expr("COALESCE(EXCLUDED.connector_present, device_interfaces.connector_present)")},
		)

		seen := make([]int, 0, len(inv.Interfaces))
		for _, it := range inv.Interfaces {
			seen = append(seen, it.Index)
			row := models.DeviceInterface{
				DeviceID: id, IfIndex: it.Index, Name: it.Name, Descr: it.Descr, Alias: it.Alias, IfType: it.Type,
				SpeedBps: it.SpeedBps, MAC: it.MAC, AdminStatus: it.AdminStatus, OperStatus: it.OperStatus,
				LastChangeSeconds: it.LastChangeSeconds, Present: true, UpdatedAt: at,
				ConnectorPresent: it.ConnectorPresent, HasIfX: it.HasIfX,
				CollectDefault: portmon.DefaultCollect(ifInfo(it)),
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "device_id"}, {Name: "if_index"}},
				DoUpdates: set,
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("saving interface %d: %w", it.Index, err)
			}
		}
		q := tx.Model(&models.DeviceInterface{}).Where("device_id = ? AND present", id)
		if len(seen) > 0 {
			q = q.Where("if_index NOT IN ?", seen)
		}
		if err := q.Updates(map[string]any{"present": false, "updated_at": at}).Error; err != nil {
			return fmt.Errorf("marking absent interfaces: %w", err)
		}
		return nil
	})
}

func ifInfo(it snmp.Interface) portmon.IfInfo {
	return portmon.IfInfo{Name: it.Name, Descr: it.Descr, Type: it.Type, ConnectorPresent: it.ConnectorPresent}
}

// SaveInventoryError records a failed inventory and stamps the attempt, so it
// is retried at the next interval rather than on every poll.
func (s *DeviceService) SaveInventoryError(ctx context.Context, id uuid.UUID, detail string, at time.Time) error {
	return s.db.WithContext(ctx).Exec(
		`UPDATE devices SET status_detail = ?, last_inventory_at = ?, updated_at = now() WHERE id = ?`,
		detail, at, id).Error
}

package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ErrDeviceNotEnabled is returned (optionally wrapped) by
// PollerStore.SaveReachability when the device was disabled between being
// queued and finishing its poll. PollOnce treats it as "this poll no longer
// matters" rather than a failure: no log, no incident/notification actions,
// no inventory — the device's own disablement already took care of anything
// that needed doing.
var ErrDeviceNotEnabled = errors.New("device is no longer enabled")

// ErrCredentialUnusable is returned (optionally wrapped) by
// PollerStore.DeviceCredential when the device's credential profile is
// missing or cannot be decrypted. PollOnce treats this — and only this — as
// a configuration problem (PollBlocked); any other error from
// DeviceCredential (a database hiccup, a cancelled context) says nothing
// about whether the device is reachable, so the poll is skipped entirely
// rather than recorded as blocked.
var ErrCredentialUnusable = errors.New("credential profile unusable")

const (
	pollerTick           = 5 * time.Second
	inventoryInterval    = 15 * time.Minute
	pollerDueBatch       = 1000
	defaultPollerWorkers = 16
)

// ReachabilityUpdate is what one poll writes back to a device.
type ReachabilityUpdate struct {
	Status        string
	Failures      int
	Detail        string
	PolledAt      time.Time
	SeenAt        *time.Time
	UptimeSeconds *int64
}

// PollerStore is the poller's view of the database (DeviceService).
type PollerStore interface {
	DueDevices(ctx context.Context, now time.Time, limit int) ([]models.Device, error)
	// DeviceCredential returns a device's decrypted credential profile. A
	// missing or undecryptable profile is returned wrapping
	// ErrCredentialUnusable; any other error means the lookup itself failed
	// and says nothing about whether the credential is usable.
	DeviceCredential(ctx context.Context, credentialID uuid.UUID) (snmp.Credential, error)
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
	// SaveReachability stores one poll's outcome. It returns
	// ErrDeviceNotEnabled when the device was disabled between being queued
	// and finishing its poll (the database implementation returns it on an
	// update that touched 0 rows, since its WHERE clause requires
	// enabled = true).
	SaveReachability(ctx context.Context, deviceID uuid.UUID, u ReachabilityUpdate) error
	SaveInventory(ctx context.Context, deviceID uuid.UUID, inv snmp.Inventory, at time.Time) error
	SaveInventoryError(ctx context.Context, deviceID uuid.UUID, detail string, at time.Time) error
}

// DeviceIncidents opens and closes device incidents (IncidentService).
// OpenDeviceIncident is idempotent: called again while an incident is
// already open, it returns that one with opened=false — PollOnce relies on
// this to retry an open that failed to save on an earlier poll.
// CloseDeviceIncident returns (nil, nil) when nothing is open.
type DeviceIncidents interface {
	OpenDeviceIncident(ctx context.Context, deviceID uuid.UUID, start time.Time, reason string) (*models.Incident, bool, error)
	CloseDeviceIncident(ctx context.Context, deviceID uuid.UUID, end time.Time, note string) (*models.Incident, error)
}

// Notifier delivers alerts (NotificationManager).
type Notifier interface {
	SendNotification(ctx context.Context, m *notifications.NotificationMessage) error
}

// PortStatsPoller runs a device's stats poll (PortMonitor).
type PortStatsPoller interface {
	PollStats(ctx context.Context, d models.Device, t snmp.Target, uptimeSeconds int64)
}

// UPSPoller runs a UPS's poll (UPSMonitor).
type UPSPoller interface {
	PollUPS(ctx context.Context, d models.Device, t snmp.Target)
	// ReconcileUPS closes UPS incidents left on a device that is no longer a UPS.
	ReconcileUPS(ctx context.Context, d models.Device)
}

// DevicePoller polls every enabled device for reachability on a bounded
// worker pool, and refreshes inventory when it is due.
type DevicePoller struct {
	store     PollerStore
	client    snmp.Client
	incidents DeviceIncidents
	notifier  Notifier
	workers   int
	stats     PortStatsPoller
	ups       UPSPoller

	// Replaceable in tests.
	resolve func(ctx context.Context, host string) (net.IP, error)
	blocked func(net.IP) bool
	now     func() time.Time

	inFlight sync.Map // device id -> true while queued or polling
	logger   *log.Logger
}

// NewDevicePoller builds a poller; workers <= 0 means the default (16).
func NewDevicePoller(store PollerStore, client snmp.Client, incidents DeviceIncidents, notifier Notifier, workers int) *DevicePoller {
	if workers <= 0 {
		workers = defaultPollerWorkers
	}
	return &DevicePoller{
		store: store, client: client, incidents: incidents, notifier: notifier, workers: workers,
		resolve: resolveDeviceHost,
		blocked: netguard.IsBlocked,
		now:     time.Now,
		logger:  log.Default(),
	}
}

// resolveDeviceHost returns the first IPv4 address for host (or host itself
// if it is an IP). Resolved on every poll, so a DHCP-reserved name that moves
// is followed. Named distinctly from dns_resolve.go's resolveHost (the
// split-horizon-aware resolver certificate checks use): that one tries public
// resolvers first and returns every IP with the resolver that answered: more
// than an SNMP poll, itself always run from inside the network the device is
// on, needs.
func resolveDeviceHost(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if a.To4() != nil {
			return a, nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0], nil
	}
	return nil, fmt.Errorf("%s has no addresses", host)
}

// SetPortStats makes every successful poll of an up device run its stats
// poll too.
func (p *DevicePoller) SetPortStats(s PortStatsPoller) { p.stats = s }

// SetUPS makes every successful poll of an up device run its UPS poll when
// it is a UPS, and tidy up UPS incidents when it no longer is.
func (p *DevicePoller) SetUPS(u UPSPoller) { p.ups = u }

// TargetFor builds the SNMP target for a device at a resolved address.
func TargetFor(d models.Device, host string, cred snmp.Credential) snmp.Target {
	return snmp.Target{
		Host:       host,
		Port:       uint16(d.Port),
		Credential: cred,
		Timeout:    time.Duration(d.TimeoutMs) * time.Millisecond,
		Retries:    d.Retries,
	}
}

// Start runs the scheduler and workers until ctx is cancelled.
func (p *DevicePoller) Start(ctx context.Context) {
	jobs := make(chan models.Device, p.workers*4)
	for i := 0; i < p.workers; i++ {
		go func() {
			for d := range jobs {
				p.PollOnce(ctx, d)
				p.inFlight.Delete(d.ID)
			}
		}()
	}
	p.logger.Printf("[snmp] poller started with %d workers", p.workers)
	t := time.NewTicker(pollerTick)
	defer t.Stop()
	defer close(jobs)
	for {
		select {
		case <-ctx.Done():
			p.logger.Println("[snmp] poller stopped")
			return
		case <-t.C:
			p.Dispatch(ctx, jobs)
		}
	}
}

// Dispatch queues every due device not already in flight, without blocking:
// if the pool is saturated the rest wait for the next tick. Returns how many
// were queued.
func (p *DevicePoller) Dispatch(ctx context.Context, jobs chan<- models.Device) int {
	due, err := p.store.DueDevices(ctx, p.now(), pollerDueBatch)
	if err != nil {
		p.logger.Printf("[snmp] listing due devices: %v", err)
		return 0
	}
	queued := 0
	for _, d := range due {
		if _, busy := p.inFlight.LoadOrStore(d.ID, true); busy {
			continue
		}
		select {
		case jobs <- d:
			queued++
		default:
			p.inFlight.Delete(d.ID)
		}
	}
	return queued
}

// PollOnce polls one device, stores the outcome, applies the state machine's
// actions, and refreshes inventory when it is due.
func (p *DevicePoller) PollOnce(ctx context.Context, d models.Device) {
	now := p.now()
	result, detail := PollOK, ""
	var target snmp.Target
	var uptime *int64

	cred, err := p.store.DeviceCredential(ctx, d.CredentialID)
	switch {
	case err != nil && errors.Is(err, ErrCredentialUnusable):
		result, detail = PollBlocked, "credential profile unavailable: "+err.Error()
	case err != nil:
		// Not a verdict about the device at all (a database hiccup, a
		// cancelled context): nothing to save, nothing to act on.
		p.logger.Printf("[snmp] looking up credential for %s: %v", d.Host, err)
		return
	default:
		if ip, err := p.resolve(ctx, d.Host); err != nil {
			result, detail = PollBlocked, "cannot resolve host: "+err.Error()
		} else if p.blocked(ip) {
			result, detail = PollBlocked, fmt.Sprintf("%s is blocked by network policy (ALLOW_PRIVATE_NETWORK_TARGETS)", ip)
		} else {
			target = TargetFor(d, ip.String(), cred)
			pdus, err := p.client.Get(ctx, target, []string{snmp.OIDSysUpTime})
			if err != nil {
				result, detail = PollFailed, err.Error()
			} else if len(pdus) == 1 {
				if n, ok := pdus[0].Number(); ok {
					s := int64(n / 100)
					uptime = &s
				}
			}
		}
	}

	tr := NextDeviceState(d.Status, d.ConsecutiveFailures, result)
	update := ReachabilityUpdate{Status: tr.Status, Failures: tr.Failures, Detail: detail, PolledAt: now, UptimeSeconds: uptime}
	if result == PollOK {
		update.SeenAt = &now
	}
	if err := p.store.SaveReachability(ctx, d.ID, update); err != nil {
		if errors.Is(err, ErrDeviceNotEnabled) {
			return
		}
		p.logger.Printf("[snmp] saving poll of %s: %v", d.Host, err)
		return
	}
	p.apply(ctx, d, tr, detail, now)

	if result == PollOK && tr.Status == models.DeviceStatusUp && p.stats != nil {
		up := int64(-1)
		if uptime != nil {
			up = *uptime
		}
		p.stats.PollStats(ctx, d, target, up)
	}

	if result == PollOK && tr.Status == models.DeviceStatusUp && p.ups != nil {
		if IsUPS(d) {
			p.ups.PollUPS(ctx, d, target)
		} else {
			p.ups.ReconcileUPS(ctx, d)
		}
	}

	if result == PollOK && (d.LastInventoryAt == nil || now.Sub(*d.LastInventoryAt) >= inventoryInterval) {
		inv, err := snmp.ReadInventory(ctx, p.client, target)
		if err != nil {
			_ = p.store.SaveInventoryError(ctx, d.ID, "inventory: "+err.Error(), now)
			return
		}
		if err := p.store.SaveInventory(ctx, d.ID, inv, now); err != nil {
			p.logger.Printf("[snmp] saving inventory of %s: %v", d.Host, err)
		}
	}
}

func displayName(d models.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Host
}

func (p *DevicePoller) apply(ctx context.Context, d models.Device, tr DeviceTransition, detail string, now time.Time) {
	actions := tr.Actions
	var incident *models.Incident
	// Whether this poll's own ActionOpenIncident (if any) actually opened the
	// incident. The threshold-crossing transition pairs ActionOpenIncident
	// with ActionNotifyDown in the same actions slice; gating the notify on
	// this the same way the retry path below gates its own (opened == true
	// and err == nil) stops a down alert from going out for an open that
	// never landed - the retry path would otherwise send its own down alert
	// for the same crossing once the open actually succeeds, doubling it up.
	openedHere := true
	for _, a := range actions {
		var err error
		switch a {
		case ActionOpenIncident:
			var opened bool
			incident, opened, err = p.incidents.OpenDeviceIncident(ctx, d.ID, now, detail)
			openedHere = opened && err == nil
		case ActionCloseIncident:
			note := ""
			if len(actions) == 1 { // closed because blocked or paused, not recovered
				note = "Closed without recovery: " + detail
				if detail == "" {
					note = "Monitoring was paused."
				}
			}
			incident, err = p.incidents.CloseDeviceIncident(ctx, d.ID, now, note)
		case ActionNotifyDown, ActionNotifyRecovered:
			if a == ActionNotifyDown && !openedHere {
				continue
			}
			p.notify(ctx, d, a, incident, now)
		}
		if err != nil {
			p.logger.Printf("[snmp] %s for %s: %v", a, d.Host, err)
		}
	}

	// A device that stays down keeps making sure its incident is actually
	// open. OpenDeviceIncident is idempotent, so on every poll that finds the
	// device still down without having just opened one above (i.e. every
	// poll after the first that crossed the threshold), this is a no-op once
	// the incident exists — and the moment it isn't (the earlier open, from
	// the poll that first crossed the threshold, failed to save), it opens
	// one now and sends the down notification that poll should have sent.
	if tr.Status == models.DeviceStatusDown && !containsAction(actions, ActionOpenIncident) {
		retried, opened, err := p.incidents.OpenDeviceIncident(ctx, d.ID, now, detail)
		if err != nil {
			p.logger.Printf("[snmp] retrying incident open for %s: %v", d.Host, err)
		} else if opened {
			p.notify(ctx, d, ActionNotifyDown, retried, now)
		}
	}
}

func containsAction(actions []DeviceAction, want DeviceAction) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

func (p *DevicePoller) notify(ctx context.Context, d models.Device, a DeviceAction, incident *models.Incident, now time.Time) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	site, err := p.store.SiteName(ctx, d.SiteID)
	if err != nil {
		site = "its site"
	}
	id := d.ID
	m := &notifications.NotificationMessage{
		DeviceID:    &id,
		SiteName:    site,
		MonitorName: displayName(d),
		MonitorURL:  d.Host,
		Timestamp:   now,
	}
	if d.NotifyChannels != nil {
		m.Channels = []string(d.NotifyChannels)
	}
	if incident != nil {
		m.IncidentID = &incident.ID
	}
	if a == ActionNotifyDown {
		last := "never"
		if d.LastSeenAt != nil {
			last = d.LastSeenAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		m.Status, m.PreviousStatus = "down", d.Status
		m.Message = fmt.Sprintf("%s at %s (%s) has stopped answering SNMP. Last answered %s.", displayName(d), site, d.Host, last)
	} else {
		m.Status, m.PreviousStatus = "recovered", models.DeviceStatusDown
		// CloseDeviceIncident returning nil (nothing was open, or the close
		// itself errored) doesn't mean the device was never down — fall back
		// to the device's own record of when it was last seen, rather than
		// reporting "less than a minute" for what could be a long outage.
		dur := time.Duration(0)
		switch {
		case incident != nil:
			dur = time.Duration(incident.DurationSeconds) * time.Second
		case d.LastSeenAt != nil:
			dur = now.Sub(*d.LastSeenAt)
		}
		m.DowntimeDuration = dur
		m.Message = fmt.Sprintf("%s at %s is answering again after %s.", displayName(d), site, humanDuration(dur))
	}
	if err := p.notifier.SendNotification(ctx, m); err != nil {
		p.logger.Printf("[snmp] sending %s notification for %s: %v", m.Status, d.Host, err)
	}
}

// humanDuration renders "12 minutes", "1 minute", "3 hours 5 minutes", "2 days 1 hour".
func humanDuration(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	switch {
	case mins < 1:
		return "less than a minute"
	case mins < 60:
		return plural(mins, "minute")
	case mins < 24*60:
		if m := mins % 60; m > 0 {
			return plural(mins/60, "hour") + " " + plural(m, "minute")
		}
		return plural(mins/60, "hour")
	default:
		if h := (mins / 60) % 24; h > 0 {
			return plural(mins/(24*60), "day") + " " + plural(h, "hour")
		}
		return plural(mins/(24*60), "day")
	}
}

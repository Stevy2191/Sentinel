package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	// fastInterval is how often a watched map's linked ports are read.
	fastInterval = 10 * time.Second
	// idleStop: polling for a site stops this long after its last viewer.
	idleStop = 60 * time.Second
	// maxPortsPerDevice caps one device's fast poll.
	maxPortsPerDevice = 64
	// fastWorkers is how many devices are fast-polled at once.
	fastWorkers = 4
	// boundTTL is how long a site's linked ports are cached between reads
	// of the map.
	boundTTL = time.Minute
	// tickDeadline bounds one tick, so a device that stops answering can't
	// hold up the next one (or any other map's).
	tickDeadline = 8 * time.Second
)

// LinkRate is one link's traffic, from its rate end's device: In is what
// arrives at that device over the link, Out what leaves it. At is when the
// figures were read; nil when there are none.
type LinkRate struct {
	InBps  *float64   `json:"in_bps"`
	OutBps *float64   `json:"out_bps"`
	OperUp *bool      `json:"oper_up"`
	At     *time.Time `json:"at"`
}

// MapTraffic is a site map's latest traffic: by link key, and the status of
// each device the fast poll looked at.
type MapTraffic struct {
	SiteID  uuid.UUID            `json:"site_id"`
	At      time.Time            `json:"at"`
	Links   map[string]LinkRate  `json:"links"`
	Devices map[uuid.UUID]string `json:"devices"`
}

// LinkPollerDeps is what the fast poll needs from the rest of Sentinel.
type LinkPollerDeps struct {
	Maps interface {
		// BoundPorts is the ports at the map's link ends, and every device
		// drawn on the map.
		BoundPorts(ctx context.Context, siteID uuid.UUID) ([]BoundPort, []uuid.UUID, error)
	}
	Devices interface {
		Get(ctx context.Context, id uuid.UUID) (*DeviceView, error)
	}
	Targets interface {
		Target(ctx context.Context, d models.Device) (snmp.Target, error)
	}
	HC interface {
		HC(deviceID uuid.UUID) (hc, known bool)
	}
	Live interface {
		LivePorts(ctx context.Context, interfaceIDs []uuid.UUID) (map[uuid.UUID]LivePort, error)
	}
	Client  snmp.Client
	Publish func(siteID uuid.UUID, t MapTraffic)
}

type portRef struct {
	device  uuid.UUID
	ifIndex int
}

type boundCache struct {
	at      time.Time
	ports   []BoundPort
	devices []uuid.UUID
}

// LinkPoller reads the ports at the ends of a site map's links every 10 s,
// but only while someone is watching that map.
type LinkPoller struct {
	d           LinkPollerDeps
	now         func() time.Time
	logger      *log.Logger
	tickTimeout time.Duration
	sem         chan struct{} // fast polls in flight, across every map

	mu       sync.Mutex
	watchers map[uuid.UUID]int
	lastSeen map[uuid.UUID]time.Time // the last moment the site was watched
	readings map[portRef]*portmon.Reading
	rates    map[portRef]LinkRate
	latest   map[uuid.UUID]MapTraffic
	bound    map[uuid.UUID]boundCache
	busy     map[uuid.UUID]bool // devices being polled right now
}

func NewLinkPoller(d LinkPollerDeps) *LinkPoller {
	return &LinkPoller{d: d, now: time.Now, logger: log.Default(), tickTimeout: tickDeadline, sem: make(chan struct{}, fastWorkers),
		watchers: map[uuid.UUID]int{}, lastSeen: map[uuid.UUID]time.Time{},
		readings: map[portRef]*portmon.Reading{}, rates: map[portRef]LinkRate{}, latest: map[uuid.UUID]MapTraffic{},
		bound: map[uuid.UUID]boundCache{}, busy: map[uuid.UUID]bool{}}
}

// SetPublish sets where each tick's traffic goes (the map streams), for
// wiring that builds the streams after the poller.
func (p *LinkPoller) SetPublish(f func(siteID uuid.UUID, t MapTraffic)) { p.d.Publish = f }

// Watch counts a viewer of the site (an open stream) until release is called.
func (p *LinkPoller) Watch(siteID uuid.UUID) (release func()) {
	p.mu.Lock()
	p.watchers[siteID]++
	p.lastSeen[siteID] = p.now()
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.watchers[siteID]--; p.watchers[siteID] <= 0 {
				delete(p.watchers, siteID)
			}
			p.lastSeen[siteID] = p.now()
		})
	}
}

// Touch counts as watching the site for the next 60 s (a widget or public
// map asking for data).
func (p *LinkPoller) Touch(siteID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSeen[siteID] = p.now()
}

// watchedSites is every site with a viewer now or within the last 60 s.
func (p *LinkPoller) watchedSites(now time.Time) []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []uuid.UUID
	for site, at := range p.lastSeen {
		switch {
		case p.watchers[site] > 0:
			p.lastSeen[site] = now
			out = append(out, site)
		case now.Sub(at) <= idleStop:
			out = append(out, site)
		default:
			delete(p.lastSeen, site)
		}
	}
	return out
}

// Run ticks every 10 s until ctx ends.
func (p *LinkPoller) Run(ctx context.Context) {
	t := time.NewTicker(fastInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Tick(ctx)
		}
	}
}

func (p *LinkPoller) boundPorts(ctx context.Context, siteID uuid.UUID, now time.Time) ([]BoundPort, []uuid.UUID) {
	p.mu.Lock()
	c, ok := p.bound[siteID]
	p.mu.Unlock()
	if ok && now.Sub(c.at) < boundTTL {
		return c.ports, c.devices
	}
	ports, devices, err := p.d.Maps.BoundPorts(ctx, siteID)
	if err != nil {
		p.logger.Printf("[sitemap] linked ports of site %s: %v", siteID, err)
		return c.ports, c.devices
	}
	p.mu.Lock()
	p.bound[siteID] = boundCache{at: now, ports: ports, devices: devices}
	p.mu.Unlock()
	return ports, devices
}

// Tick polls every watched site once, side by side and within a deadline,
// and publishes each one's traffic.
func (p *LinkPoller) Tick(ctx context.Context) {
	now := p.now()
	ctx, cancel := context.WithTimeout(ctx, p.tickTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, site := range p.watchedSites(now) {
		wg.Add(1)
		go func(site uuid.UUID) {
			defer wg.Done()
			p.tickSite(ctx, site, now)
		}(site)
	}
	wg.Wait()
}

func (p *LinkPoller) tickSite(ctx context.Context, site uuid.UUID, now time.Time) {
	{
		ports, mapDevices := p.boundPorts(ctx, site, now)
		byDevice := map[uuid.UUID][]int{}
		for _, bp := range ports {
			byDevice[bp.DeviceID] = append(byDevice[bp.DeviceID], bp.IfIndex)
		}
		statuses := map[uuid.UUID]string{}
		var smu sync.Mutex
		var wg sync.WaitGroup
		for dev, indexes := range byDevice {
			if !p.claim(dev) {
				continue
			}
			wg.Add(1)
			go func(dev uuid.UUID, indexes []int) {
				defer func() { p.release(dev); wg.Done() }()
				select {
				case p.sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-p.sem }()
				if status := p.pollDevice(ctx, dev, indexes, now); status != "" {
					smu.Lock()
					statuses[dev] = status
					smu.Unlock()
				}
			}(dev, indexes)
		}
		wg.Wait()
		// Every device on the map gets its status, measured end or not: the
		// map is how people see what's down.
		for _, dev := range mapDevices {
			if _, ok := statuses[dev]; ok {
				continue
			}
			if d, err := p.d.Devices.Get(ctx, dev); err == nil {
				statuses[dev] = d.Status
			}
		}
		t := MapTraffic{SiteID: site, At: now.UTC(), Links: map[string]LinkRate{}, Devices: statuses}
		p.mu.Lock()
		for _, bp := range ports {
			if r, ok := p.rates[portRef{bp.DeviceID, bp.IfIndex}]; ok {
				t.Links[bp.LinkKey] = r
			}
		}
		p.latest[site] = t
		p.mu.Unlock()
		if p.d.Publish != nil {
			p.d.Publish(site, t)
		}
	}
}

func (p *LinkPoller) claim(dev uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy[dev] {
		return false
	}
	p.busy[dev] = true
	return true
}

func (p *LinkPoller) release(dev uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.busy, dev)
}

// pollDevice reads one device's linked ports and returns its status (empty
// when the device is gone). Only a device that is enabled, up and not
// failing is read, and only once the regular poll knows whether it has
// 64-bit counters.
func (p *LinkPoller) pollDevice(ctx context.Context, dev uuid.UUID, indexes []int, now time.Time) string {
	d, err := p.d.Devices.Get(ctx, dev)
	if err != nil {
		return ""
	}
	if !d.Enabled || d.Status != models.DeviceStatusUp || d.ConsecutiveFailures > 0 {
		p.clearDevice(dev)
		return d.Status
	}
	target, err := p.d.Targets.Target(ctx, d.Device)
	if err != nil {
		return d.Status
	}
	hc := false
	if target.Credential.Version != models.SNMPVersion1 {
		var known bool
		if hc, known = p.d.HC.HC(dev); !known {
			return d.Status
		}
	}
	if len(indexes) > maxPortsPerDevice {
		indexes = indexes[:maxPortsPerDevice]
	}
	uptime := int64(-1)
	pdus, err := p.d.Client.Get(ctx, target, []string{snmp.OIDSysUpTime})
	var refused *snmp.AgentError
	switch {
	case errors.As(err, &refused):
		// It answered, it just won't give its uptime: read the ports anyway.
	case err != nil:
		// Not answering: don't spend the tick on its counters too.
		return d.Status
	case len(pdus) == 1:
		if n, ok := pdus[0].Number(); ok {
			uptime = int64(n / 100)
		}
	}
	stats, err := snmp.ReadStats(ctx, p.d.Client, target, indexes, hc)
	if err != nil {
		p.logger.Printf("[sitemap] fast poll of %s: %v", d.Host, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ix := range indexes {
		st, ok := stats[ix]
		if !ok || !st.HaveStatus {
			continue
		}
		key := portRef{dev, ix}
		up := st.OperUp
		at := now.UTC()
		rate := LinkRate{OperUp: &up, At: &at}
		if st.HaveOctets {
			cur := portmon.Reading{At: now, UptimeSeconds: uptime, HC: st.HC, InOctets: st.InOctets, OutOctets: st.OutOctets,
				InErrors: st.InErrors, OutErrors: st.OutErrors, InDiscards: st.InDiscards, OutDiscards: st.OutDiscards, SpeedBps: st.SpeedBps}
			if r, skip := portmon.ComputeRates(p.readings[key], cur, fastInterval); skip == portmon.SkipNone && up {
				in, out := r.InBps, r.OutBps
				rate.InBps, rate.OutBps = &in, &out
			} else if prev, ok := p.rates[key]; ok && up {
				rate.InBps, rate.OutBps = prev.InBps, prev.OutBps
			}
			p.readings[key] = &cur
		}
		p.rates[key] = rate
	}
	return d.Status
}

// clearDevice forgets a device's readings, so its first rates after it comes
// back are not computed across the gap.
func (p *LinkPoller) clearDevice(dev uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k := range p.readings {
		if k.device == dev {
			delete(p.readings, k)
			delete(p.rates, k)
		}
	}
}

// Latest is the site's last fast-polled traffic, or, before any, the stored
// rates the regular poll wrote.
func (p *LinkPoller) Latest(ctx context.Context, siteID uuid.UUID) MapTraffic {
	p.mu.Lock()
	t, ok := p.latest[siteID]
	p.mu.Unlock()
	if ok {
		return t
	}
	now := p.now()
	out := MapTraffic{SiteID: siteID, At: now.UTC(), Links: map[string]LinkRate{}, Devices: map[uuid.UUID]string{}}
	ports, _ := p.boundPorts(ctx, siteID, now)
	ids := make([]uuid.UUID, 0, len(ports))
	for _, bp := range ports {
		ids = append(ids, bp.InterfaceID)
	}
	live, err := p.d.Live.LivePorts(ctx, ids)
	if err != nil {
		p.logger.Printf("[sitemap] stored rates for site %s: %v", siteID, err)
		return out
	}
	for _, bp := range ports {
		lp, ok := live[bp.InterfaceID]
		if !ok {
			continue
		}
		up := lp.OperStatus == "up"
		r := LinkRate{InBps: lp.InBps, OutBps: lp.OutBps, OperUp: &up}
		if lp.InBps != nil || lp.OutBps != nil {
			at := now.UTC()
			r.At = &at
		}
		out.Links[bp.LinkKey] = r
	}
	return out
}

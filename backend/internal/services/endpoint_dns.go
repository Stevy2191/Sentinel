package services

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Resolver is the DNS the endpoint records use: reverse lookups for names,
// forward lookups when a search is for a hostname. net.DefaultResolver is
// one.
type Resolver interface {
	LookupAddr(ctx context.Context, ip string) ([]string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

const (
	dnsTimeout  = 2 * time.Second
	dnsCacheTTL = 24 * time.Hour
	dnsWorkers  = 8
	dnsQueueLen = 1024
)

// dnsCache resolves endpoint IPs to names in the background, at most
// dnsWorkers at a time, into l2.dns_names. Reads never wait for it: a name
// found appears on the next read.
type dnsCache struct {
	db       *gorm.DB
	resolver Resolver
	logger   *log.Logger

	start   sync.Once
	queue   chan string
	pending sync.Map // ip -> true while queued or resolving
	wg      sync.WaitGroup
}

func newDNSCache(db *gorm.DB, r Resolver) *dnsCache {
	return &dnsCache{db: db, resolver: r, logger: log.New(os.Stderr, "", log.LstdFlags), queue: make(chan string, dnsQueueLen)}
}

// want queues lookups for ips; ones already queued are skipped, and so are
// any that do not fit the queue (the next read asks again).
func (c *dnsCache) want(ips []string) {
	c.start.Do(func() {
		for i := 0; i < dnsWorkers; i++ {
			go c.work()
		}
	})
	for _, ip := range ips {
		if _, busy := c.pending.LoadOrStore(ip, true); busy {
			continue
		}
		c.wg.Add(1)
		select {
		case c.queue <- ip:
		default:
			c.pending.Delete(ip)
			c.wg.Done()
		}
	}
}

func (c *dnsCache) work() {
	for ip := range c.queue {
		c.lookup(ip)
		c.pending.Delete(ip)
		c.wg.Done()
	}
}

// lookup stores ip's name, or "" when it has none, so it is not asked
// again for a day. A timeout stores nothing: the next read retries.
func (c *dnsCache) lookup(ip string) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	names, err := c.resolver.LookupAddr(ctx, ip)
	var dnsErr *net.DNSError
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &dnsErr) && (dnsErr.IsTimeout || dnsErr.IsTemporary))) {
		return
	}
	name := ""
	if err == nil && len(names) > 0 {
		name = strings.TrimSuffix(names[0], ".")
	}
	if err := c.db.Exec(`INSERT INTO l2.dns_names (ip, name, resolved_at) VALUES (?, ?, now())
		ON CONFLICT (ip) DO UPDATE SET name = EXCLUDED.name, resolved_at = EXCLUDED.resolved_at`, ip, name).Error; err != nil {
		c.logger.Printf("[l2] saving the DNS name of %s: %v", ip, err)
	}
}

// waitDNS waits for the queued lookups (tests).
func (s *EndpointService) waitDNS() { s.dns.wg.Wait() }

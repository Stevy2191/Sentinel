package services

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// The resolvers certificate checks consult, in order.
//
// Public DNS is asked first so a check sees what a visitor on the internet
// sees. The case this exists for is split-horizon DNS: where an Active
// Directory domain shares its name with the public one, the local resolver
// answers for the apex with a domain controller, and the check reads the wrong
// host's certificate — or none at all — while the www subdomain works.
//
// The system resolver is still consulted last, so a genuinely internal name
// that public DNS cannot know about continues to work.
var certResolvers = []struct {
	name string
	addr string // empty means the host's own resolver
}{
	{"Google Public DNS", "8.8.8.8:53"},
	{"Cloudflare DNS", "1.1.1.1:53"},
	{"system DNS", ""},
}

// resolverTimeout bounds one resolver. Three are tried, so a host that no
// resolver knows costs at most three times this before the check gives up.
const resolverTimeout = 5 * time.Second

// resolvedHost is the outcome of a successful lookup.
type resolvedHost struct {
	// IPs in the order they should be tried. Never empty on success.
	IPs []string
	// Via names the resolver that answered, for the error message and the log.
	Via string
}

// resolveHost looks a hostname up, trying each resolver in turn.
//
// An address literal is returned as-is: there is nothing to resolve, and
// asking a resolver about an IP fails.
func resolveHost(ctx context.Context, host string) (*resolvedHost, error) {
	if ip := net.ParseIP(host); ip != nil {
		return &resolvedHost{IPs: []string{host}, Via: "literal address"}, nil
	}

	// Queried as an absolute name. Without the trailing dot the resolver
	// applies the local search list, so a name that does not exist is retried
	// as name.<search-domain> — and where that search domain has a wildcard,
	// it resolves. A certificate check would then read a completely different
	// host's certificate and report it under the watched domain's name. The
	// name entered is the name to check, exactly.
	query := host
	if !strings.HasSuffix(query, ".") {
		query += "."
	}

	var lastErr error
	for _, r := range certResolvers {
		// Each resolver gets its own budget, so one that blackholes queries
		// cannot consume the time the next one needs.
		attemptCtx, cancel := context.WithTimeout(ctx, resolverTimeout)
		ips, err := newResolver(r.addr).LookupHost(attemptCtx, query)
		cancel()

		if err == nil && len(ips) > 0 {
			return &resolvedHost{IPs: orderIPs(ips), Via: r.name}, nil
		}
		if err != nil {
			lastErr = err
		}
		// A caller that has given up should not wait on the remaining
		// resolvers.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("resolving %s: %w", host, ctx.Err())
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("could not resolve %s with public or system DNS: %w", host, lastErr)
	}
	return nil, fmt.Errorf("could not resolve %s with public or system DNS", host)
}

// newResolver returns a resolver that queries addr, or the host's own when
// addr is empty.
func newResolver(addr string) *net.Resolver {
	if addr == "" {
		return net.DefaultResolver
	}
	return &net.Resolver{
		// Required: the cgo resolver ignores a custom Dial and would silently
		// use the host's configuration instead.
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			// network is passed through rather than forced to udp, so a
			// truncated answer can still be retried over tcp.
			return (&net.Dialer{Timeout: resolverTimeout}).DialContext(ctx, network, addr)
		},
	}
}

// orderIPs puts IPv4 addresses first.
//
// Only the first address that connects is used, and many container networks
// have no IPv6 route at all. Leading with an AAAA record there means every
// check pays a connection timeout before falling back, so the family more
// likely to work is tried first.
func orderIPs(ips []string) []string {
	v4 := make([]string, 0, len(ips))
	v6 := make([]string, 0, len(ips))
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
			v6 = append(v6, ip)
			continue
		}
		v4 = append(v4, ip)
	}
	return append(v4, v6...)
}

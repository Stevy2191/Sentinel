//go:build !windows

package nettools

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// SystemResolver returns "ip:port" of the host's first IPv4 DNS server: the
// first IPv4 nameserver in /etc/resolv.conf (inside a Docker container, the
// embedded DNS at 127.0.0.11).
func SystemResolver() (string, error) {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("reading /etc/resolv.conf: %w", err)
	}
	defer f.Close()
	return parseResolvConf(f)
}

// parseResolvConf finds the first IPv4 "nameserver" line; IPv6 servers are
// skipped.
func parseResolvConf(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if ip := net.ParseIP(fields[1]); ip != nil && ip.To4() != nil && !strings.Contains(fields[1], ":") {
			return net.JoinHostPort(ip.To4().String(), "53"), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading /etc/resolv.conf: %w", err)
	}
	return "", errors.New("no IPv4 DNS server in /etc/resolv.conf")
}

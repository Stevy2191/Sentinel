package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ErrTargetBlocked is returned for an address the network policy forbids.
var ErrTargetBlocked = errors.New("that address is blocked by network policy")

// Prober reads a device's identity on demand: Test connection and subnet
// scans. It applies the same network policy as the poller.
type Prober struct {
	creds   *SNMPCredentialService
	client  snmp.Client
	resolve func(ctx context.Context, host string) (net.IP, error)
	blocked func(net.IP) bool
}

// NewProber uses resolveDeviceHost (device_poller.go), the same resolver the
// poller uses: resolved fresh on every call, following a DHCP-reserved name
// that moves, rather than dns_resolve.go's split-horizon-aware resolveHost,
// which is built for certificate checks run from outside the network a
// device is on.
func NewProber(creds *SNMPCredentialService, client snmp.Client) *Prober {
	return &Prober{creds: creds, client: client, resolve: resolveDeviceHost, blocked: netguard.IsBlocked}
}

// Identify reads the system group using a stored credential profile.
func (p *Prober) Identify(ctx context.Context, host string, port int, credentialID uuid.UUID, timeout time.Duration, retries int) (snmp.System, error) {
	cred, err := p.creds.Decrypted(ctx, credentialID)
	if err != nil {
		return snmp.System{}, err
	}
	return p.IdentifyWith(ctx, host, port, cred, timeout, retries)
}

// IdentifyWith reads the system group with an already-decrypted credential.
func (p *Prober) IdentifyWith(ctx context.Context, host string, port int, cred snmp.Credential, timeout time.Duration, retries int) (snmp.System, error) {
	ip, err := p.resolve(ctx, host)
	if err != nil {
		return snmp.System{}, fmt.Errorf("cannot resolve %s: %v", host, err)
	}
	if p.blocked(ip) {
		return snmp.System{}, ErrTargetBlocked
	}
	return snmp.Identify(ctx, p.client, snmp.Target{
		Host: ip.String(), Port: uint16(port), Credential: cred, Timeout: timeout, Retries: retries,
	})
}

// UsableAt reports whether a credential profile may be used in a site, so Test
// connection cannot borrow another site's profile.
func (p *Prober) UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error) {
	return p.creds.UsableAt(ctx, credentialID, siteID)
}

// TargetFor builds the SNMP target for a device the way the poller does
// (device_poller.go's TargetFor): its credential decrypted, its host
// resolved fresh and checked against network policy, at its configured
// port, timeout and retries.
func (p *Prober) TargetFor(ctx context.Context, d models.Device) (snmp.Target, error) {
	cred, err := p.creds.Decrypted(ctx, d.CredentialID)
	if err != nil {
		return snmp.Target{}, err
	}
	ip, err := p.resolve(ctx, d.Host)
	if err != nil {
		return snmp.Target{}, fmt.Errorf("cannot resolve %s: %v", d.Host, err)
	}
	if p.blocked(ip) {
		return snmp.Target{}, ErrTargetBlocked
	}
	return TargetFor(d, ip.String(), cred), nil
}

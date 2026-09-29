// Package snmp talks SNMP v1/v2c/v3 through gosnmp and interprets the
// answers. Everything that interprets is pure and tested without a network;
// the network lives behind Client.
package snmp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Credential is a decrypted credential profile, held in memory only.
type Credential struct {
	Version      string // "1", "2c" or "3"
	Community    string
	Username     string
	AuthProtocol string
	AuthPassword string
	PrivProtocol string
	PrivPassword string
}

// Target is one device to talk to.
type Target struct {
	Host       string
	Port       uint16
	Credential Credential
	Timeout    time.Duration
	Retries    int
}

// PDU is one variable binding. OID has no leading dot. Value is []byte,
// int64, uint64, string (OID or IP address) or nil (no such object/instance,
// end of MIB view, NULL).
type PDU struct {
	OID   string
	Value any
}

// Text renders the value as a string.
func (p PDU) Text() string {
	switch v := p.Value.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return fmt.Sprint(v)
	case uint64:
		return fmt.Sprint(v)
	default:
		return ""
	}
}

// Number returns a numeric value (negative INTEGERs are not numbers here).
func (p PDU) Number() (uint64, bool) {
	switch v := p.Value.(type) {
	case uint64:
		return v, true
	case int64:
		if v >= 0 {
			return uint64(v), true
		}
	}
	return 0, false
}

// Client is the network side. GoSNMPClient is the real one; tests use fakes.
type Client interface {
	Get(ctx context.Context, t Target, oids []string) ([]PDU, error)
	Walk(ctx context.Context, t Target, root string) ([]PDU, error)
}

var authProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	"none": gosnmp.NoAuth, "MD5": gosnmp.MD5, "SHA": gosnmp.SHA, "SHA224": gosnmp.SHA224,
	"SHA256": gosnmp.SHA256, "SHA384": gosnmp.SHA384, "SHA512": gosnmp.SHA512,
}

// AES192/AES256 map to gosnmp's Blumenthal key extension (RFC draft used by
// Net-SNMP). Some vendors (notably Cisco) use the Reeder extension instead
// (gosnmp.AES192C/AES256C); if a device answers snmpwalk with AES-192/256 but
// not Sentinel, that is why, and a "C" variant is the fix.
var privProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	"none": gosnmp.NoPriv, "DES": gosnmp.DES, "AES": gosnmp.AES, "AES192": gosnmp.AES192, "AES256": gosnmp.AES256,
}

func buildGoSNMP(t Target) (*gosnmp.GoSNMP, error) {
	g := &gosnmp.GoSNMP{
		Target:         t.Host,
		Port:           t.Port,
		Timeout:        t.Timeout,
		Retries:        t.Retries,
		MaxOids:        gosnmp.MaxOids,
		MaxRepetitions: 25,
	}
	c := t.Credential
	switch c.Version {
	case "1":
		g.Version, g.Community = gosnmp.Version1, c.Community
	case "2c":
		g.Version, g.Community = gosnmp.Version2c, c.Community
	case "3":
		auth, ok := authProtocols[c.AuthProtocol]
		if !ok {
			return nil, fmt.Errorf("unknown auth protocol %q", c.AuthProtocol)
		}
		priv, ok := privProtocols[c.PrivProtocol]
		if !ok {
			return nil, fmt.Errorf("unknown privacy protocol %q", c.PrivProtocol)
		}
		usm := &gosnmp.UsmSecurityParameters{UserName: c.Username, AuthenticationProtocol: auth, PrivacyProtocol: priv}
		flags := gosnmp.NoAuthNoPriv
		if auth != gosnmp.NoAuth {
			usm.AuthenticationPassphrase = c.AuthPassword
			flags = gosnmp.AuthNoPriv
		}
		if priv != gosnmp.NoPriv {
			usm.PrivacyPassphrase = c.PrivPassword
			flags = gosnmp.AuthPriv
		}
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		g.MsgFlags = flags
		g.SecurityParameters = usm
	default:
		return nil, fmt.Errorf("unknown SNMP version %q", c.Version)
	}
	return g, nil
}

func toPDU(v gosnmp.SnmpPDU) PDU {
	p := PDU{OID: strings.TrimPrefix(v.Name, ".")}
	switch v.Type {
	case gosnmp.OctetString, gosnmp.Opaque:
		if b, ok := v.Value.([]byte); ok {
			p.Value = b
		}
	case gosnmp.ObjectIdentifier:
		if s, ok := v.Value.(string); ok {
			p.Value = strings.TrimPrefix(s, ".")
		}
	case gosnmp.IPAddress:
		if s, ok := v.Value.(string); ok {
			p.Value = s
		}
	case gosnmp.Integer:
		p.Value = gosnmp.ToBigInt(v.Value).Int64()
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
		p.Value = gosnmp.ToBigInt(v.Value).Uint64()
	default: // NoSuchObject, NoSuchInstance, EndOfMibView, Null
		p.Value = nil
	}
	return p
}

// GoSNMPClient is the real Client. It opens a UDP socket per call; a poll is
// one or a handful of requests, so pooling sockets is not worth its state.
type GoSNMPClient struct{}

func (GoSNMPClient) connect(ctx context.Context, t Target) (*gosnmp.GoSNMP, error) {
	g, err := buildGoSNMP(t)
	if err != nil {
		return nil, err
	}
	g.Context = ctx
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", t.Host, err)
	}
	return g, nil
}

// Get fetches the given OIDs in one request.
func (c GoSNMPClient) Get(ctx context.Context, t Target, oids []string) ([]PDU, error) {
	g, err := c.connect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer g.Conn.Close()
	pkt, err := g.Get(oids)
	if err != nil {
		return nil, err
	}
	if pkt.Error != gosnmp.NoError {
		return nil, fmt.Errorf("agent returned %s", pkt.Error)
	}
	out := make([]PDU, 0, len(pkt.Variables))
	for _, v := range pkt.Variables {
		out = append(out, toPDU(v))
	}
	return out, nil
}

// Walk returns every binding under root: GETBULK on v2c/v3, GETNEXT on v1
// (which has no GETBULK).
func (c GoSNMPClient) Walk(ctx context.Context, t Target, root string) ([]PDU, error) {
	g, err := c.connect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer g.Conn.Close()
	var vars []gosnmp.SnmpPDU
	if g.Version == gosnmp.Version1 {
		vars, err = g.WalkAll(root)
	} else {
		vars, err = g.BulkWalkAll(root)
	}
	if err != nil {
		return nil, err
	}
	out := make([]PDU, 0, len(vars))
	for _, v := range vars {
		out = append(out, toPDU(v))
	}
	return out, nil
}

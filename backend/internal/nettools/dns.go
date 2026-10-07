package nettools

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// typeCAA is not among dnsmessage's named types.
const typeCAA dnsmessage.Type = 257

var dnsTypes = map[string]dnsmessage.Type{
	"A": dnsmessage.TypeA, "AAAA": dnsmessage.TypeAAAA, "CNAME": dnsmessage.TypeCNAME,
	"MX": dnsmessage.TypeMX, "NS": dnsmessage.TypeNS, "TXT": dnsmessage.TypeTXT,
	"SOA": dnsmessage.TypeSOA, "SRV": dnsmessage.TypeSRV, "PTR": dnsmessage.TypePTR, "CAA": typeCAA,
}

// dnsTypeName is the usual name of t ("MX"), or "TYPE<n>".
func dnsTypeName(t dnsmessage.Type) string {
	for name, v := range dnsTypes {
		if v == t {
			return name
		}
	}
	return "TYPE" + strconv.Itoa(int(t))
}

// rcodeName is the usual name of an RCODE ("NXDOMAIN"), or "RCODE<n>".
func rcodeName(r dnsmessage.RCode) string {
	switch r {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeFormatError:
		return "FORMERR"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeNotImplemented:
		return "NOTIMP"
	case dnsmessage.RCodeRefused:
		return "REFUSED"
	}
	return "RCODE" + strconv.Itoa(int(r))
}

// reverseName is the in-addr.arpa name of an IPv4 address.
func reverseName(ip net.IP) string {
	v4 := ip.To4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", v4[3], v4[2], v4[1], v4[0])
}

// dnsLookup asks server ("ip:port") for s.Target's records of type
// s.Params.RecordType, recursion desired, over UDP, and again over TCP when
// the UDP answer is truncated. It emits exactly one EventAnswer. A PTR
// lookup of an IPv4 address asks for its in-addr.arpa name. A reply with an
// error RCODE (NXDOMAIN, …) is an answer, not an error; no reply within
// DNSTimeout is. s must be normalized.
func dnsLookup(ctx context.Context, s Spec, server string, emit Emitter) (DNSSummary, error) {
	qtype := dnsTypes[s.Params.RecordType]
	qname := s.Target
	if ip := net.ParseIP(qname); qtype == dnsmessage.TypePTR && ip != nil && ip.To4() != nil {
		qname = reverseName(ip)
	}
	if !strings.HasSuffix(qname, ".") {
		qname += "."
	}
	name, err := dnsmessage.NewName(qname)
	if err != nil {
		return DNSSummary{}, fmt.Errorf("%q is not a valid DNS name", s.Target)
	}
	q := dnsmessage.Question{Name: name, Type: qtype, Class: dnsmessage.ClassINET}
	id := uint16(rand.Uint32())
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return DNSSummary{}, err
	}
	if err := b.Question(q); err != nil {
		return DNSSummary{}, fmt.Errorf("%q is not a valid DNS name", s.Target)
	}
	query, err := b.Finish()
	if err != nil {
		return DNSSummary{}, err
	}

	lctx, cancel := context.WithTimeout(ctx, DNSTimeout)
	defer cancel()
	resp, rtt, err := exchangeUDP(lctx, server, query, id, q)
	if err != nil {
		return DNSSummary{}, dnsError(ctx, lctx, server, err)
	}
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil {
		return DNSSummary{}, fmt.Errorf("bad answer from %s: %w", server, err)
	}
	ans := DNSAnswer{Server: server}
	if h.Truncated {
		ans.Truncated, ans.TCP = true, true
		if resp, rtt, err = exchangeTCP(lctx, server, query, id, q); err != nil {
			return DNSSummary{}, dnsError(ctx, lctx, server, err)
		}
	}
	var m dnsmessage.Message
	if err := m.Unpack(resp); err != nil {
		return DNSSummary{}, fmt.Errorf("bad answer from %s: %w", server, err)
	}
	ans.RCode = rcodeName(m.RCode)
	ans.Authoritative = m.Authoritative
	ans.RTTMS = durationMS(rtt)
	ans.Answer, ans.Authority, ans.Additional = dnsRecords(m.Answers), dnsRecords(m.Authorities), dnsRecords(m.Additionals)
	emit(Event{Type: EventAnswer, Data: ans})
	return DNSSummary{Server: server, RCode: ans.RCode, AnswerCount: len(ans.Answer), RTTMS: ans.RTTMS}, nil
}

// dnsError turns an exchange error into what the user sees: the run's own
// cancellation or deadline as is, DNSTimeout as "no answer".
func dnsError(ctx, lctx context.Context, server string, err error) error {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		<-lctx.Done() // the connection deadline is lctx's; it ends with it
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if lctx.Err() != nil {
		return fmt.Errorf("no answer from %s within %s", server, DNSTimeout)
	}
	return fmt.Errorf("asking %s: %w", server, err)
}

// exchangeUDP sends query and returns the first reply that answers it,
// ignoring stray datagrams, and the time it took.
func exchangeUDP(ctx context.Context, server string, query []byte, id uint16, q dnsmessage.Question) ([]byte, time.Duration, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", server)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	stop := watchConn(ctx, conn)
	defer stop()
	start := time.Now()
	if _, err := conn.Write(query); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, 0, err
		}
		if answers(buf[:n], id, q) {
			return append([]byte(nil), buf[:n]...), time.Since(start), nil
		}
	}
}

// exchangeTCP sends query with its 2-byte length prefix and reads one reply.
func exchangeTCP(ctx context.Context, server string, query []byte, id uint16, q dnsmessage.Question) ([]byte, time.Duration, error) {
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp4", server)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	stop := watchConn(ctx, conn)
	defer stop()
	msg := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
	if _, err := conn.Write(append(msg, query...)); err != nil {
		return nil, 0, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, 0, err
	}
	resp := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, 0, err
	}
	if !answers(resp, id, q) {
		return nil, 0, errors.New("the TCP answer does not match the question")
	}
	return resp, time.Since(start), nil
}

// watchConn gives conn ctx's deadline and ends its reads when ctx ends.
func watchConn(ctx context.Context, conn net.Conn) (stop func() bool) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	return context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
}

// answers reports whether msg is a reply to our query: same id, and the
// same question.
func answers(msg []byte, id uint16, q dnsmessage.Question) bool {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response || h.ID != id {
		return false
	}
	got, err := p.Question()
	return err == nil && got.Type == q.Type && got.Class == q.Class && strings.EqualFold(got.Name.String(), q.Name.String())
}

// dnsRecords writes out one section's records (never nil; OPT left out).
func dnsRecords(rs []dnsmessage.Resource) []DNSRecord {
	out := []DNSRecord{}
	for _, r := range rs {
		if r.Header.Type == dnsmessage.TypeOPT {
			continue
		}
		out = append(out, DNSRecord{
			Name: r.Header.Name.String(),
			Type: dnsTypeName(r.Header.Type),
			TTL:  r.Header.TTL,
			Data: recordData(r.Body),
		})
	}
	return out
}

// recordData writes a record's data the way dig does.
func recordData(body dnsmessage.ResourceBody) string {
	switch b := body.(type) {
	case *dnsmessage.AResource:
		return net.IP(b.A[:]).String()
	case *dnsmessage.AAAAResource:
		return net.IP(b.AAAA[:]).String()
	case *dnsmessage.CNAMEResource:
		return b.CNAME.String()
	case *dnsmessage.NSResource:
		return b.NS.String()
	case *dnsmessage.PTRResource:
		return b.PTR.String()
	case *dnsmessage.MXResource:
		return fmt.Sprintf("%d %s", b.Pref, b.MX)
	case *dnsmessage.TXTResource:
		parts := make([]string, len(b.TXT))
		for i, s := range b.TXT {
			parts[i] = strconv.Quote(s)
		}
		return strings.Join(parts, " ")
	case *dnsmessage.SOAResource:
		return fmt.Sprintf("%s %s %d %d %d %d %d", b.NS, b.MBox, b.Serial, b.Refresh, b.Retry, b.Expire, b.MinTTL)
	case *dnsmessage.SRVResource:
		return fmt.Sprintf("%d %d %d %s", b.Priority, b.Weight, b.Port, b.Target)
	case *dnsmessage.UnknownResource:
		if b.Type == typeCAA {
			if s, ok := caaData(b.Data); ok {
				return s
			}
		}
		return hex.EncodeToString(b.Data)
	}
	return ""
}

// caaData reads a CAA record: flags, tag length, tag, value (RFC 8659).
func caaData(d []byte) (string, bool) {
	if len(d) < 2 || len(d) < 2+int(d[1]) {
		return "", false
	}
	tag := string(d[2 : 2+int(d[1])])
	value := string(d[2+int(d[1]):])
	return fmt.Sprintf("%d %s %s", d[0], tag, strconv.Quote(value)), true
}

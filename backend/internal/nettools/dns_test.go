package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsTestServer answers DNS queries over UDP and TCP on one 127.0.0.1 port.
// answer returns the messages to send back, in order (none = stay silent).
type dnsTestServer struct {
	addr   string
	udp    net.PacketConn
	tcp    net.Listener
	answer func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message

	mu      sync.Mutex
	queries []dnsmessage.Message
	tcpSeen int
}

func newDNSTestServer(t *testing.T, answer func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message) *dnsTestServer {
	t.Helper()
	s := &dnsTestServer{answer: answer}
	for i := 0; s.udp == nil; i++ {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		pc, err := net.ListenPacket("udp4", l.Addr().String())
		if err != nil {
			_ = l.Close()
			if i == 10 {
				t.Fatalf("no free UDP and TCP port pair: %v", err)
			}
			continue
		}
		s.tcp, s.udp, s.addr = l, pc, l.Addr().String()
	}
	go s.serveUDP()
	go s.serveTCP()
	t.Cleanup(func() {
		_ = s.udp.Close()
		_ = s.tcp.Close()
	})
	return s
}

func (s *dnsTestServer) record(q dnsmessage.Message, overTCP bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if overTCP {
		s.tcpSeen++
	}
}

func (s *dnsTestServer) lastQuery() dnsmessage.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries[len(s.queries)-1]
}

func (s *dnsTestServer) tcpQueries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcpSeen
}

func (s *dnsTestServer) serveUDP() {
	buf := make([]byte, 4096)
	for {
		n, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		var q dnsmessage.Message
		if q.Unpack(buf[:n]) != nil {
			continue
		}
		s.record(q, false)
		for _, m := range s.answer(q, false) {
			if b, err := m.Pack(); err == nil {
				_, _ = s.udp.WriteTo(b, from)
			}
		}
	}
}

func (s *dnsTestServer) serveTCP() {
	for {
		c, err := s.tcp.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			var size [2]byte
			if _, err := io.ReadFull(c, size[:]); err != nil {
				return
			}
			b := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(c, b); err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(b) != nil {
				return
			}
			s.record(q, true)
			for _, m := range s.answer(q, true) {
				if out, err := m.Pack(); err == nil {
					_, _ = c.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(out))), out...))
				}
			}
		}()
	}
}

// dnsReply answers q with rcode and the given sections.
func dnsReply(q dnsmessage.Message, rcode dnsmessage.RCode, answer, authority, additional []dnsmessage.Resource) dnsmessage.Message {
	return dnsmessage.Message{
		Header: dnsmessage.Header{ID: q.ID, Response: true, Authoritative: true,
			RecursionDesired: q.RecursionDesired, RecursionAvailable: true, RCode: rcode},
		Questions: q.Questions, Answers: answer, Authorities: authority, Additionals: additional,
	}
}

func rr(name string, ttl uint32, body dnsmessage.ResourceBody) dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, TTL: ttl},
		Body:   body,
	}
}

func mustName(s string) dnsmessage.Name { return dnsmessage.MustNewName(s) }

func dnsSpec(target, recordType string) Spec {
	return Spec{Tool: ToolDNS, Target: target, Params: Params{RecordType: recordType}}
}

// lookupOne runs dnsLookup against srv and returns its one answer event.
func lookupOne(t *testing.T, srv *dnsTestServer, s Spec) (DNSAnswer, DNSSummary) {
	t.Helper()
	var r recorder
	sum, err := dnsLookup(context.Background(), s, srv.addr, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.events) != 1 || r.events[0].Type != EventAnswer {
		t.Fatalf("events %+v, want one answer", r.events)
	}
	return r.events[0].Data.(DNSAnswer), sum
}

func TestDNSLookupA(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr("www.example.org.", 300, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 80}})},
			[]dnsmessage.Resource{rr("example.org.", 3600, &dnsmessage.NSResource{NS: mustName("ns1.example.org.")})},
			[]dnsmessage.Resource{rr("ns1.example.org.", 3600, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 53}})})}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("www.example.org", "A"))
	q := srv.lastQuery()
	if !q.RecursionDesired || len(q.Questions) != 1 || q.Questions[0].Type != dnsmessage.TypeA ||
		q.Questions[0].Class != dnsmessage.ClassINET || q.Questions[0].Name.String() != "www.example.org." {
		t.Errorf("query %+v", q)
	}
	want := DNSAnswer{
		Server: srv.addr, RCode: "NOERROR", Authoritative: true, RTTMS: ans.RTTMS,
		Answer:     []DNSRecord{{Name: "www.example.org.", Type: "A", TTL: 300, Data: "192.0.2.80"}},
		Authority:  []DNSRecord{{Name: "example.org.", Type: "NS", TTL: 3600, Data: "ns1.example.org."}},
		Additional: []DNSRecord{{Name: "ns1.example.org.", Type: "A", TTL: 3600, Data: "192.0.2.53"}},
	}
	if !reflect.DeepEqual(ans, want) {
		t.Errorf("answer\n got %+v\nwant %+v", ans, want)
	}
	if ans.RTTMS < 0 || ans.RTTMS > 1000 {
		t.Errorf("rtt %v ms", ans.RTTMS)
	}
	if sum != (DNSSummary{Server: srv.addr, RCode: "NOERROR", AnswerCount: 1, RTTMS: ans.RTTMS}) {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupRecordFormats(t *testing.T) {
	bodies := map[dnsmessage.Type]dnsmessage.ResourceBody{
		dnsmessage.TypeAAAA:  &dnsmessage.AAAAResource{AAAA: [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}},
		dnsmessage.TypeCNAME: &dnsmessage.CNAMEResource{CNAME: mustName("target.example.org.")},
		dnsmessage.TypeMX:    &dnsmessage.MXResource{Pref: 10, MX: mustName("mail.example.org.")},
		dnsmessage.TypeNS:    &dnsmessage.NSResource{NS: mustName("ns1.example.org.")},
		dnsmessage.TypeTXT:   &dnsmessage.TXTResource{TXT: []string{"v=spf1 -all", `say "hi"`}},
		dnsmessage.TypeSOA: &dnsmessage.SOAResource{NS: mustName("ns1.example.org."), MBox: mustName("hostmaster.example.org."),
			Serial: 2026100501, Refresh: 7200, Retry: 3600, Expire: 1209600, MinTTL: 300},
		dnsmessage.TypeSRV: &dnsmessage.SRVResource{Priority: 10, Weight: 60, Port: 5060, Target: mustName("sip.example.org.")},
		dnsmessage.TypePTR: &dnsmessage.PTRResource{PTR: mustName("host.example.org.")},
		typeCAA:            &dnsmessage.UnknownResource{Type: typeCAA, Data: append([]byte{0, 5}, "issueletsencrypt.org"...)},
	}
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		qq := q.Questions[0]
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr(qq.Name.String(), 60, bodies[qq.Type])}, nil, nil)}
	})
	cases := []struct{ recordType, data string }{
		{"AAAA", "2001:db8::1"},
		{"CNAME", "target.example.org."},
		{"MX", "10 mail.example.org."},
		{"NS", "ns1.example.org."},
		{"TXT", `"v=spf1 -all" "say \"hi\""`},
		{"SOA", "ns1.example.org. hostmaster.example.org. 2026100501 7200 3600 1209600 300"},
		{"SRV", "10 60 5060 sip.example.org."},
		{"PTR", "host.example.org."},
		{"CAA", `0 issue "letsencrypt.org"`},
	}
	for _, c := range cases {
		ans, _ := lookupOne(t, srv, dnsSpec("example.org", c.recordType))
		if len(ans.Answer) != 1 || ans.Answer[0].Type != c.recordType || ans.Answer[0].Data != c.data || ans.Answer[0].TTL != 60 {
			t.Errorf("%s: %+v, want data %q", c.recordType, ans.Answer, c.data)
		}
	}
}

func TestRecordDataOddCases(t *testing.T) {
	if got := recordData(&dnsmessage.UnknownResource{Type: 99, Data: []byte{0xde, 0xad}}); got != "dead" {
		t.Errorf("unknown type: %q, want hex", got)
	}
	if got := recordData(&dnsmessage.UnknownResource{Type: typeCAA, Data: []byte{0, 9, 'x'}}); got != "000978" {
		t.Errorf("malformed CAA: %q, want hex", got)
	}
	if got := dnsTypeName(99); got != "TYPE99" {
		t.Errorf("type name %q", got)
	}
	if got := rcodeName(9); got != "RCODE9" {
		t.Errorf("rcode name %q", got)
	}
}

func TestDNSLookupNXDOMAIN(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeNameError, nil,
			[]dnsmessage.Resource{rr("example.org.", 300, &dnsmessage.SOAResource{NS: mustName("ns1.example.org."),
				MBox: mustName("hostmaster.example.org."), Serial: 1, Refresh: 2, Retry: 3, Expire: 4, MinTTL: 5})}, nil)}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("nope.example.org", "A"))
	if ans.RCode != "NXDOMAIN" || ans.Answer == nil || len(ans.Answer) != 0 || len(ans.Authority) != 1 ||
		ans.Authority[0].Data != "ns1.example.org. hostmaster.example.org. 1 2 3 4 5" {
		t.Errorf("answer %+v", ans)
	}
	if sum.RCode != "NXDOMAIN" || sum.AnswerCount != 0 {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupTruncatedRetriesOverTCP(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message {
		if !overTCP {
			m := dnsReply(q, dnsmessage.RCodeSuccess, nil, nil, nil)
			m.Truncated = true
			return []dnsmessage.Message{m}
		}
		var rs []dnsmessage.Resource
		for i := byte(1); i <= 3; i++ {
			rs = append(rs, rr("big.example.org.", 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, i}}))
		}
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess, rs, nil, nil)}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("big.example.org", "A"))
	if !ans.Truncated || !ans.TCP || len(ans.Answer) != 3 || ans.Answer[2].Data != "192.0.2.3" || srv.tcpQueries() != 1 {
		t.Errorf("answer %+v after %d TCP queries", ans, srv.tcpQueries())
	}
	if sum.AnswerCount != 3 {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupPTRName(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess, nil, nil, nil)}
	})
	lookupOne(t, srv, dnsSpec("192.0.2.10", "PTR"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "10.2.0.192.in-addr.arpa." {
		t.Errorf("PTR of an address asked for %q", got)
	}
	lookupOne(t, srv, dnsSpec("10.2.0.192.in-addr.arpa", "PTR"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "10.2.0.192.in-addr.arpa." {
		t.Errorf("PTR of a name asked for %q", got)
	}
	lookupOne(t, srv, dnsSpec("192.0.2.10", "A"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "192.0.2.10." {
		t.Errorf("A of an address asked for %q", got)
	}
}

func TestDNSLookupIgnoresStrayReplies(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		a := func(last byte) []dnsmessage.Resource {
			return []dnsmessage.Resource{rr("www.example.org.", 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, last}})}
		}
		wrongID := dnsReply(q, dnsmessage.RCodeSuccess, a(1), nil, nil)
		wrongID.ID++
		wrongQuestion := dnsReply(q, dnsmessage.RCodeSuccess, a(2), nil, nil)
		wrongQuestion.Questions = []dnsmessage.Question{{Name: mustName("other.example.org."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}
		notAReply := dnsReply(q, dnsmessage.RCodeSuccess, a(4), nil, nil)
		notAReply.Response = false
		return []dnsmessage.Message{wrongID, wrongQuestion, notAReply, dnsReply(q, dnsmessage.RCodeSuccess, a(3), nil, nil)}
	})
	ans, _ := lookupOne(t, srv, dnsSpec("WWW.Example.org", "A"))
	if len(ans.Answer) != 1 || ans.Answer[0].Data != "192.0.2.3" {
		t.Errorf("answer %+v, want the reply that matches the query", ans.Answer)
	}
}

func TestDNSLookupSilentServer(t *testing.T) {
	srv := newDNSTestServer(t, func(dnsmessage.Message, bool) []dnsmessage.Message { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var r recorder
	start := time.Now()
	_, err := dnsLookup(ctx, dnsSpec("www.example.org", "A"), srv.addr, r.emit)
	if !errors.Is(err, context.DeadlineExceeded) || len(r.events) != 0 {
		t.Errorf("err %v, %d events; want the run's deadline and no answer", err, len(r.events))
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestDNSLookupClosedPort(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()
	var r recorder
	_, err = dnsLookup(context.Background(), dnsSpec("www.example.org", "A"), addr, r.emit)
	if err == nil || !strings.HasPrefix(err.Error(), "asking "+addr) && !strings.HasPrefix(err.Error(), "no answer from "+addr) {
		t.Errorf("err %v", err)
	}
}

func TestDNSLookupBadName(t *testing.T) {
	var r recorder
	_, err := dnsLookup(context.Background(), dnsSpec("a..b", "A"), "127.0.0.1:53", r.emit)
	if err == nil || err.Error() != `"a..b" is not a valid DNS name` {
		t.Errorf("err %v", err)
	}
}

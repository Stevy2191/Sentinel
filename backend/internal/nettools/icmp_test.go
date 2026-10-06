package nettools

import (
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// quotedIPv4 is the start of the datagram an ICMP error quotes: an IPv4
// header of ihl 32-bit words carrying proto, then the first bytes of its
// payload.
func quotedIPv4(ihl int, proto byte, payload []byte) []byte {
	h := make([]byte, ihl*4)
	h[0] = 0x40 | byte(ihl)
	h[8] = 1 // TTL left when it expired
	h[9] = proto
	copy(h[12:16], []byte{10, 0, 0, 5})
	copy(h[16:20], []byte{192, 0, 2, 10})
	return append(h, payload...)
}

// icmpErrorMsg is an ICMP error of type typ and code quoting q.
func icmpErrorMsg(typ, code byte, q []byte) []byte {
	return append([]byte{typ, code, 0, 0, 0, 0, 0, 0}, q...)
}

// Our echo request: id 0x1234, seq 7.
var quotedEcho = []byte{8, 0, 0xf7, 0xc3, 0x12, 0x34, 0x00, 0x07}

func TestParseICMPv4(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		ok   bool
		want parsedICMP
	}{
		{"echo reply", []byte{0, 0, 0xab, 0xcd, 0x12, 0x34, 0x00, 0x07, 'h', 'i'}, true,
			parsedICMP{kind: ReplyEcho, id: 0x1234, seq: 7}},
		{"time exceeded, plain header", icmpErrorMsg(11, 0, quotedIPv4(5, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyTimeExceeded, id: 0x1234, seq: 7}},
		{"time exceeded, header with options", icmpErrorMsg(11, 0, quotedIPv4(6, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyTimeExceeded, id: 0x1234, seq: 7}},
		{"host unreachable", icmpErrorMsg(3, 1, quotedIPv4(5, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyUnreachable, id: 0x1234, seq: 7, code: 1}},
		{"an echo request is not a reply", quotedEcho, false, parsedICMP{}},
		{"time exceeded for a UDP datagram", icmpErrorMsg(11, 0, quotedIPv4(5, 17, quotedEcho)), false, parsedICMP{}},
		{"time exceeded quoting an echo reply", icmpErrorMsg(11, 0, quotedIPv4(5, 1, []byte{0, 0, 0, 0, 0x12, 0x34, 0, 7})), false, parsedICMP{}},
		{"quoted request cut short", icmpErrorMsg(11, 0, quotedIPv4(5, 1, quotedEcho[:4])), false, parsedICMP{}},
		{"quoted header cut short", icmpErrorMsg(11, 0, quotedIPv4(5, 1, nil)[:12]), false, parsedICMP{}},
		{"quoted IHL below 5", icmpErrorMsg(11, 0, append([]byte{0x44}, quotedIPv4(5, 1, quotedEcho)[1:]...)), false, parsedICMP{}},
		{"quoted IPv6 header", icmpErrorMsg(11, 0, append([]byte{0x65}, quotedIPv4(5, 1, quotedEcho)[1:]...)), false, parsedICMP{}},
		{"redirect", icmpErrorMsg(5, 0, quotedIPv4(5, 1, quotedEcho)), false, parsedICMP{}},
		{"seven bytes", []byte{0, 0, 0, 0, 0, 0, 0}, false, parsedICMP{}},
		{"empty", nil, false, parsedICMP{}},
	}
	for _, c := range cases {
		got, ok := parseICMPv4(c.b)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// onesSum is the Internet checksum's one's-complement sum; a message with a
// correct checksum sums to 0xffff.
func onesSum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return uint16(s)
}

func TestEchoRequest(t *testing.T) {
	for _, size := range []int{0, 56, 57, 1472} {
		b, err := echoRequest(0x1234, 0x10007, size) // seq wraps to 16 bits
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != 8+size {
			t.Errorf("size %d: %d bytes, want %d", size, len(b), 8+size)
		}
		if onesSum(b) != 0xffff {
			t.Errorf("size %d: bad checksum", size)
		}
		m, err := icmp.ParseMessage(1, b)
		if err != nil {
			t.Fatal(err)
		}
		e, ok := m.Body.(*icmp.Echo)
		if m.Type != ipv4.ICMPTypeEcho || !ok || e.ID != 0x1234 || e.Seq != 7 {
			t.Errorf("size %d: %+v %+v", size, m, e)
		}
		// What a router would quote back is recognised as ours.
		got, ok := parseICMPv4(icmpErrorMsg(11, 0, quotedIPv4(5, 1, b[:8])))
		if !ok || got.id != 0x1234 || got.seq != 7 {
			t.Errorf("size %d: quoted request parsed as %+v, %v", size, got, ok)
		}
	}
}

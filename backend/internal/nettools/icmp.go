package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// ReplyKind is what answered an echo request.
type ReplyKind int

const (
	ReplyEcho         ReplyKind = iota + 1 // echo reply from the destination
	ReplyTimeExceeded                      // a router on the way: TTL ran out
	ReplyUnreachable                       // destination unreachable (Code says why)
)

// EchoReply is the answer to one echo request.
type EchoReply struct {
	Kind ReplyKind
	From net.IP
	RTT  time.Duration
	TTL  int // the reply's IP TTL, 0 when unknown
	Code int // ICMP code (unreachable)
}

var (
	ErrNoReply         = errors.New("no reply")
	ErrICMPUnavailable = errors.New("ICMP isn't available here (needs root or NET_RAW)")
)

// Prober sends ICMP echo requests. Safe for concurrent use.
type Prober interface {
	// Echo sends one echo request to dst with the given IP TTL and payload
	// size, and waits up to timeout for the matching reply: an echo reply, or
	// a time-exceeded/unreachable message quoting this request.
	// ErrNoReply when nothing matching arrives in time.
	Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error)
	// CanTrace reports whether time-exceeded messages are visible (raw ICMP
	// or the Windows ICMP API; not the unprivileged socket).
	CanTrace() bool
	Close() error
}

// ICMPv4 message types this package reads or writes.
const (
	icmpTypeEchoReply    = 0
	icmpTypeUnreachable  = 3
	icmpTypeEchoRequest  = 8
	icmpTypeTimeExceeded = 11
	ipProtocolICMP       = 1
	ipv4MinHeaderBytes   = 20
)

// parsedICMP is a received ICMP message that answers one of our echo
// requests: the echo reply itself, or an error message quoting the request.
type parsedICMP struct {
	kind    ReplyKind
	id, seq int
	code    int
}

// parseICMPv4 reads an ICMPv4 message (without its IP header). Echo replies
// carry the id and sequence in their own header; time-exceeded and
// unreachable messages quote the IPv4 header and the first 8 bytes of the
// datagram that caused them, which for our probes is the echo request's
// header. Anything else, or anything too short, is not ours: false.
func parseICMPv4(b []byte) (parsedICMP, bool) {
	if len(b) < 8 {
		return parsedICMP{}, false
	}
	switch b[0] {
	case icmpTypeEchoReply:
		return parsedICMP{
			kind: ReplyEcho,
			id:   int(binary.BigEndian.Uint16(b[4:6])),
			seq:  int(binary.BigEndian.Uint16(b[6:8])),
			code: int(b[1]),
		}, true
	case icmpTypeTimeExceeded, icmpTypeUnreachable:
		q := b[8:] // the quoted datagram
		if len(q) < ipv4MinHeaderBytes || q[0]>>4 != 4 {
			return parsedICMP{}, false
		}
		ihl := int(q[0]&0x0f) * 4
		if ihl < ipv4MinHeaderBytes || len(q) < ihl+8 || q[9] != ipProtocolICMP {
			return parsedICMP{}, false
		}
		req := q[ihl:]
		if req[0] != icmpTypeEchoRequest {
			return parsedICMP{}, false
		}
		kind := ReplyTimeExceeded
		if b[0] == icmpTypeUnreachable {
			kind = ReplyUnreachable
		}
		return parsedICMP{
			kind: kind,
			id:   int(binary.BigEndian.Uint16(req[4:6])),
			seq:  int(binary.BigEndian.Uint16(req[6:8])),
			code: int(b[1]),
		}, true
	}
	return parsedICMP{}, false
}

// echoRequest builds an ICMPv4 echo request, checksum included, with a
// payload of size bytes.
func echoRequest(id, seq, size int) ([]byte, error) {
	data := make([]byte, size)
	const fill = "SENTINEL"
	for i := range data {
		data[i] = fill[i%len(fill)]
	}
	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Body: &icmp.Echo{ID: id & 0xffff, Seq: seq & 0xffff, Data: data},
	}
	return m.Marshal(nil)
}

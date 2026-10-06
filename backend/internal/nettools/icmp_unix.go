//go:build !windows

package nettools

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// unixProber shares one ICMP socket between all of a run's probes. A raw
// socket ("ip4:icmp", root or NET_RAW) sees time-exceeded messages, so it can
// trace. The unprivileged datagram socket ("udp4", allowed by Linux's
// net.ipv4.ping_group_range) only sees echo replies to itself, and the kernel
// rewrites the echo id, so replies are matched on the sequence alone.
type unixProber struct {
	conn *icmp.PacketConn
	p4   *ipv4.PacketConn
	raw  bool
	id   int

	writeMu sync.Mutex // SetTTL + WriteTo must not interleave

	mu      sync.Mutex
	seq     uint16
	waiters map[probeKey]chan received

	done      chan struct{}
	closeOnce sync.Once
}

type probeKey struct{ id, seq int } // id is 0 on the unprivileged socket

type received struct {
	msg  parsedICMP
	from net.IP
	ttl  int
	at   time.Time
}

// NewProber opens the platform prober; ErrICMPUnavailable if it cannot.
func NewProber() (Prober, error) {
	raw := true
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		raw = false
		if conn, err = icmp.ListenPacket("udp4", "0.0.0.0"); err != nil {
			return nil, ErrICMPUnavailable
		}
	}
	p := &unixProber{
		conn:    conn,
		p4:      conn.IPv4PacketConn(),
		raw:     raw,
		id:      1 + rand.IntN(0xfffe),
		waiters: map[probeKey]chan received{},
		done:    make(chan struct{}),
	}
	// Best effort: without it the reply TTL is reported as 0.
	_ = p.p4.SetControlMessage(ipv4.FlagTTL, true)
	go p.readLoop()
	return p, nil
}

func (p *unixProber) CanTrace() bool { return p.raw }

func (p *unixProber) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.done)
		err = p.conn.Close()
	})
	return err
}

// readLoop hands every reply that answers one of our probes to its waiter.
func (p *unixProber) readLoop() {
	buf := make([]byte, 1500)
	for {
		n, cm, src, err := p.p4.ReadFrom(buf)
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		at := time.Now()
		msg, ok := parseICMPv4(buf[:n])
		if !ok {
			continue
		}
		key := probeKey{seq: msg.seq}
		if p.raw {
			key.id = msg.id
		}
		p.mu.Lock()
		ch, ok := p.waiters[key]
		delete(p.waiters, key)
		p.mu.Unlock()
		if !ok {
			continue
		}
		r := received{msg: msg, from: addrIP(src), at: at}
		if cm != nil {
			r.ttl = cm.TTL
		}
		ch <- r // buffered: never blocks
	}
}

func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP
	case *net.UDPAddr:
		return v.IP
	}
	return nil
}

func (p *unixProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	dst4 := dst.To4()
	if dst4 == nil {
		return EchoReply{}, fmt.Errorf("%v is not an IPv4 address", dst)
	}

	p.mu.Lock()
	p.seq++
	seq := int(p.seq)
	key := probeKey{seq: seq}
	if p.raw {
		key.id = p.id
	}
	ch := make(chan received, 1)
	p.waiters[key] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waiters, key)
		p.mu.Unlock()
	}()

	msg, err := echoRequest(p.id, seq, size)
	if err != nil {
		return EchoReply{}, err
	}
	var to net.Addr = &net.IPAddr{IP: dst4}
	if !p.raw {
		to = &net.UDPAddr{IP: dst4}
	}
	p.writeMu.Lock()
	if err := p.p4.SetTTL(ttl); err != nil {
		p.writeMu.Unlock()
		return EchoReply{}, fmt.Errorf("setting the TTL: %w", err)
	}
	sent := time.Now()
	_, err = p.conn.WriteTo(msg, to)
	p.writeMu.Unlock()
	if err != nil {
		return EchoReply{}, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return EchoReply{Kind: r.msg.kind, From: r.from, RTT: r.at.Sub(sent), TTL: r.ttl, Code: r.msg.code}, nil
	case <-timer.C:
		return EchoReply{}, ErrNoReply
	case <-ctx.Done():
		return EchoReply{}, ctx.Err()
	case <-p.done:
		return EchoReply{}, net.ErrClosed
	}
}

//go:build !windows

package nettools

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
)

var loopback = net.IPv4(127, 0, 0, 1)

// openProber opens the real prober, skipping the test where this user may
// not send ICMP (no root or NET_RAW, and ping_group_range excludes it).
func openProber(t *testing.T) Prober {
	t.Helper()
	p, err := NewProber()
	if errors.Is(err, ErrICMPUnavailable) {
		t.Skip("ICMP is not available to this user")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func skipIfDenied(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("sending ICMP is not permitted here: %v", err)
	}
}

// x/net's raw-socket ReadFrom overstates n by 20 when the IPv4 header carries
// options, so n can exceed the buffer; the reader must never slice past it.
func TestReadPayloadClampsLength(t *testing.T) {
	buf := make([]byte, 1500)
	for _, c := range []struct{ n, want int }{
		{0, 0}, {28, 28}, {1500, 1500}, {1516, 1500}, {1540, 1500}, {-1, 0},
	} {
		if got := len(readPayload(buf, c.n)); got != c.want {
			t.Errorf("n %d: %d bytes, want %d", c.n, got, c.want)
		}
	}
	// A full buffer read with an overstated n still parses as what it is.
	copy(buf, []byte{0, 0, 0xab, 0xcd, 0x12, 0x34, 0x00, 0x07})
	got, ok := parseICMPv4(readPayload(buf, len(buf)+16))
	if want := (parsedICMP{kind: ReplyEcho, id: 0x1234, seq: 7}); !ok || got != want {
		t.Errorf("parsed %+v, %v; want %+v, true", got, ok, want)
	}
}

// A packet whose handling panics is logged and skipped: the reader goes on
// to the next one (its probes would all time out otherwise) and stops only
// when the socket closes. No socket needed: the reads are scripted.
func TestReadPacketsSkipsAPacketThatPanics(t *testing.T) {
	packets := [][]byte{{1}, {2}, {3}}
	read := func(b []byte) (int, *ipv4.ControlMessage, net.Addr, error) {
		if len(packets) == 0 {
			return 0, nil, nil, net.ErrClosed
		}
		n := copy(b, packets[0])
		packets = packets[1:]
		return n, nil, &net.IPAddr{IP: loopback}, nil
	}
	var handled []byte
	readPackets(read, make(chan struct{}), func(payload []byte, _ *ipv4.ControlMessage, _ net.Addr, _ time.Time) {
		if payload[0] == 2 {
			panic("a packet the parser chokes on")
		}
		handled = append(handled, payload[0])
	})
	if !bytes.Equal(handled, []byte{1, 3}) {
		t.Errorf("handled packets %v, want [1 3]: the one after the panic too", handled)
	}
}

func TestProberLoopbackEcho(t *testing.T) {
	p := openProber(t)
	for i := 0; i < 2; i++ {
		r, err := p.Echo(context.Background(), loopback, 64, 56, 2*time.Second)
		skipIfDenied(t, err)
		if err != nil {
			t.Fatalf("probe %d: %v", i+1, err)
		}
		if r.Kind != ReplyEcho || !r.From.Equal(loopback) || r.RTT <= 0 || r.RTT > 2*time.Second {
			t.Errorf("probe %d: %+v, want an echo reply from 127.0.0.1", i+1, r)
		}
	}
}

func TestProberConcurrentEchoes(t *testing.T) {
	p := openProber(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := p.Echo(context.Background(), loopback, 64, 16, 2*time.Second)
			if err == nil && r.Kind != ReplyEcho {
				err = errors.New("not an echo reply")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		skipIfDenied(t, err)
		if err != nil {
			t.Error(err)
		}
	}
}

func TestProberCancelledContext(t *testing.T) {
	p := openProber(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := p.Echo(ctx, loopback, 64, 56, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

func TestProberContextEndsTheWait(t *testing.T) {
	p := openProber(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	// 192.0.2.1 (TEST-NET-1) is never routed, so nothing should answer.
	_, err := p.Echo(ctx, net.IPv4(192, 0, 2, 1), 64, 56, 5*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Skipf("TEST-NET-1 gave %v here", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the 5 s probe ignored its context: %v", d)
	}
}

func TestProberClosed(t *testing.T) {
	p := openProber(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := p.Echo(context.Background(), loopback, 64, 56, time.Second); err == nil {
		t.Error("Echo on a closed prober succeeded")
	}
}

package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func tcpSpec(ports string, timeoutMS int) Spec {
	return Spec{Tool: ToolTCP, Target: "192.0.2.10", TargetIP: "192.0.2.10", Params: Params{Ports: ports, TimeoutMS: timeoutMS}}
}

// openConn is a connection that is already established.
func openConn() net.Conn {
	a, b := net.Pipe()
	_ = b.Close()
	return a
}

func refused() error {
	return &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
}

func portResults(r *recorder) map[int]PortResult {
	out := map[int]PortResult{}
	for _, d := range r.ofType(EventPort) {
		p := d.(PortResult)
		out[p.Port] = p
	}
	return out
}

func TestTCPScanStates(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, network+" "+addr)
		mu.Unlock()
		switch addr {
		case "192.0.2.10:22":
			return openConn(), nil
		case "192.0.2.10:23":
			return nil, refused()
		case "192.0.2.10:24": // no answer: the per-port timeout ends it
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}
	}
	var r recorder
	sum, err := tcpScan(context.Background(), dial, tcpSpec("22-25", 50), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.events) == 0 || r.events[0].Type != EventStart || r.events[0].Data != (ScanStart{Total: 4}) {
		t.Fatalf("first event %+v, want start with 4 ports", r.events)
	}
	got := portResults(&r)
	if len(got) != 4 || len(r.events) != 5 {
		t.Fatalf("results %+v", got)
	}
	if p := got[22]; p.State != PortOpen || p.Service != "ssh" || p.RTTMS == nil {
		t.Errorf("22: %+v", p)
	}
	if p := got[23]; p.State != PortClosed || p.Service != "telnet" || p.RTTMS != nil {
		t.Errorf("23: %+v", p)
	}
	if p := got[24]; p.State != PortFiltered || p.Service != "" || p.RTTMS != nil {
		t.Errorf("24: %+v", p)
	}
	if p := got[25]; p.State != PortFiltered || p.Service != "smtp" {
		t.Errorf("25: %+v", p)
	}
	want := TCPSummary{Total: 4, Open: 1, Closed: 1, Filtered: 2, OpenPorts: []int{22}}
	if !reflect.DeepEqual(sum, want) {
		t.Errorf("summary %+v, want %+v", sum, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) != 4 || dialed[0][:5] != "tcp4 " {
		t.Errorf("dialed %v", dialed)
	}
}

func TestTCPScanLoopback(t *testing.T) {
	open, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	gone, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := gone.Addr().(*net.TCPAddr).Port
	_ = gone.Close()
	openPort := open.Addr().(*net.TCPAddr).Port

	s := Spec{Tool: ToolTCP, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{Ports: fmt.Sprintf("%d,%d", openPort, closedPort), TimeoutMS: 1000}}
	var r recorder
	sum, err := tcpScan(context.Background(), (&net.Dialer{}).DialContext, s, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	got := portResults(&r)
	if got[openPort].State != PortOpen || got[closedPort].State != PortClosed {
		t.Errorf("results %+v", got)
	}
	if !reflect.DeepEqual(sum.OpenPorts, []int{openPort}) || sum.Closed != 1 {
		t.Errorf("summary %+v", sum)
	}
}

// At most TCPConcurrency connects are in flight: the first 50 dials wait
// until all 50 have started, so a pool of the right size reaches exactly 50.
func TestTCPScanConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	full := make(chan struct{})
	var once sync.Once
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if n == TCPConcurrency {
			once.Do(func() { close(full) })
		}
		select {
		case <-full:
		case <-time.After(time.Second):
		}
		return nil, refused()
	}
	var r recorder
	sum, err := tcpScan(context.Background(), dial, tcpSpec("1-120", 5000), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != TCPConcurrency {
		t.Errorf("peak %d connects in flight, want %d", peak.Load(), TCPConcurrency)
	}
	if sum.Closed != 120 || len(r.ofType(EventPort)) != 120 {
		t.Errorf("summary %+v", sum)
	}
}

func TestTCPScanCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dial := func(dctx context.Context, _, addr string) (net.Conn, error) {
		switch addr {
		case "192.0.2.10:1", "192.0.2.10:2", "192.0.2.10:3":
			return nil, refused()
		}
		<-dctx.Done()
		return nil, dctx.Err()
	}
	r := recorder{}
	r.onEmit = func(Event) {
		if len(r.ofType(EventPort)) == 3 {
			cancel()
		}
	}
	start := time.Now()
	sum, err := tcpScan(ctx, dial, tcpSpec("1-10", 5000), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	want := TCPSummary{Total: 10, Closed: 3, OpenPorts: []int{}}
	if !reflect.DeepEqual(sum, want) || len(r.ofType(EventPort)) != 3 {
		t.Errorf("summary %+v, %d port events; want %+v and the cut-short ports unreported", sum, len(r.ofType(EventPort)), want)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestTCPScanBadPorts(t *testing.T) {
	var r recorder
	if _, err := tcpScan(context.Background(), nil, tcpSpec("0", 500), r.emit); err == nil || len(r.events) != 0 {
		t.Errorf("err %v, events %+v", err, r.events)
	}
}

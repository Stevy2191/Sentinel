package nettools

import (
	"context"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// dialFunc opens a connection, like (*net.Dialer).DialContext.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// tcpScan connects to each port of s.Params.Ports on s.TargetIP, at most
// TCPConcurrency at a time, each with the per-port timeout: connected is
// open, refused is closed, anything else (timeout, unreachable) is
// filtered. It emits a ScanStart, then a PortResult per port as soon as its
// state is known. s must be normalized and TargetIP an IPv4 address.
func tcpScan(ctx context.Context, dial dialFunc, s Spec, emit Emitter) (TCPSummary, error) {
	ports, err := ParsePorts(s.Params.Ports)
	if err != nil {
		return TCPSummary{}, err
	}
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	emit(Event{Type: EventStart, Data: ScanStart{Total: len(ports)}})

	jobs := make(chan int)
	results := make(chan PortResult)
	var workers sync.WaitGroup
	for i := 0; i < min(TCPConcurrency, len(ports)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for port := range jobs {
				if r, ok := probePort(ctx, dial, s.TargetIP, port, timeout); ok {
					results <- r
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, p := range ports {
			select {
			case jobs <- p:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	sum := TCPSummary{Total: len(ports), OpenPorts: []int{}}
	for r := range results {
		switch r.State {
		case PortOpen:
			sum.Open++
			sum.OpenPorts = append(sum.OpenPorts, r.Port)
		case PortClosed:
			sum.Closed++
		default:
			sum.Filtered++
		}
		emit(Event{Type: EventPort, Data: r})
	}
	sort.Ints(sum.OpenPorts)
	return sum, ctx.Err()
}

// probePort tries one port. ok is false when the run's context ended first,
// so the outcome says nothing about the port.
func probePort(ctx context.Context, dial dialFunc, ip string, port int, timeout time.Duration) (PortResult, bool) {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	conn, err := dial(pctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	rtt := time.Since(start)
	if err == nil {
		_ = conn.Close()
	}
	if ctx.Err() != nil {
		return PortResult{}, false
	}
	r := PortResult{Port: port, Service: ServiceName(port)}
	switch {
	case err == nil:
		r.State, r.RTTMS = PortOpen, ptr(durationMS(rtt))
	case isRefused(err):
		r.State = PortClosed
	default:
		r.State = PortFiltered
	}
	return r, true
}

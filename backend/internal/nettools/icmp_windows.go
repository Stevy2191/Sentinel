package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows ICMP API (iphlpapi.dll) sends echo requests without raw
// sockets or administrator rights and reports TTL-exceeded replies with the
// router's address, so it serves both ping and traceroute. It is called
// directly (no CGO); NewLazySystemDLL loads the DLL from System32 only.
var (
	modiphlpapi         = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = modiphlpapi.NewProc("IcmpCreateFile")
	procIcmpSendEcho    = modiphlpapi.NewProc("IcmpSendEcho")
	procIcmpCloseHandle = modiphlpapi.NewProc("IcmpCloseHandle")
)

// IP_STATUS values (ipexport.h).
const (
	ipSuccess             = 0
	ipDestNetUnreachable  = 11002
	ipDestHostUnreachable = 11003
	ipDestProtUnreachable = 11004
	ipDestPortUnreachable = 11005
	ipReqTimedOut         = 11010
	ipTTLExpiredTransit   = 11013
	ipTTLExpiredReassem   = 11014
)

// ipOptionInformation mirrors IP_OPTION_INFORMATION. OptionsData is a
// pointer, so on 64-bit Windows it sits at offset 8 and the struct is 16
// bytes; uintptr gives the right layout on both 32- and 64-bit.
type ipOptionInformation struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// icmpEchoReply mirrors ICMP_ECHO_REPLY: on amd64 Address 0, Status 4,
// RoundTripTime 8, DataSize 12, Reserved 14, Data 16, Options 24 (size 40).
type icmpEchoReply struct {
	Address       uint32 // IPAddr: the address bytes in network order
	Status        uint32
	RoundTripTime uint32 // milliseconds
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

type windowsProber struct{}

// NewProber opens the platform prober; ErrICMPUnavailable if it cannot.
func NewProber() (Prober, error) {
	if err := procIcmpSendEcho.Find(); err != nil {
		return nil, ErrICMPUnavailable
	}
	h, err := icmpCreateFile()
	if err != nil {
		return nil, ErrICMPUnavailable
	}
	icmpCloseHandle(h)
	return windowsProber{}, nil
}

func (windowsProber) CanTrace() bool { return true }
func (windowsProber) Close() error   { return nil }

func icmpCreateFile() (windows.Handle, error) {
	r, _, err := procIcmpCreateFile.Call()
	if h := windows.Handle(r); h != windows.InvalidHandle {
		return h, nil
	}
	return 0, err
}

func icmpCloseHandle(h windows.Handle) {
	_, _, _ = procIcmpCloseHandle.Call(uintptr(h))
}

// Echo runs the blocking IcmpSendEcho in a goroutine so ctx can end the wait
// at once; the call itself finishes on its own within timeout.
func (windowsProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	dst4 := dst.To4()
	if dst4 == nil {
		return EchoReply{}, fmt.Errorf("%v is not an IPv4 address", dst)
	}
	type result struct {
		reply EchoReply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		r, err := sendEcho(dst4, ttl, size, timeout)
		done <- result{r, err}
	}()
	select {
	case res := <-done:
		return res.reply, res.err
	case <-ctx.Done():
		return EchoReply{}, ctx.Err()
	}
}

// sendEcho sends one echo request through its own ICMP handle, so concurrent
// probes never share one.
func sendEcho(dst4 net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	h, err := icmpCreateFile()
	if err != nil {
		return EchoReply{}, ErrICMPUnavailable
	}
	defer icmpCloseHandle(h)

	payload := make([]byte, size+1) // never empty, so &payload[0] is valid
	const fill = "SENTINEL"
	for i := range payload {
		payload[i] = fill[i%len(fill)]
	}
	opts := ipOptionInformation{TTL: uint8(ttl)}
	// Room for one reply, the echoed payload, an 8-byte ICMP error and slack.
	replySize := int(unsafe.Sizeof(icmpEchoReply{})) + size + 8 + 64
	buf := make([]uint64, (replySize+7)/8) // uint64s keep the reply 8-byte aligned
	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}

	start := time.Now()
	n, _, callErr := procIcmpSendEcho.Call(
		uintptr(h),
		uintptr(binary.LittleEndian.Uint32(dst4)),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*8),
		uintptr(ms),
	)
	rtt := time.Since(start)
	if n == 0 {
		var errno syscall.Errno
		if errors.As(callErr, &errno) && errno == ipReqTimedOut {
			return EchoReply{}, ErrNoReply
		}
		return EchoReply{}, fmt.Errorf("IcmpSendEcho: %w", callErr)
	}

	reply := (*icmpEchoReply)(unsafe.Pointer(&buf[0]))
	var from [4]byte
	binary.LittleEndian.PutUint32(from[:], reply.Address)
	out := EchoReply{From: net.IPv4(from[0], from[1], from[2], from[3]).To4(), RTT: rtt, TTL: int(reply.Options.TTL)}
	switch reply.Status {
	case ipSuccess:
		out.Kind = ReplyEcho
	case ipTTLExpiredTransit, ipTTLExpiredReassem:
		out.Kind = ReplyTimeExceeded
		out.Code = int(reply.Status - ipTTLExpiredTransit)
	case ipDestNetUnreachable, ipDestHostUnreachable, ipDestProtUnreachable, ipDestPortUnreachable:
		out.Kind = ReplyUnreachable
		out.Code = int(reply.Status - ipDestNetUnreachable)
	case ipReqTimedOut:
		return EchoReply{}, ErrNoReply
	default:
		return EchoReply{}, fmt.Errorf("ICMP status %d", reply.Status)
	}
	return out, nil
}

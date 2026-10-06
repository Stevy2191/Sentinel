package nettools

import (
	"testing"
	"unsafe"
)

// The structs passed to IcmpSendEcho must match the C layout.
func TestICMPStructLayout(t *testing.T) {
	var r icmpEchoReply
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if unsafe.Offsetof(r.Data) != 16 || unsafe.Offsetof(r.Options) != 24 || unsafe.Sizeof(r) != 40 {
			t.Errorf("ICMP_ECHO_REPLY: Data at %d, Options at %d, size %d; want 16, 24, 40",
				unsafe.Offsetof(r.Data), unsafe.Offsetof(r.Options), unsafe.Sizeof(r))
		}
		if unsafe.Sizeof(ipOptionInformation{}) != 16 {
			t.Errorf("IP_OPTION_INFORMATION is %d bytes, want 16", unsafe.Sizeof(ipOptionInformation{}))
		}
		return
	}
	if unsafe.Offsetof(r.Options) != 20 || unsafe.Sizeof(r) != 28 {
		t.Errorf("32-bit ICMP_ECHO_REPLY: Options at %d, size %d; want 20, 28", unsafe.Offsetof(r.Options), unsafe.Sizeof(r))
	}
}

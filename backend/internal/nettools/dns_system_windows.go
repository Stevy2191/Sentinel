package nettools

import (
	"errors"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SystemResolver returns "ip:port" of the host's first IPv4 DNS server: the
// first IPv4 DNS server of the first network adapter that is up.
func SystemResolver() (string, error) {
	const flags = windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_FRIENDLY_NAME
	size := uint32(15000)
	var buf []byte
	for {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if errors.Is(err, windows.ERROR_NO_DATA) {
			return "", errors.New("no network adapters")
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) || size <= uint32(len(buf)) {
			return "", fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
	}
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip := d.Address.IP().To4(); ip != nil {
				return net.JoinHostPort(ip.String(), "53"), nil
			}
		}
	}
	return "", errors.New("no IPv4 DNS server on any network adapter that is up")
}

package main

import (
	"net"
	"runtime"
	"strconv"
	"strings"
)

// SystemInfo is what a host reports about itself.
//
// Sent with every heartbeat rather than with each metrics sample: these change
// when a machine is rebuilt or upgraded, not every collection cycle, so
// repeating them thousands of times a day would be waste.
type SystemInfo struct {
	Hostname        string `json:"hostname,omitempty"`
	IPAddress       string `json:"ip_address,omitempty"`
	OSVersion       string `json:"os_version,omitempty"`
	KernelVersion   string `json:"kernel_version,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	CPUModel        string `json:"cpu_model,omitempty"`
	CPUCores        int    `json:"cpu_cores,omitempty"`
	MemoryTotalMB   int64  `json:"memory_total_mb,omitempty"`
	GoVersion       string `json:"go_version,omitempty"`
	DockerAvailable bool   `json:"docker_available"`
}

// collectSystemInfo reads what it can and leaves the rest empty. A field that
// cannot be read is not worth failing a heartbeat over.
func collectSystemInfo(dockerAvailable bool) SystemInfo {
	info := SystemInfo{
		Hostname:        hostname(),
		IPAddress:       detectIPAddress(),
		OSVersion:       osVersion(),
		KernelVersion:   kernelVersion(),
		Architecture:    runtime.GOARCH,
		GoVersion:       strings.TrimPrefix(runtime.Version(), "go"),
		DockerAvailable: dockerAvailable,
	}
	info.CPUModel, info.CPUCores = cpuInfo()
	if total, err := memoryTotalMB(); err == nil {
		info.MemoryTotalMB = total
	}
	return info
}

// parseIntOr is kept for readability where a missing value is simply zero.
func parseIntOr(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}

// detectIPAddress works out the address this host should be recorded under.
//
// Private addresses are preferred over public ones. A monitored host is
// normally reached across an internal network, so 192.168.1.50 is the useful
// answer even when the machine also holds a routable address — and on a host
// behind NAT the public address it can see is not one anything can connect
// back to.
//
// Returns an empty string when nothing suitable is found, which leaves the
// server free to fall back to the address the request arrived from.
func detectIPAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var public string
	for _, iface := range interfaces {
		// Loopback tells us nothing, and an interface that is down cannot be
		// how anything reaches this host.
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			// IPv4 only: these values are read by people and pasted into
			// terminals, and a link-local IPv6 address is neither.
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String()
			}
			if public == "" {
				public = ip.String()
			}
		}
	}
	return public
}

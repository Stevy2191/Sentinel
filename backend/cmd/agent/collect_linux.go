package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/hoststats"
)

// Collector gathers host metrics. It holds the previous CPU sample because
// utilisation is a rate: /proc/stat reports cumulative jiffies since boot, and
// a single reading gives the average since boot rather than the load now.
type Collector struct {
	prevCPU  *hoststats.CPUSample
	diskPath string
}

func NewCollector(diskPath string) *Collector {
	if diskPath == "" {
		diskPath = "/"
	}
	return &Collector{diskPath: diskPath}
}

// Collect reads one cycle. Individual readings that fail are left unset rather
// than failing the cycle: a host with no swap, no Docker or an unreadable
// mount should still report everything else it can.
func (c *Collector) Collect() Metrics {
	m := Metrics{Timestamp: time.Now().UTC(), Containers: []Container{}}

	if pct, err := c.cpuPercent(); err == nil {
		m.CPUPercent = &pct
	}
	if used, total, pct, err := hoststats.Memory(); err == nil {
		m.MemoryUsedMB, m.MemoryTotalMB, m.MemoryPercent = &used, &total, &pct
	}
	if used, total, pct, err := hoststats.Disk(c.diskPath); err == nil {
		m.DiskUsedGB, m.DiskTotalGB, m.DiskPercent = &used, &total, &pct
	}
	if up, err := uptimeSeconds(); err == nil {
		m.UptimeSeconds = &up
	}
	if l1, l5, l15, err := loadAverage(); err == nil {
		m.LoadAverage1m, m.LoadAverage5m, m.LoadAverage15m = &l1, &l5, &l15
	}
	if in, out, err := network(); err == nil {
		m.NetworkInBytes, m.NetworkOutBytes = &in, &out
	}
	return m
}

// cpuPercent returns utilisation since the previous call. The first call has
// nothing to compare against and reports nothing rather than a number derived
// from uptime, which would be wrong in a way nobody would notice.
func (c *Collector) cpuPercent() (float64, error) {
	sample, err := hoststats.ReadCPUSample()
	if err != nil {
		return 0, err
	}
	prev := c.prevCPU
	c.prevCPU = &sample
	if prev == nil {
		return 0, fmt.Errorf("no previous sample yet")
	}

	totalDelta := float64(sample.Total - prev.Total)
	idleDelta := float64(sample.Idle - prev.Idle)
	if totalDelta <= 0 {
		return 0, fmt.Errorf("no elapsed CPU time")
	}
	pct := (totalDelta - idleDelta) / totalDelta * 100
	return hoststats.ClampPercent(pct), nil
}

func uptimeSeconds() (int64, error) {
	raw, err := os.ReadFile(hoststats.ProcRoot + "/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty %s/uptime", hoststats.ProcRoot)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return int64(secs), nil
}

func loadAverage() (l1, l5, l15 float64, err error) {
	raw, err := os.ReadFile(hoststats.ProcRoot + "/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected %s/loadavg format", hoststats.ProcRoot)
	}
	if l1, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return 0, 0, 0, err
	}
	if l5, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return 0, 0, 0, err
	}
	if l15, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return 0, 0, 0, err
	}
	return l1, l5, l15, nil
}

// network sums bytes in and out across real interfaces.
//
// Loopback is excluded because it is a machine talking to itself, and virtual
// interfaces because container and bridge traffic would be counted twice —
// once on the container's side and once on the host bridge.
func network() (in, out int64, err error) {
	f, err := os.Open(hoststats.ProcRoot + "/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue // the two header lines
		}
		name = strings.TrimSpace(name)
		if name == "lo" || isVirtualInterface(name) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseInt(fields[0], 10, 64)
		tx, err2 := strconv.ParseInt(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		in += rx
		out += tx
	}
	return in, out, nil
}

func isVirtualInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "tun", "tap", "cni", "flannel", "kube"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// hostname reports the host's name, preferring what the host filesystem says
// over the kernel's, since in a container the latter is the container id.
func hostname() string {
	if raw, err := os.ReadFile(envOr("HOST_ETC", "/etc") + "/hostname"); err == nil {
		if name := strings.TrimSpace(string(raw)); name != "" {
			return name
		}
	}
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// osVersion reads the distribution's pretty name from os-release.
func osVersion() string {
	raw, err := os.ReadFile(envOr("HOST_ETC", "/etc") + "/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return ""
}

// kernelVersion reads the running kernel, e.g. "Linux 6.8.0-139-generic".
//
// From /proc/sys/kernel rather than uname, so it works in a container with the
// host's /proc mounted — where uname would report the container's view.
func kernelVersion() string {
	name := readTrimmed(hoststats.ProcRoot + "/sys/kernel/ostype")
	release := readTrimmed(hoststats.ProcRoot + "/sys/kernel/osrelease")
	switch {
	case name != "" && release != "":
		return name + " " + release
	case release != "":
		return release
	default:
		return name
	}
}

// cpuInfo returns the processor model and how many cores the host has.
//
// Cores are counted from the "processor" lines rather than taken from
// runtime.NumCPU, which reports what this process may use — a container under
// a CPU limit would otherwise report the limit as the machine's size.
func cpuInfo() (model string, cores int) {
	f, err := os.Open(hoststats.ProcRoot + "/cpuinfo")
	if err != nil {
		return "", runtime.NumCPU()
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "model name", "Model", "cpu model":
			if model == "" {
				model = value
			}
		case "processor":
			cores++
		}
	}
	if cores == 0 {
		cores = runtime.NumCPU()
	}
	return model, cores
}

func readTrimmed(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// memoryTotalMB reports the host's total memory, for the heartbeat rather
// than a metrics cycle.
func memoryTotalMB() (int64, error) {
	_, total, _, err := hoststats.Memory()
	return total, err
}

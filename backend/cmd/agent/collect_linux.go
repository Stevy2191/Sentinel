package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Metrics is one collection cycle, matching the API's request body.
type Metrics struct {
	Timestamp time.Time `json:"timestamp"`

	CPUPercent    *float64 `json:"cpu_percent,omitempty"`
	MemoryPercent *float64 `json:"memory_percent,omitempty"`
	MemoryUsedMB  *int64   `json:"memory_used_mb,omitempty"`
	MemoryTotalMB *int64   `json:"memory_total_mb,omitempty"`
	DiskPercent   *float64 `json:"disk_percent,omitempty"`
	DiskUsedGB    *float64 `json:"disk_used_gb,omitempty"`
	DiskTotalGB   *float64 `json:"disk_total_gb,omitempty"`
	UptimeSeconds *int64   `json:"uptime_seconds,omitempty"`

	LoadAverage1m  *float64 `json:"load_average_1m,omitempty"`
	LoadAverage5m  *float64 `json:"load_average_5m,omitempty"`
	LoadAverage15m *float64 `json:"load_average_15m,omitempty"`

	NetworkInBytes  *int64 `json:"network_in_bytes,omitempty"`
	NetworkOutBytes *int64 `json:"network_out_bytes,omitempty"`

	Containers []Container `json:"containers"`
}

// Container is one Docker container's metrics.
type Container struct {
	ContainerID   string  `json:"container_id"`
	ContainerName string  `json:"container_name"`
	Image         string  `json:"image"`
	Status        string  `json:"status"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	MemoryUsedMB  int64   `json:"memory_used_mb"`
}

// procRoot is where the host's /proc is mounted. In a container the host's is
// bind-mounted elsewhere, so it is configurable rather than assumed.
var procRoot = envOr("HOST_PROC", "/proc")

// cpuSample is a point-in-time reading of the kernel's cumulative CPU counters.
type cpuSample struct {
	idle  uint64
	total uint64
}

// Collector gathers host metrics. It holds the previous CPU sample because
// utilisation is a rate: /proc/stat reports cumulative jiffies since boot, and
// a single reading gives the average since boot rather than the load now.
type Collector struct {
	prevCPU  *cpuSample
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
	if used, total, pct, err := memory(); err == nil {
		m.MemoryUsedMB, m.MemoryTotalMB, m.MemoryPercent = &used, &total, &pct
	}
	if used, total, pct, err := disk(c.diskPath); err == nil {
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
	sample, err := readCPUSample()
	if err != nil {
		return 0, err
	}
	prev := c.prevCPU
	c.prevCPU = &sample
	if prev == nil {
		return 0, fmt.Errorf("no previous sample yet")
	}

	totalDelta := float64(sample.total - prev.total)
	idleDelta := float64(sample.idle - prev.idle)
	if totalDelta <= 0 {
		return 0, fmt.Errorf("no elapsed CPU time")
	}
	pct := (totalDelta - idleDelta) / totalDelta * 100
	return clampPercent(pct), nil
}

func readCPUSample() (cpuSample, error) {
	f, err := os.Open(procRoot + "/stat")
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var total, idle uint64
		for i, raw := range fields[1:] {
			v, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				continue
			}
			total += v
			// Fields 4 and 5 are idle and iowait: time the CPU was not doing
			// work. iowait counts as idle because the CPU was available.
			if i == 3 || i == 4 {
				idle += v
			}
		}
		return cpuSample{idle: idle, total: total}, nil
	}
	return cpuSample{}, fmt.Errorf("no cpu line in %s/stat", procRoot)
}

// memory returns used and total in MB and the percentage in use.
//
// Used is total minus MemAvailable, not minus MemFree. The kernel keeps cache
// and buffers that it will release under pressure; counting those as used
// reports a machine as nearly full when it is fine, which is the number most
// naive readings get wrong.
func memory() (usedMB, totalMB int64, percent float64, err error) {
	f, err := os.Open(procRoot + "/meminfo")
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()

	var totalKB, availableKB, freeKB uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availableKB = v
		case "MemFree:":
			freeKB = v
		}
	}
	if totalKB == 0 {
		return 0, 0, 0, fmt.Errorf("could not read MemTotal")
	}
	// Very old kernels have no MemAvailable; MemFree is the fallback.
	if availableKB == 0 {
		availableKB = freeKB
	}

	totalMB = int64(totalKB / 1024)
	usedMB = int64((totalKB - availableKB) / 1024)
	percent = clampPercent(float64(totalKB-availableKB) / float64(totalKB) * 100)
	return usedMB, totalMB, percent, nil
}

// disk reports usage of the filesystem holding path.
//
// Capacity uses the blocks available to an unprivileged user, not the raw
// total. Filesystems reserve a slice for root, and counting it as free
// promises space an application cannot actually use.
func disk(path string) (usedGB, totalGB, percent float64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	blockSize := uint64(st.Bsize)
	total := st.Blocks * blockSize
	free := st.Bavail * blockSize
	if total == 0 {
		return 0, 0, 0, fmt.Errorf("filesystem reports no capacity")
	}
	usable := st.Blocks - (st.Bfree - st.Bavail)
	used := (usable - st.Bavail) * blockSize

	const gb = 1024 * 1024 * 1024
	usedGB = float64(used) / gb
	totalGB = float64(usable*blockSize) / gb
	percent = clampPercent(float64(used) / float64(usable*blockSize) * 100)
	_ = free
	_ = total
	return usedGB, totalGB, percent, nil
}

func uptimeSeconds() (int64, error) {
	raw, err := os.ReadFile(procRoot + "/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty %s/uptime", procRoot)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return int64(secs), nil
}

func loadAverage() (l1, l5, l15 float64, err error) {
	raw, err := os.ReadFile(procRoot + "/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected %s/loadavg format", procRoot)
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
	f, err := os.Open(procRoot + "/net/dev")
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

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

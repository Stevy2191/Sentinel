// Package hoststats reads CPU, memory and disk usage from the host the
// process is running on.
//
// Shared by the Sentinel server and the monitoring agent. Both need the same
// readings from the same files, and two implementations of /proc parsing would
// eventually disagree about what "used memory" means.
//
// Inside a container /proc/stat and /proc/meminfo report the host's figures
// rather than the container's, so CPU and memory need no special mounting.
// Disk does: the container's own root is an overlay, so the path to measure
// has to be one that reaches the host's filesystem.
package hoststats

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcRoot is where the host's /proc is mounted. Configurable because a
// container may have the host's mounted somewhere other than /proc.
var ProcRoot = envOr("HOST_PROC", "/proc")

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// CPUSample is a point-in-time reading of the kernel's cumulative CPU counters.
type CPUSample struct {
	Idle  uint64
	Total uint64
}

func ReadCPUSample() (CPUSample, error) {
	f, err := os.Open(ProcRoot + "/stat")
	if err != nil {
		return CPUSample{}, err
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
		return CPUSample{Idle: idle, Total: total}, nil
	}
	return CPUSample{}, fmt.Errorf("no cpu line in %s/stat", ProcRoot)
}

// memory returns used and total in MB and the percentage in use.
//
// Used is total minus MemAvailable, not minus MemFree. The kernel keeps cache
// and buffers that it will release under pressure; counting those as used
// reports a machine as nearly full when it is fine, which is the number most
// naive readings get wrong.
func Memory() (usedMB, totalMB int64, percent float64, err error) {
	f, err := os.Open(ProcRoot + "/meminfo")
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
	percent = ClampPercent(float64(totalKB-availableKB) / float64(totalKB) * 100)
	return usedMB, totalMB, percent, nil
}

// disk reports usage of the filesystem holding path.
//
// Capacity uses the blocks available to an unprivileged user, not the raw
// total. Filesystems reserve a slice for root, and counting it as free
// promises space an application cannot actually use.
func Disk(path string) (usedGB, totalGB, percent float64, err error) {
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
	percent = ClampPercent(float64(used) / float64(usable*blockSize) * 100)
	_ = free
	_ = total
	return usedGB, totalGB, percent, nil
}

func ClampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Snapshot is the host's resource usage at one moment.
type Snapshot struct {
	// CPUPercent is nil until two samples exist, since utilisation is a rate
	// and a single reading of a cumulative counter cannot express one.
	CPUPercent    *float64 `json:"cpu_percent"`
	MemoryPercent float64  `json:"memory_percent"`
	MemoryUsedMB  int64    `json:"memory_used_mb"`
	MemoryTotalMB int64    `json:"memory_total_mb"`
	DiskPercent   float64  `json:"disk_percent"`
	DiskUsedGB    float64  `json:"disk_used_gb"`
	DiskTotalGB   float64  `json:"disk_total_gb"`
}

// sampleEvery is how often the CPU counters are re-read.
//
// Utilisation is the difference between two readings, so the interval decides
// what the figure describes: this reports the load over the last few seconds
// rather than the average since boot.
const sampleEvery = 5 * time.Second

// Sampler reads the host's resources on a timer and serves the latest result.
//
// Sampled in the background rather than per request because CPU needs two
// readings separated in time. Computing it per request would make the figure
// depend on how often somebody happened to load the page, and two viewers
// would consume each other's baseline.
type Sampler struct {
	diskPath string

	mu       sync.RWMutex
	prev     *CPUSample
	snapshot Snapshot
}

func NewSampler(diskPath string) *Sampler {
	if strings.TrimSpace(diskPath) == "" {
		diskPath = "/"
	}
	s := &Sampler{diskPath: diskPath}
	// Taken once now so memory and disk are available immediately; CPU needs
	// the second sample and appears a few seconds later.
	s.sample()
	return s
}

// Start samples until the context is cancelled.
func (s *Sampler) Start(ctx context.Context) {
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

// Snapshot returns the most recent reading.
func (s *Sampler) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Sampler) sample() {
	next := Snapshot{}

	if cur, err := ReadCPUSample(); err == nil {
		s.mu.Lock()
		prev := s.prev
		s.prev = &cur
		s.mu.Unlock()

		if prev != nil {
			totalDelta := float64(cur.Total - prev.Total)
			idleDelta := float64(cur.Idle - prev.Idle)
			if totalDelta > 0 {
				pct := ClampPercent((totalDelta - idleDelta) / totalDelta * 100)
				next.CPUPercent = &pct
			}
		}
	}

	if used, total, pct, err := Memory(); err == nil {
		next.MemoryUsedMB, next.MemoryTotalMB, next.MemoryPercent = used, total, pct
	}
	if used, total, pct, err := Disk(s.diskPath); err == nil {
		next.DiskUsedGB, next.DiskTotalGB, next.DiskPercent = used, total, pct
	}

	s.mu.Lock()
	// A reading that failed leaves the previous value rather than reporting
	// zero, which would look like an idle machine instead of a failed read.
	if next.CPUPercent == nil {
		next.CPUPercent = s.snapshot.CPUPercent
	}
	s.snapshot = next
	s.mu.Unlock()
}

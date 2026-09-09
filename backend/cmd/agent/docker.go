package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DockerCollector reads container metrics from the Docker Engine API.
//
// Speaks to the socket over plain HTTP rather than pulling in the Docker SDK.
// The SDK brings a large dependency tree for what amounts to two endpoints,
// and this agent is meant to be a single small binary an operator can drop
// onto a host.
type DockerCollector struct {
	client *http.Client
	// available is false when there is no socket to talk to, which is the
	// normal case on a host that simply does not run Docker.
	available bool
	// apiVersion is negotiated with the daemon at startup. It cannot be
	// hardcoded in either direction: modern daemons refuse a version below
	// their minimum, and old daemons refuse one above their own.
	apiVersion string
}

// preferredDockerAPI is the version asked for when the daemon supports it.
// Old enough to be widely available, new enough to satisfy the minimum modern
// daemons enforce.
const preferredDockerAPI = "1.41"

// fallbackDockerAPI is used when the daemon cannot be asked what it supports.
const fallbackDockerAPI = "1.41"

func NewDockerCollector(socketPath string) *DockerCollector {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	d := &DockerCollector{}
	if _, err := os.Stat(socketPath); err != nil {
		return d
	}
	d.client = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	d.available = true
	d.apiVersion = d.negotiateVersion()
	return d
}

// dockerVersion is the subset of /version used to pick an API version.
type dockerVersion struct {
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
}

// negotiateVersion asks the daemon what it speaks and picks a version it will
// accept.
//
// /version is the one endpoint that answers without a version prefix, which is
// what makes the negotiation possible at all.
func (d *DockerCollector) negotiateVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return fallbackDockerAPI
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fallbackDockerAPI
	}
	defer resp.Body.Close()

	var v dockerVersion
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return fallbackDockerAPI
	}

	// Below the daemon's minimum it refuses us; above its own it cannot
	// answer. Preferred sits between the two whenever that range allows.
	if v.MinAPIVersion != "" && compareAPIVersions(preferredDockerAPI, v.MinAPIVersion) < 0 {
		return v.MinAPIVersion
	}
	if v.APIVersion != "" && compareAPIVersions(preferredDockerAPI, v.APIVersion) > 0 {
		return v.APIVersion
	}
	return preferredDockerAPI
}

// compareAPIVersions compares dotted versions like "1.41" and "1.7",
// numerically rather than lexically so 1.41 is not treated as older than 1.7.
func compareAPIVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func (d *DockerCollector) Available() bool { return d.available }

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

// dockerStats is the subset of the stats response needed for CPU and memory.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

// Collect returns metrics for every running container, or an empty slice when
// Docker is not present. A host without Docker is not an error.
func (d *DockerCollector) Collect(ctx context.Context) ([]Container, error) {
	if !d.available {
		return []Container{}, nil
	}

	var list []dockerContainer
	if err := d.get(ctx, "/containers/json", &list); err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	out := make([]Container, len(list))
	// Stats are fetched concurrently. Each call blocks for about a second
	// while the daemon takes its two samples, so doing them in turn would make
	// a cycle on a busy host take longer than the interval between cycles.
	// Bounded so a host running many containers does not open a connection for
	// every one of them at once.
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for i, c := range list {
		out[i] = Container{
			ContainerID:   shortID(c.ID),
			ContainerName: containerName(c.Names),
			Image:         c.Image,
			Status:        firstNonEmpty(c.State, c.Status),
		}

		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			// stream=false without one-shot: the daemon takes two samples a
			// second apart and returns both, which is what makes a CPU
			// percentage possible. one-shot returns a single sample with an
			// empty precpu_stats, so every container reads 0%.
			var stats dockerStats
			if err := d.get(ctx, "/containers/"+id+"/stats?stream=false", &stats); err != nil {
				return
			}
			out[i].CPUPercent = containerCPUPercent(stats)
			used, pct := containerMemory(stats)
			out[i].MemoryUsedMB = used
			out[i].MemoryPercent = pct
		}(i, c.ID)
	}
	wg.Wait()
	return out, nil
}

func (d *DockerCollector) get(ctx context.Context, path string, into interface{}) error {
	// The host part is ignored for a unix socket but must be syntactically
	// valid for the request to be built.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v"+d.apiVersion+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker returned %d for %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// containerCPUPercent converts cumulative counters into a percentage, the same
// way `docker stats` does: the container's share of system CPU time since the
// previous sample, scaled by the number of CPUs so a fully busy container on a
// four-core host reads 400%, matching what an operator sees in Docker itself.
func containerCPUPercent(s dockerStats) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpus == 0 {
		cpus = 1
	}
	return round2(cpuDelta / systemDelta * cpus * 100)
}

// containerMemory returns usage in MB and as a percentage of the limit.
//
// Page cache is subtracted, as `docker stats` does. Counting it makes a
// container that has merely read files look near its limit.
func containerMemory(s dockerStats) (usedMB int64, percent float64) {
	usage := s.MemoryStats.Usage
	if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok && cache < usage {
		usage -= cache
	} else if cache, ok := s.MemoryStats.Stats["total_inactive_file"]; ok && cache < usage {
		usage -= cache
	} else if cache, ok := s.MemoryStats.Stats["cache"]; ok && cache < usage {
		usage -= cache
	}

	usedMB = int64(usage / (1024 * 1024))
	if s.MemoryStats.Limit > 0 {
		percent = round2(float64(usage) / float64(s.MemoryStats.Limit) * 100)
	}
	return usedMB, percent
}

func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	// Docker returns names with a leading slash.
	return strings.TrimPrefix(names[0], "/")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

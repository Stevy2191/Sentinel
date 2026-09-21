package main

import (
	"os"
	"strings"
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

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

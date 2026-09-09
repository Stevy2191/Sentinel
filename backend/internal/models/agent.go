package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Agent statuses.
const (
	// AgentPending is an agent that has been registered but has never
	// reported. Distinct from offline, which means it reported and then
	// stopped: one is work outstanding, the other is a fault.
	AgentPending = "pending"
	AgentActive  = "active"
	AgentOffline = "offline"
)

// Supported operating systems for install instructions. The value only selects
// which commands are shown; the agent itself is the same binary.
var agentOSTypes = map[string]bool{
	"ubuntu": true, "debian": true, "centos": true,
	"rhel": true, "windows": true, "linux": true,
}

// Agent configuration bounds.
const (
	MinAgentInterval = 1
	MaxAgentInterval = 3600
	MinAgentRetries  = 1
	MaxAgentRetries  = 10

	DefaultAgentInterval = 60
	DefaultAgentRetries  = 3

	// MaxAgentNameLength keeps a name to something a table column can show.
	MaxAgentNameLength = 100
)

// AgentOfflineAfter is how long without a heartbeat marks an agent offline.
//
// Generous relative to the five-minute heartbeat so a single missed beat — a
// brief network blip, a host under load — does not raise an alarm. Three
// consecutive misses is a real signal.
const AgentOfflineAfter = 16 * time.Minute

// Agent is a monitoring agent installed on a host.
type Agent struct {
	ID   uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name string    `json:"name" gorm:"column:name;not null"`

	// AgentID is the readable identifier the agent presents. It appears in
	// install commands and logs, where a UUID is awkward to read and type.
	AgentID string `json:"agent_id" gorm:"column:agent_id;not null;uniqueIndex"`

	// ServerToken is the shared secret. Cleared by HideToken before any list
	// response, so it only ever leaves the server on creation or on an
	// explicit admin fetch of one agent.
	ServerToken string `json:"server_token,omitempty" gorm:"column:server_token;not null;uniqueIndex"`

	OSType        string `json:"os_type" gorm:"column:os_type;not null;default:linux"`
	CheckInterval int    `json:"check_interval" gorm:"column:check_interval;not null;default:60"`
	RetryAttempts int    `json:"retry_attempts" gorm:"column:retry_attempts;not null;default:3"`

	Status        string     `json:"status" gorm:"column:status;not null;default:pending"`
	LastHeartbeat *time.Time `json:"last_heartbeat" gorm:"column:last_heartbeat"`
	IPAddress     *string    `json:"ip_address" gorm:"column:ip_address"`
	Hostname      *string    `json:"hostname" gorm:"column:hostname"`
	OSVersion     *string    `json:"os_version" gorm:"column:os_version"`
	AgentVersion  *string    `json:"agent_version" gorm:"column:agent_version"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table, since GORM would otherwise pluralise to "agents"
// by convention and quietly break if the convention changed.
func (Agent) TableName() string { return "agents" }

// HideToken clears the secret for responses that list agents.
//
// The token is a credential: anyone holding it can submit metrics as this
// agent. It is returned once when the agent is created, and afterwards only
// from the single-agent admin endpoint that exists to rebuild the install
// command.
func (a *Agent) HideToken() { a.ServerToken = "" }

// DeriveStatus reports what the agent's status should be given its last
// heartbeat. An agent that has never reported stays pending.
func (a *Agent) DeriveStatus(now time.Time) string {
	if a.LastHeartbeat == nil {
		return AgentPending
	}
	if now.Sub(*a.LastHeartbeat) > AgentOfflineAfter {
		return AgentOffline
	}
	return AgentActive
}

// ValidateAgentOS reports whether the OS type is one we generate instructions
// for.
func ValidateAgentOS(os string) error {
	if !agentOSTypes[strings.ToLower(strings.TrimSpace(os))] {
		return fmt.Errorf("os_type must be one of ubuntu, debian, centos, rhel, windows, linux")
	}
	return nil
}

// NewAgentID returns a readable, unique identifier, e.g. "agent_9f2c4b1e8a".
func NewAgentID() (string, error) {
	suffix, err := randomHex(5)
	if err != nil {
		return "", err
	}
	return "agent_" + suffix, nil
}

// NewServerToken returns the agent's shared secret, e.g. "srv_<64 hex>".
//
// 32 bytes from crypto/rand. The prefix is there so a leaked token is
// recognisable for what it is in a log or a paste, which makes it likelier to
// be revoked rather than ignored.
func NewServerToken() (string, error) {
	suffix, err := randomHex(32)
	if err != nil {
		return "", err
	}
	return "srv_" + suffix, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random identifier: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// AgentMetric is one collection cycle's system metrics.
type AgentMetric struct {
	ID      int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	AgentID uuid.UUID `json:"agent_id" gorm:"column:agent_id;type:uuid;not null;index"`

	Timestamp time.Time `json:"timestamp" gorm:"column:timestamp;not null"`

	CPUPercent    *float64 `json:"cpu_percent" gorm:"column:cpu_percent"`
	MemoryPercent *float64 `json:"memory_percent" gorm:"column:memory_percent"`
	MemoryUsedMB  *int64   `json:"memory_used_mb" gorm:"column:memory_used_mb"`
	MemoryTotalMB *int64   `json:"memory_total_mb" gorm:"column:memory_total_mb"`
	DiskPercent   *float64 `json:"disk_percent" gorm:"column:disk_percent"`
	DiskUsedGB    *float64 `json:"disk_used_gb" gorm:"column:disk_used_gb"`
	DiskTotalGB   *float64 `json:"disk_total_gb" gorm:"column:disk_total_gb"`
	UptimeSeconds *int64   `json:"uptime_seconds" gorm:"column:uptime_seconds"`

	LoadAverage1m  *float64 `json:"load_average_1m" gorm:"column:load_average_1m"`
	LoadAverage5m  *float64 `json:"load_average_5m" gorm:"column:load_average_5m"`
	LoadAverage15m *float64 `json:"load_average_15m" gorm:"column:load_average_15m"`

	NetworkInBytes  *int64 `json:"network_in_bytes" gorm:"column:network_in_bytes"`
	NetworkOutBytes *int64 `json:"network_out_bytes" gorm:"column:network_out_bytes"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (AgentMetric) TableName() string { return "agent_metrics" }

// AgentContainer is one container's metrics from a collection cycle.
type AgentContainer struct {
	ID      int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	AgentID uuid.UUID `json:"agent_id" gorm:"column:agent_id;type:uuid;not null;index"`

	ContainerID   string  `json:"container_id" gorm:"column:container_id;not null"`
	ContainerName string  `json:"container_name" gorm:"column:container_name"`
	Image         string  `json:"image" gorm:"column:image"`
	Status        string  `json:"status" gorm:"column:status"`
	CPUPercent    float64 `json:"cpu_percent" gorm:"column:cpu_percent"`
	MemoryPercent float64 `json:"memory_percent" gorm:"column:memory_percent"`
	MemoryUsedMB  int64   `json:"memory_used_mb" gorm:"column:memory_used_mb"`

	Timestamp time.Time `json:"timestamp" gorm:"column:timestamp;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (AgentContainer) TableName() string { return "agent_containers" }

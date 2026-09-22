# Agent Resource Thresholds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator configure a per-server CPU/memory/disk usage threshold that alerts once when crossed and once when recovered, reusing the existing offline-alert architecture.

**Architecture:** A pure decision function (`evaluateThreshold`) decides, given an incoming sample and the agent's stored threshold/active-flag, whether an alert just opened, just closed, or nothing changed. `AgentService.RecordMetrics` calls it for each of the three metrics inside its existing transaction, then fires a hook (mirroring the existing `SetStatusChangeHook`) after commit. `main.go` turns that hook into a `NotificationMessage` with a new `"warning"` status, which five notification plugins learn to render distinctly from `"down"`/`"recovered"`.

**Tech Stack:** Go (Gin, GORM, Postgres), React + TypeScript.

**Spec:** `docs/superpowers/specs/2026-09-21-agent-resource-thresholds-design.md`

## Global Constraints

- Three metrics only: CPU%, memory%, disk% — no network or load-average thresholds.
- Per-server thresholds, not a global default. New servers default to 90% (all three, enabled); existing servers get `NULL` (disabled) until edited.
- Alert on the first breach — no consecutive-sample requirement.
- One shared `agents.notify_channels` list covers both connectivity and threshold alerts.
- A breach is `value >= threshold`. A `nil` metric reading is skipped, never treated as 0%.
- Editing a threshold (raise, lower, or disable) resets its active-alert flag without notifying.
- New status value `"warning"` — never reuse `"down"` for a threshold breach.

---

### Task 1: Schema and model fields

**Files:**
- Create: `backend/migrations/042_agent_resource_thresholds.sql`
- Modify: `backend/internal/models/agent.go:33-44` (constants), `:54-104` (`Agent` struct)

**Interfaces:**
- Produces: `models.Agent.CPUThresholdPercent/MemoryThresholdPercent/DiskThresholdPercent *int`, `models.Agent.CPUAlertActive/MemoryAlertActive/DiskAlertActive bool`, `models.DefaultThresholdPercent = 90` — every later task reads these exact names.

- [ ] **Step 1: Write the migration**

```sql
-- Configurable resource-usage alert thresholds for server agents.
--
-- An agent could only alert on connectivity until now (029/040): went
-- offline, came back. Every metrics sample it reports - CPU, memory, disk -
-- was stored and shown, but nothing ever evaluated it. A server could sit at
-- 98% disk for weeks and Sentinel would never say a word.
--
-- No column-level default on the threshold values: an existing server must
-- not suddenly start alerting on numbers it has always run at. NULL means
-- "not configured". A new server's 90% default lives in Go
-- (models.DefaultThresholdPercent), applied by the create-agent handler, the
-- same way DefaultAgentInterval already works.
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS cpu_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS memory_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS disk_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS cpu_alert_active BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS memory_alert_active BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS disk_alert_active BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_cpu_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_cpu_threshold_check
    CHECK (cpu_threshold_percent IS NULL OR cpu_threshold_percent BETWEEN 1 AND 100);

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_memory_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_memory_threshold_check
    CHECK (memory_threshold_percent IS NULL OR memory_threshold_percent BETWEEN 1 AND 100);

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_disk_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_disk_threshold_check
    CHECK (disk_threshold_percent IS NULL OR disk_threshold_percent BETWEEN 1 AND 100);
```

- [ ] **Step 2: Verify the migration applies cleanly, including against a table that already has rows**

Run (spins up a scratch Postgres, applies every migration up to but not including the new one — the same state the real production database is in today — inserts a row, then applies the new migration on top of it):

```bash
docker run -d --name threshold-migration-check -e POSTGRES_DB=sentinel -e POSTGRES_USER=sentinel -e POSTGRES_PASSWORD=test -p 127.0.0.1:55432:5432 postgres:16-alpine
sleep 3
for f in $(ls backend/migrations/*.sql | sort -V | grep -v 042_agent_resource_thresholds); do
  echo "applying $f"
  PGPASSWORD=test psql -h 127.0.0.1 -p 55432 -U sentinel -d sentinel -f "$f" || break
done
PGPASSWORD=test psql -h 127.0.0.1 -p 55432 -U sentinel -d sentinel -c \
  "INSERT INTO agents (name, agent_id, server_token) VALUES ('existing-server', 'agent_deadbeef01', 'srv_test');"
echo "applying 042_agent_resource_thresholds.sql"
PGPASSWORD=test psql -h 127.0.0.1 -p 55432 -U sentinel -d sentinel -f backend/migrations/042_agent_resource_thresholds.sql
PGPASSWORD=test psql -h 127.0.0.1 -p 55432 -U sentinel -d sentinel -c \
  "SELECT cpu_threshold_percent, cpu_alert_active FROM agents WHERE agent_id = 'agent_deadbeef01';"
docker rm -f threshold-migration-check
```

Expected: no `ERROR:` line anywhere, and the final `SELECT` shows the pre-existing row got `cpu_threshold_percent` as `NULL` and `cpu_alert_active` as `f` — confirming an already-deployed server is left disabled, not retroactively opted into alerting.

- [ ] **Step 3: Add the model fields**

In `backend/internal/models/agent.go`, add to the constants block:

```go
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

	// DefaultThresholdPercent is what a newly created server's CPU/memory/disk
	// alert thresholds default to. A server added to Sentinel should be
	// watched from the start, not silently unmonitored until someone remembers.
	DefaultThresholdPercent = 90
)
```

Add to the `Agent` struct, after `NotifyChannels` (currently ending at line 86):

```go
	// CPU/memory/disk alert thresholds, each independently optional. nil means
	// that metric is not watched. A breach fires once when crossed and once
	// when it recovers - see AgentService.RecordMetrics.
	CPUThresholdPercent    *int `json:"cpu_threshold_percent" gorm:"column:cpu_threshold_percent"`
	MemoryThresholdPercent *int `json:"memory_threshold_percent" gorm:"column:memory_threshold_percent"`
	DiskThresholdPercent   *int `json:"disk_threshold_percent" gorm:"column:disk_threshold_percent"`
	// *AlertActive is true while a threshold breach is open, mirroring how
	// Status itself gates the offline alert: the flag is the one-shot record
	// of "is there an open alert for this metric right now", not a counter.
	CPUAlertActive    bool `json:"-" gorm:"column:cpu_alert_active"`
	MemoryAlertActive bool `json:"-" gorm:"column:memory_alert_active"`
	DiskAlertActive   bool `json:"-" gorm:"column:disk_alert_active"`
```

(`json:"-"`: the active flags are internal bookkeeping, not something a settings form needs to see or send back.)

- [ ] **Step 4: Verify it builds**

Run: `cd backend && go build ./...`
Expected: no output, exit code 0.

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/042_agent_resource_thresholds.sql backend/internal/models/agent.go
git commit -m "feat(agents): add resource-threshold columns to the agents table"
```

---

### Task 2: Pure threshold-evaluation logic

**Files:**
- Create: `backend/internal/services/agent_thresholds.go`
- Modify: `backend/internal/services/agent_service.go:22-28` (`AgentService` struct — adds the `onThresholdChange` field the new file's hook methods need)
- Test: `backend/internal/services/agent_thresholds_test.go`

**Interfaces:**
- Consumes: nothing new (only stdlib + `models` already imported by the package).
- Produces: `services.ThresholdMetricCPU/ThresholdMetricMemory/ThresholdMetricDisk` (string consts), `services.AgentThresholdChange{Agent models.Agent; Metric string; Value float64; Threshold int; Breached bool}`, `evaluateThreshold(value *float64, threshold *int, currentlyActive bool) thresholdTransition` (unexported, used by Task 3), `normalizeThreshold(v *int) *int` (unexported, used by Task 4), `(*AgentService).SetThresholdChangeHook(fn func(context.Context, AgentThresholdChange))`, `(*AgentService).notifyThresholdChange(ctx, AgentThresholdChange)` (unexported, used by Task 3).

- [ ] **Step 1: Write the failing tests**

```go
package services

import "testing"

func TestEvaluateThreshold(t *testing.T) {
	val := func(v float64) *float64 { return &v }
	thr := func(v int) *int { return &v }

	cases := []struct {
		name           string
		value          *float64
		threshold      *int
		currentlyActive bool
		wantActive     bool
		wantNotify     bool
		wantBreached   bool
	}{
		{"not configured, no value", nil, nil, false, false, false, false},
		{"not configured, has a value", val(95), nil, false, false, false, false},
		{"configured, no reading this cycle", nil, thr(90), false, false, false, false},
		{"configured, no reading this cycle, was already active", nil, thr(90), true, true, false, false},
		{"under threshold, was ok", val(50), thr(90), false, false, false, false},
		{"under threshold, was already active", val(50), thr(90), true, false, true, false},
		{"exactly at threshold breaches", val(90), thr(90), false, true, true, true},
		{"over threshold, first breach", val(95), thr(90), false, true, true, true},
		{"over threshold, already active", val(96), thr(90), true, true, false, false},
		{"drops back to exactly under threshold recovers", val(89), thr(90), true, false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evaluateThreshold(c.value, c.threshold, c.currentlyActive)
			if got.newActive != c.wantActive {
				t.Errorf("newActive = %v, want %v", got.newActive, c.wantActive)
			}
			if got.notify != c.wantNotify {
				t.Errorf("notify = %v, want %v", got.notify, c.wantNotify)
			}
			if got.breached != c.wantBreached {
				t.Errorf("breached = %v, want %v", got.breached, c.wantBreached)
			}
		})
	}
}

func TestNormalizeThreshold(t *testing.T) {
	v := func(n int) *int { return &n }

	if got := normalizeThreshold(nil); got != nil {
		t.Errorf("nil input should stay nil, got %v", *got)
	}
	if got := normalizeThreshold(v(0)); got != nil {
		t.Errorf("0 is the clear sentinel and should normalize to nil, got %v", *got)
	}
	if got := normalizeThreshold(v(90)); got == nil || *got != 90 {
		t.Errorf("90 should pass through unchanged, got %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/services/... -run 'TestEvaluateThreshold|TestNormalizeThreshold' -v`
Expected: FAIL — `evaluateThreshold`/`normalizeThreshold`/`thresholdTransition` undefined.

- [ ] **Step 3: Write the implementation**

```go
package services

import "context"

// Names for the three metrics a resource threshold can watch. Used both as
// the AgentThresholdChange.Metric value and, in main.go, as the key into a
// per-metric human label for the alert text.
const (
	ThresholdMetricCPU    = "cpu"
	ThresholdMetricMemory = "memory"
	ThresholdMetricDisk   = "disk"
)

// AgentThresholdChange describes a resource metric crossing its configured
// alert threshold, in either direction.
type AgentThresholdChange struct {
	Agent models.Agent
	// Metric is one of the ThresholdMetric* constants.
	Metric string
	// Value is the sample that caused the transition.
	Value float64
	// Threshold is the configured value it crossed.
	Threshold int
	// Breached is true when the metric just crossed at-or-over the
	// threshold, false when it just dropped back under it.
	Breached bool
}

// SetThresholdChangeHook registers a callback invoked when a metric crosses
// its configured alert threshold, in either direction.
//
// A hook rather than a notification manager dependency, for the same reason
// SetStatusChangeHook is one: this service's job is agent state, and wiring
// delivery into it would pull the whole notification graph in behind it.
// main decides what a transition means.
func (s *AgentService) SetThresholdChangeHook(fn func(context.Context, AgentThresholdChange)) {
	s.onThresholdChange = fn
}

func (s *AgentService) notifyThresholdChange(ctx context.Context, change AgentThresholdChange) {
	if s.onThresholdChange == nil {
		return
	}
	s.onThresholdChange(ctx, change)
}

// thresholdTransition is what evaluating one metric against its configured
// threshold decided.
type thresholdTransition struct {
	// newActive is what the agent's *AlertActive column should become.
	newActive bool
	// notify is true only when this cycle's result differs from the stored
	// flag - a real transition, not a repeat of the current state.
	notify bool
	// breached is the direction of the notification when notify is true:
	// true means "just crossed over", false means "just recovered".
	breached bool
}

// evaluateThreshold decides whether a metric's alert-active flag should
// change, given the incoming sample, the configured threshold (nil means
// not configured), and the flag's current value.
//
// A nil value - a reading that was not reported this cycle, which every
// AgentMetric field allows for exactly this reason - leaves the current
// state exactly as it is: a missing sample must never look like a sudden
// drop to 0% and silently recover an open alert.
func evaluateThreshold(value *float64, threshold *int, currentlyActive bool) thresholdTransition {
	if value == nil || threshold == nil {
		return thresholdTransition{newActive: currentlyActive}
	}
	breached := *value >= float64(*threshold)
	switch {
	case breached && !currentlyActive:
		return thresholdTransition{newActive: true, notify: true, breached: true}
	case !breached && currentlyActive:
		return thresholdTransition{newActive: false, notify: true, breached: false}
	default:
		return thresholdTransition{newActive: currentlyActive}
	}
}

// normalizeThreshold turns the wire's 0-or-value sentinel into what gets
// stored: 0 clears the threshold (nil), matching how an empty
// ip_address_override clears that field elsewhere in this service. Anything
// else passes through unchanged.
func normalizeThreshold(v *int) *int {
	if v == nil || *v == 0 {
		return nil
	}
	return v
}
```

Add the import this file needs, and the new struct field the hook methods use, to the existing files:

In `backend/internal/services/agent_service.go`, add `"github.com/Stevy2191/Sentinel/backend/internal/models"` is already imported; add one field to the `AgentService` struct (currently lines 22-28):

```go
type AgentService struct {
	db     *gorm.DB
	logger *log.Logger
	// onStatusChange is called when an agent goes silent or starts reporting
	// again. Optional: nil simply means nothing is listening.
	onStatusChange func(context.Context, AgentStatusChange)
	// onThresholdChange is called when a metric crosses its configured
	// threshold, in either direction. Optional, same reasoning.
	onThresholdChange func(context.Context, AgentThresholdChange)
}
```

And add `"github.com/Stevy2191/Sentinel/backend/internal/models"` import to the new file:

```go
package services

import (
	"context"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/services/... -run 'TestEvaluateThreshold|TestNormalizeThreshold' -v`
Expected: PASS, all subtests green.

- [ ] **Step 5: Run the full test suite and vet**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: no vet warnings, all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/agent_thresholds.go backend/internal/services/agent_thresholds_test.go backend/internal/services/agent_service.go
git commit -m "feat(agents): add pure threshold-crossing decision logic"
```

---

### Task 3: Evaluate thresholds on every incoming metrics sample

**Files:**
- Modify: `backend/internal/services/agent_service.go:326-374` (`RecordMetrics`)

**Interfaces:**
- Consumes: `evaluateThreshold`, `thresholdTransition`, `AgentThresholdChange`, `ThresholdMetricCPU/Memory/Disk`, `(*AgentService).notifyThresholdChange` (all from Task 2).
- Produces: `RecordMetrics`'s new behavior — every later task (the API handler, main.go) relies on threshold notifications now firing from here.

- [ ] **Step 1: Replace `RecordMetrics`**

Find this in `backend/internal/services/agent_service.go`:

```go
func (s *AgentService) RecordMetrics(ctx context.Context, agent *models.Agent, metric *models.AgentMetric, containers []models.AgentContainer) error {
	// Metrics arriving counts as a heartbeat, so this is also where a silent
	// agent can come back — the recovery has to be announced from here too, or
	// an agent that only submits metrics would recover without a word.
	wasOffline := agent.Status == models.AgentOffline
	if metric.Timestamp.IsZero() {
		metric.Timestamp = time.Now()
	}
	metric.AgentID = agent.ID

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(metric).Error; err != nil {
			return fmt.Errorf("storing metrics: %w", err)
		}
		if len(containers) > 0 {
			for i := range containers {
				containers[i].AgentID = agent.ID
				if containers[i].Timestamp.IsZero() {
					containers[i].Timestamp = metric.Timestamp
				}
			}
			// One statement rather than one per container: a busy host can
			// report dozens, and a round trip each would make the write cost
			// scale with how much the host is running.
			if err := tx.CreateInBatches(containers, 100).Error; err != nil {
				return fmt.Errorf("storing container metrics: %w", err)
			}
		}
		return tx.Model(&models.Agent{}).Where("id = ?", agent.ID).
			Updates(map[string]interface{}{
				"last_heartbeat": time.Now(),
				"status":         models.AgentActive,
				"updated_at":     time.Now(),
			}).Error
	})
	if err != nil {
		return err
	}
	if wasOffline {
		s.notifyStatusChange(ctx, AgentStatusChange{
			Agent: *agent, From: models.AgentOffline, To: models.AgentActive,
		})
	}
	return nil
}
```

Replace it with:

```go
func (s *AgentService) RecordMetrics(ctx context.Context, agent *models.Agent, metric *models.AgentMetric, containers []models.AgentContainer) error {
	// Metrics arriving counts as a heartbeat, so this is also where a silent
	// agent can come back — the recovery has to be announced from here too, or
	// an agent that only submits metrics would recover without a word.
	wasOffline := agent.Status == models.AgentOffline
	if metric.Timestamp.IsZero() {
		metric.Timestamp = time.Now()
	}
	metric.AgentID = agent.ID

	// Decided before the transaction: the decision only reads the incoming
	// sample and the agent's already-loaded state, and doing it here keeps
	// the transaction body free of anything but database writes.
	cpu := evaluateThreshold(metric.CPUPercent, agent.CPUThresholdPercent, agent.CPUAlertActive)
	mem := evaluateThreshold(metric.MemoryPercent, agent.MemoryThresholdPercent, agent.MemoryAlertActive)
	disk := evaluateThreshold(metric.DiskPercent, agent.DiskThresholdPercent, agent.DiskAlertActive)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(metric).Error; err != nil {
			return fmt.Errorf("storing metrics: %w", err)
		}
		if len(containers) > 0 {
			for i := range containers {
				containers[i].AgentID = agent.ID
				if containers[i].Timestamp.IsZero() {
					containers[i].Timestamp = metric.Timestamp
				}
			}
			// One statement rather than one per container: a busy host can
			// report dozens, and a round trip each would make the write cost
			// scale with how much the host is running.
			if err := tx.CreateInBatches(containers, 100).Error; err != nil {
				return fmt.Errorf("storing container metrics: %w", err)
			}
		}
		return tx.Model(&models.Agent{}).Where("id = ?", agent.ID).
			Updates(map[string]interface{}{
				"last_heartbeat":      time.Now(),
				"status":              models.AgentActive,
				"updated_at":          time.Now(),
				"cpu_alert_active":    cpu.newActive,
				"memory_alert_active": mem.newActive,
				"disk_alert_active":   disk.newActive,
			}).Error
	})
	if err != nil {
		return err
	}
	if wasOffline {
		s.notifyStatusChange(ctx, AgentStatusChange{
			Agent: *agent, From: models.AgentOffline, To: models.AgentActive,
		})
	}
	// Notified only after the write commits, same reasoning as the offline
	// recovery above: never announce a change that could still roll back.
	if cpu.notify {
		s.notifyThresholdChange(ctx, AgentThresholdChange{
			Agent: *agent, Metric: ThresholdMetricCPU,
			Value: *metric.CPUPercent, Threshold: *agent.CPUThresholdPercent, Breached: cpu.breached,
		})
	}
	if mem.notify {
		s.notifyThresholdChange(ctx, AgentThresholdChange{
			Agent: *agent, Metric: ThresholdMetricMemory,
			Value: *metric.MemoryPercent, Threshold: *agent.MemoryThresholdPercent, Breached: mem.breached,
		})
	}
	if disk.notify {
		s.notifyThresholdChange(ctx, AgentThresholdChange{
			Agent: *agent, Metric: ThresholdMetricDisk,
			Value: *metric.DiskPercent, Threshold: *agent.DiskThresholdPercent, Breached: disk.breached,
		})
	}
	return nil
}
```

(Dereferencing `*metric.CPUPercent`/`*agent.CPUThresholdPercent` etc. is always safe here: `evaluateThreshold` only sets `notify: true` after its own nil-check on both, per Task 2's implementation.)

- [ ] **Step 2: Verify it builds and existing tests still pass**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: no errors, all packages `ok`. (No new test in this task: `RecordMetrics` needs a real Postgres to exercise, which no test in this codebase does today — Task 2's pure-function tests already cover every branch of the decision logic this task wires in.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/services/agent_service.go
git commit -m "feat(agents): evaluate resource thresholds on every metrics sample"
```

---

### Task 4: Let an operator configure thresholds

**Files:**
- Modify: `backend/internal/services/agent_service.go:206-243` (`AgentSettings`, `Update`)

**Interfaces:**
- Consumes: `normalizeThreshold` (Task 2).
- Produces: `AgentSettings.CPUThresholdPercent/MemoryThresholdPercent/DiskThresholdPercent *int` — Task 5 (the API handler) sets these.

- [ ] **Step 1: Extend `AgentSettings` and `Update`**

Find:

```go
type AgentSettings struct {
	Name          string
	OSType        string
	CheckInterval int
	RetryAttempts int
	// IPOverride nil clears any pinned address.
	IPOverride *string
	// NotifyChannels is applied only when non-nil. Nil means "leave as is",
	// which is different from an empty slice meaning "alert nowhere".
	NotifyChannels *models.StringSlice
}

func (s *AgentService) Update(ctx context.Context, agentID string, settings AgentSettings) (*models.Agent, error) {
	agent, err := s.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name":           settings.Name,
		"os_type":        settings.OSType,
		"check_interval": settings.CheckInterval,
		"retry_attempts": settings.RetryAttempts,
		// nil clears it, which is how an operator goes back to detection.
		"ip_address_override": settings.IPOverride,
		"updated_at":          time.Now(),
	}
	// Applied only when supplied, so an update that says nothing about
	// notifications leaves the current selection alone rather than silently
	// resetting it to "every channel".
	if settings.NotifyChannels != nil {
		updates["notify_channels"] = *settings.NotifyChannels
	}
	if err := s.db.WithContext(ctx).Model(&models.Agent{}).
		Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("updating agent %s: %w", agentID, err)
	}
	return s.Get(ctx, agentID)
}
```

Replace with:

```go
type AgentSettings struct {
	Name          string
	OSType        string
	CheckInterval int
	RetryAttempts int
	// IPOverride nil clears any pinned address.
	IPOverride *string
	// NotifyChannels is applied only when non-nil. Nil means "leave as is",
	// which is different from an empty slice meaning "alert nowhere".
	NotifyChannels *models.StringSlice
	// Each threshold is applied only when non-nil - nil means "leave this
	// threshold as it is". Within a non-nil pointer, 0 disables the
	// threshold and 1-100 sets it: the same sentinel-value convention
	// IPOverride's empty string already uses for "clear", rather than a
	// second convention for the same idea (see normalizeThreshold).
	CPUThresholdPercent    *int
	MemoryThresholdPercent *int
	DiskThresholdPercent   *int
}

func (s *AgentService) Update(ctx context.Context, agentID string, settings AgentSettings) (*models.Agent, error) {
	agent, err := s.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name":           settings.Name,
		"os_type":        settings.OSType,
		"check_interval": settings.CheckInterval,
		"retry_attempts": settings.RetryAttempts,
		// nil clears it, which is how an operator goes back to detection.
		"ip_address_override": settings.IPOverride,
		"updated_at":          time.Now(),
	}
	// Applied only when supplied, so an update that says nothing about
	// notifications leaves the current selection alone rather than silently
	// resetting it to "every channel".
	if settings.NotifyChannels != nil {
		updates["notify_channels"] = *settings.NotifyChannels
	}
	// Editing a threshold - raising it, lowering it, or disabling it -
	// resets its active-alert flag without notifying: nothing about the
	// server itself changed, only what is being watched. The next incoming
	// sample re-evaluates fresh against whatever the threshold now is.
	if settings.CPUThresholdPercent != nil {
		updates["cpu_threshold_percent"] = normalizeThreshold(settings.CPUThresholdPercent)
		updates["cpu_alert_active"] = false
	}
	if settings.MemoryThresholdPercent != nil {
		updates["memory_threshold_percent"] = normalizeThreshold(settings.MemoryThresholdPercent)
		updates["memory_alert_active"] = false
	}
	if settings.DiskThresholdPercent != nil {
		updates["disk_threshold_percent"] = normalizeThreshold(settings.DiskThresholdPercent)
		updates["disk_alert_active"] = false
	}
	if err := s.db.WithContext(ctx).Model(&models.Agent{}).
		Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("updating agent %s: %w", agentID, err)
	}
	return s.Get(ctx, agentID)
}
```

- [ ] **Step 2: Verify it builds**

Run: `cd backend && go build ./... && go vet ./...`
Expected: no errors. (`normalizeThreshold`'s own behavior is already covered by Task 2's test; nothing new and pure to test here.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/services/agent_service.go
git commit -m "feat(agents): let thresholds be configured through Update"
```

---

### Task 5: API request/response wiring

**Files:**
- Modify: `backend/internal/api/agent_handler.go:78-196` (`createAgentRequest`, `CreateAgentHandler`), `:233-305` (`updateAgentRequest`, `UpdateAgentHandler`)
- Test: `backend/internal/api/agent_handler_test.go` (new)

**Interfaces:**
- Consumes: `models.DefaultThresholdPercent` (Task 1), `services.AgentSettings` fields (Task 4).
- Produces: `validateThreshold(c *gin.Context, name string, v *int) bool` (unexported) — a pure-enough helper this task's own test covers directly.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestValidateThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}
	v := func(n int) *int { return &n }

	cases := []struct {
		name string
		in   *int
		want bool
	}{
		{"nil is valid (not sent)", nil, true},
		{"0 is valid (the clear sentinel)", v(0), true},
		{"1 is valid", v(1), true},
		{"100 is valid", v(100), true},
		{"101 is invalid", v(101), false},
		{"negative is invalid", v(-1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateThreshold(newCtx(), "cpu_threshold_percent", c.in); got != c.want {
				t.Errorf("validateThreshold(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/api/... -run TestValidateThreshold -v`
Expected: FAIL — `validateThreshold` undefined.

- [ ] **Step 3: Add the request fields, validation helper, and wire both handlers**

In `backend/internal/api/agent_handler.go`, add to `createAgentRequest` (currently lines 78-89):

```go
type createAgentRequest struct {
	Name          string `json:"name"`
	OSType        string `json:"os_type"`
	CheckInterval *int   `json:"check_interval"`
	RetryAttempts *int   `json:"retry_attempts"`
	// IPAddressOverride pins the address this host is recorded under. Empty
	// means use whatever the agent detects.
	IPAddressOverride *string `json:"ip_address_override"`
	// NotifyChannels selects where this agent alerts. Omitted means every
	// enabled channel; an explicit empty list means nowhere.
	NotifyChannels *[]string `json:"notify_channels"`
	// Each threshold: omitted or 0 disables it, 1-100 sets it. Omitted
	// (rather than DefaultThresholdPercent) is treated as "use the default"
	// only by CreateAgentHandler, which is the one place a brand-new
	// threshold configuration is decided.
	CPUThresholdPercent    *int `json:"cpu_threshold_percent"`
	MemoryThresholdPercent *int `json:"memory_threshold_percent"`
	DiskThresholdPercent   *int `json:"disk_threshold_percent"`
}
```

Add the validation helper right after `validateAgentSettings` (currently ending at line 114):

```go
// validateThreshold checks a threshold value against the wire's 0-or-1-100
// convention (see normalizeThreshold in the services package): nil means
// "not sent", 0 means "disable", 1-100 is a real value. Anything else is
// rejected with a message naming the field, since create and update each
// validate three of these under different field names.
func validateThreshold(c *gin.Context, field string, v *int) bool {
	if v == nil {
		return true
	}
	if *v < 0 || *v > 100 {
		respondError(c, http.StatusBadRequest, field+" must be between 1 and 100, or 0 to disable it")
		return false
	}
	return true
}
```

In `CreateAgentHandler`, find:

```go
		override, ok := parseIPOverride(c, req.IPAddressOverride)
		if !ok {
			return
		}

		agent := &models.Agent{
			Name:              name,
			OSType:            osType,
			CheckInterval:     interval,
			RetryAttempts:     retries,
			IPAddressOverride: override,
		}
		// nil is left as nil on purpose: it means "every enabled channel",
		// which is the right default for a server that has gone silent.
		if req.NotifyChannels != nil {
			agent.NotifyChannels = models.StringSlice(*req.NotifyChannels)
		}
```

Replace with:

```go
		override, ok := parseIPOverride(c, req.IPAddressOverride)
		if !ok {
			return
		}
		if !validateThreshold(c, "cpu_threshold_percent", req.CPUThresholdPercent) ||
			!validateThreshold(c, "memory_threshold_percent", req.MemoryThresholdPercent) ||
			!validateThreshold(c, "disk_threshold_percent", req.DiskThresholdPercent) {
			return
		}

		agent := &models.Agent{
			Name:              name,
			OSType:            osType,
			CheckInterval:     interval,
			RetryAttempts:     retries,
			IPAddressOverride: override,
			// A newly added server should be watched from the start: omitted
			// defaults to DefaultThresholdPercent rather than "disabled",
			// unlike an update, where omitted means "leave alone" (there is
			// nothing yet to leave alone here). An explicit 0 still disables
			// it for whoever unchecks a threshold before submitting.
			CPUThresholdPercent:    orDefaultThreshold(req.CPUThresholdPercent),
			MemoryThresholdPercent: orDefaultThreshold(req.MemoryThresholdPercent),
			DiskThresholdPercent:   orDefaultThreshold(req.DiskThresholdPercent),
		}
		// nil is left as nil on purpose: it means "every enabled channel",
		// which is the right default for a server that has gone silent.
		if req.NotifyChannels != nil {
			agent.NotifyChannels = models.StringSlice(*req.NotifyChannels)
		}
```

Add `orDefaultThreshold` next to `validateThreshold`:

```go
// orDefaultThreshold resolves a create request's threshold: omitted becomes
// the default (a new server should be watched from the start), an explicit
// 0 still disables it, and anything else passes through.
func orDefaultThreshold(v *int) *int {
	if v == nil {
		d := models.DefaultThresholdPercent
		return &d
	}
	if *v == 0 {
		return nil
	}
	return v
}
```

In `updateAgentRequest` (currently lines 233-242), add the three fields:

```go
type updateAgentRequest struct {
	Name              *string `json:"name"`
	OSType            *string `json:"os_type"`
	CheckInterval     *int    `json:"check_interval"`
	RetryAttempts     *int    `json:"retry_attempts"`
	IPAddressOverride *string `json:"ip_address_override"`
	// NotifyChannels omitted leaves the current selection alone; an explicit
	// empty list turns alerts off for this agent.
	NotifyChannels *[]string `json:"notify_channels"`
	// Each threshold: omitted leaves it as it is, 0 disables it, 1-100 sets
	// it (see normalizeThreshold in the services package).
	CPUThresholdPercent    *int `json:"cpu_threshold_percent"`
	MemoryThresholdPercent *int `json:"memory_threshold_percent"`
	DiskThresholdPercent   *int `json:"disk_threshold_percent"`
}
```

In `UpdateAgentHandler`, find:

```go
		settings := services.AgentSettings{
			Name:          name,
			OSType:        osType,
			CheckInterval: interval,
			RetryAttempts: retries,
			IPOverride:    override,
		}
		if req.NotifyChannels != nil {
			channels := models.StringSlice(*req.NotifyChannels)
			settings.NotifyChannels = &channels
		}
```

Replace with:

```go
		if !validateThreshold(c, "cpu_threshold_percent", req.CPUThresholdPercent) ||
			!validateThreshold(c, "memory_threshold_percent", req.MemoryThresholdPercent) ||
			!validateThreshold(c, "disk_threshold_percent", req.DiskThresholdPercent) {
			return
		}

		settings := services.AgentSettings{
			Name:                   name,
			OSType:                 osType,
			CheckInterval:          interval,
			RetryAttempts:          retries,
			IPOverride:             override,
			CPUThresholdPercent:    req.CPUThresholdPercent,
			MemoryThresholdPercent: req.MemoryThresholdPercent,
			DiskThresholdPercent:   req.DiskThresholdPercent,
		}
		if req.NotifyChannels != nil {
			channels := models.StringSlice(*req.NotifyChannels)
			settings.NotifyChannels = &channels
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/api/... -run TestValidateThreshold -v`
Expected: PASS, all subtests green.

- [ ] **Step 5: Run the full backend suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: no errors, all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/agent_handler.go backend/internal/api/agent_handler_test.go
git commit -m "feat(agents): accept resource thresholds on create and update"
```

---

### Task 6: Deliver the notification

**Files:**
- Modify: `backend/cmd/sentinel/main.go:350-353` (hook wiring), after `:844` (new function alongside `notifyAgentStatusChange`)

**Interfaces:**
- Consumes: `services.AgentThresholdChange`, `services.ThresholdMetricCPU/Memory/Disk`, `(*services.AgentService).SetThresholdChangeHook` (Task 2/3).
- Produces: a `NotificationMessage` with `Status: "warning"` — Task 7-11 make every plugin render it correctly.

- [ ] **Step 1: Wire the hook**

Find in `backend/cmd/sentinel/main.go`:

```go
	agentService.SetStatusChangeHook(func(ctx context.Context, change services.AgentStatusChange) {
		notifyAgentStatusChange(ctx, notificationManager, change)
	})
	go agentService.StartOfflineSweep(loopCtx)
```

Replace with:

```go
	agentService.SetStatusChangeHook(func(ctx context.Context, change services.AgentStatusChange) {
		notifyAgentStatusChange(ctx, notificationManager, change)
	})
	agentService.SetThresholdChangeHook(func(ctx context.Context, change services.AgentThresholdChange) {
		notifyAgentThresholdChange(ctx, notificationManager, change)
	})
	go agentService.StartOfflineSweep(loopCtx)
```

- [ ] **Step 2: Add `notifyAgentThresholdChange`**

Add this after `notifyAgentStatusChange` and its `lastContact` helper (after the closing brace that currently ends around line 852):

```go
// thresholdMetricLabels names each threshold metric for an alert's message
// text.
var thresholdMetricLabels = map[string]string{
	services.ThresholdMetricCPU:    "CPU usage",
	services.ThresholdMetricMemory: "Memory usage",
	services.ThresholdMetricDisk:   "Disk usage",
}

// notifyAgentThresholdChange alerts when a server's resource usage crosses a
// configured threshold, in either direction.
//
// Status is "warning", never "down": every plugin's status handling treats
// anything but the literal string "down" as green/good today, and reusing
// "down" here would have every channel announce the server is offline when
// it is actually still up and merely short on a resource.
func notifyAgentThresholdChange(
	ctx context.Context,
	notificationManager *notifications.NotificationManager,
	change services.AgentThresholdChange,
) {
	agent := change.Agent
	if !agent.NotifiesAnyChannel() {
		log.Printf("[agent] %s crossed its %s threshold but has notifications disabled", agent.Name, change.Metric)
		return
	}

	target := agent.AgentID
	if ip := agent.EffectiveIP(); ip != nil && *ip != "" {
		target = *ip
	}
	if agent.Hostname != nil && *agent.Hostname != "" {
		target = *agent.Hostname
	}

	label := thresholdMetricLabels[change.Metric]
	status := "warning"
	previous := ""
	message := fmt.Sprintf("%s is at %.0f%%, at or above the %d%% threshold.", label, change.Value, change.Threshold)
	if !change.Breached {
		status = "recovered"
		previous = "warning"
		message = fmt.Sprintf("%s is back under the %d%% threshold (currently %.0f%%).", label, change.Threshold, change.Value)
	}

	agentID := agent.ID
	if err := notificationManager.SendNotification(ctx, &notifications.NotificationMessage{
		AgentID:        &agentID,
		MonitorName:    agent.Name,
		MonitorURL:     target,
		Status:         status,
		Message:        message,
		PreviousStatus: previous,
		Timestamp:      time.Now(),
		Channels:       agent.NotifyChannels,
	}); err != nil {
		log.Printf("[agent] sending %s threshold notification for %s: %v", change.Metric, agent.Name, err)
	}
}
```

- [ ] **Step 3: Verify it builds**

Run: `cd backend && go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/sentinel/main.go
git commit -m "feat(agents): send a notification when a resource threshold is crossed"
```

---

### Task 7: Render "warning" in email

**Files:**
- Modify: `backend/internal/notifications/email.go` (`const` block near line 27, `buildSubject`, `statusStyle`)
- Test: `backend/internal/notifications/email_test.go` (add to existing file)

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/notifications/email_test.go`:

```go
func TestStatusStyleWarning(t *testing.T) {
	color, label := statusStyle("warning")
	if color != colorWarning {
		t.Errorf("expected colorWarning, got %q", color)
	}
	if label != "WARNING" {
		t.Errorf("expected WARNING, got %q", label)
	}
	// The regression this guards: before "warning" had its own case, an
	// unrecognized status fell through to the default branch, which is
	// harmless for statusStyle (it just upper-cases the status) but is
	// exactly the bug in the plugins that defaulted to green/good instead.
	if color == colorSuccess {
		t.Error("warning must not render as the success color")
	}
}

func TestBuildSubjectWarning(t *testing.T) {
	p := &EmailPlugin{}
	got := p.buildSubject(&NotificationMessage{MonitorName: "web-01", Status: "warning"})
	want := "[WARNING] web-01"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/notifications/... -run 'TestStatusStyleWarning|TestBuildSubjectWarning' -v`
Expected: FAIL — `colorWarning` undefined, and the subject falls through to the default `"[Sentinel] web-01 status: warning"` instead of `"[WARNING] web-01"`.

- [ ] **Step 3: Add the color constant and the two cases**

In `backend/internal/notifications/email.go`, find the color constants (currently lines 27-30):

```go
	colorSuccess = "#10b981" // emerald
	colorError   = "#ef4444" // red
	colorText    = "#334155" // slate
	colorMuted   = "#64748b"
```

Add `colorWarning` alongside them:

```go
	colorSuccess = "#10b981" // emerald
	colorError   = "#ef4444" // red
	colorWarning = "#f59e0b" // amber
	colorText    = "#334155" // slate
	colorMuted   = "#64748b"
```

Find `buildSubject`:

```go
func (p *EmailPlugin) buildSubject(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s is DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s has recovered", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}
```

Add a `"warning"` case:

```go
func (p *EmailPlugin) buildSubject(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s is DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s has recovered", m.MonitorName)
	case "warning":
		return fmt.Sprintf("[WARNING] %s", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}
```

Find `statusStyle`:

```go
func statusStyle(status string) (color, label string) {
	switch status {
	case "down":
		return colorError, "DOWN"
	case "up":
		return colorSuccess, "UP"
	case "recovered":
		return colorSuccess, "RECOVERED"
	default:
		return colorMuted, strings.ToUpper(status)
	}
}
```

Add a `"warning"` case:

```go
func statusStyle(status string) (color, label string) {
	switch status {
	case "down":
		return colorError, "DOWN"
	case "up":
		return colorSuccess, "UP"
	case "recovered":
		return colorSuccess, "RECOVERED"
	case "warning":
		return colorWarning, "WARNING"
	default:
		return colorMuted, strings.ToUpper(status)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/notifications/... -run 'TestStatusStyleWarning|TestBuildSubjectWarning' -v`
Expected: PASS.

- [ ] **Step 5: Run the full notifications package suite**

Run: `cd backend && go test ./internal/notifications/...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/notifications/email.go backend/internal/notifications/email_test.go
git commit -m "feat(notifications): render a warning status distinctly in email"
```

---

### Task 8: Render "warning" in Slack

**Files:**
- Modify: `backend/internal/notifications/slack.go` (`buildPayload`)
- Test: `backend/internal/notifications/slack_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package notifications

import "testing"

func TestSlackBuildPayloadWarning(t *testing.T) {
	p := &SlackPlugin{}
	payload := p.buildPayload(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if len(payload.Attachments) == 0 {
		t.Fatal("expected at least one attachment")
	}
	if payload.Attachments[0].Color != colorWarning {
		t.Errorf("got color %q, want colorWarning (%q)", payload.Attachments[0].Color, colorWarning)
	}
	if payload.Attachments[0].Color == colorSuccess {
		t.Error("warning must not render with the success color")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/notifications/... -run TestSlackBuildPayloadWarning -v`
Expected: FAIL — color is `colorSuccess` (the default), not `colorWarning`.

- [ ] **Step 3: Add the case**

In `backend/internal/notifications/slack.go`, find:

```go
func (p *SlackPlugin) buildPayload(m *NotificationMessage) slackPayload {
	emoji := "🟢"
	color := colorSuccess
	if m.Status == "down" {
		emoji = "🔴"
		color = colorError
	}
```

Replace with:

```go
func (p *SlackPlugin) buildPayload(m *NotificationMessage) slackPayload {
	emoji := "🟢"
	color := colorSuccess
	switch m.Status {
	case "down":
		emoji = "🔴"
		color = colorError
	case "warning":
		emoji = "🟡"
		color = colorWarning
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/notifications/... -run TestSlackBuildPayloadWarning -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/notifications/slack.go backend/internal/notifications/slack_test.go
git commit -m "feat(notifications): render a warning status distinctly in Slack"
```

---

### Task 9: Render "warning" in Discord

**Files:**
- Modify: `backend/internal/notifications/discord.go` (color consts near line 24, `buildPayload`)
- Test: `backend/internal/notifications/discord_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package notifications

import "testing"

func TestDiscordBuildPayloadWarning(t *testing.T) {
	p := &DiscordPlugin{}
	payload := p.buildPayload(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if len(payload.Embeds) == 0 {
		t.Fatal("expected at least one embed")
	}
	if payload.Embeds[0].Color != colorDiscordWarning {
		t.Errorf("got color %#x, want colorDiscordWarning (%#x)", payload.Embeds[0].Color, colorDiscordWarning)
	}
	if payload.Embeds[0].Color == colorDiscordUp {
		t.Error("warning must not render with the up/success color")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/notifications/... -run TestDiscordBuildPayloadWarning -v`
Expected: FAIL — `colorDiscordWarning` undefined.

- [ ] **Step 3: Add the constant and the case**

In `backend/internal/notifications/discord.go`, find:

```go
// Discord embed colors (decimal RGB).
const (
	colorDiscordDown = 0xEF4444 // red
	colorDiscordUp   = 0x10B981 // emerald
)
```

Replace with:

```go
// Discord embed colors (decimal RGB).
const (
	colorDiscordDown    = 0xEF4444 // red
	colorDiscordUp      = 0x10B981 // emerald
	colorDiscordWarning = 0xF59E0B // amber
)
```

Find:

```go
func (p *DiscordPlugin) buildPayload(m *NotificationMessage) discordPayload {
	emoji := "🟢"
	color := colorDiscordUp
	if m.Status == "down" {
		emoji = "🔴"
		color = colorDiscordDown
	}
```

Replace with:

```go
func (p *DiscordPlugin) buildPayload(m *NotificationMessage) discordPayload {
	emoji := "🟢"
	color := colorDiscordUp
	switch m.Status {
	case "down":
		emoji = "🔴"
		color = colorDiscordDown
	case "warning":
		emoji = "🟡"
		color = colorDiscordWarning
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/notifications/... -run TestDiscordBuildPayloadWarning -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/notifications/discord.go backend/internal/notifications/discord_test.go
git commit -m "feat(notifications): render a warning status distinctly in Discord"
```

---

### Task 10: Render "warning" in Telegram

**Files:**
- Modify: `backend/internal/notifications/telegram.go` (`buildText`)
- Test: `backend/internal/notifications/telegram_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package notifications

import (
	"strings"
	"testing"
)

func TestTelegramBuildTextWarning(t *testing.T) {
	p := &TelegramPlugin{}
	got := p.buildText(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if !strings.Contains(got, "🟡") {
		t.Errorf("expected the warning emoji in the message, got: %s", got)
	}
	if strings.HasPrefix(got, "*web-01* \\- 🟢") {
		t.Error("warning must not render with the green/good emoji")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/notifications/... -run TestTelegramBuildTextWarning -v`
Expected: FAIL — the message contains 🟢 (the default), not 🟡.

- [ ] **Step 3: Add the case**

In `backend/internal/notifications/telegram.go`, find:

```go
func (p *TelegramPlugin) buildText(m *NotificationMessage) string {
	emoji := "🟢"
	if m.Status == "down" {
		emoji = "🔴"
	}
```

Replace with:

```go
func (p *TelegramPlugin) buildText(m *NotificationMessage) string {
	emoji := "🟢"
	switch m.Status {
	case "down":
		emoji = "🔴"
	case "warning":
		emoji = "🟡"
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/notifications/... -run TestTelegramBuildTextWarning -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/notifications/telegram.go backend/internal/notifications/telegram_test.go
git commit -m "feat(notifications): render a warning status distinctly in Telegram"
```

---

### Task 11: Render "warning" in ntfy

**Files:**
- Modify: `backend/internal/notifications/ntfy.go` (`buildTitle`, `buildTags`)
- Test: `backend/internal/notifications/ntfy_test.go` (new — this file does not exist yet)

- [ ] **Step 1: Write the failing test**

```go
package notifications

import "testing"

func TestNtfyBuildTitleAndTagsWarning(t *testing.T) {
	p := &NtfyPlugin{}
	msg := &NotificationMessage{MonitorName: "web-01", Status: "warning"}

	title := p.buildTitle(msg)
	if title != "[WARNING] web-01" {
		t.Errorf("got title %q, want %q", title, "[WARNING] web-01")
	}

	tags := p.buildTags(msg)
	if tags != "warning" {
		t.Errorf("got tags %q, want %q", tags, "warning")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/notifications/... -run TestNtfyBuildTitleAndTagsWarning -v`
Expected: FAIL — title falls through to the default `"[Sentinel] web-01 status: warning"`, tags is `"green_circle"`.

- [ ] **Step 3: Add the cases**

In `backend/internal/notifications/ntfy.go`, find:

```go
func (p *NtfyPlugin) buildTitle(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s went DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s recovered", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}
```

Replace with:

```go
func (p *NtfyPlugin) buildTitle(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s went DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s recovered", m.MonitorName)
	case "warning":
		return fmt.Sprintf("[WARNING] %s", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}
```

Find:

```go
	var tags []string
	switch m.Status {
	case "down":
		tags = append(tags, "red_circle")
	default:
		tags = append(tags, "green_circle")
	}
```

Replace with:

```go
	var tags []string
	switch m.Status {
	case "down":
		tags = append(tags, "red_circle")
	case "warning":
		// "warning" is a standard GitHub-style emoji shortcode (⚠️) that
		// ntfy resolves the same way "red_circle"/"green_circle" already are.
		tags = append(tags, "warning")
	default:
		tags = append(tags, "green_circle")
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/notifications/... -run TestNtfyBuildTitleAndTagsWarning -v`
Expected: PASS.

- [ ] **Step 5: Run the entire backend test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./... && gofmt -l .`
Expected: no errors, all packages `ok`, `gofmt -l .` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/notifications/ntfy.go backend/internal/notifications/ntfy_test.go
git commit -m "feat(notifications): render a warning status distinctly in ntfy"
```

---

### Task 12: Frontend types

**Files:**
- Modify: `frontend/src/hooks/useAgents.ts:7-42` (`Agent`), `:80-89` (`CreateAgentInput`)

**Interfaces:**
- Produces: `Agent.cpu_threshold_percent/memory_threshold_percent/disk_threshold_percent: number | null`, `CreateAgentInput.cpu_threshold_percent?/memory_threshold_percent?/disk_threshold_percent?: number` — Tasks 13-15 read/write these.

- [ ] **Step 1: Add the fields**

In `frontend/src/hooks/useAgents.ts`, find the end of the `Agent` interface's configuration fields:

```ts
  /** Channels this server alerts on. null means every enabled channel. */
  notify_channels?: string[] | null
  status: AgentStatus
```

Replace with:

```ts
  /** Channels this server alerts on. null means every enabled channel. */
  notify_channels?: string[] | null
  /** null means that metric is not watched. */
  cpu_threshold_percent: number | null
  memory_threshold_percent: number | null
  disk_threshold_percent: number | null
  status: AgentStatus
```

Find `CreateAgentInput`:

```ts
export interface CreateAgentInput {
  name: string
  os_type: AgentOS
  check_interval: number
  retry_attempts: number
  /** Empty or omitted lets the agent detect its own address. */
  ip_address_override?: string
  /** null means every enabled channel; an empty array means alert nowhere. */
  notify_channels?: string[] | null
}
```

Replace with:

```ts
export interface CreateAgentInput {
  name: string
  os_type: AgentOS
  check_interval: number
  retry_attempts: number
  /** Empty or omitted lets the agent detect its own address. */
  ip_address_override?: string
  /** null means every enabled channel; an empty array means alert nowhere. */
  notify_channels?: string[] | null
  /** 0 disables the threshold (the API's clear sentinel); 1-100 sets it. */
  cpu_threshold_percent?: number
  memory_threshold_percent?: number
  disk_threshold_percent?: number
}
```

- [ ] **Step 2: Verify it typechecks**

Run: `cd frontend && npm run typecheck`
Expected: a clean pass with no errors at all, including for the new required `Agent` fields — nothing in this frontend ever constructs a full `Agent` object literal by hand (it's always read back from an API response via `res.data.data`), so adding required fields to the interface has no existing call site to break. `CreateAgentInput`'s three new fields are optional (`?`), so existing `create`/`update` calls that don't yet mention them (Tasks 14/15 add that next) also keep compiling.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useAgents.ts
git commit -m "feat(agents): add resource-threshold fields to the frontend agent types"
```

---

### Task 13: Threshold fields in the shared settings form

**Files:**
- Modify: `frontend/src/components/AgentSettingsFields.tsx`

**Interfaces:**
- Consumes: nothing new.
- Produces: `AgentSettings.cpuThresholdEnabled/cpuThresholdPercent/memoryThresholdEnabled/memoryThresholdPercent/diskThresholdEnabled/diskThresholdPercent`, `thresholdPayload(enabled: boolean, percent: number): number` — Tasks 14/15 use both.

- [ ] **Step 1: Extend `AgentSettings`, add `thresholdPayload`, extend errors and validation**

Find:

```ts
/** The settings an operator owns. Credentials are not among them. */
export interface AgentSettings {
  name: string
  osType: AgentOS
  ipOverride: string
  interval: number
  retries: number
  /** Whether this server alerts at all. */
  notifyEnabled: boolean
  /** Which channels it alerts on. Empty with notifyEnabled means every one. */
  notifyChannels: string[]
}

/**
 * Turns the two notification fields into what the API expects.
 *
 * null means "every enabled channel" and an empty array means "nowhere" — the
 * same convention monitors use, so a server that has gone silent alerts by
 * default rather than quietly telling no one.
 */
export function notifyChannelsPayload(settings: AgentSettings): string[] | null {
  if (!settings.notifyEnabled) return []
  return settings.notifyChannels.length > 0 ? settings.notifyChannels : null
}

export interface AgentSettingsErrors {
  name?: string
  ip?: string
  interval?: string
  retries?: string
}
```

Replace with:

```ts
/** The settings an operator owns. Credentials are not among them. */
export interface AgentSettings {
  name: string
  osType: AgentOS
  ipOverride: string
  interval: number
  retries: number
  /** Whether this server alerts at all. */
  notifyEnabled: boolean
  /** Which channels it alerts on. Empty with notifyEnabled means every one. */
  notifyChannels: string[]
  cpuThresholdEnabled: boolean
  cpuThresholdPercent: number
  memoryThresholdEnabled: boolean
  memoryThresholdPercent: number
  diskThresholdEnabled: boolean
  diskThresholdPercent: number
}

/**
 * Turns the two notification fields into what the API expects.
 *
 * null means "every enabled channel" and an empty array means "nowhere" — the
 * same convention monitors use, so a server that has gone silent alerts by
 * default rather than quietly telling no one.
 */
export function notifyChannelsPayload(settings: AgentSettings): string[] | null {
  if (!settings.notifyEnabled) return []
  return settings.notifyChannels.length > 0 ? settings.notifyChannels : null
}

/**
 * Turns an enabled+value pair into what the API expects: 0 disables the
 * threshold (the same sentinel value an empty ip_address_override plays for
 * that field), any other value 1-100 sets it.
 */
export function thresholdPayload(enabled: boolean, percent: number): number {
  return enabled ? percent : 0
}

export interface AgentSettingsErrors {
  name?: string
  ip?: string
  interval?: string
  retries?: string
  cpuThreshold?: string
  memoryThreshold?: string
  diskThreshold?: string
}

/** True for a whole number 1-100, the bound the backend enforces. */
function isValidPercent(n: number): boolean {
  return Number.isInteger(n) && n >= 1 && n <= 100
}
```

Find `validateAgentSettings`:

```ts
export function validateAgentSettings(
  values: AgentSettings,
  touched: boolean,
): { errors: AgentSettingsErrors; valid: boolean } {
  const errors: AgentSettingsErrors = {}
  if (touched && !values.name.trim()) errors.name = 'A server name is required'
  if (values.ipOverride.trim() !== '' && !isIPAddress(values.ipOverride.trim())) {
    errors.ip = 'Must be an IP address, e.g. 192.168.1.50'
  }
  if (values.interval < 1 || values.interval > 3600) {
    errors.interval = 'Must be between 1 and 3600 seconds'
  }
  if (values.retries < 1 || values.retries > 10) errors.retries = 'Must be between 1 and 10'

  // Validity ignores the name's touched gate: a blank name is invalid whether
  // or not anyone has typed into the field yet.
  const valid =
    values.name.trim() !== '' && !errors.ip && !errors.interval && !errors.retries
  return { errors, valid }
}
```

Replace with:

```ts
export function validateAgentSettings(
  values: AgentSettings,
  touched: boolean,
): { errors: AgentSettingsErrors; valid: boolean } {
  const errors: AgentSettingsErrors = {}
  if (touched && !values.name.trim()) errors.name = 'A server name is required'
  if (values.ipOverride.trim() !== '' && !isIPAddress(values.ipOverride.trim())) {
    errors.ip = 'Must be an IP address, e.g. 192.168.1.50'
  }
  if (values.interval < 1 || values.interval > 3600) {
    errors.interval = 'Must be between 1 and 3600 seconds'
  }
  if (values.retries < 1 || values.retries > 10) errors.retries = 'Must be between 1 and 10'
  const thresholdError = 'Must be a whole number between 1 and 100'
  if (values.cpuThresholdEnabled && !isValidPercent(values.cpuThresholdPercent)) {
    errors.cpuThreshold = thresholdError
  }
  if (values.memoryThresholdEnabled && !isValidPercent(values.memoryThresholdPercent)) {
    errors.memoryThreshold = thresholdError
  }
  if (values.diskThresholdEnabled && !isValidPercent(values.diskThresholdPercent)) {
    errors.diskThreshold = thresholdError
  }

  // Validity ignores the name's touched gate: a blank name is invalid whether
  // or not anyone has typed into the field yet.
  const valid =
    values.name.trim() !== '' &&
    !errors.ip &&
    !errors.interval &&
    !errors.retries &&
    !errors.cpuThreshold &&
    !errors.memoryThreshold &&
    !errors.diskThreshold
  return { errors, valid }
}
```

- [ ] **Step 2: Add the "Alert Thresholds" section**

Find the closing of the "Collection" section and the start of "Notifications":

```tsx
      </section>

      {/* The same control the monitor and domain dialogs use, rather than a
          second one written for servers: a server going silent is the same kind
          of event as a monitor going down, and two controls would drift. */}
      <section>
        <h4 className="mb-3 text-xs font-semibold uppercase tracking-widest text-slate-300">
          Notifications
        </h4>
        <NotificationsSection
          enabled={values.notifyEnabled}
          onEnabledChange={(v) => set('notifyEnabled', v)}
          selected={values.notifyChannels}
          onSelectedChange={(ids) => set('notifyChannels', ids)}
          silentNote="this server stops reporting or comes back"
          showHeading={false}
        />
      </section>
    </>
  )
}
```

Replace with:

```tsx
      </section>

      <div className="border-t border-white/10" />

      <section>
        <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
          Alert Thresholds
        </h3>
        <p className="mb-4 text-xs text-slate-500">
          Notify once when a resource crosses a percentage, not again until it recovers and
          crosses it a second time.
        </p>
        <div className="space-y-4">
          <div>
            <label className="mb-1 flex items-center gap-2">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-slate-300 text-primary-400 focus:ring-primary-500"
                checked={values.cpuThresholdEnabled}
                onChange={(e) => set('cpuThresholdEnabled', e.target.checked)}
              />
              <span className="text-sm font-medium text-white">CPU Usage</span>
            </label>
            {values.cpuThresholdEnabled && (
              <div className="mt-2 flex items-center gap-2 pl-6">
                <input
                  type="number"
                  min={1}
                  max={100}
                  value={values.cpuThresholdPercent}
                  onChange={(e) => set('cpuThresholdPercent', Number(e.target.value))}
                  className={`w-24 ${field} ${errors.cpuThreshold ? 'border-red-500/60' : ''}`}
                />
                <span className="text-sm text-slate-400">%</span>
              </div>
            )}
            {errors.cpuThreshold && (
              <p className="mt-1 pl-6 text-xs text-red-400">{errors.cpuThreshold}</p>
            )}
          </div>

          <div>
            <label className="mb-1 flex items-center gap-2">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-slate-300 text-primary-400 focus:ring-primary-500"
                checked={values.memoryThresholdEnabled}
                onChange={(e) => set('memoryThresholdEnabled', e.target.checked)}
              />
              <span className="text-sm font-medium text-white">Memory Usage</span>
            </label>
            {values.memoryThresholdEnabled && (
              <div className="mt-2 flex items-center gap-2 pl-6">
                <input
                  type="number"
                  min={1}
                  max={100}
                  value={values.memoryThresholdPercent}
                  onChange={(e) => set('memoryThresholdPercent', Number(e.target.value))}
                  className={`w-24 ${field} ${errors.memoryThreshold ? 'border-red-500/60' : ''}`}
                />
                <span className="text-sm text-slate-400">%</span>
              </div>
            )}
            {errors.memoryThreshold && (
              <p className="mt-1 pl-6 text-xs text-red-400">{errors.memoryThreshold}</p>
            )}
          </div>

          <div>
            <label className="mb-1 flex items-center gap-2">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-slate-300 text-primary-400 focus:ring-primary-500"
                checked={values.diskThresholdEnabled}
                onChange={(e) => set('diskThresholdEnabled', e.target.checked)}
              />
              <span className="text-sm font-medium text-white">Disk Usage</span>
            </label>
            {values.diskThresholdEnabled && (
              <div className="mt-2 flex items-center gap-2 pl-6">
                <input
                  type="number"
                  min={1}
                  max={100}
                  value={values.diskThresholdPercent}
                  onChange={(e) => set('diskThresholdPercent', Number(e.target.value))}
                  className={`w-24 ${field} ${errors.diskThreshold ? 'border-red-500/60' : ''}`}
                />
                <span className="text-sm text-slate-400">%</span>
              </div>
            )}
            {errors.diskThreshold && (
              <p className="mt-1 pl-6 text-xs text-red-400">{errors.diskThreshold}</p>
            )}
          </div>
        </div>
      </section>

      {/* The same control the monitor and domain dialogs use, rather than a
          second one written for servers: a server going silent is the same kind
          of event as a monitor going down, and two controls would drift. */}
      <section>
        <h4 className="mb-3 text-xs font-semibold uppercase tracking-widest text-slate-300">
          Notifications
        </h4>
        <NotificationsSection
          enabled={values.notifyEnabled}
          onEnabledChange={(v) => set('notifyEnabled', v)}
          selected={values.notifyChannels}
          onSelectedChange={(ids) => set('notifyChannels', ids)}
          silentNote="this server stops reporting, comes back, or crosses a resource threshold"
          showHeading={false}
        />
      </section>
    </>
  )
}
```

- [ ] **Step 3: Verify it typechecks**

Run: `cd frontend && npm run typecheck`
Expected: errors in `AddServerAgentModal.tsx` and `EditServerAgentModal.tsx` (their `AgentSettings` object literals are now missing the six new required fields) — this is expected until Tasks 14 and 15. No errors inside `AgentSettingsFields.tsx` itself.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/AgentSettingsFields.tsx
git commit -m "feat(agents): add an Alert Thresholds section to the server settings form"
```

---

### Task 14: Wire thresholds into the create-server flow

**Files:**
- Modify: `frontend/src/components/AddServerAgentModal.tsx:21-32` (`BLANK`), `:135-153` (`submit`)

- [ ] **Step 1: Update `BLANK` and the import**

Find:

```ts
import AgentSettingsFields, {
  notifyChannelsPayload,
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
```

Replace with:

```ts
import AgentSettingsFields, {
  notifyChannelsPayload,
  thresholdPayload,
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
```

Find:

```ts
const BLANK: AgentSettings = {
  name: '',
  osType: 'ubuntu',
  ipOverride: '',
  interval: 60,
  retries: 3,
  // On by default with no channels chosen, which the API reads as "every
  // enabled channel". A server going quiet is the thing someone adding one
  // wants to hear about; defaulting to silence would look broken.
  notifyEnabled: true,
  notifyChannels: [],
}
```

Replace with:

```ts
const BLANK: AgentSettings = {
  name: '',
  osType: 'ubuntu',
  ipOverride: '',
  interval: 60,
  retries: 3,
  // On by default with no channels chosen, which the API reads as "every
  // enabled channel". A server going quiet is the thing someone adding one
  // wants to hear about; defaulting to silence would look broken.
  notifyEnabled: true,
  notifyChannels: [],
  // On by default at a sensible number: a newly added server should be
  // watched from the start, not silently unmonitored until someone remembers.
  cpuThresholdEnabled: true,
  cpuThresholdPercent: 90,
  memoryThresholdEnabled: true,
  memoryThresholdPercent: 90,
  diskThresholdEnabled: true,
  diskThresholdPercent: 90,
}
```

- [ ] **Step 2: Send the thresholds on submit**

Find:

```ts
      const result = await create({
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        ip_address_override: values.ipOverride.trim(),
        notify_channels: notifyChannelsPayload(values),
      })
```

Replace with:

```ts
      const result = await create({
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        ip_address_override: values.ipOverride.trim(),
        notify_channels: notifyChannelsPayload(values),
        cpu_threshold_percent: thresholdPayload(values.cpuThresholdEnabled, values.cpuThresholdPercent),
        memory_threshold_percent: thresholdPayload(
          values.memoryThresholdEnabled,
          values.memoryThresholdPercent,
        ),
        disk_threshold_percent: thresholdPayload(values.diskThresholdEnabled, values.diskThresholdPercent),
      })
```

- [ ] **Step 3: Verify it typechecks**

Run: `cd frontend && npm run typecheck`
Expected: no errors from this file. (`EditServerAgentModal.tsx` still errors until Task 15.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/AddServerAgentModal.tsx
git commit -m "feat(agents): default new servers to a 90% resource-alert threshold"
```

---

### Task 15: Wire thresholds into the edit-server flow

**Files:**
- Modify: `frontend/src/components/EditServerAgentModal.tsx:1-33` (imports, `settingsOf`), `:74-103` (`changed`, `save`)

- [ ] **Step 1: Update the import and `settingsOf`**

Find:

```ts
import AgentSettingsFields, {
  notifyChannelsPayload,
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
```

Replace with:

```ts
import AgentSettingsFields, {
  notifyChannelsPayload,
  thresholdPayload,
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
```

Find:

```ts
function settingsOf(agent: Agent): AgentSettings {
  return {
    name: agent.name,
    osType: agent.os_type,
    ipOverride: agent.ip_address_override ?? '',
    interval: agent.check_interval,
    retries: agent.retry_attempts,
    // null means every enabled channel, an empty list means none — the same
    // convention the API stores, kept rather than flattened so reopening the
    // form shows what is actually configured.
    notifyEnabled: agent.notify_channels === null || (agent.notify_channels ?? []).length > 0,
    notifyChannels: agent.notify_channels ?? [],
  }
}
```

Replace with:

```ts
function settingsOf(agent: Agent): AgentSettings {
  return {
    name: agent.name,
    osType: agent.os_type,
    ipOverride: agent.ip_address_override ?? '',
    interval: agent.check_interval,
    retries: agent.retry_attempts,
    // null means every enabled channel, an empty list means none — the same
    // convention the API stores, kept rather than flattened so reopening the
    // form shows what is actually configured.
    notifyEnabled: agent.notify_channels === null || (agent.notify_channels ?? []).length > 0,
    notifyChannels: agent.notify_channels ?? [],
    // null means the metric isn't watched; falling back to 90 only matters
    // for the number input's value while the checkbox is unchecked.
    cpuThresholdEnabled: agent.cpu_threshold_percent !== null,
    cpuThresholdPercent: agent.cpu_threshold_percent ?? 90,
    memoryThresholdEnabled: agent.memory_threshold_percent !== null,
    memoryThresholdPercent: agent.memory_threshold_percent ?? 90,
    diskThresholdEnabled: agent.disk_threshold_percent !== null,
    diskThresholdPercent: agent.disk_threshold_percent ?? 90,
  }
}
```

- [ ] **Step 2: Extend the dirty-check and the save payload**

Find:

```ts
  const { errors, valid } = validateAgentSettings(values, true)
  const changed =
    values.name !== initial.name ||
    values.osType !== initial.osType ||
    values.ipOverride !== initial.ipOverride ||
    values.interval !== initial.interval ||
    values.retries !== initial.retries
```

Replace with:

```ts
  const { errors, valid } = validateAgentSettings(values, true)
  const changed =
    values.name !== initial.name ||
    values.osType !== initial.osType ||
    values.ipOverride !== initial.ipOverride ||
    values.interval !== initial.interval ||
    values.retries !== initial.retries ||
    values.cpuThresholdEnabled !== initial.cpuThresholdEnabled ||
    values.cpuThresholdPercent !== initial.cpuThresholdPercent ||
    values.memoryThresholdEnabled !== initial.memoryThresholdEnabled ||
    values.memoryThresholdPercent !== initial.memoryThresholdPercent ||
    values.diskThresholdEnabled !== initial.diskThresholdEnabled ||
    values.diskThresholdPercent !== initial.diskThresholdPercent
```

Find:

```ts
      await update(agent.agent_id, {
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        // Sent even when empty: that is how an override is cleared and the
        // address goes back to whatever the agent detects.
        ip_address_override: values.ipOverride.trim(),
        notify_channels: notifyChannelsPayload(values),
      })
```

Replace with:

```ts
      await update(agent.agent_id, {
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        // Sent even when empty: that is how an override is cleared and the
        // address goes back to whatever the agent detects.
        ip_address_override: values.ipOverride.trim(),
        notify_channels: notifyChannelsPayload(values),
        cpu_threshold_percent: thresholdPayload(values.cpuThresholdEnabled, values.cpuThresholdPercent),
        memory_threshold_percent: thresholdPayload(
          values.memoryThresholdEnabled,
          values.memoryThresholdPercent,
        ),
        disk_threshold_percent: thresholdPayload(values.diskThresholdEnabled, values.diskThresholdPercent),
      })
```

- [ ] **Step 3: Verify it typechecks, lints, and builds**

Run: `cd frontend && npm run typecheck && npm run lint && npm run build`
Expected: typecheck clean, lint shows only the pre-existing `react-refresh/only-export-components` warnings (none new), build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/EditServerAgentModal.tsx
git commit -m "feat(agents): let a server's resource thresholds be edited"
```

---

### Task 16: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Full backend verification**

Run: `cd backend && go build ./... && go vet ./... && go test ./... && gofmt -l .`
Expected: no errors, every package `ok`, `gofmt -l .` prints nothing.

- [ ] **Step 2: Full frontend verification**

Run: `cd frontend && npm run typecheck && npm run lint && npm run build`
Expected: typecheck clean, lint shows only pre-existing warnings, build succeeds.

- [ ] **Step 3: Confirm the migration is the next one CI will apply**

Run: `ls backend/migrations/*.sql | sort -V | tail -3`
Expected: `042_agent_resource_thresholds.sql` is the last file, immediately after `041_email_channel_recipients.sql`.

- [ ] **Step 4: Review the full diff**

Run: `git log --oneline main..HEAD` (or the equivalent for however many commits this plan produced) and `git diff <first commit of this plan>~1..HEAD --stat`
Expected: every file this plan touched appears, nothing unexpected.

---

### Task 17: Release

**Files:** none.

- [ ] **Step 1: Push to main**

```bash
git push origin main
```

- [ ] **Step 2: Wait for CI, then confirm it succeeded**

```bash
gh run list --limit 1 --branch main
```

Expected: `completed` / `success` for the "Build and Push Docker Images" workflow.

- [ ] **Step 3: Tag and push the release**

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

- [ ] **Step 4: Confirm the release pipeline completed**

```bash
gh run list --limit 1
gh release view v0.2.0
```

Expected: the tag's workflow run is `completed`/`success` (including the `create-release` job, which only runs on a `v*` tag), and `gh release view v0.2.0` shows a published release with generated notes.

- [ ] **Step 5: Deploy**

Following the same steps used for every prior fix this session: `git pull` in `/srv/docker/Sentinel`, `docker pull` the two new images, `docker compose up -d --no-build backend frontend`, then confirm both containers report healthy and the startup log shows migration `042_agent_resource_thresholds.sql` applying cleanly.

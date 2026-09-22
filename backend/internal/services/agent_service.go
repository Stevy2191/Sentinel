package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ErrAgentNotFound is returned when no agent matches the given id or token.
var ErrAgentNotFound = errors.New("agent not found")

// AgentService registers agents and ingests what they report.
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

func NewAgentService(db *gorm.DB) *AgentService {
	return &AgentService{db: db, logger: log.Default()}
}

// Register creates an agent and its credentials.
//
// The caller supplies the name and preferences; the identifier and token are
// generated here so a client cannot choose a predictable token.
func (s *AgentService) Register(ctx context.Context, agent *models.Agent) error {
	agentID, err := models.NewAgentID()
	if err != nil {
		return err
	}
	token, err := models.NewServerToken()
	if err != nil {
		return err
	}

	agent.ID = uuid.New()
	agent.AgentID = agentID
	agent.ServerToken = token
	agent.Status = models.AgentPending
	now := time.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(agent).Error; err != nil {
		return fmt.Errorf("registering agent %q: %w", agent.Name, err)
	}
	s.logger.Printf("[agent] registered %s (%s)", agent.Name, agent.AgentID)
	return nil
}

// List returns every agent with its status brought up to date.
func (s *AgentService) List(ctx context.Context) ([]models.Agent, error) {
	var agents []models.Agent
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	// Derived on read as well as on the sweep, so a page loaded between
	// sweeps does not show an agent as active minutes after it went quiet.
	now := time.Now()
	for i := range agents {
		agents[i].Status = agents[i].DeriveStatus(now)
	}
	return agents, nil
}

// Get returns one agent by its readable agent_id.
func (s *AgentService) Get(ctx context.Context, agentID string) (*models.Agent, error) {
	var agent models.Agent
	err := s.db.WithContext(ctx).First(&agent, "agent_id = ?", agentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching agent %s: %w", agentID, err)
	}
	agent.Status = agent.DeriveStatus(time.Now())
	return &agent, nil
}

// Authenticate resolves a bearer token to the agent that owns it.
//
// The token is compared in constant time. A plain string comparison returns as
// soon as two bytes differ, which leaks how much of a guess was correct and
// makes a token recoverable byte by byte given enough attempts.
//
// Candidates are narrowed by the token's prefix rather than by matching the
// whole value in SQL, so the database is not asked to do the comparison.
func (s *AgentService) Authenticate(ctx context.Context, token string) (*models.Agent, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrAgentNotFound
	}

	var candidates []models.Agent
	if err := s.db.WithContext(ctx).
		Where("left(server_token, 12) = ?", safePrefix(token, 12)).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("authenticating agent: %w", err)
	}

	for i := range candidates {
		if subtle.ConstantTimeCompare([]byte(candidates[i].ServerToken), []byte(token)) == 1 {
			return &candidates[i], nil
		}
	}
	return nil, ErrAgentNotFound
}

func safePrefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// ReconnectResult says what reconnecting did.
type ReconnectResult struct {
	Agent *models.Agent `json:"agent"`
	// Recreated is true when the agent had been deleted and its registration
	// was restored, false when an existing agent's token was replaced.
	Recreated bool `json:"recreated"`
}

// Reconnect issues a fresh token for an agent id, whether or not the agent
// still exists.
//
// Two situations, one action. An agent that was deleted by mistake is still
// running on its host and still knows its own id, so its registration is
// recreated under that id and it can reconnect by being given the new token —
// no reinstall. An agent that still exists simply has its token replaced,
// which is what to do when a token has leaked or a host has the wrong one.
//
// The id is the only thing the host cannot be talked out of, so it is what
// this is keyed on. The token is always new: reusing the old one would mean
// storing it somewhere retrievable after deletion, and a credential that
// survives deletion is worse than one that has to be re-copied.
func (s *AgentService) Reconnect(ctx context.Context, agentID string, name string) (*ReconnectResult, error) {
	if err := models.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	token, err := models.NewServerToken()
	if err != nil {
		return nil, err
	}

	existing, err := s.Get(ctx, agentID)
	if err != nil && !errors.Is(err, ErrAgentNotFound) {
		return nil, err
	}

	now := time.Now()
	if existing != nil {
		if err := s.db.WithContext(ctx).Model(&models.Agent{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{"server_token": token, "updated_at": now}).Error; err != nil {
			return nil, fmt.Errorf("rotating token for %s: %w", agentID, err)
		}
		existing.ServerToken = token
		s.logger.Printf("[agent] token rotated for %s", agentID)
		return &ReconnectResult{Agent: existing, Recreated: false}, nil
	}

	if strings.TrimSpace(name) == "" {
		name = agentID
	}
	agent := &models.Agent{
		ID:            uuid.New(),
		Name:          strings.TrimSpace(name),
		AgentID:       agentID,
		ServerToken:   token,
		OSType:        "linux",
		CheckInterval: models.DefaultAgentInterval,
		RetryAttempts: models.DefaultAgentRetries,
		// Pending rather than active: nothing has reported under this
		// registration yet, and it stays pending until the host does.
		Status:    models.AgentPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(agent).Error; err != nil {
		return nil, fmt.Errorf("re-registering agent %s: %w", agentID, err)
	}
	s.logger.Printf("[agent] re-registered %s after deletion", agentID)
	return &ReconnectResult{Agent: agent, Recreated: true}, nil
}

// Update changes the settings an operator owns. Credentials are not among
// them: rotating a token would silently break the installed agent.
// AgentSettings is the editable configuration of an agent.
//
// A struct rather than a growing parameter list: this call already carried six
// positional arguments, and a seventh of the same type as its neighbour is the
// kind of signature that gets mis-called without the compiler noticing.
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

// Delete unregisters an agent. Its metrics and container rows go with it via
// the foreign keys.
func (s *AgentService) Delete(ctx context.Context, agentID string) error {
	res := s.db.WithContext(ctx).Where("agent_id = ?", agentID).Delete(&models.Agent{})
	if res.Error != nil {
		return fmt.Errorf("deleting agent %s: %w", agentID, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrAgentNotFound
	}
	s.logger.Printf("[agent] unregistered %s", agentID)
	return nil
}

// AgentSystemInfo is what a host tells us about itself on a heartbeat.
type AgentSystemInfo struct {
	IPAddress    string
	Hostname     string
	OSVersion    string
	AgentVersion string

	KernelVersion   string
	Architecture    string
	CPUModel        string
	CPUCores        int
	MemoryTotalMB   int64
	GoVersion       string
	DockerAvailable *bool
}

// Heartbeat records that an agent is alive and refreshes what it reports about
// itself.
func (s *AgentService) Heartbeat(ctx context.Context, agent *models.Agent, info AgentSystemInfo) error {
	// Captured before the update: an agent that was offline and has just
	// reported is a recovery worth announcing.
	wasOffline := agent.Status == models.AgentOffline
	now := time.Now()
	updates := map[string]interface{}{
		"last_heartbeat": now,
		"status":         models.AgentActive,
		"updated_at":     now,
	}
	// Only overwritten when supplied, so a heartbeat that omits a field does
	// not blank what an earlier one established.
	setIf := func(col, v string) {
		if strings.TrimSpace(v) != "" {
			updates[col] = strings.TrimSpace(v)
		}
	}
	setIf("ip_address", info.IPAddress)
	setIf("hostname", info.Hostname)
	setIf("os_version", info.OSVersion)
	setIf("agent_version", info.AgentVersion)
	setIf("kernel_version", info.KernelVersion)
	setIf("architecture", info.Architecture)
	setIf("cpu_model", info.CPUModel)
	setIf("go_version", info.GoVersion)
	// Numbers use the same rule: zero means the agent could not read it, so
	// the value already on record is left alone.
	if info.CPUCores > 0 {
		updates["cpu_cores"] = info.CPUCores
	}
	if info.MemoryTotalMB > 0 {
		updates["memory_total_mb"] = info.MemoryTotalMB
	}
	if info.DockerAvailable != nil {
		updates["docker_available"] = *info.DockerAvailable
	}

	if err := s.db.WithContext(ctx).Model(&models.Agent{}).
		Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("recording heartbeat for %s: %w", agent.AgentID, err)
	}
	if wasOffline {
		s.notifyStatusChange(ctx, AgentStatusChange{
			Agent: *agent, From: models.AgentOffline, To: models.AgentActive,
		})
	}
	return nil
}

// RecordMetrics stores one collection cycle and counts it as a heartbeat.
//
// Metrics arriving is itself proof the agent is alive, so a run of successful
// submissions keeps an agent active even if a heartbeat is lost.
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

// MetricsSince returns an agent's metrics newer than the cutoff, newest first.
func (s *AgentService) MetricsSince(ctx context.Context, agent *models.Agent, since time.Time, limit int) ([]models.AgentMetric, error) {
	var metrics []models.AgentMetric
	q := s.db.WithContext(ctx).
		Where("agent_id = ? AND timestamp >= ?", agent.ID, since).
		Order("timestamp DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&metrics).Error; err != nil {
		return nil, fmt.Errorf("fetching metrics for %s: %w", agent.AgentID, err)
	}
	return metrics, nil
}

// LatestMetric returns the most recent metrics, or nil when none exist.
func (s *AgentService) LatestMetric(ctx context.Context, agent *models.Agent) (*models.AgentMetric, error) {
	var m models.AgentMetric
	err := s.db.WithContext(ctx).Where("agent_id = ?", agent.ID).
		Order("timestamp DESC").First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching latest metrics for %s: %w", agent.AgentID, err)
	}
	return &m, nil
}

// LatestContainers returns the containers from the agent's most recent cycle.
func (s *AgentService) LatestContainers(ctx context.Context, agent *models.Agent) ([]models.AgentContainer, error) {
	var latest time.Time
	row := s.db.WithContext(ctx).Model(&models.AgentContainer{}).
		Select("MAX(timestamp)").Where("agent_id = ?", agent.ID).Row()
	var nullable *time.Time
	if err := row.Scan(&nullable); err != nil || nullable == nil {
		return []models.AgentContainer{}, nil
	}
	latest = *nullable

	var containers []models.AgentContainer
	if err := s.db.WithContext(ctx).
		Where("agent_id = ? AND timestamp = ?", agent.ID, latest).
		Order("container_name ASC").Find(&containers).Error; err != nil {
		return nil, fmt.Errorf("fetching containers for %s: %w", agent.AgentID, err)
	}
	return containers, nil
}

// AgentStatusChange describes an agent crossing between reporting and silent.
type AgentStatusChange struct {
	Agent models.Agent
	From  string
	To    string
}

// SetStatusChangeHook registers a callback invoked when an agent goes silent or
// starts reporting again.
//
// A hook rather than a notification manager dependency: this service's job is
// agent state, and wiring delivery into it would pull the whole notification
// graph in behind it. main decides what a transition means.
func (s *AgentService) SetStatusChangeHook(fn func(context.Context, AgentStatusChange)) {
	s.onStatusChange = fn
}

func (s *AgentService) notifyStatusChange(ctx context.Context, change AgentStatusChange) {
	if s.onStatusChange == nil {
		return
	}
	s.onStatusChange(ctx, change)
}

// MarkStaleOffline flips agents that have stopped reporting to offline.
//
// Persisted rather than only derived on read so the transition is a fact the
// rest of the system can act on later — an alert, an audit entry — instead of
// something recomputed by whoever happens to load the page.
func (s *AgentService) MarkStaleOffline(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-models.AgentOfflineAfter)

	// Read the rows that are about to flip before flipping them. The caller
	// needs to know which agents went silent, not only how many, and an UPDATE
	// cannot say who it touched portably.
	var stale []models.Agent
	if err := s.db.WithContext(ctx).
		Where("status = ? AND last_heartbeat IS NOT NULL AND last_heartbeat < ?",
			models.AgentActive, cutoff).
		Find(&stale).Error; err != nil {
		return 0, fmt.Errorf("finding stale agents: %w", err)
	}
	if len(stale) == 0 {
		return 0, nil
	}

	ids := make([]uuid.UUID, 0, len(stale))
	for _, a := range stale {
		ids = append(ids, a.ID)
	}
	res := s.db.WithContext(ctx).Model(&models.Agent{}).
		// Still constrained on status, so an agent that reported in between the
		// read and this write is not dragged offline behind its own heartbeat.
		Where("id IN ? AND status = ?", ids, models.AgentActive).
		Updates(map[string]interface{}{"status": models.AgentOffline, "updated_at": time.Now()})
	if res.Error != nil {
		return 0, fmt.Errorf("marking stale agents offline: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		s.logger.Printf("[agent] %d agent(s) marked offline after %s without contact",
			res.RowsAffected, models.AgentOfflineAfter)
	}

	for i := range stale {
		s.notifyStatusChange(ctx, AgentStatusChange{
			Agent: stale[i], From: models.AgentActive, To: models.AgentOffline,
		})
	}
	return res.RowsAffected, nil
}

// StartOfflineSweep marks silent agents offline on a fixed interval.
func (s *AgentService) StartOfflineSweep(ctx context.Context) {
	const every = 2 * time.Minute
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	s.logger.Printf("[agent] offline sweep running every %s", every)
	for {
		select {
		case <-ctx.Done():
			s.logger.Println("[agent] offline sweep stopped")
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, time.Minute)
			if _, err := s.MarkStaleOffline(sweepCtx); err != nil {
				s.logger.Printf("[agent] offline sweep failed: %v", err)
			}
			cancel()
		}
	}
}

// PurgeMetrics deletes agent metrics and container rows older than the window,
// in batches, for the same reason check history is batched: this table gains a
// row per agent per interval and is among the fastest-growing in the schema.
func (s *AgentService) PurgeMetrics(ctx context.Context, days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	const batch = 10000

	var total int64
	for _, table := range []string{"agent_metrics", "agent_containers"} {
		for {
			if err := ctx.Err(); err != nil {
				return total, fmt.Errorf("purging %s: %w", table, err)
			}
			res := s.db.WithContext(ctx).Exec(
				fmt.Sprintf(`DELETE FROM %s WHERE ctid IN (
				     SELECT ctid FROM %s WHERE timestamp < ? LIMIT ?)`, table, table),
				cutoff, batch)
			if res.Error != nil {
				return total, fmt.Errorf("purging %s older than %d days: %w", table, days, res.Error)
			}
			total += res.RowsAffected
			if res.RowsAffected < batch {
				break
			}
		}
	}
	if total > 0 {
		s.logger.Printf("[agent] purged %d metric row(s) recorded before %s",
			total, cutoff.Format("2006-01-02"))
	}
	return total, nil
}

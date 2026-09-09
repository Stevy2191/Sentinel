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

// Update changes the settings an operator owns. Credentials are not among
// them: rotating a token would silently break the installed agent.
func (s *AgentService) Update(ctx context.Context, agentID string, name string, osType string, interval, retries int) (*models.Agent, error) {
	agent, err := s.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name":           name,
		"os_type":        osType,
		"check_interval": interval,
		"retry_attempts": retries,
		"updated_at":     time.Now(),
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

// Heartbeat records that an agent is alive and refreshes what it reports about
// itself.
func (s *AgentService) Heartbeat(ctx context.Context, agent *models.Agent, ip, hostname, osVersion, agentVersion string) error {
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
	setIf("ip_address", ip)
	setIf("hostname", hostname)
	setIf("os_version", osVersion)
	setIf("agent_version", agentVersion)

	if err := s.db.WithContext(ctx).Model(&models.Agent{}).
		Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("recording heartbeat for %s: %w", agent.AgentID, err)
	}
	return nil
}

// RecordMetrics stores one collection cycle and counts it as a heartbeat.
//
// Metrics arriving is itself proof the agent is alive, so a run of successful
// submissions keeps an agent active even if a heartbeat is lost.
func (s *AgentService) RecordMetrics(ctx context.Context, agent *models.Agent, metric *models.AgentMetric, containers []models.AgentContainer) error {
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

// MarkStaleOffline flips agents that have stopped reporting to offline.
//
// Persisted rather than only derived on read so the transition is a fact the
// rest of the system can act on later — an alert, an audit entry — instead of
// something recomputed by whoever happens to load the page.
func (s *AgentService) MarkStaleOffline(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-models.AgentOfflineAfter)
	res := s.db.WithContext(ctx).Model(&models.Agent{}).
		Where("status = ? AND last_heartbeat IS NOT NULL AND last_heartbeat < ?",
			models.AgentActive, cutoff).
		Updates(map[string]interface{}{"status": models.AgentOffline, "updated_at": time.Now()})
	if res.Error != nil {
		return 0, fmt.Errorf("marking stale agents offline: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		s.logger.Printf("[agent] %d agent(s) marked offline after %s without contact",
			res.RowsAffected, models.AgentOfflineAfter)
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

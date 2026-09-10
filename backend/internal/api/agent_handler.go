package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// agentContextKey holds the authenticated agent for the request.
const agentContextKey = "agent"

// ---- authentication --------------------------------------------------------

// RequireAgentToken authenticates an agent by its server token.
//
// A separate scheme from the user session on purpose. An agent is not a person:
// it holds a long-lived credential, it has no password to rotate and no MFA,
// and it may do exactly two things — say it is alive, and report its own
// metrics. Letting it through the user middleware would give it the API surface
// of a signed-in account.
func RequireAgentToken(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			respondAuthError(c, http.StatusUnauthorized, "agent token required")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		agent, err := agents.Authenticate(c.Request.Context(), token)
		if err != nil {
			// Deliberately vague: distinguishing "no such token" from "wrong
			// token" tells an attacker when a guess has the right shape.
			respondAuthError(c, http.StatusUnauthorized, "invalid agent token")
			return
		}
		c.Set(agentContextKey, agent)
		c.Next()
	}
}

// agentFromContext returns the agent RequireAgentToken authenticated.
func agentFromContext(c *gin.Context) (*models.Agent, bool) {
	v, ok := c.Get(agentContextKey)
	if !ok {
		return nil, false
	}
	agent, ok := v.(*models.Agent)
	return agent, ok
}

// requireOwnAgent checks that the token used belongs to the agent named in the
// path, so a valid agent cannot write another agent's metrics.
func requireOwnAgent(c *gin.Context) (*models.Agent, bool) {
	agent, ok := agentFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "agent token required")
		return nil, false
	}
	if agent.AgentID != c.Param("agent_id") {
		respondAuthError(c, http.StatusForbidden, "this token cannot submit data for that agent")
		return nil, false
	}
	return agent, true
}

// ---- registration and management (admin) -----------------------------------

type createAgentRequest struct {
	Name          string `json:"name"`
	OSType        string `json:"os_type"`
	CheckInterval *int   `json:"check_interval"`
	RetryAttempts *int   `json:"retry_attempts"`
	// IPAddressOverride pins the address this host is recorded under. Empty
	// means use whatever the agent detects.
	IPAddressOverride *string `json:"ip_address_override"`
}

// validateAgentSettings applies the shared bounds for create and update.
func validateAgentSettings(c *gin.Context, name, osType string, interval, retries int) bool {
	if strings.TrimSpace(name) == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return false
	}
	if utf8.RuneCountInString(name) > models.MaxAgentNameLength {
		respondError(c, http.StatusBadRequest, "name is too long")
		return false
	}
	if err := models.ValidateAgentOS(osType); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return false
	}
	if interval < models.MinAgentInterval || interval > models.MaxAgentInterval {
		respondError(c, http.StatusBadRequest, "check_interval must be between 1 and 3600 seconds")
		return false
	}
	if retries < models.MinAgentRetries || retries > models.MaxAgentRetries {
		respondError(c, http.StatusBadRequest, "retry_attempts must be between 1 and 10")
		return false
	}
	return true
}

// parseIPOverride validates an operator-supplied address.
//
// Empty is valid and means "detect it", which is the default. Anything else
// has to be a real address: a hostname here would be recorded and displayed as
// though it were one, and never resolve.
func parseIPOverride(c *gin.Context, raw *string) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, true
	}
	if net.ParseIP(value) == nil {
		respondError(c, http.StatusBadRequest,
			"ip_address_override must be an IP address, e.g. 192.168.1.50")
		return nil, false
	}
	return &value, true
}

// CreateAgentHandler handles POST /api/v1/agents.
func CreateAgentHandler(agents *services.AgentService, settings *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		interval := models.DefaultAgentInterval
		if req.CheckInterval != nil {
			interval = *req.CheckInterval
		}
		retries := models.DefaultAgentRetries
		if req.RetryAttempts != nil {
			retries = *req.RetryAttempts
		}
		name := strings.TrimSpace(req.Name)
		osType := strings.ToLower(strings.TrimSpace(req.OSType))
		if osType == "" {
			osType = "linux"
		}
		if !validateAgentSettings(c, name, osType, interval, retries) {
			return
		}

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
		if err := agents.Register(c.Request.Context(), agent); err != nil {
			respondInternal(c, "CreateAgentHandler", err)
			return
		}

		// The token is returned here and nowhere else in a list response: this
		// is the moment the operator needs it to build the install command.
		urls := resolveSentinelURLs(c, settings)
		respondSuccess(c, http.StatusCreated, gin.H{
			"agent":        agent,
			"external_url": urls.External,
			"internal_url": urls.Internal,
			// Kept as an alias so a client written against the earlier shape
			// keeps working.
			"sentinel_url": urls.External,
		})
	}
}

// ListAgentsHandler handles GET /api/v1/agents.
func ListAgentsHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := agents.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "ListAgentsHandler", err)
			return
		}
		for i := range list {
			list[i].HideToken()
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

// GetAgentHandler handles GET /api/v1/agents/:agent_id. Admin-only, and the
// one place the token can be read back — the install command cannot be
// rebuilt without it.
func GetAgentHandler(agents *services.AgentService, settings *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agent, err := agents.Get(c.Request.Context(), c.Param("agent_id"))
		if err != nil {
			respondAgentError(c, err)
			return
		}
		urls := resolveSentinelURLs(c, settings)
		respondSuccess(c, http.StatusOK, gin.H{
			"agent":        agent,
			"external_url": urls.External,
			"internal_url": urls.Internal,
			"sentinel_url": urls.External,
		})
	}
}

type updateAgentRequest struct {
	Name              *string `json:"name"`
	OSType            *string `json:"os_type"`
	CheckInterval     *int    `json:"check_interval"`
	RetryAttempts     *int    `json:"retry_attempts"`
	IPAddressOverride *string `json:"ip_address_override"`
}

// UpdateAgentHandler handles PATCH /api/v1/agents/:agent_id.
func UpdateAgentHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		current, err := agents.Get(c.Request.Context(), c.Param("agent_id"))
		if err != nil {
			respondAgentError(c, err)
			return
		}
		var req updateAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}

		name, osType := current.Name, current.OSType
		interval, retries := current.CheckInterval, current.RetryAttempts
		if req.Name != nil {
			name = strings.TrimSpace(*req.Name)
		}
		if req.OSType != nil {
			osType = strings.ToLower(strings.TrimSpace(*req.OSType))
		}
		if req.CheckInterval != nil {
			interval = *req.CheckInterval
		}
		if req.RetryAttempts != nil {
			retries = *req.RetryAttempts
		}
		if !validateAgentSettings(c, name, osType, interval, retries) {
			return
		}

		override := current.IPAddressOverride
		if req.IPAddressOverride != nil {
			parsed, ok := parseIPOverride(c, req.IPAddressOverride)
			if !ok {
				return
			}
			override = parsed
		}

		updated, err := agents.Update(c.Request.Context(), current.AgentID, name, osType, interval, retries, override)
		if err != nil {
			respondAgentError(c, err)
			return
		}
		updated.HideToken()
		respondSuccess(c, http.StatusOK, updated)
	}
}

// DeleteAgentHandler handles DELETE /api/v1/agents/:agent_id.
func DeleteAgentHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := agents.Delete(c.Request.Context(), c.Param("agent_id")); err != nil {
			respondAgentError(c, err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "agent unregistered"})
	}
}

// ---- agent-facing endpoints ------------------------------------------------

type heartbeatRequest struct {
	AgentID      string `json:"agent_id"`
	Hostname     string `json:"hostname"`
	IPAddress    string `json:"ip_address"`
	OSVersion    string `json:"os_version"`
	AgentVersion string `json:"agent_version"`

	// What the host reports about itself. Carried on the heartbeat rather than
	// with metrics because these change when a machine is rebuilt, not every
	// collection cycle.
	KernelVersion   string `json:"kernel_version"`
	Architecture    string `json:"architecture"`
	CPUModel        string `json:"cpu_model"`
	CPUCores        int    `json:"cpu_cores"`
	MemoryTotalMB   int64  `json:"memory_total_mb"`
	GoVersion       string `json:"go_version"`
	DockerAvailable *bool  `json:"docker_available"`
}

// HeartbeatHandler handles POST /api/v1/agents/heartbeat, authenticated by the
// agent's own token.
func HeartbeatHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agent, ok := agentFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "agent token required")
			return
		}
		var req heartbeatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		// The body's agent_id is checked rather than trusted: a token
		// identifies the agent, so a mismatch means misconfiguration worth
		// reporting instead of silently recording against the wrong host.
		if req.AgentID != "" && req.AgentID != agent.AgentID {
			respondAuthError(c, http.StatusForbidden, "this token does not belong to that agent")
			return
		}

		ip := req.IPAddress
		if ip == "" {
			// Falling back to the peer address means an agent behind NAT still
			// records something useful without having to discover its own.
			ip = c.ClientIP()
		}
		info := services.AgentSystemInfo{
			IPAddress:       ip,
			Hostname:        req.Hostname,
			OSVersion:       req.OSVersion,
			AgentVersion:    req.AgentVersion,
			KernelVersion:   req.KernelVersion,
			Architecture:    req.Architecture,
			CPUModel:        truncate(req.CPUModel, 255),
			CPUCores:        req.CPUCores,
			MemoryTotalMB:   req.MemoryTotalMB,
			GoVersion:       req.GoVersion,
			DockerAvailable: req.DockerAvailable,
		}
		if err := agents.Heartbeat(c.Request.Context(), agent, info); err != nil {
			respondInternal(c, "HeartbeatHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"message": "heartbeat recorded",
			// Echoed so an agent picks up a changed interval without being
			// reinstalled.
			"check_interval": agent.CheckInterval,
			"retry_attempts": agent.RetryAttempts,
		})
	}
}

// containerPayload is one container as the agent reports it.
type containerPayload struct {
	ContainerID   string  `json:"container_id"`
	ContainerName string  `json:"container_name"`
	Image         string  `json:"image"`
	Status        string  `json:"status"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	MemoryUsedMB  int64   `json:"memory_used_mb"`
}

type metricsRequest struct {
	Timestamp *time.Time `json:"timestamp"`

	CPUPercent    *float64 `json:"cpu_percent"`
	MemoryPercent *float64 `json:"memory_percent"`
	MemoryUsedMB  *int64   `json:"memory_used_mb"`
	MemoryTotalMB *int64   `json:"memory_total_mb"`
	DiskPercent   *float64 `json:"disk_percent"`
	DiskUsedGB    *float64 `json:"disk_used_gb"`
	DiskTotalGB   *float64 `json:"disk_total_gb"`
	UptimeSeconds *int64   `json:"uptime_seconds"`

	LoadAverage1m  *float64 `json:"load_average_1m"`
	LoadAverage5m  *float64 `json:"load_average_5m"`
	LoadAverage15m *float64 `json:"load_average_15m"`

	NetworkInBytes  *int64 `json:"network_in_bytes"`
	NetworkOutBytes *int64 `json:"network_out_bytes"`

	Containers []containerPayload `json:"containers"`
}

// maxContainersPerReport bounds one submission. A host running more than this
// is unusual, and without a limit a single request could insert without end.
const maxContainersPerReport = 500

// SubmitMetricsHandler handles POST /api/v1/agents/:agent_id/metrics.
func SubmitMetricsHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agent, ok := requireOwnAgent(c)
		if !ok {
			return
		}
		var req metricsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(req.Containers) > maxContainersPerReport {
			respondError(c, http.StatusBadRequest, "too many containers in one report")
			return
		}

		metric := &models.AgentMetric{
			CPUPercent:      req.CPUPercent,
			MemoryPercent:   req.MemoryPercent,
			MemoryUsedMB:    req.MemoryUsedMB,
			MemoryTotalMB:   req.MemoryTotalMB,
			DiskPercent:     req.DiskPercent,
			DiskUsedGB:      req.DiskUsedGB,
			DiskTotalGB:     req.DiskTotalGB,
			UptimeSeconds:   req.UptimeSeconds,
			LoadAverage1m:   req.LoadAverage1m,
			LoadAverage5m:   req.LoadAverage5m,
			LoadAverage15m:  req.LoadAverage15m,
			NetworkInBytes:  req.NetworkInBytes,
			NetworkOutBytes: req.NetworkOutBytes,
		}
		if req.Timestamp != nil {
			metric.Timestamp = *req.Timestamp
		}

		containers := make([]models.AgentContainer, 0, len(req.Containers))
		for _, ct := range req.Containers {
			if strings.TrimSpace(ct.ContainerID) == "" {
				continue
			}
			containers = append(containers, models.AgentContainer{
				ContainerID:   truncate(ct.ContainerID, 255),
				ContainerName: truncate(ct.ContainerName, 255),
				Image:         truncate(ct.Image, 255),
				Status:        truncate(ct.Status, 50),
				CPUPercent:    ct.CPUPercent,
				MemoryPercent: ct.MemoryPercent,
				MemoryUsedMB:  ct.MemoryUsedMB,
			})
		}

		if err := agents.RecordMetrics(c.Request.Context(), agent, metric, containers); err != nil {
			respondInternal(c, "SubmitMetricsHandler", err)
			return
		}
		respondSuccess(c, http.StatusAccepted, gin.H{
			"message":        "metrics recorded",
			"check_interval": agent.CheckInterval,
		})
	}
}

// truncate bounds a string to what its column accepts, so an over-long value
// from a host is trimmed rather than failing the whole submission.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---- dashboard reads -------------------------------------------------------

// GetAgentMetricsHandler handles GET /api/v1/agents/:agent_id/metrics.
func GetAgentMetricsHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		agent, err := agents.Get(c.Request.Context(), c.Param("agent_id"))
		if err != nil {
			respondAgentError(c, err)
			return
		}

		duration := time.Hour
		if raw := strings.TrimSpace(c.Query("duration")); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				respondError(c, http.StatusBadRequest, "invalid 'duration': use a value like 30m, 6h or 24h")
				return
			}
			const maxDuration = 90 * 24 * time.Hour
			if d > maxDuration {
				d = maxDuration
			}
			duration = d
		}

		limit := 500
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				respondError(c, http.StatusBadRequest, "invalid 'limit'")
				return
			}
			// Capped so one request cannot ask for an unbounded result set.
			if n > 5000 {
				n = 5000
			}
			limit = n
		}

		metrics, err := agents.MetricsSince(c.Request.Context(), agent, time.Now().Add(-duration), limit)
		if err != nil {
			respondInternal(c, "GetAgentMetricsHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"agent_id": agent.AgentID,
			"duration": duration.String(),
			"count":    len(metrics),
			"metrics":  metrics,
		})
	}
}

// GetAgentStatusHandler handles GET /api/v1/agents/:agent_id/status.
func GetAgentStatusHandler(agents *services.AgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		agent, err := agents.Get(ctx, c.Param("agent_id"))
		if err != nil {
			respondAgentError(c, err)
			return
		}
		latest, err := agents.LatestMetric(ctx, agent)
		if err != nil {
			respondInternal(c, "GetAgentStatusHandler", err)
			return
		}
		containers, err := agents.LatestContainers(ctx, agent)
		if err != nil {
			respondInternal(c, "GetAgentStatusHandler", err)
			return
		}
		agent.HideToken()
		respondSuccess(c, http.StatusOK, gin.H{
			"agent":      agent,
			"latest":     latest,
			"containers": containers,
		})
	}
}

func respondAgentError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAgentNotFound) {
		respondError(c, http.StatusNotFound, "no such agent")
		return
	}
	respondInternal(c, "agent", err)
}

// RegisterAgentRoutes mounts the management routes, which are admin-gated by
// the caller's group.
func RegisterAgentRoutes(rg *gin.RouterGroup, agents *services.AgentService, settings *services.SettingsService, users adminChecker) {
	// Reading agents and their metrics is available to any signed-in user, the
	// same as monitors: it is dashboard data. Registering, changing and
	// removing an agent are administrative.
	g := rg.Group("/agents")
	g.GET("", ListAgentsHandler(agents))
	g.GET("/:agent_id/metrics", GetAgentMetricsHandler(agents))
	g.GET("/:agent_id/status", GetAgentStatusHandler(agents))

	admin := rg.Group("/agents", RequireAdmin(users))
	admin.POST("", CreateAgentHandler(agents, settings))
	// Returns the token, so it is admin-only and sits apart from the read
	// routes above.
	admin.GET("/:agent_id", GetAgentHandler(agents, settings))
	admin.PATCH("/:agent_id", UpdateAgentHandler(agents))
	admin.DELETE("/:agent_id", DeleteAgentHandler(agents))
	// Reachability check for the configured URLs, so an operator finds out
	// here rather than from an agent that silently never reports.
	admin.POST("/urls/test", TestSentinelURLsHandler(settings))
}

// RegisterAgentIngestRoutes mounts the routes agents themselves call. They are
// registered on the router rather than inside the authenticated API group,
// because an agent presents its own token and must not pass through the user
// session middleware.
func RegisterAgentIngestRoutes(router *gin.Engine, agents *services.AgentService) {
	g := router.Group("/api/v1/agents", RequireAgentToken(agents))
	g.POST("/heartbeat", HeartbeatHandler(agents))
	g.POST("/:agent_id/metrics", SubmitMetricsHandler(agents))
}

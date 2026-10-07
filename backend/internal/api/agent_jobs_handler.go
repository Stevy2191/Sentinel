package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// agentJobsHandler serves the agent's side of network tools: collecting a
// job, posting its events while it runs, and reporting how it ended.
type agentJobsHandler struct {
	runs *toolruns.Service
	// wait is how long jobs/next holds a poll open (toolruns.LongPollWait;
	// tests shorten it).
	wait time.Duration
}

// agentEventsRequest is the body of an events post.
type agentEventsRequest struct {
	Events []toolruns.AgentEvent `json:"events"`
}

// agentFinishStatuses are the outcomes an agent may report: timed_out is its
// tool reaching the job's deadline.
var agentFinishStatuses = map[string]bool{
	models.ToolRunDone: true, models.ToolRunFailed: true, models.ToolRunRefused: true, models.ToolRunTimedOut: true,
}

// RegisterAgentJobRoutes mounts the job routes beside the other agent ingest
// routes: the agent's own bearer token, never the user session, and only for
// the agent named in the path (requireOwnAgent).
func RegisterAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service) {
	registerAgentJobRoutes(router, agents, runs, toolruns.LongPollWait)
}

func registerAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service, wait time.Duration) {
	h := &agentJobsHandler{runs: runs, wait: wait}
	g := router.Group("/api/v1/agents", RequireAgentToken(agents))
	g.GET("/:agent_id/jobs/next", h.next)
	g.POST("/:agent_id/jobs/:run_id/events", h.events)
	g.POST("/:agent_id/jobs/:run_id/finish", h.finish)
}

// next handles GET /agents/:agent_id/jobs/next: the oldest queued run for
// this agent, waiting up to h.wait for one to be queued. 204 when none came;
// 403 tools_disabled while Sentinel's switch for the agent is off.
func (h *agentJobsHandler) next(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	job, err := h.runs.NextJob(c.Request.Context(), agent, h.wait)
	switch {
	case errors.Is(err, toolruns.ErrToolsDisabled):
		respondToolError(c, http.StatusForbidden, codeToolsDisabled, "network tools are switched off for this agent in Sentinel")
	case err != nil && c.Request.Context().Err() != nil:
		// The agent hung up, or Sentinel is shutting down: nobody to answer.
		c.Status(http.StatusNoContent)
	case err != nil:
		respondInternal(c, "collecting an agent job", err)
	case job == nil:
		c.Status(http.StatusNoContent)
	default:
		respondSuccess(c, http.StatusOK, job)
	}
}

// events handles POST /agents/:agent_id/jobs/:run_id/events: a batch of the
// run's events (an empty batch is fine; its reply is how the agent learns of
// a cancel). The reply's cancel is true once the run has been cancelled.
func (h *agentJobsHandler) events(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	runID, ok := jobRunID(c)
	if !ok {
		return
	}
	var req agentEventsRequest
	if !bindAgentBody(c, &req) {
		return
	}
	if req.Events == nil {
		req.Events = []toolruns.AgentEvent{}
	}
	for _, ev := range req.Events {
		if ev.Seq < 1 || strings.TrimSpace(ev.Type) == "" || len(ev.Data) == 0 {
			respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams,
				"each event needs a seq of 1 or more, a type and data")
			return
		}
	}
	cancel, err := h.runs.AgentEvents(c.Request.Context(), agent, runID, req.Events)
	if err != nil {
		respondJobError(c, "storing agent events", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"cancel": cancel})
}

// finish handles POST /agents/:agent_id/jobs/:run_id/finish. A finish for a
// cancelled run only fills in its summary (ruling 8). The error text is
// bounded by the service.
func (h *agentJobsHandler) finish(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	runID, ok := jobRunID(c)
	if !ok {
		return
	}
	var req toolruns.AgentFinish
	if !bindAgentBody(c, &req) {
		return
	}
	if !agentFinishStatuses[req.Status] {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, toolruns.ErrBadFinishStatus.Error())
		return
	}
	if err := h.runs.AgentFinish(c.Request.Context(), agent, runID, req); err != nil {
		respondJobError(c, "finishing an agent job", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"ok": true})
}

// jobRunID reads :run_id.
func jobRunID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("run_id"))
	if err != nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "run_id must be a run id")
		return uuid.Nil, false
	}
	return id, true
}

// bindAgentBody decodes an agent post of at most MaxAgentPostBytes: 413 past
// that, 400 for anything that is not the expected JSON.
func bindAgentBody(c *gin.Context, into any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, toolruns.MaxAgentPostBytes)
	if err := c.ShouldBindJSON(into); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondToolError(c, http.StatusRequestEntityTooLarge, toolruns.CodeLimit, "an agent post may be at most 64 KB")
			return false
		}
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return false
	}
	return true
}

// respondJobError maps the dispatcher's errors: a run that is not running on
// this agent is 409 (the agent stops), too much output is 413, and a finish
// status the service refuses is 400 (finish checks it first; this is the
// backstop).
func respondJobError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, toolruns.ErrBadFinishStatus):
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, toolruns.ErrBadFinishStatus.Error())
	case errors.Is(err, toolruns.ErrRunNotActive):
		respondToolError(c, http.StatusConflict, toolruns.CodeConflict, "that run is not running on this agent")
	case errors.Is(err, toolruns.ErrTooMuchOutput):
		respondToolError(c, http.StatusRequestEntityTooLarge, toolruns.CodeLimit, "too much output")
	default:
		respondInternal(c, op, err)
	}
}

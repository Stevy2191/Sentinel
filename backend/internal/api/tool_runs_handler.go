package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// The SSE frames of a run stream (shared contract: "Hub keys and frames").
const (
	sseEvent  = "event"
	sseStatus = "status"
	sseEnd    = "end"
)

const msgNoSuchRun = "no such tool run"

// toolRunsHandler serves the user side of network tools.
type toolRunsHandler struct {
	runs *toolruns.Service
	// hub is the one the service publishes to; streams subscribe to it.
	hub *stream.Hub
	// keepAlive is how often an idle stream sends a comment
	// (stream.KeepAlive; tests shorten it).
	keepAlive time.Duration
	// afterReplay, when set, runs between the replay of stored events and
	// the switch to live ones. Tests use it to land events in that window.
	afterReplay func(runID uuid.UUID)
}

// RegisterToolRoutes mounts the network-tools routes on the authenticated v1
// group. Every route requires RequireNetTools; starting a run is also rate
// limited per user, and a run refused for permission reasons is audited.
func RegisterToolRoutes(rg *gin.RouterGroup, runs *toolruns.Service, hub *stream.Hub, users adminChecker, audit auditRecorder) {
	registerToolRoutes(rg, &toolRunsHandler{runs: runs, hub: hub, keepAlive: stream.KeepAlive}, users, audit)
}

func registerToolRoutes(rg *gin.RouterGroup, h *toolRunsHandler, users adminChecker, audit auditRecorder) {
	if h.keepAlive <= 0 {
		h.keepAlive = stream.KeepAlive
	}
	guard := RequireNetTools(users)
	// 30 runs a minute per user, burst 10. Its 429 carries the limiter's
	// plain error string (ruling 11); the per-target limit and the active-run
	// caps answer 429 with code "limit" from the service.
	limit := NewRateLimiter(toolruns.UserRatePerMinute, time.Minute, toolruns.UserRateBurst).Middleware("tool-runs", ByUser)

	g := rg.Group("/tools")
	g.GET("/vantages", guard, h.vantages)
	g.POST("/runs", auditToolRefusal(audit), guard, limit, h.create)
	g.GET("/runs", guard, h.list)
	g.GET("/runs/:id", guard, h.get)
	g.GET("/runs/:id/events", guard, h.follow)
	g.POST("/runs/:id/cancel", guard, h.cancel)
}

// vantages handles GET /tools/vantages: where a run can start from, and
// whether the allowlist is empty (grant holders cannot read the settings).
func (h *toolRunsHandler) vantages(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.runs.Vantages(ctx)
	if err != nil {
		respondInternal(c, "listing tool vantages", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"vantages": list, "allowlist_empty": h.runs.AllowlistEmpty(ctx)})
}

// maxCreateBodyBytes bounds a POST /tools/runs body: far above any real
// request (the longest field, the port list, stops at 8 KB).
const maxCreateBodyBytes = 16 << 10

// create handles POST /tools/runs.
func (h *toolRunsHandler) create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateBodyBytes)
	var req toolruns.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondToolError(c, http.StatusRequestEntityTooLarge, toolruns.CodeLimit, "the request is too large")
			return
		}
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return
	}
	run, err := h.runs.Create(c.Request.Context(), requesterFrom(c), req)
	if err != nil {
		if !writeRefusal(c, err) {
			respondInternal(c, "creating a tool run", err)
		}
		return
	}
	respondSuccess(c, http.StatusCreated, run)
}

// list handles GET /tools/runs: the history, newest first, with the total in
// X-Total-Count. mine=1 narrows it to the caller's own runs.
func (h *toolRunsHandler) list(c *gin.Context) {
	f := toolruns.ListFilter{
		Tool:    c.Query("tool"),
		AgentID: c.Query("agent_id"),
		Target:  strings.TrimSpace(c.Query("target")),
		Status:  c.Query("status"),
	}
	if mine := c.Query("mine"); mine == "1" || mine == "true" {
		id := requesterFrom(c).UserID
		f.UserID = &id
	} else if raw := c.Query("user_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "user_id must be a user id")
			return
		}
		f.UserID = &id
	}
	var ok bool
	if f.Limit, ok = toolQueryInt(c, "limit"); !ok {
		return
	}
	if f.Offset, ok = toolQueryInt(c, "offset"); !ok {
		return
	}
	runs, total, err := h.runs.List(c.Request.Context(), f)
	if err != nil {
		respondInternal(c, "listing tool runs", err)
		return
	}
	if runs == nil {
		runs = []models.ToolRun{}
	}
	c.Header("X-Total-Count", strconv.FormatInt(total, 10))
	respondSuccess(c, http.StatusOK, runs)
}

// toolQueryInt reads an optional whole-number query value, 0 when absent. It
// answers 400 itself when the value is malformed.
func toolQueryInt(c *gin.Context, name string) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, name+" must be a whole number")
		return 0, false
	}
	return n, true
}

// get handles GET /tools/runs/:id: the run with all its stored events.
func (h *toolRunsHandler) get(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	run, err := h.runs.Get(ctx, id)
	if err != nil {
		respondRunError(c, "reading a tool run", err)
		return
	}
	events, err := h.runs.Events(ctx, id, 0)
	if err != nil {
		respondInternal(c, "reading tool run events", err)
		return
	}
	if events == nil {
		events = []models.ToolRunEvent{}
	}
	respondSuccess(c, http.StatusOK, gin.H{"run": run, "events": events})
}

// cancel handles POST /tools/runs/:id/cancel: the owner's own run, or any
// run for an admin.
func (h *toolRunsHandler) cancel(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	run, err := h.runs.Cancel(c.Request.Context(), requesterFrom(c), id)
	if err != nil {
		respondRunError(c, "cancelling a tool run", err)
		return
	}
	respondSuccess(c, http.StatusOK, run)
}

// runIDParam reads :id. A malformed id names no run, so it is a 404.
func runIDParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, msgNoSuchRun)
		return uuid.Nil, false
	}
	return id, true
}

// respondRunError answers a refusal in its own shape, a missing run as 404
// and anything else as a logged 500.
func respondRunError(c *gin.Context, op string, err error) {
	if writeRefusal(c, err) {
		return
	}
	if errors.Is(err, toolruns.ErrRunNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, msgNoSuchRun)
		return
	}
	respondInternal(c, op, err)
}

// follow handles GET /tools/runs/:id/events: the run's events as
// Server-Sent Events, replayed from the database and then followed live.
//
// No gap and no duplicate (Review Focus 2). The hub subscription is taken
// before anything is read, so every event published from then on reaches
// it, and every event stored before the replay query is in the replay; an
// event in both is skipped by its seq. The run is re-read after the replay:
// a run that has ended gets the rest of its stored events and its end frame
// from the database, and one still active ends with the hub's end frame,
// which the service publishes when the run reaches a final status
// (including the sweeper's timed_out, Review Focus 1).
func (h *toolRunsHandler) follow(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := h.runs.Get(ctx, id); err != nil {
		respondRunError(c, "opening a tool run stream", err)
		return
	}
	sent := lastEventID(c.GetHeader("Last-Event-ID"))

	sub := h.hub.Subscribe(id.String())
	defer sub.Close()

	w, err := stream.NewWriter(c.Writer)
	if err != nil {
		respondInternal(c, "opening a tool run stream", err)
		return
	}
	replay := func() bool {
		events, err := h.runs.Events(ctx, id, sent)
		if err != nil {
			log.Printf("[tools] replaying run %s: %v", id, err)
			return false
		}
		for _, ev := range events {
			data, err := json.Marshal(ev)
			if err != nil || w.Send(stream.Message{ID: strconv.Itoa(ev.Seq), Event: sseEvent, Data: data}) != nil {
				return false
			}
			sent = ev.Seq
		}
		return true
	}
	if !replay() {
		return
	}
	if h.afterReplay != nil {
		h.afterReplay(id)
	}

	run, err := h.runs.Get(ctx, id)
	if err != nil {
		log.Printf("[tools] re-reading run %s: %v", id, err)
		return
	}
	if !models.ToolRunActive(run.Status) {
		if replay() {
			_ = sendSSEJSON(w, sseEnd, run)
		}
		return
	}
	// Where the run stands now, so a page that connects after the agent
	// picked the run up does not wait for a change that already happened.
	if sendSSEJSON(w, sseStatus, gin.H{"status": run.Status}) != nil {
		return
	}

	keepAlive := time.NewTicker(h.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if w.Comment("keep-alive") != nil {
				return
			}
		case m, ok := <-sub.C():
			if !ok {
				// Dropped for falling behind: the browser reconnects with
				// Last-Event-ID and replays from the database.
				return
			}
			if m.Event == sseEvent {
				seq, err := strconv.Atoi(m.ID)
				if err != nil || seq <= sent {
					continue
				}
				sent = seq
			}
			if w.Send(m) != nil || m.Event == sseEnd {
				return
			}
		}
	}
}

// sendSSEJSON sends v as one frame without an id.
func sendSSEJSON(w *stream.Writer, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.Send(stream.Message{Event: event, Data: data})
}

// lastEventID is the seq a reconnecting EventSource saw last, 0 if none.
func lastEventID(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

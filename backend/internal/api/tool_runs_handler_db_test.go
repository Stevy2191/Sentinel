package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// testJWTSecret satisfies NewAuthService's minimum length.
const testJWTSecret = "test-secret-test-secret-test-secret-0123"

// newTestToolRuns is the run service on db, publishing to hub.
func newTestToolRuns(db *gorm.DB, hub *stream.Hub) *toolruns.Service {
	return toolruns.New(toolruns.Deps{
		DB:       db,
		Settings: services.NewSettingsService(db),
		Audit:    services.NewAuditService(db),
		Hub:      hub,
	})
}

// toolRunRow is a run inserted straight into the database. Handler tests
// insert rows rather than going through Create, so they control the status,
// the owner and the stored events exactly.
type toolRunRow struct {
	status   string
	user     uuid.UUID
	agent    *models.Agent // nil: a run on the Sentinel server
	events   int           // events 1..events are stored, and event_count matches
	created  time.Time     // zero: now
	deadline time.Time     // zero: two minutes from now
}

// insertToolRun stores a ping run against 10.0.0.5 and returns its id.
func insertToolRun(t *testing.T, db *gorm.DB, r toolRunRow) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	created, deadline := r.created, r.deadline
	if created.IsZero() {
		created = now
	}
	if deadline.IsZero() {
		deadline = now.Add(2 * time.Minute)
	}
	kind, name := models.VantageSentinel, "Sentinel"
	var agentID *uuid.UUID
	var agentRef *string
	if r.agent != nil {
		kind, name, agentID, agentRef = models.VantageAgent, r.agent.Name, &r.agent.ID, &r.agent.AgentID
	}
	var started, finished *time.Time
	if r.status != models.ToolRunQueued {
		started = &now
	}
	if !models.ToolRunActive(r.status) {
		finished = &now
	}
	testdb.Exec(t, db, `INSERT INTO tool_runs (id, tool, status, user_id, username, vantage_kind, agent_id, agent_ref,
		vantage_name, target, target_ip, params, event_count, created_at, started_at, finished_at, deadline)
		VALUES (?, 'ping', ?, ?, 'someone', ?, ?, ?, ?, '10.0.0.5', '10.0.0.5',
		'{"count":5,"interval_ms":1000,"size":56,"timeout_ms":2000}', ?, ?, ?, ?, ?)`,
		id, r.status, r.user, kind, agentID, agentRef, name, r.events, created, started, finished, deadline)
	for seq := 1; seq <= r.events; seq++ {
		testdb.Exec(t, db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, ?, ?, 'reply', ?)`,
			id, seq, now, fmt.Sprintf(`{"seq":%d,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`, seq))
	}
	return id
}

// registerToolAgent registers an agent that reports ENABLE_TOOLS and has
// Sentinel's switch set to enabled. It is returned with its token.
func registerToolAgent(t *testing.T, db *gorm.DB, name string, enabled bool) *models.Agent {
	t.Helper()
	ctx := context.Background()
	agents := services.NewAgentService(db)
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, agents.Register(ctx, a))
	testdb.Exec(t, db, `UPDATE agents SET tools_enabled = ?, tools_local = true WHERE id = ?`, enabled, a.ID)
	got, err := agents.Get(ctx, a.AgentID)
	testdb.Must(t, err)
	return got
}

// grantedUser makes a non-admin holding the network tools grant.
func grantedUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `UPDATE users SET net_tools = true WHERE id = ?`, id)
	return id
}

// toolsRig is the tool routes on a real database, as seen by one user.
type toolsRig struct {
	db   *gorm.DB
	hub  *stream.Hub
	runs *toolruns.Service
	h    *toolRunsHandler
}

func newToolsRig(t *testing.T) *toolsRig {
	t.Helper()
	db := testdb.Open(t)
	hub := stream.NewHub(64)
	runs := newTestToolRuns(db, hub)
	return &toolsRig{db: db, hub: hub, runs: runs, h: &toolRunsHandler{runs: runs, hub: hub}}
}

// routerFor mounts the routes for user, with RequireNetTools reading the
// database. isAdmin is the token's claim.
func (rig *toolsRig) routerFor(t *testing.T, user uuid.UUID, isAdmin bool) *gin.Engine {
	t.Helper()
	var name string
	testdb.Must(t, rig.db.Raw(`SELECT username FROM users WHERE id = ?`, user).Scan(&name).Error)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", name)
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	registerToolRoutes(v1, rig.h, services.NewAuthService(rig.db, testJWTSecret), services.NewAuditService(rig.db))
	return r
}

// toolData decodes a success envelope's data into out.
func toolData(t *testing.T, body []byte, out any) {
	t.Helper()
	var env struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || !env.Success {
		t.Fatalf("body %s: %v", body, err)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("data %s: %v", env.Data, err)
	}
}

// A refusal from the service reaches the browser in the coded shape, with
// its own status. The allowlist is empty out of the box, so nothing may run.
func TestDBCreateToolRunAnswersRefusalsInTheCodedShape(t *testing.T) {
	rig := newToolsRig(t)
	r := rig.routerFor(t, grantedUser(t, rig.db), false)
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs",
		`{"tool":"ping","vantage":{"kind":"sentinel"},"target":"10.0.0.5","params":{}}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "target_not_allowed" || msg == "" {
		t.Errorf("refusal = %q %q, want target_not_allowed with a message", code, msg)
	}
	var runs int64
	testdb.Must(t, rig.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&runs).Error)
	if runs != 0 {
		t.Errorf("%d runs stored for a refused request", runs)
	}
}

// A run from a ready agent is queued and returned as the API shows runs: the
// readable agent id under agent_id, the resolved address, the defaults filled.
func TestDBCreateToolRunQueuesAnAgentRun(t *testing.T) {
	rig := newToolsRig(t)
	ctx := context.Background()
	_, err := toolruns.SaveSettings(ctx, services.NewSettingsService(rig.db),
		toolruns.Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	// One poll makes the agent ready (it counts for 60 s).
	if job, err := rig.runs.NextJob(ctx, agent, time.Millisecond); err != nil || job != nil {
		t.Fatalf("NextJob = %v, %v; want an empty poll", job, err)
	}
	user := grantedUser(t, rig.db)
	w := toolRequest(rig.routerFor(t, user, false), http.MethodPost, "/api/v1/tools/runs",
		`{"tool":"ping","vantage":{"kind":"agent","agent_id":"`+agent.AgentID+`"},"target":"10.0.0.5","params":{}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", w.Code, w.Body.String())
	}
	var run struct {
		Status      string    `json:"status"`
		Tool        string    `json:"tool"`
		UserID      uuid.UUID `json:"user_id"`
		VantageKind string    `json:"vantage_kind"`
		AgentID     string    `json:"agent_id"`
		VantageName string    `json:"vantage_name"`
		TargetIP    string    `json:"target_ip"`
		Params      struct {
			Count int `json:"count"`
		} `json:"params"`
	}
	toolData(t, w.Body.Bytes(), &run)
	if run.Status != "queued" || run.Tool != "ping" || run.UserID != user || run.VantageKind != "agent" ||
		run.AgentID != agent.AgentID || run.VantageName != "file-server" || run.TargetIP != "10.0.0.5" || run.Params.Count != 5 {
		t.Errorf("run = %+v", run)
	}
}

// POST /tools/runs reads at most 16 KB of body: a larger one is refused with
// 413 limit and nothing is created, even a valid request padded out. The
// same request unpadded is accepted, so the size alone was refused.
func TestDBCreateToolRunRefusesAnOversizedBody(t *testing.T) {
	rig := newToolsRig(t)
	ctx := context.Background()
	_, err := toolruns.SaveSettings(ctx, services.NewSettingsService(rig.db),
		toolruns.Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	if job, err := rig.runs.NextJob(ctx, agent, time.Millisecond); err != nil || job != nil {
		t.Fatalf("NextJob = %v, %v; want an empty poll", job, err)
	}
	r := rig.routerFor(t, grantedUser(t, rig.db), false)
	body := `{"tool":"ping","vantage":{"kind":"agent","agent_id":"` + agent.AgentID + `"},"target":"10.0.0.5","params":{}}`
	padded := body[:len(body)-1] + strings.Repeat(" ", 16<<10) + "}"

	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", padded)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", w.Code, w.Body.String())
	}
	if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "limit" || msg != "the request is too large" {
		t.Errorf("refusal = %q %q, want limit / the request is too large", code, msg)
	}
	var runs int64
	testdb.Must(t, rig.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&runs).Error)
	if runs != 0 {
		t.Errorf("%d runs stored for an oversized request", runs)
	}

	if w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", body); w.Code != http.StatusCreated {
		t.Errorf("the same request unpadded: status %d, want 201: %s", w.Code, w.Body.String())
	}
}

// Grant holders see every run; mine=1 narrows to their own. Newest first,
// with the total in X-Total-Count.
func TestDBListToolRunsFiltersAndCounts(t *testing.T) {
	rig := newToolsRig(t)
	alice, bob := grantedUser(t, rig.db), grantedUser(t, rig.db)
	older := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: alice, created: time.Now().UTC().Add(-time.Hour)})
	newer := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: bob})
	r := rig.routerFor(t, bob, false)

	w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs", "")
	var all []struct {
		ID uuid.UUID `json:"id"`
	}
	toolData(t, w.Body.Bytes(), &all)
	if len(all) != 2 || all[0].ID != newer || all[1].ID != older || w.Header().Get("X-Total-Count") != "2" {
		t.Errorf("all runs = %+v, total %q; want newer then older, 2", all, w.Header().Get("X-Total-Count"))
	}

	w = toolRequest(r, http.MethodGet, "/api/v1/tools/runs?mine=1", "")
	var mine []struct {
		ID uuid.UUID `json:"id"`
	}
	toolData(t, w.Body.Bytes(), &mine)
	if len(mine) != 1 || mine[0].ID != newer || w.Header().Get("X-Total-Count") != "1" {
		t.Errorf("mine = %+v, total %q; want only bob's run", mine, w.Header().Get("X-Total-Count"))
	}

	if w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs?limit=lots", ""); w.Code != http.StatusBadRequest {
		t.Errorf("limit=lots: status %d, want 400", w.Code)
	}
}

func TestDBGetToolRunReturnsTheRunWithItsEvents(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 3})
	r := rig.routerFor(t, user, false)

	w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs/"+id.String(), "")
	var detail struct {
		Run struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"run"`
		Events []struct {
			Seq  int    `json:"seq"`
			Type string `json:"type"`
		} `json:"events"`
	}
	toolData(t, w.Body.Bytes(), &detail)
	if detail.Run.ID != id || detail.Run.Status != "done" || len(detail.Events) != 3 ||
		detail.Events[0].Seq != 1 || detail.Events[2].Seq != 3 || detail.Events[1].Type != "reply" {
		t.Errorf("detail = %+v", detail)
	}
	for _, path := range []string{"/api/v1/tools/runs/" + uuid.NewString(), "/api/v1/tools/runs/not-a-run"} {
		w := toolRequest(r, http.MethodGet, path, "")
		if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusNotFound || code != "not_found" {
			t.Errorf("%s: status %d code %q, want 404 not_found", path, w.Code, code)
		}
	}
}

// Grant holders cancel their own runs only; admins cancel any run; a run
// that has ended cannot be cancelled.
func TestDBCancelToolRunChecksOwnership(t *testing.T) {
	rig := newToolsRig(t)
	owner, other := grantedUser(t, rig.db), grantedUser(t, rig.db)
	admin := testdb.NewUser(t, rig.db, true)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: owner, agent: agent})
	path := "/api/v1/tools/runs/" + id.String() + "/cancel"

	w := toolRequest(rig.routerFor(t, other, false), http.MethodPost, path, "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("another user's cancel: status %d code %q, want 403 forbidden", w.Code, code)
	}
	var status string
	testdb.Must(t, rig.db.Raw(`SELECT status FROM tool_runs WHERE id = ?`, id).Scan(&status).Error)
	if status != models.ToolRunQueued {
		t.Fatalf("status %q after a refused cancel, want queued", status)
	}

	w = toolRequest(rig.routerFor(t, admin, true), http.MethodPost, path, "")
	var run struct {
		Status string `json:"status"`
	}
	toolData(t, w.Body.Bytes(), &run)
	if run.Status != models.ToolRunCancelled {
		t.Errorf("admin cancel: status %q, want cancelled", run.Status)
	}

	w = toolRequest(rig.routerFor(t, owner, false), http.MethodPost, path, "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusConflict || code != "conflict" {
		t.Errorf("cancelling an ended run: status %d code %q, want 409 conflict", w.Code, code)
	}
}

// The Tools page reads both lists from one call: grant holders cannot read
// the settings, so the empty-allowlist flag comes with the vantages.
func TestDBVantagesComeWithTheAllowlistFlag(t *testing.T) {
	rig := newToolsRig(t)
	w := toolRequest(rig.routerFor(t, grantedUser(t, rig.db), false), http.MethodGet, "/api/v1/tools/vantages", "")
	var got struct {
		Vantages []struct {
			Kind  string `json:"kind"`
			Ready bool   `json:"ready"`
		} `json:"vantages"`
		AllowlistEmpty bool `json:"allowlist_empty"`
	}
	toolData(t, w.Body.Bytes(), &got)
	if len(got.Vantages) == 0 || got.Vantages[0].Kind != "sentinel" || !got.Vantages[0].Ready || !got.AllowlistEmpty {
		t.Errorf("vantages = %+v, want Sentinel first and ready, and allowlist_empty", got)
	}
}

package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// netToolsAgent registers an agent and returns it.
func netToolsAgent(t *testing.T, svc *AgentService, name string) *models.Agent {
	t.Helper()
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, svc.Register(context.Background(), a))
	return a
}

// insertToolRun writes a minimal valid run with the given status, tool and
// vantage kind, returning the error so CHECK tests can expect one.
func insertToolRun(db *gorm.DB, id uuid.UUID, tool, status, vantage string, user, agent *uuid.UUID) error {
	now := time.Now().UTC()
	return db.Exec(`INSERT INTO tool_runs (id, tool, status, user_id, username, vantage_kind, agent_id,
			vantage_name, target, params, created_at, deadline)
		VALUES (?, ?, ?, ?, 'alice', ?, ?, 'Sentinel', '10.0.0.5', '{}', ?, ?)`,
		id, tool, status, user, vantage, agent, now, now.Add(time.Minute)).Error
}

func TestDBToolRunsSchema(t *testing.T) {
	db := testdb.Open(t)
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	user := testdb.NewUser(t, db, false)

	run := uuid.New()
	testdb.Must(t, insertToolRun(db, run, "ping", models.ToolRunRunning, models.VantageAgent, &user, &agent.ID))
	testdb.Exec(t, db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{"seq":1}')`, run)

	for _, bad := range []struct{ name, tool, status, vantage string }{
		{"unknown tool", "nmap", models.ToolRunQueued, models.VantageSentinel},
		{"unknown status", "ping", "paused", models.VantageSentinel},
		{"unknown vantage", "ping", models.ToolRunQueued, "moon"},
	} {
		if err := insertToolRun(db, uuid.New(), bad.tool, bad.status, bad.vantage, nil, nil); err == nil {
			t.Errorf("%s was accepted", bad.name)
		}
	}
	if err := db.Exec(`INSERT INTO tool_runs (id, tool, status, username, vantage_kind, vantage_name, target, params, created_at, deadline)
		VALUES (?, 'ping', NULL, 'a', 'sentinel', 'Sentinel', 'x', '{}', now(), now())`, uuid.New()).Error; err == nil {
		t.Error("a NULL status was accepted")
	}
	if err := db.Exec(`INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{}')`, run).Error; err == nil {
		t.Error("a duplicate (run_id, seq) was accepted")
	}

	// Deleting the agent and the user keeps the run, unlinked.
	testdb.Must(t, agents.Delete(context.Background(), agent.AgentID))
	testdb.Exec(t, db, `DELETE FROM users WHERE id = ?`, user)
	var row struct {
		AgentID *uuid.UUID
		UserID  *uuid.UUID
	}
	testdb.Must(t, db.Raw(`SELECT agent_id, user_id FROM tool_runs WHERE id = ?`, run).Scan(&row).Error)
	if row.AgentID != nil || row.UserID != nil {
		t.Errorf("after deleting the agent and the user: agent_id %v, user_id %v, want both NULL", row.AgentID, row.UserID)
	}

	// Deleting the run takes its events with it.
	testdb.Exec(t, db, `DELETE FROM tool_runs WHERE id = ?`, run)
	var events int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM tool_run_events WHERE run_id = ?`, run).Scan(&events).Error)
	if events != 0 {
		t.Errorf("%d events outlived their run", events)
	}
}

// A ToolRun read back through GORM keeps a NULL summary as nil.
func TestDBToolRunModelRoundTrip(t *testing.T) {
	db := testdb.Open(t)
	id := uuid.New()
	testdb.Must(t, insertToolRun(db, id, "dns", models.ToolRunQueued, models.VantageSentinel, nil, nil))
	var run models.ToolRun
	testdb.Must(t, db.First(&run, "id = ?", id).Error)
	if run.Summary != nil || run.Error != nil || run.TargetIP != nil || run.AgentUUID != nil || string(run.Params) != "{}" {
		t.Errorf("run = %+v, want nil summary, error, target_ip and agent, params {}", run)
	}
	testdb.Exec(t, db, `UPDATE tool_runs SET summary = '{"sent":5}' WHERE id = ?`, id)
	testdb.Must(t, db.First(&run, "id = ?", id).Error)
	if run.Summary == nil || string(*run.Summary) != `{"sent": 5}` {
		t.Errorf("summary = %v, want {\"sent\": 5}", run.Summary)
	}
}

func TestDBHeartbeatToolsLocal(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	yes, no := true, false

	stored := func() *bool {
		t.Helper()
		var v *bool
		testdb.Must(t, db.Raw(`SELECT tools_local FROM agents WHERE id = ?`, agent.ID).Scan(&v).Error)
		return v
	}
	if v := stored(); v != nil {
		t.Fatalf("a new agent has tools_local %v, want NULL", *v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{ToolsLocal: &yes}))
	if v := stored(); v == nil || !*v {
		t.Errorf("after tools_local true: %v", v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{ToolsLocal: &no}))
	if v := stored(); v == nil || *v {
		t.Errorf("after tools_local false: %v", v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{}))
	if v := stored(); v != nil {
		t.Errorf("after a heartbeat without the flag: %v, want NULL", *v)
	}
}

func TestDBSetToolsEnabled(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	if agent.ToolsEnabled {
		t.Fatal("a new agent has tools enabled")
	}
	got, err := agents.SetToolsEnabled(ctx, agent.AgentID, true)
	testdb.Must(t, err)
	if !got.ToolsEnabled || got.AgentID != agent.AgentID {
		t.Errorf("SetToolsEnabled(true) = %+v", got)
	}
	got, err = agents.SetToolsEnabled(ctx, agent.AgentID, false)
	testdb.Must(t, err)
	if got.ToolsEnabled {
		t.Error("SetToolsEnabled(false) left tools on")
	}
	if _, err := agents.SetToolsEnabled(ctx, "agent_ffffffffff", true); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("unknown agent: %v, want ErrAgentNotFound", err)
	}
}

func TestDBSetNetTools(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	auth := NewAuthService(db, "0123456789abcdef0123456789abcdef")
	id := testdb.NewUser(t, db, false)

	u, err := auth.GetUserByID(ctx, id)
	testdb.Must(t, err)
	if u.NetTools {
		t.Fatal("a new user has the network tools grant")
	}
	u, err = auth.SetNetTools(ctx, id, true)
	testdb.Must(t, err)
	if !u.NetTools {
		t.Error("SetNetTools(true) did not grant")
	}
	u, err = auth.SetNetTools(ctx, id, false)
	testdb.Must(t, err)
	if u.NetTools {
		t.Error("SetNetTools(false) did not revoke")
	}
	if _, err := auth.SetNetTools(ctx, uuid.New(), true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("unknown user: %v, want gorm.ErrRecordNotFound", err)
	}
}

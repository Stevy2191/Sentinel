package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestToolRunActive(t *testing.T) {
	for status, want := range map[string]bool{
		ToolRunQueued: true, ToolRunRunning: true,
		ToolRunDone: false, ToolRunFailed: false, ToolRunRefused: false,
		ToolRunCancelled: false, ToolRunTimedOut: false, ToolRunInterrupted: false,
		"": false,
	} {
		if got := ToolRunActive(status); got != want {
			t.Errorf("ToolRunActive(%q) = %v, want %v", status, got, want)
		}
	}
}

// The API calls the readable agent id agent_id; the UUID foreign key never
// leaves the server, and a run with no summary says null rather than {}.
func TestToolRunJSON(t *testing.T) {
	agent := uuid.New()
	ref := "agent_0123456789"
	run := ToolRun{
		ID: uuid.New(), Tool: "ping", Status: ToolRunQueued, Username: "alice",
		VantageKind: VantageAgent, AgentUUID: &agent, AgentRef: &ref, VantageName: "file-server",
		Target: "10.0.0.5", Params: RawJSON(`{"count":5}`),
		CreatedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), Deadline: time.Date(2026, 10, 5, 12, 2, 30, 0, time.UTC),
	}
	b, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"agent_id":"agent_0123456789"`, `"summary":null`, `"params":{"count":5}`, `"target_ip":null`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s lacks %s", s, want)
		}
	}
	if strings.Contains(s, agent.String()) {
		t.Errorf("JSON %s leaks the agent's UUID", s)
	}
}

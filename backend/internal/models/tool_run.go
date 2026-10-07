package models

import (
	"time"

	"github.com/google/uuid"
)

// Tool run statuses. A run is queued (an agent run waiting to be claimed) or
// running until it reaches one of the final statuses.
const (
	ToolRunQueued      = "queued"
	ToolRunRunning     = "running"
	ToolRunDone        = "done"
	ToolRunFailed      = "failed"
	ToolRunRefused     = "refused"
	ToolRunCancelled   = "cancelled"
	ToolRunTimedOut    = "timed_out"
	ToolRunInterrupted = "interrupted"

	// Where a run executes.
	VantageSentinel = "sentinel"
	VantageAgent    = "agent"
)

// ToolRunActive reports whether a run in this status is still in progress
// (queued or running), which is what the active-run caps count.
func ToolRunActive(status string) bool {
	return status == ToolRunQueued || status == ToolRunRunning
}

// ToolRun is one run of a network tool (spec 2026-10-05-tools-s1).
//
// AgentUUID is the foreign key and is never serialised; AgentRef, the
// agent's readable id snapshotted at creation, is what the API calls
// agent_id. VantageName and Username are snapshots too, so history still
// reads correctly after the agent or the user is deleted.
type ToolRun struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	Tool        string     `json:"tool" gorm:"column:tool"`
	Status      string     `json:"status" gorm:"column:status"`
	UserID      *uuid.UUID `json:"user_id" gorm:"column:user_id"`
	Username    string     `json:"username" gorm:"column:username"`
	VantageKind string     `json:"vantage_kind" gorm:"column:vantage_kind"`
	AgentUUID   *uuid.UUID `json:"-" gorm:"column:agent_id"`
	AgentRef    *string    `json:"agent_id" gorm:"column:agent_ref"`
	VantageName string     `json:"vantage_name" gorm:"column:vantage_name"`
	Target      string     `json:"target" gorm:"column:target"`
	TargetIP    *string    `json:"target_ip" gorm:"column:target_ip"`
	Params      RawJSON    `json:"params" gorm:"column:params;type:jsonb"`
	Summary     *RawJSON   `json:"summary" gorm:"column:summary;type:jsonb"`
	Error       *string    `json:"error" gorm:"column:error"`
	EventCount  int        `json:"event_count" gorm:"column:event_count"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at"`
	StartedAt   *time.Time `json:"started_at" gorm:"column:started_at"`
	FinishedAt  *time.Time `json:"finished_at" gorm:"column:finished_at"`
	Deadline    time.Time  `json:"deadline" gorm:"column:deadline"`
}

// TableName pins the table name.
func (ToolRun) TableName() string { return "tool_runs" }

// ToolRunEvent is one event a run reported. Seq numbers a run's events from 1.
type ToolRunEvent struct {
	RunID uuid.UUID `json:"-" gorm:"column:run_id;type:uuid;primaryKey"`
	Seq   int       `json:"seq" gorm:"column:seq;primaryKey"`
	At    time.Time `json:"at" gorm:"column:at"`
	Type  string    `json:"type" gorm:"column:type"`
	Data  RawJSON   `json:"data" gorm:"column:data;type:jsonb"`
}

// TableName pins the table name.
func (ToolRunEvent) TableName() string { return "tool_run_events" }

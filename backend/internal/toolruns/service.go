// Package toolruns owns the lifecycle of network tool runs (spec
// 2026-10-05-tools-s1): the guardrails at creation, running on the Sentinel
// server, the agent job queue, recording events, cancelling, sweeping and
// pruning. The tool_runs table is the record; the stream hub only carries
// live events to open browsers.
package toolruns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// Limits and timings (spec "Limits" and "Timeouts and sweeper").
const (
	UserRatePerMinute   = 30
	UserRateBurst       = 10
	TargetRatePerMinute = 20
	MaxActivePerUser    = 3
	MaxActivePerTarget  = 3
	MaxActiveOverall    = 10
	MaxActivePerAgent   = 2
	MaxEventsPerRun     = 5000
	MaxAgentPostBytes   = 64 << 10
	PickupTimeout       = 30 * time.Second
	OverdueGrace        = 15 * time.Second
	ReadyWindow         = 60 * time.Second
	LongPollWait        = 25 * time.Second
	FlushEvery          = 250 * time.Millisecond
	SweepEvery          = 5 * time.Second

	DefaultRetentionDays, MinRetentionDays, MaxRetentionDays = 30, 1, 365
)

// sentinelVantageName is the vantage_name of runs on the Sentinel server.
const sentinelVantageName = "Sentinel"

// resolveTimeout bounds the one lookup Create makes for a host name.
const resolveTimeout = 5 * time.Second

// AuditRecorder is the part of *services.AuditService the runs use.
type AuditRecorder interface {
	Record(ctx context.Context, actor services.Actor, action, resourceType string, resourceID *uuid.UUID, changes models.AuditChanges)
}

// Deps are the service's collaborators. Only DB and Settings are required.
type Deps struct {
	DB       *gorm.DB
	Settings *services.SettingsService
	Audit    AuditRecorder                                            // nil → nothing is audited
	Hub      *stream.Hub                                              // nil → a private hub
	Runner   *nettools.Runner                                         // nil → &nettools.Runner{}
	Resolve  func(ctx context.Context, host string) ([]net.IP, error) // nil → net.DefaultResolver.LookupIP(ctx, "ip", host)
	Now      func() time.Time                                         // nil → time.Now().UTC()
}

// Service runs and records network tool runs.
type Service struct {
	db       *gorm.DB
	settings *services.SettingsService
	audit    AuditRecorder
	hub      *stream.Hub
	resolve  func(ctx context.Context, host string) ([]net.IP, error)
	now      func() time.Time

	// runTool executes a normalized spec: the Runner's Run. Tests replace it
	// to script a tool's events without touching the network.
	runTool func(ctx context.Context, spec nettools.Spec, emit nettools.Emitter) (any, error)
	// launch starts a run on the Sentinel server once it is inserted. Tests
	// replace it to observe or suppress local execution.
	launch func(run *models.ToolRun, spec nettools.Spec)

	targets *targetLimiter
	polls   *pollTracker

	mu      sync.Mutex
	wakers  map[uuid.UUID]chan struct{}      // per agent: a run was queued for it
	cancels map[uuid.UUID]context.CancelFunc // per local run: stops its tool
}

// New builds the service.
func New(d Deps) *Service {
	s := &Service{
		db:       d.DB,
		settings: d.Settings,
		audit:    d.Audit,
		hub:      d.Hub,
		resolve:  d.Resolve,
		now:      d.Now,
		targets:  newTargetLimiter(),
		polls:    newPollTracker(),
		wakers:   make(map[uuid.UUID]chan struct{}),
		cancels:  make(map[uuid.UUID]context.CancelFunc),
	}
	if s.audit == nil {
		s.audit = noAudit{}
	}
	if s.hub == nil {
		s.hub = stream.NewHub(256)
	}
	if s.resolve == nil {
		s.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	runner := d.Runner
	if runner == nil {
		runner = &nettools.Runner{}
	}
	s.runTool = runner.Run
	s.launch = s.launchLocal
	return s
}

type noAudit struct{}

func (noAudit) Record(context.Context, services.Actor, string, string, *uuid.UUID, models.AuditChanges) {
}

// Requester is who asks for a run, as the API authenticated them.
type Requester struct {
	UserID   uuid.UUID
	Username string
	IsAdmin  bool
	IP       string
}

func (r Requester) actor() services.Actor {
	return services.Actor{UserID: r.UserID, Username: r.Username, IP: r.IP}
}

// Vantage is where a run executes: the Sentinel server, or an agent by its
// readable id.
type Vantage struct {
	Kind    string `json:"kind"`               // models.VantageSentinel | models.VantageAgent
	AgentID string `json:"agent_id,omitempty"` // readable agent id
}

// CreateRequest is the body of POST /tools/runs.
type CreateRequest struct {
	Tool    nettools.Tool   `json:"tool"`
	Vantage Vantage         `json:"vantage"`
	Target  string          `json:"target"`
	Params  nettools.Params `json:"params"`
}

// Refusal is a failure the user sees; the API writes it with
// respondToolError.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// Refusal codes.
const (
	CodeForbidden        = "forbidden"
	CodeInvalidParams    = "invalid_params"
	CodeTargetNotAllowed = "target_not_allowed"
	CodeResolveFailed    = "resolve_failed"
	CodeIPv6Unsupported  = "ipv6_unsupported"
	CodeVantageNotReady  = "vantage_not_ready"
	CodeLimit            = "limit"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
)

func refuse(status int, code, format string, args ...any) *Refusal {
	return &Refusal{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// wakeChan is agentID's wake signal: a channel of one, filled when a run is
// queued for the agent. A missed signal costs at most one long-poll cycle,
// because the table is the queue.
func (s *Service) wakeChan(agentID uuid.UUID) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.wakers[agentID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.wakers[agentID] = ch
	}
	return ch
}

// wake signals agentID's waiting poll, if any, without blocking.
func (s *Service) wake(agentID uuid.UUID) {
	select {
	case s.wakeChan(agentID) <- struct{}{}:
	default:
	}
}

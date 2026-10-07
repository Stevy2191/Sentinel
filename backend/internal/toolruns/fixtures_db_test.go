package toolruns

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// testAllowlist is what newEnv saves unless a test passes its own.
var testAllowlist = []string{"10.0.0.0/24", "fileserver.example.org", "*.lab.example.org"}

// testHosts is the fake resolver's zone.
var testHosts = map[string][]net.IP{
	"fileserver.example.org": {net.ParseIP("10.0.0.5")},
	"meta.lab.example.org":   {net.ParseIP("169.254.169.254")},
	"v6only.example.org":     {net.ParseIP("2001:db8::5")},
	"dual.lab.example.org":   {net.ParseIP("2001:db8::6"), net.ParseIP("10.0.0.6")},
}

func fakeResolve(_ context.Context, host string) ([]net.IP, error) {
	if ips, ok := testHosts[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// clock is a settable time source for Deps.Now.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now().UTC().Truncate(time.Millisecond)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type auditEntry struct {
	actor        services.Actor
	action       string
	resourceType string
	resourceID   *uuid.UUID
	changes      models.AuditChanges
}

// fakeAudit records what the service audits.
type fakeAudit struct {
	mu      sync.Mutex
	entries []auditEntry
}

func (f *fakeAudit) Record(_ context.Context, actor services.Actor, action, resourceType string, resourceID *uuid.UUID, changes models.AuditChanges) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, auditEntry{actor, action, resourceType, resourceID, changes})
}

func (f *fakeAudit) byAction(action string) []auditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auditEntry
	for _, e := range f.entries {
		if e.action == action {
			out = append(out, e)
		}
	}
	return out
}

type launchCall struct {
	run  *models.ToolRun
	spec nettools.Spec
}

// env is a service under test on a fresh database. Its launch hook records
// calls instead of running anything; tests of local runs put the real
// launcher back.
type env struct {
	db       *gorm.DB
	settings *services.SettingsService
	svc      *Service
	clock    *clock
	audit    *fakeAudit
	hub      *stream.Hub
	launched chan launchCall
}

func newEnv(t *testing.T, allowlist ...string) *env {
	t.Helper()
	db := testdb.Open(t)
	ss := services.NewSettingsService(db)
	if allowlist == nil {
		allowlist = testAllowlist
	}
	_, err := SaveSettings(context.Background(), ss, Settings{Allowlist: allowlist, ServerEnabled: true, RetentionDays: DefaultRetentionDays})
	testdb.Must(t, err)
	e := &env{db: db, settings: ss, clock: newClock(), audit: &fakeAudit{}, hub: stream.NewHub(64),
		launched: make(chan launchCall, 64)}
	e.svc = New(Deps{DB: db, Settings: ss, Audit: e.audit, Hub: e.hub, Resolve: fakeResolve, Now: e.clock.Now})
	e.svc.launch = func(run *models.ToolRun, spec nettools.Spec) { e.launched <- launchCall{run, spec} }
	return e
}

// user inserts a user and returns them as a requester.
func (e *env) user(t *testing.T, admin bool) Requester {
	t.Helper()
	id := testdb.NewUser(t, e.db, admin)
	return Requester{UserID: id, Username: "u" + id.String()[:8], IsAdmin: admin, IP: "192.0.2.10"}
}

func boolPtr(b bool) *bool { return &b }

// newAgent registers an agent with the given switches; local nil means the
// agent never reported tools_local (too old).
func (e *env) newAgent(t *testing.T, name string, enabled bool, local *bool) *models.Agent {
	t.Helper()
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, services.NewAgentService(e.db).Register(context.Background(), a))
	testdb.Exec(t, e.db, `UPDATE agents SET tools_enabled = ?, tools_local = ? WHERE id = ?`, enabled, local, a.ID)
	a.ToolsEnabled, a.ToolsLocal = enabled, local
	return a
}

// readyAgent is an agent with both switches on that polled just now.
func (e *env) readyAgent(t *testing.T, name string) *models.Agent {
	t.Helper()
	a := e.newAgent(t, name, true, boolPtr(true))
	e.svc.polls.seen(a.ID, e.clock.Now())
	return a
}

func pingReq(target string) CreateRequest {
	return CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageSentinel}, Target: target}
}

// portReq is a port check from Sentinel: the only tool the allowlist fences.
func portReq(target string) CreateRequest {
	return CreateRequest{Tool: nettools.ToolTCP, Vantage: Vantage{Kind: models.VantageSentinel}, Target: target,
		Params: nettools.Params{Ports: "22"}}
}

func agentPing(a *models.Agent, target string) CreateRequest {
	return CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageAgent, AgentID: a.AgentID}, Target: target}
}

// refusal returns err as a *Refusal, failing the test when it is not one.
func refusal(t *testing.T, err error) *Refusal {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want a *Refusal", err)
	}
	return r
}

// insertRun writes a run straight to the table: a running ping from the
// Sentinel server against 10.0.0.200 unless mutate says otherwise.
func (e *env) insertRun(t *testing.T, mutate func(r *models.ToolRun)) *models.ToolRun {
	t.Helper()
	now := e.clock.Now()
	target := "10.0.0.200"
	r := &models.ToolRun{
		ID: uuid.New(), Tool: string(nettools.ToolPing), Status: models.ToolRunRunning, Username: "seed",
		VantageKind: models.VantageSentinel, VantageName: sentinelVantageName,
		Target: target, TargetIP: &target, Params: models.RawJSON(`{}`),
		CreatedAt: now, StartedAt: &now, Deadline: now.Add(2 * time.Minute),
	}
	if mutate != nil {
		mutate(r)
	}
	testdb.Must(t, e.db.Create(r).Error)
	return r
}

// reload reads a run back.
func (e *env) reload(t *testing.T, id uuid.UUID) *models.ToolRun {
	t.Helper()
	var r models.ToolRun
	testdb.Must(t, e.db.First(&r, "id = ?", id).Error)
	return &r
}

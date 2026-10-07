package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A run from the Sentinel server is inserted running, with its deadline,
// normalized parameters and snapshots, audited, and handed to launch with
// the checked address as TargetIP.
func TestDBCreateSentinelRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.user(t, false)
	now := e.clock.Now()

	run, err := e.svc.Create(ctx, who, pingReq("fileserver.example.org"))
	testdb.Must(t, err)

	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunRunning || got.StartedAt == nil || !got.StartedAt.Equal(now) ||
		!got.Deadline.Equal(now.Add(2*time.Minute)) || !got.CreatedAt.Equal(now) {
		t.Errorf("run = %+v, want running, started now, deadline now+2m", got)
	}
	if got.Target != "fileserver.example.org" || got.TargetIP == nil || *got.TargetIP != "10.0.0.5" ||
		got.VantageKind != models.VantageSentinel || got.VantageName != "Sentinel" || got.AgentUUID != nil ||
		got.AgentRef != nil || got.Username != who.Username || got.UserID == nil || *got.UserID != who.UserID {
		t.Errorf("run = %+v, want the typed target, 10.0.0.5, the Sentinel vantage and the requester", got)
	}
	var params nettools.Params
	testdb.Must(t, json.Unmarshal(got.Params, &params))
	if params.Count != 5 || params.IntervalMS != 1000 || params.TimeoutMS != 2000 || params.Size == nil || *params.Size != 56 {
		t.Errorf("params = %+v, want the ping defaults 5 / 1000 / 2000 / 56", params)
	}

	select {
	case call := <-e.launched:
		if call.run.ID != run.ID || call.spec.TargetIP != "10.0.0.5" || call.spec.Params.Count != 5 {
			t.Errorf("launch(%v, %+v), want the run and its normalized spec with TargetIP 10.0.0.5", call.run.ID, call.spec)
		}
	default:
		t.Fatal("launch was not called")
	}

	started := e.audit.byAction(models.ActionToolRunStarted)
	if len(started) != 1 || started[0].resourceID == nil || *started[0].resourceID != run.ID ||
		started[0].actor.UserID != who.UserID || started[0].actor.IP != "192.0.2.10" ||
		started[0].changes.Summary["tool"] != "ping" || started[0].changes.Summary["target"] != "fileserver.example.org" {
		t.Errorf("audit = %+v, want one tool_run_started for the run by the requester", started)
	}
}

// An agent run is queued, keeps the agent's readable id and name, gets a
// deadline that outlasts the pickup timeout, and wakes the agent's poll.
func TestDBCreateAgentRunQueues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	now := e.clock.Now()

	run, err := e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunQueued || got.StartedAt != nil || got.AgentUUID == nil || *got.AgentUUID != agent.ID ||
		got.AgentRef == nil || *got.AgentRef != agent.AgentID || got.VantageName != "file-server" ||
		!got.Deadline.Equal(now.Add(PickupTimeout+2*time.Minute)) {
		t.Errorf("run = %+v, want queued for file-server with deadline now+30s+2m", got)
	}
	select {
	case <-e.svc.wakeChan(agent.ID):
	default:
		t.Error("the agent's wake signal was not sent")
	}
	select {
	case <-e.launched:
		t.Error("an agent run was launched locally")
	default:
	}
}

func TestDBCreateRefusals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.user(t, false)
	offline := e.newAgent(t, "offline-box", true, boolPtr(true)) // never polled
	tooOld := e.newAgent(t, "old-box", true, nil)

	cases := []struct {
		name   string
		req    CreateRequest
		status int
		code   string
		msg    string
	}{
		{"parameters that cannot finish in time",
			CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5",
				Params: nettools.Params{Count: 100, IntervalMS: 5000}},
			http.StatusUnprocessableEntity, CodeInvalidParams, "at most 115 seconds"},
		{"an unknown tool",
			CreateRequest{Tool: "nmap", Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5"},
			http.StatusUnprocessableEntity, CodeInvalidParams, "tool"},
		{"an unknown vantage kind",
			CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: "moon"}, Target: "10.0.0.5"},
			http.StatusUnprocessableEntity, CodeInvalidParams, "vantage: kind must be sentinel or agent"},
		{"an unknown agent",
			agentPing(&models.Agent{AgentID: "agent_ffffffffff"}, "10.0.0.5"),
			http.StatusNotFound, CodeNotFound, "no such agent"},
		{"an agent that has not polled", agentPing(offline, "10.0.0.5"),
			http.StatusConflict, CodeVantageNotReady, "offline-box hasn't asked for jobs in the last minute (offline?)"},
		{"an agent too old for tools", agentPing(tooOld, "10.0.0.5"),
			http.StatusConflict, CodeVantageNotReady, "This agent's version can't run tools — update it"},
		{"an address outside the allowlist", pingReq("10.9.9.9"),
			http.StatusUnprocessableEntity, CodeTargetNotAllowed, "10.9.9.9 is not on the network tools allowlist"},
		{"a name outside the allowlist", pingReq("printer.example.org"),
			http.StatusUnprocessableEntity, CodeResolveFailed, "printer.example.org doesn't resolve from Sentinel — use its IP address"},
		{"an IPv6 address", pingReq("2001:db8::1"),
			http.StatusUnprocessableEntity, CodeIPv6Unsupported, "2001:db8::1 is an IPv6 address; S1 tools are IPv4 only"},
		{"a name with only IPv6 addresses", pingReq("v6only.example.org"),
			http.StatusUnprocessableEntity, CodeIPv6Unsupported, "v6only.example.org only has IPv6 addresses; S1 tools are IPv4 only"},
		// Review Focus 3: *.lab.example.org allows the name, but it resolves
		// to the cloud metadata address, which is always blocked.
		{"an allowed name that resolves to the metadata address", pingReq("meta.lab.example.org"),
			http.StatusUnprocessableEntity, CodeTargetNotAllowed, "meta.lab.example.org (169.254.169.254) is not on the network tools allowlist"},
	}
	for _, c := range cases {
		_, err := e.svc.Create(ctx, who, c.req)
		r := refusal(t, err)
		if r.Status != c.status || r.Code != c.code || !strings.Contains(r.Message, c.msg) {
			t.Errorf("%s: %d %s %q, want %d %s containing %q", c.name, r.Status, r.Code, r.Message, c.status, c.code, c.msg)
		}
	}
	var n int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d runs were inserted by refused creates", n)
	}
	// Only the two allowlist refusals are audited.
	refused := e.audit.byAction(models.ActionToolRunRefused)
	if len(refused) != 2 || refused[0].resourceID != nil || refused[0].resourceType != models.ResourceToolRun ||
		refused[1].changes.Summary["target_ip"] != "169.254.169.254" || refused[1].changes.Summary["code"] != CodeTargetNotAllowed {
		t.Errorf("refusal audit = %+v, want two tool_run_refused entries", refused)
	}

	// A name resolving to IPv6 and IPv4 uses the IPv4 address.
	run, err := e.svc.Create(ctx, who, pingReq("dual.lab.example.org"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "10.0.0.6" {
		t.Errorf("dual-stack name probed %v, want 10.0.0.6", run.TargetIP)
	}
}

// With the server switched off, Sentinel runs are refused; an empty
// allowlist refuses every target.
func TestDBCreateServerOffAndEmptyAllowlist(t *testing.T) {
	e := newEnv(t, []string{}...)
	ctx := context.Background()
	who := e.user(t, false)
	_, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeTargetNotAllowed {
		t.Errorf("empty allowlist: %s, want target_not_allowed", r.Code)
	}
	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: testAllowlist, ServerEnabled: false, RetentionDays: 30})
	testdb.Must(t, err)
	_, err = e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusConflict || r.Code != CodeVantageNotReady ||
		r.Message != "runs from the Sentinel server are switched off" {
		t.Errorf("server off: %+v", r)
	}
}

// A lookup through the vantage's own resolver contacts no target, so it
// needs no allowlist entry and has no target_ip; a named server is a target.
func TestDBCreateDNS(t *testing.T) {
	e := newEnv(t, "10.0.0.53")
	ctx := context.Background()
	who := e.user(t, false)
	dns := func(server string) CreateRequest {
		return CreateRequest{Tool: nettools.ToolDNS, Vantage: Vantage{Kind: models.VantageSentinel},
			Target: "example.com", Params: nettools.Params{RecordType: "mx", Server: server}}
	}

	run, err := e.svc.Create(ctx, who, dns(""))
	testdb.Must(t, err)
	if run.TargetIP != nil || run.Status != models.ToolRunRunning || !run.Deadline.Equal(e.clock.Now().Add(15*time.Second)) {
		t.Errorf("system-resolver lookup = %+v, want no target_ip and a 15 s deadline", run)
	}
	call := <-e.launched
	if call.spec.TargetIP != "" || call.spec.Params.RecordType != "MX" {
		t.Errorf("launched spec %+v, want no TargetIP and record type MX", call.spec)
	}

	_, err = e.svc.Create(ctx, who, dns("8.8.8.8"))
	if r := refusal(t, err); r.Code != CodeTargetNotAllowed || r.Message != "8.8.8.8 is not on the network tools allowlist" {
		t.Errorf("a server outside the allowlist: %+v", r)
	}

	run, err = e.svc.Create(ctx, who, dns("10.0.0.53:5353"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "10.0.0.53" {
		t.Errorf("named server: target_ip %v, want 10.0.0.53", run.TargetIP)
	}
	if call := <-e.launched; call.spec.TargetIP != "10.0.0.53" || call.spec.Params.Server != "10.0.0.53:5353" {
		t.Errorf("launched spec %+v, want TargetIP 10.0.0.53 and the server with its port", call.spec)
	}
}

// Each cap refuses the run that would pass it, with its own message.
func TestDBCreateCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ip := func(s string) *string { return &s }
	clear := func() { testdb.Exec(t, e.db, `DELETE FROM tool_runs`) }

	// Per user: 3 in progress.
	who := e.user(t, false)
	for i := 0; i < MaxActivePerUser; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.UserID = &who.UserID; r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i)) })
	}
	_, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusTooManyRequests || r.Code != CodeLimit || r.Message != "You already have 3 runs in progress" {
		t.Errorf("per user: %+v", r)
	}
	// A finished run does not count.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'done' WHERE id = (SELECT id FROM tool_runs LIMIT 1)`)
	_, err = e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	clear()

	// Per target address, across users.
	for i := 0; i < MaxActivePerTarget; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.TargetIP = ip("10.0.0.5") })
	}
	_, err = e.svc.Create(ctx, e.user(t, false), pingReq("fileserver.example.org"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "3 runs are already running against 10.0.0.5" {
		t.Errorf("per target: %+v", r)
	}
	clear()

	// Overall.
	for i := 0; i < MaxActiveOverall; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i)) })
	}
	_, err = e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "Sentinel is already running 10 tool runs" {
		t.Errorf("overall: %+v", r)
	}
	clear()

	// Per agent.
	agent := e.readyAgent(t, "file-server")
	for i := 0; i < MaxActivePerAgent; i++ {
		e.insertRun(t, func(r *models.ToolRun) {
			r.VantageKind, r.AgentUUID, r.Status = models.VantageAgent, &agent.ID, models.ToolRunQueued
			r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i))
		})
	}
	_, err = e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "file-server is already running 2 tool runs" {
		t.Errorf("per agent: %+v", r)
	}
}

// createConcurrently runs n creates at once (released together by a start
// barrier) and returns how many succeeded and the refusal codes.
func createConcurrently(t *testing.T, e *env, n int, who func(i int) Requester, req func(i int) CreateRequest) (int, map[string]int) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, codes := 0, map[string]int{}
	for i := 0; i < n; i++ {
		w, r := who(i), req(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.svc.Create(context.Background(), w, r)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if rf, isRefusal := err.(*Refusal); isRefusal {
				codes[rf.Code]++
			} else {
				codes["error: "+err.Error()]++
			}
		}()
	}
	close(start)
	wg.Wait()
	return ok, codes
}

// Concurrent creates cannot overshoot a cap: exactly the cap succeeds.
func TestDBCreateCapsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	target := func(i int) CreateRequest { return pingReq(fmt.Sprintf("10.0.0.%d", 10+i)) }

	one := e.user(t, false)
	ok, codes := createConcurrently(t, e, 10, func(int) Requester { return one }, target)
	if ok != MaxActivePerUser || codes[CodeLimit] != 10-MaxActivePerUser {
		t.Errorf("one user, 10 creates: %d succeeded, refusals %v; want exactly 3 and 7 limit", ok, codes)
	}
	testdb.Exec(t, e.db, `DELETE FROM tool_runs`)

	users := make([]Requester, 12)
	for i := range users {
		users[i] = e.user(t, false)
	}
	ok, codes = createConcurrently(t, e, 12, func(i int) Requester { return users[i] }, target)
	if ok != MaxActiveOverall || codes[CodeLimit] != 12-MaxActiveOverall {
		t.Errorf("12 users, 12 targets: %d succeeded, refusals %v; want exactly 10 and 2 limit", ok, codes)
	}
	var active int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_runs WHERE status IN ('queued','running')`).Scan(&active).Error)
	if active != MaxActiveOverall {
		t.Errorf("%d active runs in the table, want 10", active)
	}
}

// The per-target rate: after 20 runs against one address in a minute the
// next is refused until a token refills.
func TestDBCreateTargetRate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := 0; i < TargetRatePerMinute; i++ {
		e.svc.targets.allow("10.0.0.5", e.clock.Now())
	}
	_, err := e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusTooManyRequests || r.Code != CodeLimit ||
		r.Message != "too many runs against 10.0.0.5; wait a minute" {
		t.Errorf("21st run: %+v", r)
	}
	e.clock.Add(4 * time.Second)
	if _, err := e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5")); err != nil {
		t.Errorf("after a token refilled: %v", err)
	}
}

// Review Focus 3, by address: an always-blocked address is refused even when
// an allowlisted CIDR covers it, whether it is the probed host or a DNS
// lookup's named server; a neighbour in the same CIDR is allowed.
func TestDBCreateAlwaysBlockedInsideAllowedCIDR(t *testing.T) {
	e := newEnv(t, "169.254.0.0/16", "224.0.0.0/8")
	ctx := context.Background()
	who := e.user(t, false)
	dns := func(server string) CreateRequest {
		return CreateRequest{Tool: nettools.ToolDNS, Vantage: Vantage{Kind: models.VantageSentinel},
			Target: "example.com", Params: nettools.Params{Server: server}}
	}
	for _, c := range []struct {
		name string
		req  CreateRequest
		msg  string
	}{
		{"the metadata address", pingReq("169.254.169.254"), "169.254.169.254 is not on the network tools allowlist"},
		{"a multicast address", pingReq("224.0.0.1"), "224.0.0.1 is not on the network tools allowlist"},
		{"the metadata address as a DNS server", dns("169.254.169.254:53"), "169.254.169.254 is not on the network tools allowlist"},
	} {
		_, err := e.svc.Create(ctx, who, c.req)
		if r := refusal(t, err); r.Code != CodeTargetNotAllowed || r.Message != c.msg {
			t.Errorf("%s: %s %q, want target_not_allowed %q", c.name, r.Code, r.Message, c.msg)
		}
	}
	if n := len(e.audit.byAction(models.ActionToolRunRefused)); n != 3 {
		t.Errorf("%d refusals audited, want 3", n)
	}

	run, err := e.svc.Create(ctx, who, pingReq("169.254.1.1"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "169.254.1.1" {
		t.Errorf("neighbour: target_ip %v, want 169.254.1.1", run.TargetIP)
	}
}

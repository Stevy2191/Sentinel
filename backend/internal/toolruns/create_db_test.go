package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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
		{"a port check outside the allowlist", portReq("10.9.9.9"),
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
			http.StatusUnprocessableEntity, CodeTargetNotAllowed, "meta.lab.example.org (169.254.169.254) is an address network tools never contact"},
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
	// Only the two target refusals are audited.
	refused := e.audit.byAction(models.ActionToolRunRefused)
	if len(refused) != 2 || refused[0].resourceID != nil || refused[0].resourceType != models.ResourceToolRun ||
		refused[1].changes.Summary["target_ip"] != "169.254.169.254" || refused[1].changes.Summary["code"] != CodeTargetNotAllowed {
		t.Errorf("refusal audit = %+v, want two tool_run_refused entries", refused)
	}

	// The allowlist only fences port checks: a ping to an address outside it
	// runs.
	if run, err := e.svc.Create(ctx, who, pingReq("10.9.9.9")); err != nil || run.TargetIP == nil || *run.TargetIP != "10.9.9.9" {
		t.Errorf("ping outside the allowlist = %+v, %v; want a run against 10.9.9.9", run, err)
	}

	// A name resolving to IPv6 and IPv4 uses the IPv4 address.
	run, err := e.svc.Create(ctx, who, pingReq("dual.lab.example.org"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "10.0.0.6" {
		t.Errorf("dual-stack name probed %v, want 10.0.0.6", run.TargetIP)
	}
}

// With the server switched off, Sentinel runs are refused; an empty
// allowlist refuses every port check but no ping.
func TestDBCreateServerOffAndEmptyAllowlist(t *testing.T) {
	e := newEnv(t, []string{}...)
	ctx := context.Background()
	who := e.user(t, false)
	_, err := e.svc.Create(ctx, who, portReq("10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeTargetNotAllowed {
		t.Errorf("empty allowlist, port check: %s, want target_not_allowed", r.Code)
	}
	if _, err := e.svc.Create(ctx, who, pingReq("10.0.0.5")); err != nil {
		t.Errorf("empty allowlist, ping: %v, want a run", err)
	}
	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: testAllowlist, ServerEnabled: false, RetentionDays: 30})
	testdb.Must(t, err)
	_, err = e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusConflict || r.Code != CodeVantageNotReady ||
		r.Message != "runs from the Sentinel server are switched off" {
		t.Errorf("server off: %+v", r)
	}
}

// A lookup through the vantage's own resolver contacts no target and has no
// target_ip; a named server is the target, needs no allowlist entry (the
// allowlist only fences port checks) and is never an always-blocked address.
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

	run, err = e.svc.Create(ctx, who, dns("8.8.8.8"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "8.8.8.8" {
		t.Errorf("a server outside the allowlist: target_ip %v, want 8.8.8.8", run.TargetIP)
	}
	<-e.launched

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

// Review Focus 3, by address: an always-blocked address is refused for every
// tool, even when an allowlisted CIDR covers it, whether it is the probed
// host or a DNS lookup's named server; a neighbour in the same CIDR is
// allowed.
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
		{"the metadata address", pingReq("169.254.169.254"), "169.254.169.254 is an address network tools never contact"},
		{"a multicast address", pingReq("224.0.0.1"), "224.0.0.1 is an address network tools never contact"},
		{"the metadata address as a DNS server", dns("169.254.169.254:53"), "169.254.169.254 is an address network tools never contact"},
		{"a port check of the metadata address", portReq("169.254.169.254"), "169.254.169.254 is an address network tools never contact"},
	} {
		_, err := e.svc.Create(ctx, who, c.req)
		if r := refusal(t, err); r.Code != CodeTargetNotAllowed || r.Message != c.msg {
			t.Errorf("%s: %s %q, want target_not_allowed %q", c.name, r.Code, r.Message, c.msg)
		}
	}
	if n := len(e.audit.byAction(models.ActionToolRunRefused)); n != 4 {
		t.Errorf("%d refusals audited, want 4", n)
	}

	run, err := e.svc.Create(ctx, who, pingReq("169.254.1.1"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "169.254.1.1" {
		t.Errorf("neighbour: target_ip %v, want 169.254.1.1", run.TargetIP)
	}
}

// zone returns a resolver that answers from hosts and falls back to the
// fixture zone.
func zone(hosts map[string]string) func(ctx context.Context, host string) ([]net.IP, error) {
	return func(ctx context.Context, host string) ([]net.IP, error) {
		if ip, ok := hosts[host]; ok {
			return []net.IP{net.ParseIP(ip)}, nil
		}
		return fakeResolve(ctx, host)
	}
}

// A host-name entry (exact or wildcard) allows its name wherever it
// resolves: the run is created against the resolved address even though no
// address or CIDR entry covers it.
func TestDBCreateAllowedByHostNameOnly(t *testing.T) {
	e := newEnv(t, "10.0.0.0/24", "backup.example.org", "*.lab.example.org")
	e.svc.resolve = zone(map[string]string{"backup.example.org": "192.168.7.20", "nas.lab.example.org": "172.16.4.9"})
	ctx := context.Background()
	who := e.user(t, false)
	for _, c := range []struct{ name, ip string }{
		{"backup.example.org", "192.168.7.20"},
		{"nas.lab.example.org", "172.16.4.9"},
	} {
		run, err := e.svc.Create(ctx, who, portReq(c.name))
		if err != nil {
			t.Errorf("%s: %v, want allowed by its host-name entry", c.name, err)
			continue
		}
		got := e.reload(t, run.ID)
		if got.Target != c.name || got.TargetIP == nil || *got.TargetIP != c.ip {
			t.Errorf("%s: stored target %s / %v, want %s / %s", c.name, got.Target, got.TargetIP, c.name, c.ip)
		}
		if call := <-e.launched; call.spec.TargetIP != c.ip {
			t.Errorf("%s: launched against %q, want %s", c.name, call.spec.TargetIP, c.ip)
		}
	}
	if n := len(e.audit.byAction(models.ActionToolRunRefused)); n != 0 {
		t.Errorf("%d refusals audited, want none", n)
	}
}

// A name that resolves but matches no host-name entry, to an address no
// address or CIDR entry covers, is refused naming both.
func TestDBCreateResolvableNameOutsideTheAllowlist(t *testing.T) {
	e := newEnv(t)
	e.svc.resolve = zone(map[string]string{"intranet.example.com": "192.168.9.9"})
	_, err := e.svc.Create(context.Background(), e.user(t, false), portReq("intranet.example.com"))
	r := refusal(t, err)
	if r.Status != http.StatusUnprocessableEntity || r.Code != CodeTargetNotAllowed ||
		r.Message != "intranet.example.com (192.168.9.9) is not on the network tools allowlist" {
		t.Errorf("refusal = %+v", r)
	}
	var n int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&n).Error)
	refused := e.audit.byAction(models.ActionToolRunRefused)
	if n != 0 || len(refused) != 1 || refused[0].changes.Summary["target_ip"] != "192.168.9.9" {
		t.Errorf("%d runs inserted, refusal audit %+v; want none and one entry for 192.168.9.9", n, refused)
	}
}

// A run's clock starts when it is inserted, not when the request arrived:
// a slow lookup or a wait for the caps lock never eats into the tool's time
// limit (a maximum-length ping leaves only 5 s of slack), for runs on the
// server and for an agent's pickup window alike.
func TestDBCreateClockStartsAtInsert(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
		e.clock.Add(4 * time.Second) // a slow lookup
		return fakeResolve(ctx, host)
	}
	arrived := e.clock.Now()

	// Hold the caps lock, and move the clock while Create waits for it.
	tx := e.db.Begin()
	testdb.Must(t, tx.Error)
	defer tx.Rollback()
	testdb.Must(t, tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('tool_runs_caps'))`).Error)
	type result struct {
		run *models.ToolRun
		err error
	}
	done := make(chan result, 1)
	who := e.user(t, false)
	go func() {
		run, err := e.svc.Create(ctx, who, pingReq("fileserver.example.org"))
		done <- result{run, err}
	}()
	waitFor(t, "Create to wait for the caps lock", func() bool {
		var waiting int64
		testdb.Must(t, e.db.Raw(`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting).Error)
		return waiting == 1
	})
	e.clock.Add(3 * time.Second)
	testdb.Must(t, tx.Commit().Error)
	res := <-done
	testdb.Must(t, res.err)

	start := arrived.Add(7 * time.Second)
	got := e.reload(t, res.run.ID)
	if got.StartedAt == nil || !got.StartedAt.Equal(start) || !got.Deadline.Equal(start.Add(2*time.Minute)) ||
		!got.CreatedAt.Equal(start) || !res.run.Deadline.Equal(got.Deadline) {
		t.Errorf("run created %v, started %v, deadline %v (returned %v); want all from %v (+2m)",
			got.CreatedAt, got.StartedAt, got.Deadline, res.run.Deadline, start)
	}
	if call := <-e.launched; !call.run.Deadline.Equal(start.Add(2 * time.Minute)) {
		t.Errorf("launched with deadline %v, want %v", call.run.Deadline, start.Add(2*time.Minute))
	}

	// An agent run's pickup window and deadline count from its insert too.
	agent := e.readyAgent(t, "file-server")
	before := e.clock.Now()
	run, err := e.svc.Create(ctx, e.user(t, false), agentPing(agent, "fileserver.example.org"))
	testdb.Must(t, err)
	queued := before.Add(4 * time.Second)
	got = e.reload(t, run.ID)
	if !got.CreatedAt.Equal(queued) || !got.Deadline.Equal(queued.Add(PickupTimeout+2*time.Minute)) {
		t.Errorf("agent run created %v, deadline %v; want %v and +30s+2m", got.CreatedAt, got.Deadline, queued)
	}
}

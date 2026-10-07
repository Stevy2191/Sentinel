# Tools S1: rulings and deferred follow-ups

This records the decisions taken while planning and executing `2026-10-05-tools-s1-network-tools.md`, the limitations S1 ships with, and the work deliberately left for later. Done on `feature/tools-s1`, awaiting merge to `dev`.

## Rulings

Spec amendments made while planning (the spec's last section has the reasons):

- Old agents are detected by a missing flag, not by version: `agents.tools_local` is nullable, and NULL means the agent never reported it. CI stamps branch builds with the branch name, so versions cannot be compared.
- `tool_runs.target_ip` is TEXT, not INET; `tool_runs.agent_ref` snapshots the readable agent id, which the API returns as `agent_id`.
- The agent toggle has its own audited endpoint, `PUT /agents/:agent_id/tools`.
- `GET /tools/vantages` also returns `allowlist_empty`, because grant holders cannot read the settings but need the banner.
- The Linux agent's config file is `/etc/sentinel/agent.conf`.

Settled while planning (the spec left these open):

1. Ping must fit its deadline: `Normalize` refuses count × interval + timeout over 115 s ("count × interval is too long: at most 115 seconds").
2. Traceroute rows stop at the destination. When it is not reached they run to the last TTL that ever answered plus one, so the table shows where the path stops without 30 rows of timeouts.
3. Traceroute rounds start at least 1 s apart and end when every probe has answered or timed out.
4. `round_done` carries the cumulative hop statistics, so the browser never re-implements the maths.
5. Jitter is the mean absolute difference between consecutive reply RTTs; stdev is the population standard deviation.
6. TCP scans emit a `start` event with the number of ports, for the progress bar.
7. DNS sends no EDNS0 and retries a truncated UDP answer over TCP; the OPT record never appears in the sections.
8. Cancel wins: cancelling marks the run cancelled at once, and a later finish (local or agent) only fills a NULL summary. An agent posting events for a cancelled run gets `200 {"cancel": true}`.
9. The heartbeat field is `tools_local`; absent means NULL.
10. The run's JSON `agent_id` is `agent_ref`; the UUID column is not serialised.
11. The per-user rate limit is the existing `RateLimiter` (30 a minute, burst 10), so its 429 carries a plain string error; the frontend treats any 429 as a limit.
12. `RequireNetTools` answers 403 `forbidden` in the coded shape and passes the database-fresh admin flag to the handlers.
13. The HTTP server's `BaseContext` is cancelled just before `Shutdown`, so open SSE streams and long-polls end at once.
14. Finishes written by the sweeper are audited with the actor "system".
15. The run lifecycle lives in `internal/toolruns`, which imports `services`; `services` never imports it.
16. `GET /tools/runs/:id` returns `{run, events}`, `GET /tools/runs` sets `X-Total-Count`, and `GET /tools/vantages` returns `{vantages, allowlist_empty}`.
17. Hop names are looked up once per distinct address, in the background, with a 1 s timeout; a failed lookup emits nothing.
18. The Tools page URL carries the form (`/tools?tool=…&target=…&from=sentinel|agent:<agent_id>&params=…`); the entry points and "Run again" use it.

Contract changes made while planning:

- (T15) The `Agent` and `ManagedUser` fields live in `hooks/useAgents.ts` and `hooks/useUserManagement.ts`, where those types are; `types/index.ts` is only a barrel.
- (T6) `GET /users` sends `net_tools` to admin callers, because the Users page reads each user's grant there.
- (T15) `toolsQuery` takes `tool` and `target` optionally, since the server page's entry point sets only the vantage.
- (T15) `types/netTools.ts` adds `HopProbe`, the `hop` event's data.
- (T17) The result panels' view models live in `utils/netToolViews.ts`, and `utils/colors.ts` gained `chartChrome` for the ping chart.
- (T16) The DNS record type and the port list sit in the form's main row; only the numeric options are in the collapsed Options section. Run stays enabled with an empty allowlist, because a lookup through the vantage's own resolver needs no entry.

From the plan's "Contract changes made while writing the tasks":

- `nettools.Runner.Run` summaries: a nil summary for any error other than the context's (and for `dns` also when the context ends); ping, traceroute and tcp return a typed partial summary when the context ends. Summaries are values, not pointers. `emit` is called from one goroutine per run. Consumers (T9, T13) treat a nil summary as "no summary".
- Traceroute `hop` events can, in round 1, include TTLs past the destination; the browser builds the MTR table only from `round_done` (and the final summary).
- `Normalize` canonicalises `Params.Server` (`ip` or `ip:port`) and sets `Ports` to `"common"` when empty. For a named DNS server, `Create` sets `TargetIP` to the server's address; the Runner uses it with the port from `Params.Server`.
- `ParamError.Field` is the JSON name (`count`, `interval_ms`, `timeout_ms`, `size`, `max_hops`, `rounds`, `record_type`, `server`, `ports`, `tool`, `target`).
- `internal/toolruns` also exports `(*Service).Subscribe(runID uuid.UUID) *stream.Subscription` and `ErrBadFinishStatus`; `service.go` is split into `service.go`, `create.go` and `vantages.go`. The batch insert `insertEvents` is defined in T9 and reused by T10. An agent's finish error text is clipped to 500 bytes. `AgentEvents` accepts an empty batch (the agent polls for a cancel every 250 ms).
- `internal/stream` also exports `ErrNoFlush` (returned by `NewWriter`).
- `SetNetTools` on an unknown user wraps `gorm.ErrRecordNotFound`.
- API: the agent job routes answer in the standard `{"success": true, "data": …}` envelope (`data` is a `Job`, `{"cancel": bool}` or `{"ok": true}`); a too-large agent post or too much output answers 413 with code `limit`; internal errors use the existing `respondInternal`. On connecting to an active run, the SSE stream sends a `status` frame with the current status after the replay. `RegisterToolRoutes` takes the hub (to subscribe before replaying) and the audit recorder (to audit a `RequireNetTools` refusal of `POST /tools/runs` as `tool_run_refused`).
- Frontend: `utils/netTools.ts` exports extra helpers (form defaults and limits, `paramError`, `cleanParams`, `vantageKey`/`vantageFromKey`, `monitorHost`, `knownHosts`, `runTargetText`, `runStatusText`, status colour maps, `agentToolsStateText`, `allowlistErrors`, `lineErrors`, `formatMs`, `formatLoss`). `/tools/runs/:id` is routed in T17.
- Additive during execution (T3): `HopProbe` gained `Error string json:"error,omitempty"` (TS `HopProbe.error?: string`); `AgentFinish.Status` accepts `timed_out` (T10).

Rulings recorded while executing (each with its cost if wrong):

- (pre-flight) T12's respondJobError also maps toolruns.ErrBadFinishStatus → 400 invalid_params (the handler's own pre-check stays) — defence in depth, matches the contract — cost if wrong: one dead case.
- (pre-flight) T12 drops its own 2000-byte clip of the agent's finish error; the service's 500-byte clip (T10) is the single rule — one rule, one place — cost if wrong: none (500 is stricter).
- (pre-flight) malformed agent event batches: the handler's 400 for the whole batch (T12) stands; the service's silent drop (T10) stays as defence in depth — cost if wrong: an agent bug loses a batch instead of a few events.
- (pre-flight) an agent run that reaches its deadline is reported as timed_out: T10's AgentFinish accepts done|failed|refused|timed_out, T13's agent sends status "timed_out" when its tool ctx hits the job deadline — history must read the same for Sentinel and agent runs — cost if wrong: small, two tasks touch one switch each.
- (pre-flight) T6 uses time.Now().UTC() for updated_at (Global Constraints) — cost: none.
- (pre-flight) -race needs cgo; race runs use the Debian image: GO_IMAGE=golang:1.26 $S/bin/go-docker test -race … (and GO_IMAGE=golang:1.26 for go-test-db runs needing -race). T7's TestHubConcurrentUse must also assert something after the concurrent phase (a fresh subscriber receives a publish; no subscriptions leak) — a test that asserts nothing is a rubric defect — cost: a few lines.
- (pre-flight) T12's commit includes any file it modifies (dispatch.go if Step 6 changes it) — cost: none.
- (pre-flight) T18: Escape and backdrop close of EditServerAgentModal are disabled while the tools toggle save is in flight, as Cancel/Save are — cost: none.
- (pre-flight) T15/T16: every place showing the too-old agent text uses the verbatim string "This agent's version can't run tools — update it" (Global Constraints) — cost: none.
- (pre-flight) T12 adds one API-level long-poll wake test (jobs/next with a 2 s wait returns 200 with the job shortly after a run is queued for that agent from another goroutine) — spec Testing asks for it — cost: one test.
- (pre-flight) DB tests may start with the plan's env/rig helpers instead of literally `db := testdb.Open(t)`, as long as they call testdb.Open and are named TestDB… — the constraint's intent is skip-without-DB — cost: none.
- (pre-flight) "Contract change N" references inside tasks point at the part writers' local numbered lists, now folded (by topic) into the shared contract's "Contract changes made while writing the tasks"; each task's own Files list wins over the plan's File structure section — tell implementers so — cost: none.
- (T2) fix the plan-mandated readLoop slicing (clamp n to len(buf), via a small pure helper with a unit test that feeds n > len(buf)) — a packet from outside must never crash the process; the plan text was simply wrong about x/net — cost if wrong: none.
- (T3) hop_name TTL — lookups start at the end of each round, only for addresses in the current rows, carrying that row's TTL (also drops lookups for addresses seen only past the end); test names the destination and asserts every hop_name.ttl <= HopCount — the spec defines hop_name by TTL and the plan's code got it wrong in most runs — cost if wrong: none.
- (T3) traceroute probe errors other than ErrNoReply are surfaced: HopProbe gains `Error string json:"error,omitempty"` (additive contract change; TS HopProbe gains `error?: string` in Task 15), the probe still counts as unanswered in the stats; and when every probe of round 1 failed with such an error (nothing answered, no plain timeouts) the run returns the first error so it ends failed with a reason — a diagnostic tool must not show local send failures as packet loss — cost if wrong: one optional JSON field.
- (T3) a fully failed trace (every round-1 probe errored) emits no round_done; the run's error explains it — no misleading 100%-loss table next to it — cost if wrong: one line.
- (T7) a Sonnet-written commit's 'Co-Authored-By: Claude Sonnet 5.5' trailer stands (accurate attribution) — cost: none.
- (T8) carried into Task 9 (which wires the launcher that honours run.Deadline): create.go takes a fresh s.now() for StartedAt/Deadline after resolution/lock — otherwise a max-length ping (115 s) can be cut by a slow resolve — cost: two lines.
- (T8) carried into Task 9 as two extra Create tests (a name allowed only by a host-name entry resolving outside every CIDR; a resolvable name outside both kinds → target_not_allowed "name (ip)") — security-relevant coverage — cost: two tests.
- (T12) the shutdown BaseContext cancel stays global (every in-flight request ends at shutdown, not only streams/long-polls) — reviewer verified backups remove their .partial file, restores roll back (--single-transaction), a cancelled safety backup prevents psql from starting; it is the same path as a client closing the tab — cost if wrong: a backup/restore running at the moment Sentinel stops fails and must be rerun (previously it had up to 30 s).

## Fixed in the final whole-branch review

(filled in after the final review)

## Known limitations

- IPv4 only: a target that resolves only to IPv6 is refused (`ipv6_unsupported`). DNS lookups still return AAAA records.
- The limits are fixed in code: 30 runs a minute per user (burst 10), 20 a minute per target address, and at most 3 active per user, 3 per target, 10 overall and 2 per agent.
- The per-target rate buckets and the agents' last-poll times live in memory. A restart resets them, and agents show as offline until their next poll, within 25 s.
- Rate-limit refusals (429) are not audited, by design, so they cannot flood the log.
- Traceroute needs raw ICMP (root or NET_RAW, or the Windows ICMP API). The unprivileged ICMP socket serves ping only. Hosts or firewalls that drop ICMP Time Exceeded show timeouts past the first hop, which is the honest result.
- DNS has no EDNS0 and no DNSSEC. The system resolver is the first IPv4 nameserver only: no search domains and no fallback to a second server.
- The port check is a TCP connect only. "Filtered" covers both no answer and host unreachable.
- A host-name allowlist entry is matched against the typed name, and the probe goes to the address resolved when the run was created. A later DNS change does not move a run.
- The Tools page follows only the run started from it. Switching tool closes the stream, but the run carries on and appears in the history.
- Every grant holder sees every user's runs (a team troubleshooting tool, as the spec decided).
- The Windows ICMP, system-resolver and TCP-refused paths are vet- and cross-compile-checked only; they have not run on a Windows machine (owner checks in `STATUS.md`).

## Deferred follow-ups

Out of scope for S1 (spec):

- IPv6 targets
- subnet sweeps
- banner and version detection, and nmap (S2)
- an audit-log screen
- tunable limits
- UDP probes and UDP traceroute
- scheduled or recurring tool runs
- alerts raised from tool runs
- moving the existing monitors onto `nettools`

Found while planning:

- Network phase 6 (live site maps) should reuse `internal/stream` (the SSE writer and hub) rather than build a second push channel.

Found while executing and left for later (minor review findings, by area; items marked "final-review triage" are settled in the section above once it is filled):

### nettools and stream (`backend/internal/nettools`, `backend/internal/stream`)

- (T1) Ports string length unbounded; ParsePorts cost O(parts×range) (params.go:187, ports.go:56) — cap Ports length (~4-8 KB) or count iterations
- (T1) ports_test.go:67 t.Errorf then index nil slice → use t.Fatalf
- (T1) params_test "ping size 0" doesn't assert 0 kept; check order tool→target not pinned
- (T1) single-label wildcard (*.lan, *.local) refused by design — confirm with owner
- (T2) readLoop no back-off on a persistent read error (icmp_unix.go:86-98)
- (T2) Windows IcmpSendEcho 0-return with IP_STATUS 11000-11050 other than timeout gives misleading FormatMessage text (icmp_windows.go:154-160) — route through status wording
- (T2) ErrICMPUnavailable hides real cause (EMFILE etc.)
- (T2) random-id collision between raw probers in one process (~1/65534 per pair) — package-level atomic id counter
- (T2) Windows Close is a no-op; Echo after Close still sends (unix fails)
- (T2) payload fill duplicated icmp.go:115-119 / icmp_windows.go:128-132 (plan-mandated) — fillPayload helper
- (T2) raw-path dispatch untested in CI (non-root containers)
- (T2) with IP options the last 20 bytes of the clamped slice are stale buffer contents (clear(buf) before read would remove)
- (T3) Reached can disagree with the table when a router's unreachable ends the path below the TTL where the destination echoed (traceroute.go:117-118,170-180)
- (T3) round-1 hop events past the end of the path (allowed by the brief) — consumers must ignore them (frontend builds from round_done)
- (T3) mid-round cancel can leave rows with Sent 0 showing 0% loss (traceroute.go:108-110,233-235)
- (T3) test gaps — recorder reused in TestTracerouteUnreachableEndsThePath; TestPingErrors covers only 2 codes; no mid-round cancel test; no completion-order test
- (T3) a name emitted in round 1 keeps its TTL if a later round lowers the path end (frontend matches by address)
- (T4) failed TCP retry reported as a silent server / "asking X: EOF" (dns.go:132-134,157-158) — say "answered over UDP with TC but not over TCP" (most useful)
- (T4) replies with empty question section ignored → timeout instead of RCODE (dns.go:235-236)
- (T4) SVCB/HTTPS bodies print "" (dns.go:289)
- (T4) malformed known-type record fails whole reply (acceptable)
- (T4) record-type list duplicated dnsTypes vs DNSRecordTypes — add a test that every DNSRecordTypes entry is in dnsTypes
- (T4) ruling 7 untested (no OPT in query; OPT filter); case-insensitive match not exercised; two error paths untested; TXT quoting differs from dig; Windows picks first up adapter, not by metric
- (T5) Windows SYN-retry after RST may report closed ports as filtered at 1.5 s timeout (tcp_refused_windows.go, tcp.go:79-80) — verify on a Windows agent (owner check)
- (T5) runner named-server test can't distinguish TargetIP from Params.Server host (runner_test.go:101,109)
- (T5) no Runner test for dns nil summary on ctx end; partial-summary test checks only type
- (T5) partial() decides ctx-end from error type, not ctx.Err() (runner.go:75-80)
- (T5) IPv4 check written three times (runner.go:103, params.go:170, dns_system_unix.go:36)
- (T7) ID/Event/comment text not sanitised for CR/LF; bare CR in Data (sse.go:58-66,73-74) — safe today; document or normalise for phase 6
- (T7) untested NewHub(<1) clamp and Send write-error path (sse.go:78-80)
- (T7) undocumented: Message.Data shared by reference; Writer not concurrency-safe

### Run service, settings and data (`backend/internal/toolruns`, `backend/internal/services`, auth)

- (T6) MFA paths do GetUserByID + full-row Save (auth_service.go:251,275,310,330) — a concurrent SetNetTools(false) can be undone (pre-existing pattern; now affects the grant) — scope those writes to their columns (final review triage)
- (T6) NULL-status test can't exercise the CHECK's IS NOT NULL (column NOT NULL fires first)
- (T6) report TDD evidence summarised, not quoted
- (T8) IPv4-mapped literal ::ffff:a.b.c.d accepted (create.go:175-178) — no security impact; convention says refuse ':'
- (T8) SaveSettings three non-atomic writes (settings.go:75-81)
- (T8) SaveSettings checks retention first (entries unreported when both bad); entries lower-cased before parse so EntryError.Entry isn't as typed (settings.go:62-69)
- (T8) corrupt stored allowlist logs on every LoadSettings (settings.go:41)
- (T8) Vantages/Create load full agent rows incl. server_token (vantages.go:76, create.go:40) — Select needed columns
- (T8) audit assertions thin (started entry fields; 429 not audited untested); target_ip string vs *string in refused vs started entries
- (T9) failed flush loses its whole batch (local.go:64-68); a jsonb-rejected \u0000 (possible in DNS names if dnsmessage passes NUL) fails a batch / blocks finish's summary cast (recorder.go:137) — requeue failed batch; retry finish with NULL summary
- (T9) ticker flush has no timeout (local.go:116) — bound with finalWriteTimeout
- (T9) no log when an event can't be encoded (local.go:41)
- (T9) no test of a local run hitting its deadline / ctx deadline == run.Deadline
- (T9) List limit/offset rules untested (query.go:73-84)
- (T9) no recover in runLocal — a tool panic crashes the server (local.go:122)
- (T9) events after `end` on cancel are inherent (cancel wins) → SSE/UI should re-fetch the run after `end` (carry to Task 17 view: final counts from GET run)
- (T9) tool errors not wrapping ctx.Err() at deadline map to failed not timed_out — guard on ctx.Err()
- (T10) pickup-timeout sweep can fail a run an agent claimed in the same ms (sweeper.go:62-66 + recorder.go:139-141) — robust fix: pickup-timeout finish moves only queued runs (from-statuses arg to finish)
- (T10) AgentEvents check-then-insert window — a batch can land on a just-cancelled run (benign) — guard the insert CTE on status = 'running'
- (T10) params decode failure after claim strands the run until deadline+15 s (dispatch.go:91-94) — finish failed instead
- (T10) prune at shutdown logs a spurious context canceled (sweeper.go:114-115)
- (T10) only ping's claim deadline asserted — table test over the four tools
- (T10) InterruptStale runs in `go toolRuns.Start` concurrently with server start — a run created in the first instant could be interrupted (very unlikely; filter on created_at < construction time)

### API (`backend/internal/api`)

- (T11) SSE handler subscribes via a separately passed hub while Service.Subscribe exists unused (plan-mandated contract change) — risk only if wired with different hubs; Task 12 wires one hub
- (T11) log noise on client disconnect mid-replay (tool_runs_handler.go:246)
- (T11) coverage gaps — grant holder cancelling own run at API level; second-replay-with-new-events path; user_id/offset parsing; plain-string 429 body
- (T11) mine=1 overrides user_id; mine=true accepted; registerToolRoutes writes h.keepAlive in place
- (T12) PATCH /users/:id/net-tools on a user deleted between read and write → 500 not 404 (net_tools_admin_handler.go:108-112) — map gorm.ErrRecordNotFound
- (T12) events/finish posts in flight at shutdown logged as internal errors (agent_jobs_handler.go:104,131) — skip logging when ctx done
- (T12) main.go comment should say every in-flight request is cancelled at shutdown (main.go:489-491)
- (T12) no-op grant/toggle changes still audited (net_tools_admin_handler.go:113,139)
- (T12) backup_handler.go:27-29 comment wrongly says the dump is bounded only by the service's timeout

### Agent (`backend/cmd/agent`)

- (T13) agent stopping mid-run sends no finish → run shows running until deadline+15 s then timed_out (jobs.go:264,228,240,316-323) — send a finish on a short background ctx ("the agent stopped")
- (T13) clock skew ≥15 s ahead makes the agent refuse every DNS job ("deadline passed") with no hint (jobs.go:164-165) — mention clock in the reason or estimate skew from Date header
- (T13) 413 then finish always 409 → logged as failure (jobs.go:234,323) — wording/log level
- (T13) test gaps — 413 path; unknown tool refusal; posts stop after 409; events kept & re-posted after a 5xx
- (T13) redundant TargetIP trim (jobs.go:163)

### Install scripts

- (T14) ENABLE_TOOLS not validated in the Linux heredoc (true|false case would catch typos/injection)
- (T14) Windows TOOLS_ALLOWED_TARGETS not validated (agent rejects bad values anyway)
- (T14) weak test assertions satisfied by header comments; rejection path untested

### Frontend

- (T15) portsError refuses empty list items the backend accepts (utils/netTools.ts:343-345)
- (T15) validServer accepts leading-zero octets Go rejects (utils/netTools.ts:330-337)
- (T15) settings save can be overwritten by an in-flight GET (useNetTools.ts:171-178)
- (T15) unparseable end frame leaves page on Running (useRunStream.ts:73-80) — set error
- (T16) duplicate history fetch when a finished run is on screen (NetworkTools.tsx:84-86)
- (T16) run started just before a tool switch vanishes until the history poll (by design)
- (T16) "No runs yet." shown when filters match nothing / under a load error (RunHistory.tsx:100)
- (T17) detail page's single reload on `end` usually precedes an agent's late events/summary after a cancel, and `cancelled` beats `stored` after a self-cancel (ToolRunDetail.tsx:49-52,73) — prefer stored once final; optional second reload
- (T17) traceroute note says "destination did not answer" for a stopped run (TracerouteResult.tsx:27-28)
- (T17) hop count from view.hops.length not summary.hop_count (TracerouteResult.tsx:27)
- (T17) `?? 5` duplicated default vs DEFAULT_PARAMS (TracerouteResult.tsx:16)
- (T17) Cancel button hidden after a stream error (decision 3 trade-off) — user cancels via Open run
- (T18) EditServerAgentModal re-seeds an open modal on every 15 s agent poll (pre-existing; EditServerAgentModal.tsx:197-206) — the new "Allow network tools" tick is reset before Save unless saved within the poll window; re-seed only on open
- (T18) header X close button not gated on saving (EditServerAgentModal.tsx:168-174) — completes the Esc/backdrop ruling
- (T18) NetToolsSettings load can flash the error box for a frame (436-444)
- (T18) allowlist text typed during a save is overwritten (NetToolsSettings.tsx:446-468,484-491)
- (T18) ?tab= matches prototype keys (Settings.tsx:987) — use tabs.includes
- (T18) AdminUsers toolsBusy holds one id (591,613-627)
- (T18) partial failure message/refresh in agent modal (281-289)
- (T18) fragment indentation nit (ServerDetail.tsx:880-897, DeviceDetail.tsx:1105-1131)


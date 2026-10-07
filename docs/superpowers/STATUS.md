# Where Sentinel stands

Updated 2026-10-07. Read this first when picking the work up on another machine.

## Branches

- **`main`** — what runs in production. Behind `dev`: none of the network-monitoring work below is on `main` yet.
- **`dev`** — the integration branch, pushed to GitHub. Everything below lives here. **Work from `dev`** (or a `feature/*` branch made from it).
- Feature work happens on `feature/*` branches in a git worktree and is fast-forwarded into `dev` when done and tested. Merging to `main` (production) is a separate, deliberate step the owner asks for.

## Network monitoring roadmap

Roadmap: `docs/superpowers/specs/2026-09-28-network-monitoring-roadmap.md`. Each phase has a spec in `docs/superpowers/specs/` and a plan in `docs/superpowers/plans/`.

| Phase | What | State |
|---|---|---|
| 0 | Groundwork: TimescaleDB, sites, sharing | Done (`dev`) |
| 1 | SNMP foundation: credentials, devices, inventory, polling | Done (`dev`); follow-ups in `plans/2026-09-29-network-phase1-followups.md` |
| 2 | Ports and bandwidth: metrics store, faceplate, port events and alerts, roles, site traffic | Done (`dev`); follow-ups in `plans/2026-09-30-network-phase2-followups.md` |
| — | UPS monitoring (UPS-MIB readings, Power panel, on-battery/low-battery/high-load alerts) | Done (`dev`); spec `specs/2026-10-01-ups-monitoring-design.md` |
| 3 | MIB library and custom metrics: upload/browse/test-walk MIBs, profiles, Cisco switch health, Health section, metric-rule alerts | Done (`dev`, 2026-10-02); follow-ups in `plans/2026-10-02-network-phase3-followups.md` |
| 4 | Custom dashboards: grid editor, 11 widget types (incl. site power), sharing, fullscreen wall display, admin public links | Done (`dev`, 2026-10-03); follow-ups in `plans/2026-10-02-network-phase4-followups.md` |
| 5 | Metric reports: a Metrics report type (ports, port roles, devices, sites), exact 95th percentiles, totals, previous-period comparison, scheduled and shared like the other reports | Done (`feature/network-phase5`, awaiting merge to `dev`); follow-ups in `plans/2026-10-03-network-phase5-followups.md` |
| 6 | Live site maps (LLDP, SSE push) — needs phase 4 | Not started; unblocked by phase 4 |
| 7 | UniFi controller source | **Next** |

Phase 7 is next; phase 6 is unblocked (it needed phase 4, which is done) and can follow. Where both sit against the tools and security track below is the owner's call.

## Tools and security track

Roadmap: `docs/superpowers/specs/2026-10-04-tools-and-security-roadmap.md` (agreed 2026-10-04; S1 designed in `specs/2026-10-05-tools-s1-network-tools-design.md` and built). Phases are numbered S1–S6 to stay distinct from the network phases. Every phase is admin-only (or a granted permission), rate limited and audited; port checks (and later scans) are limited to an allowlist of target subnets, while ping, traceroute and DNS may reach any address that is not always blocked.

| Phase | What | State |
|---|---|---|
| S1 | Network tools: ping, traceroute (MTR-style), DNS lookup, port check/quick scan — from Sentinel or any server agent, streamed live | Done (`feature/tools-s1`, awaiting merge to `dev`); follow-ups in `plans/2026-10-05-tools-s1-followups.md` |
| S2 | nmap: bundled scan profiles (quick, full TCP, service/version, OS, NSE categories), guarded raw mode, parsed results, history and diff — needs S1 | Not started; unblocked by S1 |
| S3 | Attack surface: scheduled scans, open ports and exposed services per host, change alerts, CVE-based vulnerability overview, inside (agent) vs outside view — needs S2 | Not started |
| S4 | Flow analysis: NetFlow/IPFIX/sFlow collector, top talkers, protocols, who-talks-to-whom per site and interface — needs network phase 2 only | Not started |
| S5 | Per-server traffic: connections and traffic per process from the agents, live on the server page — needs S1 | Not started; unblocked by S1 |
| S6 | Packet capture: bounded on-demand capture, protocol breakdown, .pcap download — needs S1; last | Not started; unblocked by S1 |

S1 → S2 → S3 is one chain; S4 can go at any point; S5 follows S1; S6 is last. Where the track sits against network phases 6 and 7 is the owner's call.

## To be checked by the owner

Tools S1 (after updating the work install from `dev`; Claude had no browser, so none of these has been run in one):
1. With the allowlist empty, a ping to any address (e.g. 8.8.8.8) runs, while the Ports tab shows "No port-check targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools" and a port check is refused with "… is not on the network tools allowlist". A ping to 169.254.169.254 is refused with "… is an address network tools never contact".
2. In Settings → Network tools, add the office subnet (e.g. a 10.x /16). A line such as 10.0.0.0/7 is named with "too broad: use /8 or narrower" and nothing is saved.
3. From the Sentinel server: ping a host on the list (replies arrive about one a second and the chart and tiles fill in), traceroute it, look up a name through the system resolver and through a named DNS server on the list, and check ports with "common".
4. Enable a Linux agent: re-run its install with "Enable network tools on this server" ticked (or add `ENABLE_TOOLS=true` to `/etc/sentinel/agent.conf` and restart it), then Edit → Allow network tools. Within a minute it can be chosen under "Run from"; run each tool from it.
5. A Windows agent, if one is to hand: the same as 4, and its traceroute shows the hops.
6. Through Caddy, watch a 10-round traceroute: the table updates round by round, not all at the end.
7. Cancel a run mid-way, once from the Sentinel server and once from an agent: it shows Cancelled within a second, and the agent stops.
8. Stop an agent's service in the middle of a traceroute: by the deadline plus 15 s the run shows Timed out and the page no longer says Running.
9. Grant a non-admin "Network tools" on the Users page: they see Network Tools in the menu, can run tools and see everyone's runs, cannot open Settings → Network tools, and get no Cancel on someone else's running run.
10. An agent that has not been updated says "This agent's version can't run tools — update it" in its Edit form and is disabled under "Run from".
11. `GET /api/v1/audit-log?resource_type=tool_run` lists `tool_run_started` and `tool_run_finished` entries, and a `tool_run_refused` for an off-list target.
12. Windows agent, ping and traceroute (it uses IcmpSendEcho): replies arrive, and a traceroute shows the intermediate hops (time-exceeded replies), not only the destination.
13. Windows agent, DNS lookup with no named server: it goes through the system resolver, which picks the adapter's DNS server (the first up adapter, not by metric); the answer matches `nslookup` on that machine.
14. Windows agent, TCP scan of a host with one closed port and one open port: the closed port shows `closed`, not `filtered` (the refused-connection path retries after an RST; a 1.5 s timeout could misreport it).
15. In the agent's Edit form, ticking "Allow network tools" survives until Save: wait over 15 s (the agent list re-polls) before pressing Save, and the tick must still be there. A pre-existing re-seed of the open modal on each poll is being triaged and may reset it.

Phase 5 (after updating the work install from `dev`; Claude had no browser, so none of these has been run in one, and 6-11 come from the code reviews):
1. Create a Metrics report for the two WAN ports, period last month; the PDF shows 95th in/out, the billable 95th and the total moved, and the numbers look right against the ISP's portal.
2. Create a Metrics report for a whole site with a schedule; the email arrives with the "ports · billable 95th · moved" summary line.
3. A Port roles scope picks up a port given the Uplink role after the report was created (run it again).
4. Create a report for a scope larger than 500 ports (if one exists) or check the wizard's size line on the biggest site; the PDF says how many were left out.
5. The wizard: Type → Scope → Metrics → Period; Uptime and Incident reports still build exactly as before.
6. In the wizard on a slow network, the size line shows "Sizing the scope…" and the port picker never shows another device's ports.
7. A member with access to one site sees only that site's devices and ports in the pickers.
8. A hidden or removed subject (e.g. a port deleted after it was picked) shows the red "A chosen port, device or site is not available" message, and Next stays disabled.
9. On the Metrics step, no more than 10 metrics can be chosen; changing the scope before editing the list brings in the new scope's defaults, and after editing keeps your list (naming any metric the new scope dropped); the PDF follows the chosen order.
10. Leaving the wizard while a report generates does not pull you back to it when the report finishes.
11. In the browser's devtools, `GET /reports` carries `scope_data` and the share-link response does not.

Phase 4 (after updating the work install from `dev`; the browser and visual checks were not done by Claude, so 5-7 are yours):
1. Create a site dashboard with the standard widgets; every widget shows data within a minute.
2. Leave it fullscreen on a screen for a day; it is still updating the next morning (no Stale markers).
3. As an admin, create a public link, open it in a private window, then turn the link off; the page says the link is no longer available.
4. Stop the backend for a few minutes; the widgets show Stale, then recover on their own.
5. In the editor, add, drag and resize widgets and save. Opening the editor on a dashboard without changes must not show "Discard your changes?" on Cancel.
6. Open a public link signed out in a private window, on a phone-width screen as well. Widgets stack and no link into Sentinel is clickable.
7. Every widget type renders on a site dashboard, and the settings forms save without errors.

Phase 3 (not yet confirmed on real equipment; verified against the SNMP simulator and in the sandbox):
1. MIB library lists 14 built-in modules as Ready; upload the 8 Cisco files linked on that page (needed only for names in the browser).
2. MIB browser → test-walk a 3850 (e.g. the ENTITY-SENSOR or ENVMON tables) shows real rows.
3. A 3850 and a 4500-X show the Health section within two minutes: CPU per switch, temperatures with sensible values, fans and power supplies with states.
4. Copy the starter profile, add a metric from the browser with a preview, save, and see it on the device page (a copy starts with no match prefixes — attach it or add prefixes).

## How to resume

1. `git fetch && git checkout dev` (the repo's `CLAUDE.md` has the commands, conventions and gotchas).
2. Check this file's "Next" row and the latest follow-ups doc.
3. For a new phase: brainstorm → write the spec → write the plan → execute task by task with review, on a `feature/*` branch made from `dev`.

## Owner-specific environment (home server only)

The owner's home server also has a sandbox instance (`/srv/docker/sentinel-dev`, ports 3200/3201) and the live instance; neither exists at work. At work, use `backend/scripts/test-db.sh` (Docker) and the simulator in `deploy/snmpsim` for testing.

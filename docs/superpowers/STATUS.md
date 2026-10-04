# Where Sentinel stands

Updated 2026-10-04. Read this first when picking the work up on another machine.

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
| 5 | Metric reports: a Metrics report type (ports, port roles, devices, sites), exact 95th percentiles, totals, previous-period comparison | **In progress** (`feature/network-phase5`); spec `specs/2026-10-03-network-phase5-metric-reports-design.md`, plan `plans/2026-10-03-network-phase5-metric-reports.md` |
| 6 | Live site maps (LLDP, SSE push) — needs phase 4 | Not started; unblocked by phase 4 |
| 7 | UniFi controller source | Not started |

Phases 6 and 7 can go in either order after phase 5; 6 needed 4, which is done.

## Tools and security track

Roadmap: `docs/superpowers/specs/2026-10-04-tools-and-security-roadmap.md` (agreed 2026-10-04; nothing designed yet). Phases are numbered S1–S6 to stay distinct from the network phases. Every phase is admin-only (or a granted permission), limited to an allowlist of target subnets, rate limited and audited.

| Phase | What | State |
|---|---|---|
| S1 | Network tools: ping, traceroute (MTR-style), DNS lookup, port check/quick scan — from Sentinel or any server agent, streamed live | Not started |
| S2 | nmap: bundled scan profiles (quick, full TCP, service/version, OS, NSE categories), guarded raw mode, parsed results, history and diff — needs S1 | Not started |
| S3 | Attack surface: scheduled scans, open ports and exposed services per host, change alerts, CVE-based vulnerability overview, inside (agent) vs outside view — needs S2 | Not started |
| S4 | Flow analysis: NetFlow/IPFIX/sFlow collector, top talkers, protocols, who-talks-to-whom per site and interface — needs network phase 2 only | Not started |
| S5 | Per-server traffic: connections and traffic per process from the agents, live on the server page — needs S1 | Not started |
| S6 | Packet capture: bounded on-demand capture, protocol breakdown, .pcap download — needs S1; last | Not started |

S1 → S2 → S3 is one chain; S4 can go at any point; S5 follows S1; S6 is last. Where the track sits against network phases 6 and 7 is the owner's call.

## To be checked by the owner

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

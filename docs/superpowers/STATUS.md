# Where Sentinel stands

Updated 2026-10-02. Read this first when picking the work up on another machine.

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
| 4 | Custom dashboards (includes the **site power widget**: every UPS in a site at a glance) | **Next** — not designed yet |
| 5 | Metric reports | Not started |
| 6 | Live site maps (LLDP, SSE push) — needs phase 4 | Not started |
| 7 | UniFi controller source | Not started |

Phases 4, 5 and 7 depend only on phase 2 and can go in any order; 6 needs 4.

## To be checked by the owner on real equipment

Phase 3 was verified against the SNMP simulator and in the sandbox, not yet on real switches. After rebuilding the work install from `dev`:
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

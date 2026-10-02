# Sentinel — notes for Claude Code

Sentinel is a self-hosted monitoring app: uptime monitors (HTTP/DNS/ping/TCP), server agents, SSL/domain expiry, incidents, notifications, reports, and SNMP network monitoring (sites, devices, ports, UPSes, MIB-based custom metrics).

**Start here when resuming work:** read `docs/superpowers/STATUS.md` (where the roadmap stands, which branch has what, and what's next). Design docs live in `docs/superpowers/specs/`, implementation plans in `docs/superpowers/plans/`; the network roadmap is `docs/superpowers/specs/2026-09-28-network-monitoring-roadmap.md`.

## Stack and layout

- Backend: Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB, gosnmp, gosmi (MIB parsing). `backend/cmd/sentinel/main.go` wires everything; migrations auto-run at startup.
- Frontend: React 18 + TypeScript + Vite + Tailwind + Recharts in `frontend/src`.
- Docker Compose deployment; `deploy/snmpsim` is an SNMP simulator used by tests.
- Module path: `github.com/Stevy2191/Sentinel/backend`.

## Commands (run from `backend/` unless noted)

- `go vet ./...` and `go test ./...` — unit tests (DB tests skip without a database).
- `./scripts/test-db.sh` — full suite including DB integration tests against a throwaway TimescaleDB container (needs Docker). `./scripts/test-db.sh -run <Name> -v` for one test (the `make test-db` target ignores args).
- Simulator tests: `cd deploy/snmpsim && SNMPSIM_VERSION=1.2.2 ./run.sh`, then prefix test commands with `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161`.
- Frontend check (from the repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"` — never run npm as root (root-owned node_modules break worktree cleanup). There are no frontend unit tests; this check is the gate.

## Branches and workflow

- `main` = what runs in production; `dev` = integration branch; features are built on `feature/*` branches (in git worktrees) and fast-forwarded into `dev`. Push and merge only when asked.
- Larger work follows brainstorm → written spec → written plan → task-by-task execution with review (the superpowers skills). Each phase ships to a sandbox first.

## Conventions and gotchas (learned the hard way)

- **Migrations:** `backend/migrations/NNN_name.sql`, applied once in filename order. Always `ls backend/migrations | tail -1` and use the next number — specs often name the wrong one. Never edit an applied migration; add a new one. Each file runs as one implicit transaction, so TimescaleDB continuous aggregates must be created `WITH NO DATA`.
- **Timestamps:** always `TIMESTAMPTZ` in new tables; prefer `time.Now().UTC()`.
- **SQL CHECKs:** `col IN (...)` is NULL (not false) when col is NULL — add `col IS NOT NULL`. A CHECK tying two columns breaks `ON DELETE SET NULL` FKs.
- **Gin middleware must abort:** writing a 403 and returning does not stop the chain; use `c.Abort()`/`AbortWithStatusJSON`. `respondError` does not abort (fine in handlers, never in middleware). Test that the guarded handler did NOT run.
- **Two API error shapes:** `respondError` → `error` string; `respondAuthError` → `{code, message}`. Frontend readers handle both.
- **Settings:** key/value `settings` table; env seeds the value once, then the stored value wins. Resolve settings per use (e.g. `BaseURLFunc`), never capture at construction. `DEFAULT_CHECK_INTERVAL` (env) is the scheduler tick, not the per-monitor default.
- **notify_channels** is three-valued everywhere: `null` = all channels (incl. future), `[]` = none, list = pinned.
- **Users** have both `is_admin` (the authority used by RequireAdmin) and `role`; keep them in sync.
- **Backups/restore:** dumps use `--table='public.*'` (never `--schema=public`), restores run `--single-transaction`, and a restore is refused when the backup's schema version differs. Any change touching backup/restore needs a real restore test.
- **Incidents:** device-down incidents have `interface_id` and `condition` NULL; port incidents set both; UPS incidents set `condition` (ups_*); metric-rule incidents use `condition = 'metric'` with `metric_key`/`metric_instance`. Queries meaning "device down" must filter `condition IS NULL`. Monitors that keep state re-read open incidents each poll (open incidents are the truth).
- **SNMP:** some agents refuse a whole GET over one value (Tripp Lite PowerAlert → badValue on sysUpTime); use `snmp.GetEach`, and treat an agent error status as "reachable". Cisco MIBs cannot be bundled (no licence) — only IETF modules are embedded; the Cisco health profile uses numeric OIDs.
- **Frontend design system:** slate/Tailwind, dark-only; colours come from `src/utils/colors.ts` (no hand-written hex, no `dark:` variants). Component files export only components (react-refresh lint) — helpers go in `src/utils/` or hooks. No `window.confirm`/`alert`; confirmations are in-page. Guard async results so a late response can't land after inputs changed.
- **CSS comments:** never write `*/` inside a `/* */` comment (e.g. `primary-*/text`) — it ends the comment and breaks the build.
- **Agent install URL:** with `sentinel_external_url`/`base_url` unset, agents are told to report to whatever URL the admin's browser used (often localhost). Check those settings first when an agent never connects.

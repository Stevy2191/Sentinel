#!/usr/bin/env bash
#
# Sentinel Repair & Maintenance Script
# Fix common issues without a full reinstall.
#
# Run from the repository root (next to docker-compose.yml):
#   ./repair.sh
#
set -uo pipefail

# ---------------------------------------------------------------------------
# Setup: locate the repo, set up logging and colors.
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR" || exit 1
LOG="$SCRIPT_DIR/repair.log"

# Colors only when writing to a terminal and NO_COLOR is unset.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BLUE=$'\033[34m'
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RESET=$'\033[0m'
else
  RED=; GREEN=; YELLOW=; BLUE=; BOLD=; DIM=; RESET=
fi

log_line() { printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S')" "$*" >> "$LOG"; }
info() { printf '%sℹ %s%s\n' "$BLUE"  "$*" "$RESET"; log_line "INFO:  $*"; }
ok()   { printf '%s✓ %s%s\n' "$GREEN" "$*" "$RESET"; log_line "OK:    $*"; }
warn() { printf '%s⚠ %s%s\n' "$YELLOW" "$*" "$RESET"; log_line "WARN:  $*"; }
err()  { printf '%s✗ %s%s\n' "$RED"   "$*" "$RESET"; log_line "ERROR: $*"; }
bold() { printf '%s%s%s\n'   "$BOLD"  "$*" "$RESET"; }

# run CMD... — echo, execute, and tee combined output to the log. Returns the
# command's exit status (not tee's).
run() {
  log_line "RUN: $*"
  printf '%s$ %s%s\n' "$DIM" "$*" "$RESET"
  "$@" 2>&1 | tee -a "$LOG"
  return "${PIPESTATUS[0]}"
}

confirm() { # confirm "question" [default:y|n]  -> returns 0 for yes
  local q="$1" def="${2:-n}" ans prompt
  if [ "$def" = "y" ]; then prompt="(Y/n)"; else prompt="(y/N)"; fi
  read -r -p "$q $prompt " ans || ans=""
  ans="${ans:-$def}"
  case "$ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# ---------------------------------------------------------------------------
# Environment detection: compose command, ports, adminer.
# ---------------------------------------------------------------------------
DC=""
detect_compose() {
  if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
  elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
  fi
}

# Load port settings from .env (falling back to defaults) so health checks hit
# the right host ports.
FRONTEND_PORT=3000; BACKEND_PORT=3001; DB_PORT=5432; ADMINER_PORT=8080
ADMINER_ENABLED=""; COMPOSE_PROFILES_VAL=""
load_env() {
  [ -f "$SCRIPT_DIR/.env" ] || return 0
  # Read only the keys we care about; ignore everything else.
  local line key val
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      \#*|"") continue ;;
    esac
    key="${line%%=*}"; val="${line#*=}"
    val="${val%\"}"; val="${val#\"}"
    case "$key" in
      FRONTEND_PORT) FRONTEND_PORT="$val" ;;
      BACKEND_PORT)  BACKEND_PORT="$val" ;;
      DB_PORT)       DB_PORT="$val" ;;
      ADMINER_PORT)  ADMINER_PORT="$val" ;;
      ADMINER_ENABLED) ADMINER_ENABLED="$val" ;;
      COMPOSE_PROFILES) COMPOSE_PROFILES_VAL="$val" ;;
    esac
  done < "$SCRIPT_DIR/.env"
}

adminer_enabled() {
  [ "$ADMINER_ENABLED" = "true" ] && return 0
  case "$COMPOSE_PROFILES_VAL" in *adminer*) return 0 ;; esac
  return 1
}

# ---------------------------------------------------------------------------
# Prerequisites.
# ---------------------------------------------------------------------------
check_prereqs() {
  local ok_all=0
  if command -v docker >/dev/null 2>&1; then ok "docker found"; else err "docker is not installed"; ok_all=1; fi
  if [ -n "$DC" ]; then ok "compose command: $DC"; else err "Docker Compose not found (need 'docker compose' or 'docker-compose')"; ok_all=1; fi
  if docker info >/dev/null 2>&1; then
    ok "Docker daemon is running"
  else
    err "Docker daemon is not running or not accessible"
    info "  Start Docker (e.g. 'sudo systemctl start docker' or open Docker Desktop) and retry."
    ok_all=1
  fi
  if [ ! -f "$SCRIPT_DIR/docker-compose.yml" ]; then
    err "docker-compose.yml not found in $SCRIPT_DIR"
    info "  Run this script from the Sentinel repository root."
    ok_all=1
  fi
  return $ok_all
}

# ---------------------------------------------------------------------------
# Option 1: Run database migrations.
#
# There is no separate migrate command — the backend applies pending SQL
# migrations automatically on startup (idempotent, tracked in schema_migrations).
# Restarting the backend re-runs any not-yet-applied migrations.
# ---------------------------------------------------------------------------
do_migrations() {
  bold "Running database migrations..."
  info "Migrations apply automatically when the backend starts; restarting it to apply any pending ones."
  info "(For migrations added by new code, rebuild images first — option 2.)"
  while true; do
    if run $DC up -d backend && run $DC restart backend; then
      sleep 3
      info "Recent backend migration log:"
      $DC logs --tail 60 backend 2>&1 | grep -iE 'migration|schema_migrations' | tee -a "$LOG" || true
      ok "Migrations step completed"
      return 0
    fi
    err "Migration step failed"
    confirm "Retry?" n || { info "Skipping. Check '$DC logs backend' for details."; return 1; }
  done
}

# ---------------------------------------------------------------------------
# Option 2: Rebuild Docker images.
# ---------------------------------------------------------------------------
do_rebuild() {
  bold "Rebuilding Docker images (no cache)..."
  while true; do
    if run $DC build --no-cache; then
      ok "Images rebuilt"
      info "Apply them with a restart (option 3) or '$DC up -d'."
      return 0
    fi
    err "Image rebuild failed"
    info "  Troubleshooting: check disk space ('docker system df'), network access, and the build output above."
    confirm "Retry?" n || return 1
  done
}

# ---------------------------------------------------------------------------
# Option 3: Restart services.
# ---------------------------------------------------------------------------
do_restart() {
  bold "Restarting all services..."
  local profile_args=()
  adminer_enabled && profile_args=(--profile adminer)
  if run $DC "${profile_args[@]}" up -d; then
    info "Waiting 30s for services to start..."
    sleep 30
    bold "Service status:"
    run $DC ps
    ok "Restart completed"
    return 0
  fi
  err "Restart failed — inspect '$DC ps' and '$DC logs'"
  return 1
}

# ---------------------------------------------------------------------------
# Option 4: Check service health.
# ---------------------------------------------------------------------------
# check_http URL LABEL — healthy on 2xx/3xx, slow if >2s, else down.
check_http() {
  local url="$1" label="$2" resp code ttime
  if ! command -v curl >/dev/null 2>&1; then
    warn "$label: curl not available on host; skipping HTTP check"
    return 0
  fi
  resp="$(curl -s -o /dev/null -w '%{http_code} %{time_total}' --max-time 5 "$url" 2>/dev/null)" || resp=""
  if [ -z "$resp" ]; then
    err "$label is down (no response from $url)"
    return 1
  fi
  code="${resp%% *}"; ttime="${resp##* }"
  if [ "$code" -ge 200 ] && [ "$code" -lt 400 ]; then
    if awk "BEGIN{exit !($ttime>2.0)}"; then
      warn "$label is slow (${ttime}s) at $url"
    else
      ok "$label is healthy (${code}, ${ttime}s)"
    fi
    return 0
  fi
  err "$label returned HTTP $code at $url"
  return 1
}

do_health() {
  bold "Checking service health..."
  local failures=0

  # Postgres: use pg_isready inside the container (independent of host port).
  if $DC exec -T postgres pg_isready -U sentinel >/dev/null 2>&1; then
    ok "Postgres is healthy (pg_isready)"
  else
    err "Postgres is down (pg_isready failed)"
    info "  Try: $DC logs postgres | tail -n 50"
    failures=$((failures+1))
  fi

  check_http "http://localhost:${BACKEND_PORT}/health" "Backend" || {
    info "  Try: $DC logs backend | tail -n 50"; failures=$((failures+1)); }
  check_http "http://localhost:${FRONTEND_PORT}/" "Frontend" || {
    info "  Try: $DC logs frontend | tail -n 50"; failures=$((failures+1)); }

  if adminer_enabled; then
    check_http "http://localhost:${ADMINER_PORT}/" "Adminer" || failures=$((failures+1))
  else
    info "Adminer is not enabled (skipping)"
  fi

  echo
  if [ "$failures" -eq 0 ]; then
    ok "All checked services are healthy"
  else
    warn "$failures service(s) reported problems — see suggestions above"
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Option 5: Clear cache / temp files.
# ---------------------------------------------------------------------------
do_clear_cache() {
  bold "Clearing temporary files..."
  # Truncate container logs (best-effort; needs privileges on some systems).
  info "Truncating container logs..."
  local ids id
  ids="$($DC ps -q 2>/dev/null)"
  for id in $ids; do
    local logfile
    logfile="$(docker inspect --format '{{.LogPath}}' "$id" 2>/dev/null)"
    if [ -n "$logfile" ] && [ -w "$logfile" ]; then
      : > "$logfile" && info "  cleared log for $id"
    fi
  done

  if confirm "Prune dangling Docker build cache and unused images? (safe, keeps volumes)" n; then
    run docker builder prune -f || true
    run docker image prune -f || true
  fi

  info "Browser cache: if the UI looks stale, hard-refresh (Ctrl+Shift+R / Cmd+Shift+R) or clear site data."
  ok "Cache cleared"
  return 0
}

# ---------------------------------------------------------------------------
# Safety: optional database backup.
# ---------------------------------------------------------------------------
backup_database() {
  local ts file
  ts="$(date +%s)"
  file="$SCRIPT_DIR/backup-${ts}.sql"
  bold "Backing up database..."
  if $DC exec -T postgres pg_dump -U sentinel sentinel > "$file" 2>>"$LOG"; then
    if [ -s "$file" ]; then
      ok "Backup saved to $(basename "$file")"
      log_line "BACKUP: $file"
      return 0
    fi
    rm -f "$file"
    err "Backup produced an empty file (is postgres running?)"
    return 1
  fi
  rm -f "$file"
  err "Backup failed — check that the postgres service is running"
  return 1
}

# ---------------------------------------------------------------------------
# Option 6: Full repair.
# ---------------------------------------------------------------------------
do_full_repair() {
  bold "══════════ FULL REPAIR ══════════"
  info "This runs prerequisites → (optional) git pull → image pull → migrations → rebuild → restart → health → cache clear."
  confirm "Continue with full repair?" y || { info "Cancelled."; return 1; }

  if confirm "Back up the database first?" y; then
    backup_database || confirm "Backup failed — continue anyway?" n || { info "Aborting full repair."; return 1; }
  fi

  local step=0 total=8
  step_banner() { step=$((step+1)); echo; bold "[Step $step/$total] $1"; }

  step_banner "Checking prerequisites"
  check_prereqs || { err "Prerequisites failed; fix the above and re-run."; return 1; }

  step_banner "Pulling latest code (git)"
  if [ -d "$SCRIPT_DIR/.git" ] && command -v git >/dev/null 2>&1; then
    if [ -n "$(git -C "$SCRIPT_DIR" status --porcelain 2>/dev/null)" ]; then
      warn "Working tree has local changes; skipping 'git pull' to avoid conflicts."
    elif confirm "Run 'git pull'?" y; then
      run git -C "$SCRIPT_DIR" pull --ff-only || warn "git pull failed; continuing with current code."
    else
      info "Skipped git pull."
    fi
  else
    info "Not a git checkout; skipping git pull."
  fi

  step_banner "Pulling latest images"
  run $DC pull || warn "Image pull failed (this is expected if you build locally); continuing."

  step_banner "Running database migrations"
  do_migrations || warn "Migrations step reported an issue; continuing."

  step_banner "Rebuilding images"
  do_rebuild || warn "Rebuild reported an issue; continuing."

  step_banner "Restarting services"
  do_restart || warn "Restart reported an issue; continuing."

  step_banner "Checking health"
  do_health

  step_banner "Clearing cache"
  do_clear_cache

  echo
  ok "Full repair completed!"
  info "Web UI:  http://localhost:${FRONTEND_PORT}"
  info "Backend: http://localhost:${BACKEND_PORT}/health"
  info "Log:     $(basename "$LOG")"
  return 0
}

# ---------------------------------------------------------------------------
# Menu.
# ---------------------------------------------------------------------------
banner() {
  echo
  bold "╔══════════════════════════════════════════════════╗"
  bold "║   Sentinel Repair & Maintenance Script v1.0      ║"
  bold "║   Fix common issues without a full reinstall     ║"
  bold "╚══════════════════════════════════════════════════╝"
  printf '%sOS: %s   Compose: %s   Log: %s%s\n' "$DIM" "$(uname -s)" "${DC:-<none>}" "$(basename "$LOG")" "$RESET"
  echo
}

menu() {
  bold "What would you like to do?"
  cat <<'MENU'
  1. Run database migrations
  2. Rebuild Docker images
  3. Restart all services
  4. Check service health
  5. Clear cache/temp files
  6. Full repair (do all of the above)
  7. Exit
MENU
}

main() {
  detect_compose
  load_env
  log_line "=== repair.sh started ==="
  banner

  # A single command-line argument runs that option non-interactively.
  if [ "$#" -ge 1 ]; then
    case "$1" in
      1) check_prereqs && do_migrations ;;
      2) check_prereqs && do_rebuild ;;
      3) check_prereqs && do_restart ;;
      4) check_prereqs && do_health ;;
      5) check_prereqs && do_clear_cache ;;
      6) do_full_repair ;;
      *) err "Unknown option '$1' (use 1-6)"; exit 2 ;;
    esac
    exit $?
  fi

  while true; do
    echo
    menu
    read -r -p "What would you like to do? (1-7): " choice || choice=7
    echo
    case "$choice" in
      1) check_prereqs && do_migrations ;;
      2) check_prereqs && do_rebuild ;;
      3) check_prereqs && do_restart ;;
      4) check_prereqs && do_health ;;
      5) check_prereqs && do_clear_cache ;;
      6) do_full_repair ;;
      7) info "Goodbye."; log_line "=== repair.sh exited ==="; exit 0 ;;
      *) warn "Please enter a number from 1 to 7." ;;
    esac
  done
}

main "$@"

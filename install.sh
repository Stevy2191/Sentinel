#!/usr/bin/env bash
#
# Sentinel installation script — interactive setup for a Docker deployment.
#
set -euo pipefail

# ---- Colors ----
if [ -t 1 ]; then
  BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m'); RESET=$(printf '\033[0m')
  RED=$(printf '\033[31m'); GREEN=$(printf '\033[32m'); YELLOW=$(printf '\033[33m'); CYAN=$(printf '\033[36m')
else
  BOLD=""; DIM=""; RESET=""; RED=""; GREEN=""; YELLOW=""; CYAN=""
fi

info()  { printf '%s\n' "$*"; }
ok()    { printf '%s✓%s %s\n' "$GREEN" "$RESET" "$*"; }
warn()  { printf '%s!%s %s\n' "$YELLOW" "$RESET" "$*"; }
err()   { printf '%s✗%s %s\n' "$RED" "$RESET" "$*" >&2; }
fatal() { err "$*"; exit 1; }

# Work from the script's directory (repo root).
cd "$(dirname "$0")"

# Host ports the stack binds (see docker-compose.yml). Frontend, backend, and
# postgres are env-backed (reassignable); adminer is fixed in compose.
FRONTEND_PORT=3000
BACKEND_PORT=3001
PORT_DB=5432
ADMINER_ENABLED=true
ADMINER_PORT=8080

# port_in_use PORT → returns 0 if something is listening on PORT.
# Prefer ss/netstat (which read /proc/net and see every listener regardless of
# owner); lsof without root only sees the current user's sockets and would miss
# a service started by another user.
port_in_use() {
  local p="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -tuln 2>/dev/null | grep -Eq ":$p[[:space:]]"
  elif command -v netstat >/dev/null 2>&1; then
    netstat -tuln 2>/dev/null | grep -Eq ":$p[[:space:]]"
  elif command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$p" -sTCP:LISTEN -t >/dev/null 2>&1
  else
    return 1 # no tool available; assume free
  fi
}

# kill_port PORT → kill the listening process(es); returns 0 if the port is free after.
kill_port() {
  local p="$1" pids=""
  if command -v lsof >/dev/null 2>&1; then
    pids=$(lsof -tiTCP:"$p" -sTCP:LISTEN 2>/dev/null || true)
  fi
  if [ -z "$pids" ]; then
    warn "Could not identify the process on port $p (lsof needed)."
    return 1
  fi
  # shellcheck disable=SC2086
  kill -9 $pids 2>/dev/null || true
  sleep 1
  ! port_in_use "$p"
}

# resolve_port VARNAME LABEL REASSIGNABLE(yes|no)
resolve_port() {
  local __name="$1" label="$2" reassign="$3"
  local port="${!__name}"
  if ! port_in_use "$port"; then
    ok "Port $port ($label) is available."
    return
  fi
  warn "Port $port ($label) is already in use!"
  while port_in_use "$port"; do
    info "  Options:"
    [ "$reassign" = "yes" ] && info "    ${BOLD}n${RESET}) Use a different port"
    info "    ${BOLD}k${RESET}) Kill the process using port $port"
    info "    ${BOLD}s${RESET}) Skip and continue anyway"
    if ! read -r -p "  Choose [n/k/s]: " CHOICE; then
      CHOICE="s" # EOF (non-interactive) → skip
    fi
    case "$CHOICE" in
      n|N)
        if [ "$reassign" != "yes" ]; then
          err "This port is fixed in docker-compose.yml and can't be reassigned here."
          continue
        fi
        read -r -p "  New port for $label: " NEWPORT || NEWPORT=""
        if printf '%s' "$NEWPORT" | grep -Eq '^[0-9]+$' && [ "$NEWPORT" -ge 1 ] && [ "$NEWPORT" -le 65535 ]; then
          port="$NEWPORT"
          printf -v "$__name" '%s' "$NEWPORT"
        else
          err "Invalid port number."
        fi
        ;;
      k|K)
        if kill_port "$port"; then ok "Freed port $port."; else err "Could not free port $port."; fi
        ;;
      s|S)
        warn "Skipping port $port — startup may fail if it stays occupied."
        return
        ;;
      *) err "Unknown option." ;;
    esac
  done
  ok "Port $port ($label) is now available."
}

# ask_port VARNAME LABEL DEFAULT [EXCLUDE_PORT]
# Prompts for a port (1024-65535), optionally requiring it to differ from
# EXCLUDE_PORT, warns on conflict, and stores the result in VARNAME.
ask_port() {
  local __name="$1" label="$2" default="$3" exclude="${4:-}"
  local chosen
  while true; do
    if ! read -r -p "  Enter ${label} port (default ${default}): " INPUT; then INPUT=""; fi
    chosen="${INPUT:-$default}"
    if ! printf '%s' "$chosen" | grep -Eq '^[0-9]+$' || [ "$chosen" -lt 1024 ] || [ "$chosen" -gt 65535 ]; then
      err "Port must be a number between 1024 and 65535."
      continue
    fi
    if [ -n "$exclude" ] && [ "$chosen" = "$exclude" ]; then
      err "This port must differ from the other chosen port (${exclude})."
      continue
    fi
    if port_in_use "$chosen"; then
      warn "Port $chosen is already in use."
      if ! read -r -p "  Use a different port? (Y/n) " AGAIN; then AGAIN="n"; fi
      case "$AGAIN" in
        n|N|no|NO) warn "Continuing with $chosen anyway — startup may fail if it stays occupied." ;;
        *) continue ;;
      esac
    else
      ok "Port $chosen is available."
    fi
    printf -v "$__name" '%s' "$chosen"
    return
  done
}

# prompt_for_ports asks for the frontend (web UI) and backend (API) host ports.
prompt_for_ports() {
  info "${BOLD}Port Configuration${RESET}"
  info "What port should the web UI run on?"
  ask_port FRONTEND_PORT "frontend web UI" 3000 ""
  info "What port should the API run on?"
  ask_port BACKEND_PORT "backend API" 3001 "$FRONTEND_PORT"
  echo
}

# check_port_conflicts asks for the app ports, then verifies the backing
# service ports before docker-compose starts.
check_port_conflicts() {
  prompt_for_ports
  info "Verifying the backing service ports..."
  resolve_port PORT_DB "postgres" yes
  ok "Port check complete."
  echo
}

# prompt_for_adminer asks whether to include the optional Adminer database admin
# tool and, if so, on which host port. Sets ADMINER_ENABLED (true/false) and,
# when enabled, ADMINER_PORT.
prompt_for_adminer() {
  info "${BOLD}Database Administration Tool (Optional)${RESET}"
  info "  Adminer is a web-based tool to manage your database."
  info "  You can browse tables, run SQL queries, and manage data."
  info "  It's optional — you can manage data via the API instead."
  if ! read -r -p "Do you need database admin access? (Y/n) " ANS; then ANS="y"; fi
  case "$ANS" in
    n|N|no|NO)
      ADMINER_ENABLED=false
      warn "Adminer disabled — its container will not be started."
      ;;
    *)
      ADMINER_ENABLED=true
      info "What port should Adminer run on?"
      ask_port ADMINER_PORT "Adminer" 8080 ""
      ;;
  esac
  echo
}

# ---- 1. Banner ----
cat <<BANNER
${CYAN}${BOLD}
  ____             _   _            _
 / ___|  ___ _ __ | |_(_)_ __   ___| |
 \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |
  ___) |  __/ | | | |_| | | | |  __/ |
 |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|
${RESET}
 ${BOLD}Sentinel Installation Script v1.0${RESET}
 ${DIM}Self-hosted uptime monitoring made easy${RESET}

BANNER

# ---- 2. Prerequisites ----
info "${BOLD}Checking prerequisites...${RESET}"

if ! command -v docker >/dev/null 2>&1; then
  fatal "Docker is not installed. Install it from https://docs.docker.com/get-docker/ and re-run."
fi
ok "Docker: $(docker --version | sed 's/,.*//')"

if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  fatal "Docker Compose is not installed. Install Docker Compose v2 (https://docs.docker.com/compose/install/) and re-run."
fi
ok "Docker Compose: $($COMPOSE version --short 2>/dev/null || echo present) (${COMPOSE})"

if ! command -v git >/dev/null 2>&1; then
  fatal "Git is not installed. Install git and re-run."
fi
ok "Git: $(git --version | awk '{print $3}')"

# .env overwrite guard
if [ -f .env ]; then
  warn "A .env file already exists."
  read -r -p "Overwrite existing .env? (y/N) " OVERWRITE
  case "${OVERWRITE:-n}" in
    y|Y|yes|YES) info "Overwriting .env." ;;
    *) fatal "Aborted; leaving existing .env in place." ;;
  esac
fi
echo

# ---- 3. Timezone ----
info "${BOLD}Timezone${RESET}"
TZ_DETECTED="UTC"
if command -v timedatectl >/dev/null 2>&1; then
  TZ_DETECTED=$(timedatectl show -p Timezone --value 2>/dev/null || echo UTC)
elif command -v systemsetup >/dev/null 2>&1; then
  TZ_DETECTED=$(systemsetup -gettimezone 2>/dev/null | awk '{print $NF}' || echo UTC)
fi
[ -z "$TZ_DETECTED" ] && TZ_DETECTED="UTC"
info "Detected timezone: ${BOLD}${TZ_DETECTED}${RESET}"
read -r -p "Use this timezone? Press ENTER to accept, or type a custom one: " TZ_INPUT
TIMEZONE="${TZ_INPUT:-$TZ_DETECTED}"
ok "Using timezone: ${TIMEZONE}"
echo

# ---- Port conflict check (before anything is started) ----
check_port_conflicts

# ---- Optional database admin tool ----
prompt_for_adminer

# ---- 4. Database password ----
info "${BOLD}Database password${RESET} ${DIM}(min 12 characters)${RESET}"
DB_PASSWORD=""
while true; do
  read -r -s -p "Enter database password: " DB_PASSWORD; echo
  if [ "${#DB_PASSWORD}" -lt 12 ]; then
    err "Password too short (${#DB_PASSWORD} chars); need at least 12. Try again."
    continue
  fi
  read -r -s -p "Confirm database password: " DB_PASSWORD2; echo
  if [ "$DB_PASSWORD" != "$DB_PASSWORD2" ]; then
    err "Passwords do not match. Try again."
    continue
  fi
  break
done
ok "Database password set."
echo

# ---- 5. JWT secret ----
info "${BOLD}Generating JWT secret...${RESET}"
if command -v openssl >/dev/null 2>&1; then
  JWT_SECRET=$(openssl rand -base64 32)
else
  JWT_SECRET=$(head -c 32 /dev/urandom | base64 2>/dev/null || date +%s%N | sha256sum | awk '{print $1}')
fi
ok "Generated JWT secret: ••••••••••••••••"
echo

# ---- 5b. User registration ----
info "${BOLD}User registration${RESET}"
info "  Should new users be able to create their own accounts?"
info "  You can always create the first (admin) account, and change this later"
info "  under Security. Leaving it closed is recommended for a private instance."
if ! read -r -p "Allow open user registration? (y/N) " REG_ANSWER; then REG_ANSWER="n"; fi
case "$REG_ANSWER" in
  y|Y|yes|YES) REGISTRATION_ENABLED=true;  ok "Registration will be open." ;;
  *)           REGISTRATION_ENABLED=false; ok "Registration closed (first account still allowed)." ;;
esac
echo

# ---- 6. Optional SMTP ----
SMTP_HOST=""; SMTP_PORT="587"; SMTP_USER=""; SMTP_PASSWORD=""; SMTP_FROM=""
info "${BOLD}Email alerts (optional)${RESET}"
read -r -p "Set up email (SMTP) alerts now? (y/N) " SMTP_ANSWER
case "${SMTP_ANSWER:-n}" in
  y|Y|yes|YES)
    read -r -p "  SMTP host (e.g. smtp.gmail.com): " SMTP_HOST
    read -r -p "  SMTP port [587]: " SMTP_PORT_IN; SMTP_PORT="${SMTP_PORT_IN:-587}"
    read -r -p "  SMTP username (email): " SMTP_USER
    read -r -s -p "  SMTP password (app password): " SMTP_PASSWORD; echo
    read -r -p "  From address [${SMTP_USER}]: " SMTP_FROM_IN; SMTP_FROM="${SMTP_FROM_IN:-$SMTP_USER}"
    ok "SMTP configured for ${SMTP_HOST}."
    ;;
  *)
    info "Skipping email setup (you can add SMTP_* to .env later)."
    ;;
esac
echo

# ---- 7. Write .env ----
info "${BOLD}Writing .env...${RESET}"
# COMPOSE_PROFILES activates the "adminer" profile so docker-compose starts the
# Adminer container. Left empty when the user opts out, so the profiled service
# is skipped on every `docker compose up` (no manual editing required).
if [ "$ADMINER_ENABLED" = "true" ]; then COMPOSE_PROFILES="adminer"; else COMPOSE_PROFILES=""; fi
cat > .env <<ENV
# Generated by install.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)

# Database
DB_PASSWORD=${DB_PASSWORD}
DB_PORT=${PORT_DB}

# Server
FRONTEND_PORT=${FRONTEND_PORT}
BACKEND_PORT=${BACKEND_PORT}
ENVIRONMENT=production

# Database admin tool (Adminer). ADMINER_ENABLED is informational; the
# COMPOSE_PROFILES line below is what actually starts/skips the container.
ADMINER_ENABLED=${ADMINER_ENABLED}
ADMINER_PORT=${ADMINER_PORT}
COMPOSE_PROFILES=${COMPOSE_PROFILES}
LOG_LEVEL=info
TIMEZONE=${TIMEZONE}

# Authentication
JWT_SECRET=${JWT_SECRET}
REGISTRATION_ENABLED=${REGISTRATION_ENABLED}

# SMTP (email alerts)
SMTP_HOST=${SMTP_HOST}
SMTP_PORT=${SMTP_PORT}
SMTP_USER=${SMTP_USER}
SMTP_PASSWORD=${SMTP_PASSWORD}
SMTP_FROM=${SMTP_FROM}

# Other notification channels (optional)
SLACK_WEBHOOK_URL=
DISCORD_WEBHOOK_URL=
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
NTFY_URL=https://ntfy.sh
NTFY_TOPIC=
WEBHOOK_URL=

# Docker images
DOCKER_REGISTRY=ghcr.io
IMAGE_TAG=latest
ENV
chmod 600 .env
ok ".env created (permissions 600)."
echo

# ---- 8. Start services ----
# The Adminer service is gated behind a compose profile, so activate it only
# when the user opted in. COMPOSE_PROFILES is also written to .env so later
# plain `docker compose up` calls honor the same choice.
PROFILE_FLAGS=""
if [ "$ADMINER_ENABLED" = "true" ]; then PROFILE_FLAGS="--profile adminer"; fi
info "${BOLD}Ready to launch.${RESET}"
read -r -p "Start Sentinel now with '${COMPOSE} ${PROFILE_FLAGS} up -d --build'? (Y/n) " START_ANSWER
case "${START_ANSWER:-y}" in
  n|N|no|NO)
    info "Skipping startup. When ready, run: ${BOLD}${COMPOSE} ${PROFILE_FLAGS} up -d --build${RESET}"
    ;;
  *)
    info "Building and starting containers (this may take a few minutes)..."
    $COMPOSE $PROFILE_FLAGS up -d --build
    echo
    ok "Sentinel is starting."
    info "  Web UI:   ${BOLD}http://localhost:${FRONTEND_PORT}${RESET}"
    info "  Backend:  http://localhost:${BACKEND_PORT}/api/v1"
    if [ "$ADMINER_ENABLED" = "true" ]; then
      info "  DB admin: http://localhost:${ADMINER_PORT} (Adminer)"
    fi
    echo
    info "First run: open the web UI and register — the first account becomes the admin."
    ;;
esac

echo
ok "Done."

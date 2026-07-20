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

# Host ports the stack binds (see docker-compose.yml). Frontend and postgres are
# env-backed (reassignable); backend and adminer are fixed in compose.
PORT_FRONTEND=3000
PORT_BACKEND=3001
PORT_DB=5432
PORT_ADMINER=8080

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

# find_free_port START → prints the first free port at/after START (capped).
find_free_port() {
  local p="$1" cap=$(( $1 + 100 ))
  while [ "$p" -le "$cap" ] && port_in_use "$p"; do
    p=$((p + 1))
  done
  echo "$p"
}

# choose_frontend_port asks the user which port to serve the web UI on, with a
# smart default that avoids a port already in use.
choose_frontend_port() {
  info "${BOLD}What port should the Sentinel web UI run on?${RESET}"
  local suggestion=3000
  if port_in_use 3000; then
    suggestion=$(find_free_port 3002)
    warn "Port 3000 appears to be in use by another application."
    info "  Recommended free port: ${BOLD}${suggestion}${RESET}"
  else
    info "  Recommended: ${BOLD}3000${RESET}"
  fi

  while true; do
    read -r -p "  Enter port (default ${suggestion}): " INPUT || INPUT=""
    local chosen="${INPUT:-$suggestion}"
    if ! printf '%s' "$chosen" | grep -Eq '^[0-9]+$' || [ "$chosen" -lt 1 ] || [ "$chosen" -gt 65535 ]; then
      err "Please enter a valid port number (1-65535)."
      continue
    fi
    if port_in_use "$chosen"; then
      warn "Port $chosen is already in use."
      if ! read -r -p "  Try another port? (Y/n) " AGAIN; then AGAIN="n"; fi
      case "$AGAIN" in
        n|N|no|NO)
          warn "Using port $chosen anyway — startup may fail if it stays occupied."
          PORT_FRONTEND="$chosen"
          return
          ;;
        *) continue ;;
      esac
    fi
    ok "Port $chosen is available."
    PORT_FRONTEND="$chosen"
    return
  done
}

# check_port_conflicts asks for the web-UI port up front, then verifies the
# remaining service ports before docker-compose starts.
check_port_conflicts() {
  info "${BOLD}Configuring ports...${RESET}"
  choose_frontend_port
  echo
  info "Verifying the backing service ports..."
  resolve_port PORT_BACKEND "backend API" no
  resolve_port PORT_DB "postgres" yes
  resolve_port PORT_ADMINER "adminer" no
  ok "Port check complete."
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
cat > .env <<ENV
# Generated by install.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)

# Database
DB_PASSWORD=${DB_PASSWORD}
DB_PORT=${PORT_DB}

# Server
PORT=${PORT_FRONTEND}
ENVIRONMENT=production
LOG_LEVEL=info
TIMEZONE=${TIMEZONE}

# Authentication
JWT_SECRET=${JWT_SECRET}

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
info "${BOLD}Ready to launch.${RESET}"
read -r -p "Start Sentinel now with '${COMPOSE} up -d --build'? (Y/n) " START_ANSWER
case "${START_ANSWER:-y}" in
  n|N|no|NO)
    info "Skipping startup. When ready, run: ${BOLD}${COMPOSE} up -d --build${RESET}"
    ;;
  *)
    info "Building and starting containers (this may take a few minutes)..."
    $COMPOSE up -d --build
    echo
    ok "Sentinel is starting."
    info "  Web UI:   ${BOLD}http://localhost:3000${RESET}"
    info "  Backend:  http://localhost:3001/api/v1"
    info "  DB admin: http://localhost:8080 (Adminer)"
    echo
    info "First run: open the web UI and register — the first account becomes the admin."
    ;;
esac

echo
ok "Done."

package api

// bashInstallScript installs the agent as a systemd service.
//
// Written to be run twice safely: an existing install is stopped, replaced and
// restarted rather than refused, since the usual reason to run it again is to
// upgrade or to correct a mistyped token.
const bashInstallScript = `#!/usr/bin/env bash
#
# Sentinel monitoring agent installer.
#
#   SERVER_TOKEN="srv_..." AGENT_ID="agent_..." sudo -E bash ./server-agent.sh
#
# Installs the agent to /usr/local/bin, writes a systemd unit, and starts it.
set -euo pipefail

SENTINEL_URL="${SENTINEL_URL:-${POCKETBASE_URL:-{{.SentinelURL}}}}"
AGENT_ID="${AGENT_ID:-}"
SERVER_TOKEN="${SERVER_TOKEN:-}"
SERVER_NAME="${SERVER_NAME:-$(hostname)}"
OS_TYPE="${OS_TYPE:-linux}"
CHECK_INTERVAL="${CHECK_INTERVAL:-60}"
RETRY_ATTEMPTS="${RETRY_ATTEMPTS:-3}"
DISK_PATH="${DISK_PATH:-/}"

BIN_PATH=/usr/local/bin/sentinel-agent
CONF_DIR=/etc/sentinel
CONF_PATH="$CONF_DIR/agent.conf"
UNIT_PATH=/etc/systemd/system/sentinel-agent.service
LOG_PATH=/var/log/sentinel-agent.log

die() { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# --- prerequisites ----------------------------------------------------------
[ "$(id -u)" -eq 0 ] || die "run as root (use: sudo -E bash $0)"
command -v curl >/dev/null 2>&1 || die "curl is required"
command -v systemctl >/dev/null 2>&1 || die "systemd is required; use the Docker installer on a host without it"
[ -n "$AGENT_ID" ] || die "AGENT_ID is not set — copy the command from Sentinel"
[ -n "$SERVER_TOKEN" ] || die "SERVER_TOKEN is not set — copy the command from Sentinel"

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

info "installing the Sentinel agent"
echo "    server:   $SENTINEL_URL"
echo "    agent:    $AGENT_ID"
echo "    interval: ${CHECK_INTERVAL}s"

# --- download ---------------------------------------------------------------
# To a temporary file first, so a failed download cannot leave a half-written
# binary where a working one used to be.
TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT
info "downloading the agent for linux/$ARCH"
curl -fSL --retry 3 --retry-delay 2 -o "$TMP_BIN" "$SENTINEL_URL/agent/download/linux/$ARCH" \
  || die "could not download the agent from $SENTINEL_URL"
[ -s "$TMP_BIN" ] || die "the downloaded agent is empty"

if systemctl is-active --quiet sentinel-agent 2>/dev/null; then
  info "stopping the running agent"
  systemctl stop sentinel-agent
fi

install -m 0755 "$TMP_BIN" "$BIN_PATH"

# --- configuration ----------------------------------------------------------
# The token lives in a file readable only by root rather than in the unit,
# because a unit file is world-readable and systemctl show would print it.
info "writing configuration to $CONF_PATH"
mkdir -p "$CONF_DIR"
umask 077
cat > "$CONF_PATH" <<EOF
SENTINEL_URL=$SENTINEL_URL
AGENT_ID=$AGENT_ID
SERVER_TOKEN=$SERVER_TOKEN
SERVER_NAME=$SERVER_NAME
OS_TYPE=$OS_TYPE
CHECK_INTERVAL=$CHECK_INTERVAL
RETRY_ATTEMPTS=$RETRY_ATTEMPTS
DISK_PATH=$DISK_PATH
EOF
chmod 0600 "$CONF_PATH"

# --- service ----------------------------------------------------------------
info "writing the systemd unit"
cat > "$UNIT_PATH" <<EOF
[Unit]
Description=Sentinel monitoring agent
Documentation=$SENTINEL_URL
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$CONF_PATH
ExecStart=$BIN_PATH
Restart=always
RestartSec=10
StandardOutput=append:$LOG_PATH
StandardError=append:$LOG_PATH

# The agent reads /proc and the Docker socket and writes nothing but its log,
# so it is confined to that.
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=full
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

touch "$LOG_PATH" && chmod 0640 "$LOG_PATH"

info "starting the service"
systemctl daemon-reload
systemctl enable --quiet sentinel-agent
systemctl restart sentinel-agent

# --- verify -----------------------------------------------------------------
# Reporting success without checking would leave a broken token looking like a
# working install until somebody noticed the host was missing.
sleep 3
if systemctl is-active --quiet sentinel-agent; then
  info "agent installed and running"
  echo
  echo "    status: systemctl status sentinel-agent"
  echo "    logs:   tail -f $LOG_PATH"
  echo
  if grep -qi "rejected by server" "$LOG_PATH" 2>/dev/null; then
    echo "warning: the server rejected the agent's credentials. Check AGENT_ID and SERVER_TOKEN." >&2
    exit 1
  fi
  echo "The host should appear in Sentinel under Server Monitoring within a minute."
else
  echo "error: the agent did not stay running. Recent output:" >&2
  journalctl -u sentinel-agent -n 20 --no-pager >&2 || tail -n 20 "$LOG_PATH" >&2
  exit 1
fi
`

// dockerInstallScript runs the agent as a container.
const dockerInstallScript = `#!/usr/bin/env bash
#
# Sentinel monitoring agent installer (Docker).
#
#   SERVER_TOKEN="srv_..." AGENT_ID="agent_..." sudo -E bash ./server-docker-agent.sh
set -euo pipefail

SENTINEL_URL="${SENTINEL_URL:-${POCKETBASE_URL:-{{.SentinelURL}}}}"
AGENT_ID="${AGENT_ID:-}"
SERVER_TOKEN="${SERVER_TOKEN:-}"
SERVER_NAME="${SERVER_NAME:-$(hostname)}"
OS_TYPE="${OS_TYPE:-linux}"
CHECK_INTERVAL="${CHECK_INTERVAL:-60}"
RETRY_ATTEMPTS="${RETRY_ATTEMPTS:-3}"
CONTAINER_NAME="${CONTAINER_NAME:-sentinel-agent}"
IMAGE="${AGENT_IMAGE:-alpine:latest}"

die() { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

command -v docker >/dev/null 2>&1 || die "docker is required"
docker info >/dev/null 2>&1 || die "cannot talk to the Docker daemon; run with sudo or add your user to the docker group"
[ -n "$AGENT_ID" ] || die "AGENT_ID is not set — copy the command from Sentinel"
[ -n "$SERVER_TOKEN" ] || die "SERVER_TOKEN is not set — copy the command from Sentinel"

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

info "preparing the agent for linux/$ARCH"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
curl -fSL --retry 3 --retry-delay 2 -o "$WORK/sentinel-agent" "$SENTINEL_URL/agent/download/linux/$ARCH" \
  || die "could not download the agent from $SENTINEL_URL"
chmod +x "$WORK/sentinel-agent"

# Built on the host rather than pulled, so the agent needs no image registry
# and always matches the Sentinel it will report to.
cat > "$WORK/Dockerfile" <<'EOF'
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY sentinel-agent /usr/local/bin/sentinel-agent
ENTRYPOINT ["/usr/local/bin/sentinel-agent"]
EOF

info "building the agent image"
docker build -q -t sentinel-agent:local "$WORK" >/dev/null

if docker ps -a --format '{{"{{"}}.Names{{"}}"}}' | grep -qx "$CONTAINER_NAME"; then
  info "removing the existing $CONTAINER_NAME container"
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

info "starting $CONTAINER_NAME"
# The host's /proc and /etc are mounted read-only so the agent reports the
# host's metrics rather than the container's, and the Docker socket read-only
# so it can list containers without being able to control them.
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  --network host \
  --pid host \
  -v /proc:/host/proc:ro \
  -v /etc/os-release:/host/etc/os-release:ro \
  -v /etc/hostname:/host/etc/hostname:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v /:/hostfs:ro \
  -e HOST_PROC=/host/proc \
  -e HOST_ETC=/host/etc \
  -e DISK_PATH=/hostfs \
  -e SENTINEL_URL="$SENTINEL_URL" \
  -e AGENT_ID="$AGENT_ID" \
  -e SERVER_TOKEN="$SERVER_TOKEN" \
  -e SERVER_NAME="$SERVER_NAME" \
  -e OS_TYPE="$OS_TYPE" \
  -e CHECK_INTERVAL="$CHECK_INTERVAL" \
  -e RETRY_ATTEMPTS="$RETRY_ATTEMPTS" \
  sentinel-agent:local >/dev/null

sleep 4
if [ "$(docker inspect -f '{{"{{"}}.State.Running{{"}}"}}' "$CONTAINER_NAME" 2>/dev/null)" = "true" ]; then
  info "agent container running"
  echo
  echo "    logs:    docker logs -f $CONTAINER_NAME"
  echo "    stop:    docker stop $CONTAINER_NAME"
  echo
  if docker logs "$CONTAINER_NAME" 2>&1 | grep -qi "rejected by server"; then
    echo "warning: the server rejected the agent's credentials. Check AGENT_ID and SERVER_TOKEN." >&2
    exit 1
  fi
  echo "The host should appear in Sentinel under Server Monitoring within a minute."
else
  echo "error: the container did not stay running:" >&2
  docker logs "$CONTAINER_NAME" 2>&1 | tail -n 20 >&2
  exit 1
fi
`

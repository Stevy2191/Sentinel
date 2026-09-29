#!/usr/bin/env bash
# Builds and starts the simulator: UDP 1161-1190 on the host's loopback, and
# on the sandbox's compose network as sentinel-snmpsim (for the backend).
set -euo pipefail
cd "$(dirname "$0")"
: "${SNMPSIM_VERSION:?set SNMPSIM_VERSION to the pinned snmpsim version}"
docker build -q --build-arg SNMPSIM_VERSION="$SNMPSIM_VERSION" -t sentinel-snmpsim:dev . >/dev/null
docker rm -f sentinel-snmpsim >/dev/null 2>&1 || true

endpoints=()
ports=()
for p in $(seq 1161 1190); do
  endpoints+=("--agent-udpv4-endpoint=0.0.0.0:$p")
  ports+=(-p "127.0.0.1:$p:$p/udp")
done

docker run -d --name sentinel-snmpsim "${ports[@]}" sentinel-snmpsim:dev \
  "${endpoints[@]}" \
  --v3-user=sentinel-auth --v3-auth-key=authpass123 --v3-auth-proto=SHA \
  --v3-user=sentinel-priv --v3-auth-key=authpass123 --v3-auth-proto=SHA \
    --v3-priv-key=privpass123 --v3-priv-proto=AES \
  >/dev/null

if docker network inspect sentinel-dev_default >/dev/null 2>&1; then
  docker network connect sentinel-dev_default sentinel-snmpsim
fi
echo "sentinel-snmpsim running on UDP 1161-1190"

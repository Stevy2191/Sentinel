#!/usr/bin/env bash
# Runs the Go tests, including database integration tests, against a
# throwaway TimescaleDB container that is removed afterwards.
set -euo pipefail
cd "$(dirname "$0")/.."

name="sentinel-testdb-$$"
docker run -d --rm --name "$name" \
  -e POSTGRES_USER=sentinel -e POSTGRES_PASSWORD=test -e POSTGRES_DB=sentinel \
  -p 127.0.0.1::5432 \
  timescale/timescaledb:2.30.1-pg16 \
  postgres -c shared_preload_libraries=timescaledb -c timescaledb.telemetry_level=off \
  >/dev/null
trap 'docker stop "$name" >/dev/null 2>&1 || true' EXIT

for _ in $(seq 1 60); do
  if docker exec "$name" pg_isready -U sentinel -d sentinel >/dev/null 2>&1; then break; fi
  sleep 0.5
done
# The image runs a temporary postgres for initdb, then restarts into the real
# one; both answer pg_isready and even a real query, so a single successful
# query can hit the temporary instance moments before it shuts down. Wait for
# "ready to accept connections" to appear twice in the log (temp, then real)
# before trusting a query.
for _ in $(seq 1 60); do
  count="$(docker logs "$name" 2>&1 | grep -c 'database system is ready to accept connections' || true)"
  if [ "${count:-0}" -ge 2 ]; then break; fi
  sleep 0.5
done
for _ in $(seq 1 60); do
  if docker exec "$name" psql -U sentinel -d sentinel -Atc 'select 1' >/dev/null 2>&1; then break; fi
  sleep 0.5
done

port="$(docker port "$name" 5432/tcp | head -1 | awk -F: '{print $NF}')"
export SENTINEL_TEST_DATABASE_URL="postgres://sentinel:test@127.0.0.1:${port}/sentinel?sslmode=disable"
# Lets TestDBRestoreWithMetrics run pg_dump/psql inside the container (the
# host has no postgres client tools).
export SENTINEL_TEST_DB_CONTAINER="$name"
go test "$@" ./...

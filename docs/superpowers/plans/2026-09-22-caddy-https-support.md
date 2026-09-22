# Optional Caddy HTTPS Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator have Sentinel serve HTTPS directly, with automatic Let's Encrypt or self-signed certificates, as an opt-in alternative to the existing "bring your own reverse proxy" and "plain HTTP" modes — neither of which changes.

**Architecture:** A brand-new, separately-published frontend image (`sentinel-frontend-caddy`) runs Caddy instead of nginx, serving the same built SPA and proxying the same routes. An operator opts in by layering a new `docker-compose.caddy.yml` override on top of the unchanged `docker-compose.yml`. A small shell entrypoint picks between Caddy's `tls {$LETSENCRYPT_EMAIL}` (real ACME) and `tls internal` (local self-signed CA) directives based on a `TLS_MODE` env var, then hands off to Caddy itself, which handles certificate issuance, HTTP→HTTPS redirect, and renewal automatically.

**Tech Stack:** Caddy 2 (Alpine), Docker Compose, bash (`install.sh`).

**Spec:** `docs/superpowers/specs/2026-09-22-caddy-https-support-design.md`

## Global Constraints

- Three modes: bring-your-own-proxy, plain HTTP, and Caddy. Only Caddy is new work — `docker-compose.yml` and `frontend/nginx.conf` behavior must not change.
- Caddy mode supports exactly two `TLS_MODE` values: `letsencrypt` (HTTP-01 challenge only — requires a real public domain and internet reachability on ports 80/443) and `selfsigned` (Caddy's internal CA, works with no domain or internet reachability).
- DNS-01 challenge support is explicitly out of scope.
- Every proxied route in the new Caddyfile must behave the same as the equivalent route in `frontend/nginx.conf`: `/health` (direct 200, not the SPA fallback), `/scripts/*` and `/agent/*` (proxied to `backend:3001`, full path preserved, not stripped), `/api/*` (proxied to `backend:3001`, full path preserved), everything else (SPA static files, falling back to `index.html`).
- `DOMAIN` is required in both `TLS_MODE` values (Caddy needs one site address either way; in self-signed mode it need not resolve to anything real).
- `docker compose -f docker-compose.yml -f docker-compose.caddy.yml config` must show the merged `frontend` service using the Caddy image, both ports 80 and 443, and the new environment variables — confirmed empirically during planning: Compose **replaces** list-type keys (`ports`) wholesale when an override restates them, but **inherits unchanged** any key the override does not mention at all (confirmed for `depends_on`).
- Certificate storage must survive a container recreation (a named volume), since Let's Encrypt rate-limits issuance per domain.

---

### Task 1: The Caddy-based frontend image

**Files:**
- Create: `frontend/Caddyfile.template`
- Create: `frontend/caddy-entrypoint.sh`
- Create: `frontend/Dockerfile.caddy`

**Interfaces:**
- Produces: a Docker image, built from `frontend/Dockerfile.caddy`, that serves the SPA and proxies the same routes as the existing nginx image, over HTTPS, once given `DOMAIN` and `TLS_MODE` (and `LETSENCRYPT_EMAIL` when `TLS_MODE=letsencrypt`) as environment variables. Later tasks (2, 6, 7) build, run, and publish this image but do not change its internals.

- [ ] **Step 1: Write the Caddyfile template**

Create `frontend/Caddyfile.template`:

```
{$DOMAIN} {
	__TLS_DIRECTIVE__

	header {
		Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
		X-Frame-Options "DENY"
		X-Content-Type-Options "nosniff"
		Referrer-Policy "same-origin"
		Strict-Transport-Security "max-age=63072000; includeSubDomains"
		X-XSS-Protection "0"
	}

	# Liveness probe for the compose healthcheck. A dedicated handle rather
	# than falling through to the SPA, for the same reason nginx.conf's
	# /health location exists: the SPA fallback would otherwise answer any
	# path with index.html and a 200, so the healthcheck would pass no
	# matter what was actually broken.
	@health path /health
	handle @health {
		respond "ok" 200
	}

	# Agent installer endpoints. Proxied with the full path preserved (no
	# prefix stripped) — a monitored host has no reason to know this
	# container even exists, only the URL it was given.
	@agentPaths path /scripts/* /agent/*
	handle @agentPaths {
		reverse_proxy backend:3001
	}

	# API proxy. Full path preserved, same as the agent endpoints above.
	# reverse_proxy forwards the Host header and sets X-Forwarded-* headers
	# automatically, and handles WebSocket upgrades without extra config —
	# nginx.conf needs explicit directives for both; Caddy does not.
	@api path /api/*
	handle @api {
		reverse_proxy backend:3001
	}

	# Everything else: the SPA's static files, falling back to index.html
	# for client-side routing.
	handle {
		root * /srv
		try_files {path} /index.html
		file_server
	}
}
```

- [ ] **Step 2: Write the entrypoint script**

Create `frontend/caddy-entrypoint.sh`:

```sh
#!/bin/sh
set -eu

: "${DOMAIN:?DOMAIN must be set (see .env) - required in both TLS_MODE values}"

# The choice between Let's Encrypt and a self-signed cert is a different
# Caddy directive entirely, not a value that fits inside one placeholder —
# so this picks the whole directive text before Caddy ever sees the file.
# Caddy's own {$VAR} substitution (not this script) fills in
# LETSENCRYPT_EMAIL/DOMAIN from the environment when it loads the result.
case "${TLS_MODE:-selfsigned}" in
  letsencrypt)
    : "${LETSENCRYPT_EMAIL:?LETSENCRYPT_EMAIL must be set when TLS_MODE=letsencrypt}"
    TLS_DIRECTIVE='tls {$LETSENCRYPT_EMAIL}'
    ;;
  selfsigned)
    TLS_DIRECTIVE="tls internal"
    ;;
  *)
    echo "caddy-entrypoint: TLS_MODE must be 'letsencrypt' or 'selfsigned', got '${TLS_MODE}'" >&2
    exit 1
    ;;
esac

sed "s|__TLS_DIRECTIVE__|${TLS_DIRECTIVE}|" /etc/caddy/Caddyfile.template > /etc/caddy/Caddyfile

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
```

- [ ] **Step 3: Write the Dockerfile**

Create `frontend/Dockerfile.caddy`. The builder stage is identical to `frontend/Dockerfile`'s, so the two images build from the same SPA output:

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
# Registry is overridable for networks that cannot reach registry.npmjs.org:
#   --build-arg NPM_REGISTRY=https://your-mirror/
ARG NPM_REGISTRY=https://registry.npmjs.org
RUN --mount=type=cache,target=/root/.npm     npm config set registry "$NPM_REGISTRY" &&     npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM caddy:2-alpine
COPY Caddyfile.template /etc/caddy/Caddyfile.template
COPY caddy-entrypoint.sh /usr/local/bin/caddy-entrypoint.sh
RUN chmod +x /usr/local/bin/caddy-entrypoint.sh
COPY --from=builder /app/dist /srv
EXPOSE 80 443
ENTRYPOINT ["/usr/local/bin/caddy-entrypoint.sh"]
```

- [ ] **Step 4: Validate both TLS_MODE variants statically**

Run (no build needed for this check — it renders the template exactly as the entrypoint would and validates the result):

```bash
cd frontend
for MODE in letsencrypt selfsigned; do
  echo "=== $MODE ==="
  if [ "$MODE" = "letsencrypt" ]; then DIRECTIVE='tls {$LETSENCRYPT_EMAIL}'; else DIRECTIVE="tls internal"; fi
  sed "s|__TLS_DIRECTIVE__|${DIRECTIVE}|" Caddyfile.template > /tmp/Caddyfile.$MODE
  docker run --rm -v /tmp/Caddyfile.$MODE:/etc/caddy/Caddyfile \
    -e DOMAIN=localhost -e LETSENCRYPT_EMAIL=test@example.com \
    caddy:2-alpine caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
done
```

Expected: `Valid configuration` for both.

- [ ] **Step 5: Build the image and test the self-signed path live**

```bash
cd frontend
docker build -f Dockerfile.caddy -t sentinel-frontend-caddy:test .
docker network create caddy-test-net
docker run -d --rm --name caddy-test-backend --network caddy-test-net \
  python:3-alpine sh -c "mkdir -p /www && echo hi > /www/marker.txt && cd /www && python3 -m http.server 3001"
docker run -d --rm --name caddy-test --network caddy-test-net --network-alias backend \
  -p 18443:443 -e DOMAIN=localhost -e TLS_MODE=selfsigned \
  sentinel-frontend-caddy:test
sleep 3
curl -sk --resolve localhost:18443:127.0.0.1 https://localhost:18443/health
curl -sk --resolve localhost:18443:127.0.0.1 https://localhost:18443/some/spa/route
curl -sk --resolve localhost:18443:127.0.0.1 https://localhost:18443/api/marker.txt
docker logs caddy-test --tail 20
docker rm -f caddy-test caddy-test-backend
docker network rm caddy-test-net
```

Expected: `/health` → `ok`. The SPA route → the built `index.html`'s content (confirms `try_files` fallback works against the real built SPA, not a placeholder file). `/api/marker.txt` → the proxy reaches `backend:3001` with the full path preserved (a 200 with `hi`, or, if timing races the fake backend's startup, a 502 naming `backend:3001` specifically — not `connection refused` to the wrong host/port, and not a 404 from the SPA fallback, which would mean the path didn't match the `/api/*` handle at all).

- [ ] **Step 6: Commit**

```bash
git add frontend/Caddyfile.template frontend/caddy-entrypoint.sh frontend/Dockerfile.caddy
git commit -m "feat(frontend): add a Caddy-based frontend image with automatic HTTPS"
```

---

### Task 2: The Caddy compose override

**Files:**
- Create: `docker-compose.caddy.yml`

**Interfaces:**
- Consumes: the `sentinel-frontend-caddy` image from Task 1 (by name; does not need the built image to exist locally to write this file, only to verify it with `docker compose config`, which doesn't require the image to be pullable).
- Produces: the override file `install.sh` (Task 3) points operators at when they choose Caddy mode.

- [ ] **Step 1: Write the override file**

Create `docker-compose.caddy.yml`:

```yaml
# Optional override: HTTPS served directly by Sentinel via Caddy, instead of
# nginx behind an external proxy (or no TLS at all). Opt in with:
#   docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d
# install.sh does this for you and remembers the choice by writing
# COMPOSE_FILE into .env, so a later plain `docker compose ...` still
# includes this file automatically.
services:
  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile.caddy
      args:
        NPM_REGISTRY: ${NPM_REGISTRY:-https://registry.npmjs.org}
    image: ${DOCKER_REGISTRY:-ghcr.io}/stevy2191/sentinel-frontend-caddy:${IMAGE_TAG:-latest}
    environment:
      # Required in both TLS_MODE values - see .env.example.
      DOMAIN: ${DOMAIN:?Set DOMAIN in .env to use Caddy HTTPS mode}
      TLS_MODE: ${TLS_MODE:-selfsigned}
      LETSENCRYPT_EMAIL: ${LETSENCRYPT_EMAIL:-}
    # Restates the full list: Compose replaces (not merges) a list-type key
    # like this one when an override restates it, so both ports must be
    # listed here even though :80 also appears in the base file.
    ports:
      - "${FRONTEND_PORT:-3000}:80"
      - "${HTTPS_PORT:-443}:443"
    # nginx.conf's healthcheck curls plain HTTP; this container only serves
    # HTTPS (Caddy's automatic HTTP->HTTPS redirect answers :80 with a
    # redirect, not a 200), so the check itself must switch to HTTPS too.
    healthcheck:
      test: ["CMD", "curl", "-fsk", "https://127.0.0.1/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s
    volumes:
      # Caddy's cert/state storage, so a recreated container reuses an
      # already-issued certificate instead of requesting a new one - Let's
      # Encrypt rate-limits issuance per domain.
      - caddy_data:/data

volumes:
  caddy_data:
```

- [ ] **Step 2: Verify the merge**

Run:

```bash
docker compose -f docker-compose.yml -f docker-compose.caddy.yml config
```

(Set dummy env vars first if needed: `DOMAIN=test.example.com DB_PASSWORD=x docker compose -f docker-compose.yml -f docker-compose.caddy.yml config`.)

Expected: the merged `frontend` service shows `image: .../sentinel-frontend-caddy:...`, both port mappings (`80` and `443`), the three new environment variables, `caddy_data` mounted at `/data`, and — inherited, not restated — the same `depends_on: backend: condition: service_healthy` the base file already has. The `backend` and `postgres` services are untouched.

- [ ] **Step 3: Commit**

```bash
git add docker-compose.caddy.yml
git commit -m "feat(deploy): add a Caddy HTTPS override for docker-compose"
```

---

### Task 3: `install.sh` prompt and `.env.example`

**Files:**
- Modify: `install.sh` (add a new prompt function; call it; extend the `.env` heredoc)
- Modify: `.env.example`

**Interfaces:**
- Consumes: nothing new from earlier tasks (this task only has to know the variable names `DOMAIN`/`TLS_MODE`/`LETSENCRYPT_EMAIL`/`HTTPS_PORT`, and the override file's path `docker-compose.caddy.yml`, both already fixed by Tasks 1-2).
- Produces: a `.env` containing `DOMAIN`/`TLS_MODE`/`LETSENCRYPT_EMAIL`/`HTTPS_PORT` (when Caddy mode is chosen) and `COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml`.

- [ ] **Step 1: Add HTTPS-related state variables**

Find (in `install.sh`, near the other top-of-file state variables):

```bash
# Host ports the stack binds (see docker-compose.yml). Frontend, backend, and
# postgres are env-backed (reassignable); adminer is fixed in compose.
FRONTEND_PORT=3000
BACKEND_PORT=3001
PORT_DB=5432
ADMINER_ENABLED=true
ADMINER_PORT=8080
```

Replace with:

```bash
# Host ports the stack binds (see docker-compose.yml). Frontend, backend, and
# postgres are env-backed (reassignable); adminer is fixed in compose.
FRONTEND_PORT=3000
BACKEND_PORT=3001
PORT_DB=5432
ADMINER_ENABLED=true
ADMINER_PORT=8080

# HTTPS mode (see prompt_for_https). COMPOSE_EXTRA_FILE is appended to
# COMPOSE_FILE in .env when set, so a later plain `docker compose ...`
# keeps using docker-compose.caddy.yml without the operator remembering -f.
HTTPS_MODE="none"
DOMAIN=""
TLS_MODE="selfsigned"
LETSENCRYPT_EMAIL=""
HTTPS_PORT=443
COMPOSE_EXTRA_FILE=""
```

- [ ] **Step 2: Add the prompt function**

Find:

```bash
# prompt_for_adminer asks whether to include the optional Adminer database admin
```

Replace with (adding the new function immediately before `prompt_for_adminer`):

```bash
# prompt_for_https asks how this install should serve HTTPS. Sets HTTPS_MODE
# (informational), and when Caddy is chosen: DOMAIN, TLS_MODE,
# LETSENCRYPT_EMAIL, HTTPS_PORT, and COMPOSE_EXTRA_FILE (which .env's
# COMPOSE_FILE line uses to make the choice stick for later commands).
prompt_for_https() {
  info "${BOLD}HTTPS${RESET}"
  info "  1) I'll put my own reverse proxy in front (no changes)"
  info "  2) Let Sentinel handle it automatically via Caddy"
  info "  3) No HTTPS - plain HTTP, for local testing"
  if ! read -r -p "Choose [1-3] (default 3): " HTTPS_ANSWER; then HTTPS_ANSWER="3"; fi
  case "${HTTPS_ANSWER:-3}" in
    1)
      HTTPS_MODE="proxy"
      ok "Using plain HTTP here; put your reverse proxy in front for TLS."
      ;;
    2)
      HTTPS_MODE="caddy"
      COMPOSE_EXTRA_FILE="docker-compose.caddy.yml"
      if ! read -r -p "  Domain name Sentinel will be reached at: " DOMAIN; then DOMAIN=""; fi
      while [ -z "$DOMAIN" ]; do
        err "A domain is required for Caddy mode, even for a self-signed cert."
        if ! read -r -p "  Domain name Sentinel will be reached at: " DOMAIN; then DOMAIN=""; fi
      done
      info "  a) Let's Encrypt - needs a real public domain and this server"
      info "     reachable from the internet on ports 80 and 443 right now"
      info "  b) Self-signed - works anywhere, browsers will warn it's untrusted"
      if ! read -r -p "  Choose [a/b] (default b): " LE_ANSWER; then LE_ANSWER="b"; fi
      case "${LE_ANSWER:-b}" in
        a|A)
          TLS_MODE="letsencrypt"
          if ! read -r -p "  Email for Let's Encrypt renewal notices: " LETSENCRYPT_EMAIL; then LETSENCRYPT_EMAIL=""; fi
          while [ -z "$LETSENCRYPT_EMAIL" ]; do
            err "Let's Encrypt requires a contact email."
            if ! read -r -p "  Email for Let's Encrypt renewal notices: " LETSENCRYPT_EMAIL; then LETSENCRYPT_EMAIL=""; fi
          done
          ;;
        *)
          TLS_MODE="selfsigned"
          ;;
      esac
      info "What port should HTTPS run on?"
      ask_port HTTPS_PORT "HTTPS" 443 "$FRONTEND_PORT"
      ok "Caddy will serve ${DOMAIN} (${TLS_MODE})."
      ;;
    *)
      HTTPS_MODE="none"
      ok "No HTTPS configured; plain HTTP only."
      ;;
  esac
  echo
}

# prompt_for_adminer asks whether to include the optional Adminer database admin
```

- [ ] **Step 3: Call the new prompt**

Find:

```bash
# ---- Port conflict check (before anything is started) ----
check_port_conflicts

# ---- Optional database admin tool ----
prompt_for_adminer
```

Replace with:

```bash
# ---- Port conflict check (before anything is started) ----
check_port_conflicts

# ---- HTTPS mode ----
prompt_for_https

# ---- Optional database admin tool ----
prompt_for_adminer
```

- [ ] **Step 4: Write the new variables into `.env`**

Find:

```bash
# Docker images
DOCKER_REGISTRY='ghcr.io'
IMAGE_TAG='latest'
ENV
```

Replace with:

```bash
# Docker images
DOCKER_REGISTRY='ghcr.io'
IMAGE_TAG='latest'

# HTTPS (Caddy mode only - see README). Empty COMPOSE_EXTRA_FILE means
# COMPOSE_FILE is just the base compose file, same as before this feature.
DOMAIN=$(env_quote "$DOMAIN")
TLS_MODE=$(env_quote "$TLS_MODE")
LETSENCRYPT_EMAIL=$(env_quote "$LETSENCRYPT_EMAIL")
HTTPS_PORT=$(env_quote "$HTTPS_PORT")
COMPOSE_FILE=$(env_quote "docker-compose.yml${COMPOSE_EXTRA_FILE:+:$COMPOSE_EXTRA_FILE}")
ENV
```

- [ ] **Step 5: Update `.env.example`**

Find:

```
# Docker Registry (for pulling published images)
DOCKER_REGISTRY=ghcr.io
IMAGE_TAG=latest
```

Replace with:

```
# Docker Registry (for pulling published images)
DOCKER_REGISTRY=ghcr.io
IMAGE_TAG=latest

# HTTPS via the optional Caddy frontend (see README's HTTPS section).
# Meaningless unless you also run with:
#   docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d
# (install.sh does this for you and sets COMPOSE_FILE below so plain
# `docker compose` commands keep including it afterward.)
#
# The hostname Caddy serves and requests a certificate for. Required in
# both TLS_MODE values - in selfsigned mode it does not need to resolve to
# anything real.
DOMAIN=
# letsencrypt | selfsigned. letsencrypt needs a real public domain and this
# server reachable from the internet on ports 80/443 during issuance.
TLS_MODE=selfsigned
# Required when TLS_MODE=letsencrypt - Let's Encrypt's subscriber agreement
# uses this for renewal/problem notices.
LETSENCRYPT_EMAIL=
HTTPS_PORT=443
# Uncomment to make Caddy mode stick for every later `docker compose` command
# in this directory, not just the one install.sh already ran:
# COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml
```

- [ ] **Step 6: Check the script's own syntax**

Run: `bash -n install.sh`
Expected: no output (a syntax error would print a line number and message).

- [ ] **Step 7: Commit**

```bash
git add install.sh .env.example
git commit -m "feat(install): prompt for an HTTPS mode, wire Caddy into install.sh"
```

---

### Task 4: CI build for the Caddy image

**Files:**
- Modify: `.github/workflows/docker-build.yml`

**Interfaces:**
- Consumes: `frontend/Dockerfile.caddy` (Task 1) as the build target.
- Produces: a `sentinel-frontend-caddy` image on GHCR, tagged the same way `sentinel-backend`/`sentinel-frontend` already are.

- [ ] **Step 1: Add the new job**

Find:

```yaml
  # Turns a pushed `vX.Y.Z` tag into an actual GitHub Release, which the
```

Replace with (inserting a new job immediately before the comment, after the existing `build-frontend` job):

```yaml
  build-frontend-caddy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Lowercase repository name
        run: echo "REPO_LC=${GITHUB_REPOSITORY,,}" >> "$GITHUB_ENV"

      - name: Log in to Container Registry
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata (tags, labels)
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.REPO_LC }}-frontend-caddy
          tags: |
            type=ref,event=branch
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=sha
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push Caddy frontend image
        uses: docker/build-push-action@v5
        with:
          context: ./frontend
          file: ./frontend/Dockerfile.caddy
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}

  # Turns a pushed `vX.Y.Z` tag into an actual GitHub Release, which the
```

- [ ] **Step 2: Add the new job to the release gate**

Find:

```yaml
  create-release:
    needs: [build-backend, build-frontend]
```

Replace with:

```yaml
  create-release:
    needs: [build-backend, build-frontend, build-frontend-caddy]
```

- [ ] **Step 3: Check the workflow file's YAML syntax**

Run:

```bash
docker run --rm -v "$(pwd)/.github/workflows/docker-build.yml":/f.yml python:3-alpine python3 -c "import yaml; yaml.safe_load(open('/f.yml'))"
```

Expected: no output (a YAML syntax error would raise and print a traceback).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/docker-build.yml
git commit -m "ci: build and publish the Caddy frontend image"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md`
- Modify: `frontend/nginx.conf` (one-line comment only)

**Interfaces:**
- Consumes: nothing (pure documentation of Tasks 1-4's already-fixed interface: `docker-compose.caddy.yml`, the four new env vars, `install.sh`'s new prompt).

- [ ] **Step 1: Add an HTTPS section to the README**

Find:

```markdown
### CI/CD

`.github/workflows/docker-build.yml` builds and pushes both images to GHCR on
every push to `main` and on `v*` tags. Pull requests build but do not push.
```

Replace with:

```markdown
### HTTPS

Three ways to serve Sentinel, in order of how much Sentinel itself does:

1. **Your own reverse proxy** (nginx, Traefik, Nginx Proxy Manager, etc.) in
   front, terminating TLS there. Nothing to configure here — the bundled
   frontend already serves plain HTTP and honors `X-Forwarded-Proto` for its
   security headers.
2. **No HTTPS.** Plain HTTP, the default — fine for local testing or a
   network you already trust.
3. **Caddy**, letting Sentinel own HTTPS itself:

   ```bash
   # In .env: DOMAIN=..., TLS_MODE=letsencrypt|selfsigned, LETSENCRYPT_EMAIL=...
   # (install.sh prompts for all of this)
   docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d
   ```

   `TLS_MODE=letsencrypt` gets a real, publicly-trusted certificate, but only
   works when `DOMAIN` is a real public domain and this server is reachable
   from the internet on ports 80 and 443 during issuance (the standard
   HTTP-01 challenge — there is no DNS-01 support, so an internal-only server
   with a real domain still can't use this mode). `TLS_MODE=selfsigned` uses
   Caddy's own internal CA instead: works anywhere, including entirely
   internal networks, at the cost of a browser warning until someone trusts
   the certificate manually. Either way, renewal is automatic — no cron job
   or extra container to maintain.

### CI/CD

`.github/workflows/docker-build.yml` builds and pushes all three images
(`sentinel-backend`, `sentinel-frontend`, `sentinel-frontend-caddy`) to GHCR
on every push to `main` and on `v*` tags. Pull requests build but do not push.
```

- [ ] **Step 2: Point to the alternative from nginx.conf**

Find:

```
  # This container itself only ever listens on plain HTTP (port 80) TLS, if
  # any, is terminated by an external reverse proxy the operator puts in
  # front. Strict-Transport-Security is only meaningful (and only honored by
```

Replace with:

```
  # This container itself only ever listens on plain HTTP (port 80) TLS, if
  # any, is terminated by an external reverse proxy the operator puts in
  # front. (An operator who wants Sentinel to own HTTPS itself instead can
  # use docker-compose.caddy.yml, a separate Caddy-based image - see the
  # README's HTTPS section - rather than this one.)
  # Strict-Transport-Security is only meaningful (and only honored by
```

- [ ] **Step 3: Commit**

```bash
git add README.md frontend/nginx.conf
git commit -m "docs: document the three HTTPS modes"
```

---

### Task 6: Full verification

**Files:** none (verification only).

- [ ] **Step 1: The unchanged nginx path still works**

Run: `DB_PASSWORD=x docker compose config` (base file alone, no override — `DB_PASSWORD` is the one variable `docker-compose.yml` requires even just to render config; no real `.env` needed for this check) and confirm the `frontend` service is completely unchanged from before this plan — still the nginx image, still just port `${FRONTEND_PORT:-3000}:80`, no new environment variables. This is the regression check for the two modes that were never supposed to change.

- [ ] **Step 2: The Caddy path, end to end, against the real backend and a real DOMAIN**

**Check first whether another Sentinel stack is already running on this
host** (`docker ps --format '{{.Names}}' | grep -i sentinel`). `docker-compose.yml` hardcodes `container_name: sentinel-postgres` /
`sentinel-backend` / `sentinel-frontend`, so bringing this test up with a
plain `docker compose up` while another instance is already running under
those same names will collide with it (and `-p <project>` alone does not
prevent that — an explicit `container_name` is never namespaced by the
project). If anything is already running, use a throwaway override file to
rename this test's containers, on top of a distinct project name for
volume/network isolation:

```bash
cat > /tmp/caddy-verify-override.yml <<'EOF'
services:
  postgres: { container_name: sentinel-verify-postgres }
  backend:  { container_name: sentinel-verify-backend }
  frontend: { container_name: sentinel-verify-frontend }
EOF
cp .env.example .env.caddytest
{
  echo 'DB_PASSWORD="test-password-1234"'
  echo 'JWT_SECRET="test-jwt-secret"'
  echo 'DOMAIN="localhost"'
  echo 'TLS_MODE="selfsigned"'
  echo 'HTTPS_PORT="18443"'
  echo 'FRONTEND_PORT="18080"'
  echo 'BACKEND_PORT="18081"'
  echo 'DB_PORT="18432"'
} >> .env.caddytest
mv .env .env.bak 2>/dev/null || true
cp .env.caddytest .env
docker compose -p sentinel-verify -f docker-compose.yml -f docker-compose.caddy.yml -f /tmp/caddy-verify-override.yml up -d --build
sleep 15
docker compose -p sentinel-verify -f docker-compose.yml -f docker-compose.caddy.yml -f /tmp/caddy-verify-override.yml ps
curl -sk --resolve localhost:18443:127.0.0.1 https://localhost:18443/health
curl -sk --resolve localhost:18443:127.0.0.1 https://localhost:18443/api/v1/public/status/nonexistent
docker compose -p sentinel-verify -f docker-compose.yml -f docker-compose.caddy.yml -f /tmp/caddy-verify-override.yml down -v
rm -f .env .env.caddytest /tmp/caddy-verify-override.yml
mv .env.bak .env 2>/dev/null || true
```

If nothing else is running, the plain form from Task 6's earlier steps
(`docker compose -f docker-compose.yml -f docker-compose.caddy.yml ...`,
no `-p` or rename override needed) is simpler and fine to use instead.

Expected: all three containers (postgres, backend, frontend) healthy; `/health` returns `ok`; `/api/v1/public/status/nonexistent` returns a real JSON response from the backend (a 404 with a JSON body, not an HTML error page or a raw connection failure) — confirming the proxy reaches the real backend service, not just the throwaway fake one from Task 1.

- [ ] **Step 3: The Let's Encrypt path gets static verification only**

This environment has no publicly-routable domain, so real issuance cannot be tested. Confirm instead: `frontend/caddy-entrypoint.sh` with `TLS_MODE=letsencrypt` and a set `LETSENCRYPT_EMAIL` renders a `Caddyfile` containing the literal text `tls {$LETSENCRYPT_EMAIL}` (already covered by Task 1 Step 4's `caddy validate` run — re-run it here as part of full verification, not a new check). State explicitly in this task's completion notes that Let's Encrypt issuance itself remains unverified end-to-end, so this is not later mistaken for a fully tested path.

- [ ] **Step 4: Review the full diff**

Run: `git log --oneline <first commit of this plan>~1..HEAD` and `git diff <first commit of this plan>~1..HEAD --stat`
Expected: exactly the files named across Tasks 1-5 appear, nothing unexpected.

---

### Task 7: Push to `dev`

**Files:** none.

- [ ] **Step 1: Push**

```bash
git push origin dev
```

- [ ] **Step 2: Confirm CI succeeded, including the new job**

```bash
gh run list --limit 1 --branch dev
gh run view --job "$(gh run list --limit 1 --branch dev --json databaseId -q '.[0].databaseId')" 2>/dev/null || true
```

Expected: the "Build and Push Docker Images" run is `completed`/`success`, and its job list includes `build-frontend-caddy` alongside `build-backend`/`build-frontend`, all green.

- [ ] **Step 3: Stop here — do not merge to `main` or tag a release**

There is no production deployment currently tracking this repository (the prior `/srv/docker/Sentinel` production install was intentionally torn down and rebuilt as a `dev`-branch test deployment; a real production VM tracking `main` does not exist yet). Merging `dev` into `main` and cutting a version tag is a separate decision for whoever sets up that production VM to make when they're ready — not an automatic last step of this plan.

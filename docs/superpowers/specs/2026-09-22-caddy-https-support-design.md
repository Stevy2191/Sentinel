# Automatic HTTPS via an optional Caddy frontend

## Problem

Sentinel's frontend container (nginx) only ever serves plain HTTP. Its own
config already assumes TLS, if any, is terminated by something else in front
of it — the comment at the top of `frontend/nginx.conf` says so outright, and
the security-header logic (HSTS, `X-Forwarded-Proto`) is already written to
cooperate with an external proxy. That is a reasonable default, but there is
no option today for an operator who wants Sentinel to own HTTPS itself
without standing up a separate reverse proxy.

## Scope

Three deployment modes, of which only the third is new:

1. **Bring your own reverse proxy.** The operator already runs (or will run)
   something like nginx, Traefik, or Nginx Proxy Manager in front of
   Sentinel, terminating TLS there. Sentinel's own frontend container keeps
   serving plain HTTP behind it, exactly as today. **No code changes** — this
   already works.
2. **No HTTPS.** Plain HTTP, for local testing or a network the operator
   already trusts. **No code changes** — this is the current default.
3. **Caddy.** Sentinel's frontend container itself becomes a Caddy instance
   with automatic HTTPS: a real Let's Encrypt certificate when the operator
   has a public domain (HTTP-01 challenge), or a locally-trusted-only
   self-signed certificate via Caddy's internal CA otherwise. **This is the
   new work.**

Explicitly out of scope for this iteration:
- DNS-01 challenge support (would let an internal-only server still get a
  real Let's Encrypt cert via DNS API credentials). Deferred: it requires
  picking and integrating a specific DNS provider's API, meaningfully more
  complex than the HTTP-01-only path, and internal-only deployments are
  already served by the self-signed option.
- Any change to `frontend/nginx.conf` or the existing `sentinel-frontend`
  image. Modes 1 and 2 already work through it unchanged.
- End-to-end testing of real Let's Encrypt issuance. This environment has no
  publicly-routable domain to test against; see Testing below.

## Design

### How the three modes plug into deployment

- `docker-compose.yml` is unchanged. Used alone, it *is* modes 1 and 2 — the
  existing nginx-based `frontend` service, plain HTTP on
  `${FRONTEND_PORT:-3000}:80`.
- A new, optional override file, `docker-compose.caddy.yml`, redefines only
  the `frontend` service: a different image/build (Caddy-based, see below),
  and both port 80 (needed for the HTTP-01 challenge and the HTTP→HTTPS
  redirect) and 443 mapped. Compose overrides replace list-type keys
  (`ports`, `volumes`) wholesale rather than merging them, so this file
  restates the full `ports` list rather than appending to it.
- Choosing Caddy is opt-in: `docker compose -f docker-compose.yml -f
  docker-compose.caddy.yml up -d`. To make that choice stick for every later
  plain `docker compose ...` command (not just the first one `install.sh`
  runs), `install.sh` writes `COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml`
  into `.env` when the operator picks Caddy mode. Compose reads this special
  variable from `.env` the same way it already reads `COMPOSE_PROFILES` for
  the Adminer toggle (see `install.sh`'s existing comment on that mechanism)
  — no wrapper script needed, and every future `docker compose pull` /
  `up` / `ps` in this directory automatically includes both files.
- New env vars, meaningful only in Caddy mode:
  - `DOMAIN` — the hostname Caddy serves and requests a certificate for.
    Required in both `TLS_MODE` values: Caddy's config needs one site address
    either way. In self-signed mode it does not need to resolve to anything
    real — it can be the box's LAN hostname or IP, it is simply the name the
    self-signed cert is issued for.
  - `TLS_MODE` — `letsencrypt` or `selfsigned`.
  - `LETSENCRYPT_EMAIL` — required by Let's Encrypt's subscriber agreement
    for expiry/problem notices; only meaningful when `TLS_MODE=letsencrypt`.
  - `HTTPS_PORT` — host port for 443, defaulting to `443` (mirrors the
    existing `FRONTEND_PORT`/`BACKEND_PORT` pattern).

### The Caddy image

- New `frontend/Dockerfile.caddy`: the same npm build stage
  `frontend/Dockerfile` already uses (producing `dist/`), but the final stage
  is `FROM caddy:2-alpine` instead of `nginx:alpine`, copying in `dist/`, a
  new `frontend/Caddyfile.template`, and a small entrypoint script.
- Published as its own image, `sentinel-frontend-caddy`, tagged the same way
  the existing images are (`latest` on the default branch, `dev` on the dev
  branch, semver on release tags, `sha-<short>` always) — a new
  `build-frontend-caddy` job in `.github/workflows/docker-build.yml`,
  mirroring `build-frontend`.
- New `frontend/Caddyfile.template`, mirroring `nginx.conf`'s actual routes
  and headers so nothing regresses relative to the nginx-based mode:
  - `/health` — a liveness endpoint answering directly, not falling through
    to the SPA (same reason nginx.conf's `/health` location exists: the
    compose healthcheck must not pass just because the SPA fallback
    returned `index.html` for everything).
  - `/scripts/*` and `/agent/*` — reverse-proxied to `backend:3001`,
    unbuffered, for the agent installer script and binary downloads.
  - `/api/*` — reverse-proxied to `backend:3001`, preserving the upgrade/
    connection headers the existing proxy uses.
  - Everything else — serves the SPA's static files, falling back to
    `index.html` for client-side routing.
  - The same security headers nginx.conf sends: CSP, `X-Frame-Options`,
    `X-Content-Type-Options`, `Referrer-Policy`, `Strict-Transport-Security`
    (unconditional here, since Caddy mode *is* the TLS termination point,
    unlike nginx.conf's conditional version which only sends it when an
    external proxy said the original request was HTTPS).
- The one piece of real logic: which `tls` directive Caddy uses is not a
  value that can be filled into a placeholder, it is a different directive
  entirely (`tls {$LETSENCRYPT_EMAIL}` for Let's Encrypt's ACME flow, vs.
  `tls internal` for Caddy's own internal CA). The entrypoint script picks
  the matching snippet based on `TLS_MODE`, renders the final `Caddyfile`
  from the template, then execs `caddy run --config /etc/caddy/Caddyfile
  --adapter caddyfile`.
- Certificate persistence and renewal: a new named volume (`caddy_data`,
  mounted at Caddy's default `/data`) so issued certificates survive a
  container recreation instead of being re-requested every deploy (Let's
  Encrypt rate-limits issuance per domain). Renewal itself needs no cron or
  sidecar — Caddy renews both Let's Encrypt and its internal certs
  automatically in the background.

### `install.sh`

A new prompt after the existing port questions (`check_port_conflicts`) and
before the Adminer prompt:

> **How should Sentinel serve HTTPS?**
> 1. I'll put my own reverse proxy in front (no changes)
> 2. Let Sentinel handle it automatically via Caddy
> 3. No HTTPS — plain HTTP, for local testing

Choices 1 and 3 set nothing new; the generated `.env` and the final
`$COMPOSE up` call look exactly as they do today. Choice 2 additionally asks:

- The domain name Sentinel will be reached at (`DOMAIN`).
- Let's Encrypt or self-signed (`TLS_MODE`), with the Let's Encrypt option's
  prompt explicitly stating its requirement: a real public domain and the
  server reachable from the internet on ports 80 and 443 during issuance.
- An email address (`LETSENCRYPT_EMAIL`), only when Let's Encrypt was chosen.

`install.sh` then writes `DOMAIN`/`TLS_MODE`/`LETSENCRYPT_EMAIL`/`HTTPS_PORT`
into `.env` alongside the existing values, sets
`COMPOSE_FILE=docker-compose.yml:docker-compose.caddy.yml` in `.env` when
Caddy mode was chosen, and the existing final `$COMPOSE $PROFILE_FLAGS up -d
--build` call at the end of the script needs no change itself — `COMPOSE_FILE`
being in `.env` is enough for `docker compose` to pick up both files.

### Documentation

- `.env.example` gets the four new variables documented in the same style as
  the existing ones (a comment explaining what each does and when it's
  needed), all commented out with sensible notes since they are meaningless
  outside Caddy mode.
- README gets a new section covering all three modes, stating the Let's
  Encrypt HTTP-01 caveat explicitly: it requires a real public domain and
  the server reachable from the internet on ports 80/443 during issuance;
  internal-only deployments should use self-signed instead.
- `frontend/nginx.conf`'s existing top-of-file comment gets one line added
  noting the Caddy alternative exists, so a future reader of that file isn't
  left thinking it's the only option.

## Testing

- `frontend/Caddyfile.template`: both rendered variants (`letsencrypt` and
  `selfsigned`) validate with `caddy validate`.
- The entrypoint script's branch logic is covered directly (given
  `TLS_MODE=selfsigned` vs `letsencrypt`, the correct `tls` directive ends up
  in the rendered file; an unrecognized `TLS_MODE` fails fast with a clear
  error rather than silently defaulting).
- The self-signed path is tested end-to-end: bring up the Caddy image for
  real, confirm it serves HTTPS on 443 with Caddy's internal CA, and that
  every proxied route (`/health`, `/api/*`, `/scripts/*`, `/agent/*`, the SPA
  fallback) behaves identically to the nginx-based image today.
- The Let's Encrypt path gets static verification only (Caddyfile syntax,
  correct directive selection, docker build success) — this environment has
  no publicly-routable domain to actually request a certificate against.
  This limitation is called out explicitly in the plan's verification step
  so it is not mistaken for an end-to-end-tested path.
- `docker-compose.caddy.yml` merged with `docker-compose.yml` is verified
  with `docker compose -f docker-compose.yml -f docker-compose.caddy.yml
  config` to confirm the override actually replaces the `frontend` service
  as intended (image, ports, environment) rather than merging in a way that
  leaves stale nginx-oriented values behind.

## Release

Ships as a feature release once implemented and verified, following the
same tag-and-deploy process used for prior features (this repo's `dev` →
`main` flow, then a version tag).

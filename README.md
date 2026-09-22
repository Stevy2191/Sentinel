# Sentinel

**Advanced Uptime Monitoring with Reporting**

Sentinel is a lightweight, self-hosted uptime monitoring system with a focus on
advanced historical reporting and analytics. Built for DevOps teams and small
businesses, it delivers the uptime insights you need while keeping resource
consumption minimal thanks to a compiled **Go** backend.

---

## Key Features

- 📊 **Advanced historical reporting** with custom date ranges, saved report definitions, scheduled delivery, and PDF/CSV export
- ✅ **Uptime percentage tracking** and SLA compliance monitoring
- 🔔 **Multi-channel notifications** — Email, ntfy, Slack, Discord, Telegram, and custom webhooks
- 🌐 **Public shareable status pages** for your users and stakeholders
- 🖥️ **Server agents** — install a lightweight agent on a host to report CPU, memory, disk, and Docker container status
- 🔒 **SSL certificate monitoring** and domain registration/expiry tracking
- 🔎 **Network discovery** to find and add monitors from your local network
- 👥 **User accounts, roles, and invitations**, with optional TOTP two-factor authentication and an audit log of admin actions
- 💾 **One-click database backup and restore**
- 🔌 **RESTful API** for integrations and automation
- 🪶 **Lightweight Go backend** with minimal RAM usage
- 🐳 **Docker-first deployment**

---

## Tech Stack

| Layer     | Technology   |
| --------- | ------------ |
| Backend   | Go           |
| Frontend  | React        |
| Database  | PostgreSQL   |
| Packaging | Docker       |

---

## Quick Start

Get Sentinel running in three steps:

```bash
# 1. Clone the repository
git clone https://github.com/Stevy2191/Sentinel.git
cd Sentinel

# 2. Run the installer
./install.sh
```

Then **open [http://localhost:3000](http://localhost:3000)** (or whatever
`FRONTEND_PORT` you chose) in your browser and create the first admin account.
Sentinel requires sign-in — the first account created bootstraps the admin;
after that, self-registration stays closed until an admin opens it under
**Settings -> Security**.

---

## Installation

For detailed installation instructions — including manual setup, environment
configuration, and production deployment — see
**[GETTING_STARTED.md](GETTING_STARTED.md)**.

---

## API

Sentinel exposes a RESTful API (base URL `/api/v1`) for managing monitors,
retrieving reports, and integrating with your existing tooling. Requests
require a Bearer JWT obtained from `/api/v1/auth/login` (or a scoped API
token). There is no published API reference yet; see `backend/internal/api/`
for the route handlers.

---

## Notifications

Sentinel supports Email, ntfy, Slack, Discord, Telegram, and custom webhooks.
Channels are configured per-instance under **Settings -> Notifications**; see
**[GETTING_STARTED.md](GETTING_STARTED.md#setting-up-notifications-optional)**
for configuration details for each channel.

---

## Supported Monitors

Sentinel can monitor a wide range of services:

- **HTTP/HTTPS** — endpoint availability, status codes, and response times
- **TCP** — port connectivity checks
- **Ping** — ICMP host reachability
- **DNS** — record resolution monitoring
- **Webhooks** — inbound heartbeat / push monitoring

---

## Screenshots

_Screenshots coming soon._

<!-- TODO: Add dashboard, reporting, and status page screenshots here. -->

---

## Deployment (Docker Compose)

### Prerequisites

- Docker Engine 20.10+
- Docker Compose v2+

### Local deployment

```bash
git clone https://github.com/Stevy2191/Sentinel.git
cd Sentinel

# Configure environment
cp .env.example .env
# Edit .env (at minimum set DB_PASSWORD; add SMTP/Slack/etc. as needed)

# Build and start all services
docker compose up -d --build
```

Services:

| Service  | URL                              | Notes                          |
| -------- | -------------------------------- | ------------------------------ |
| Web UI   | http://localhost:3000            | Frontend (nginx)               |
| Backend  | http://localhost:3001/api/v1     | REST API                       |
| Adminer  | http://localhost:8080            | Database admin (server: `postgres`) |

The frontend reaches the API through nginx (relative `/api`), which proxies to
the backend container — no CORS or API URL configuration needed.

> **Ports:** the frontend is published on host port `3000` (override with
> `FRONTEND_PORT` in `.env`) and the backend on `3001` (`BACKEND_PORT`). If a
> port is already in use, set e.g. `FRONTEND_PORT=3005` in `.env` (or use
> `install.sh`, which prompts for both).

### Using published images

Once the CI has pushed images to GHCR, you can pull instead of building:

```bash
docker compose pull
docker compose up -d
```

Images are published to:

- `ghcr.io/stevy2191/sentinel-backend:latest`
- `ghcr.io/stevy2191/sentinel-frontend:latest`

For private images, authenticate first: `docker login ghcr.io` (GitHub username +
a personal access token with `read:packages`).

### Logs, update, teardown

```bash
docker compose logs -f backend      # follow logs
docker compose pull && docker compose up -d   # update to latest images
docker compose down                 # stop
docker compose down -v              # stop and DELETE the database volume
```

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

---

## Contributing

Contributions are welcome! Open an issue to discuss a change before starting
significant work, and open pull requests against `main`.

---

## License

Sentinel is licensed under the **GNU Affero General Public License v3.0
(AGPL-3.0)**. See the [LICENSE](LICENSE) file for details. Notably, the AGPL
requires that if you run a modified version of Sentinel as a network service,
you make the modified source available to its users.

---

Maintained by **Stevy2191**.

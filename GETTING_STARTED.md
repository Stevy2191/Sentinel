# Getting Started with Sentinel

## What is Sentinel?

Sentinel is a lightweight, self-hosted uptime monitoring system. It continuously
monitors your services (websites, APIs, TCP ports, DNS, hosts) and sends alerts
when they go down.

## Prerequisites

- Docker and Docker Compose (v2) installed
- 2GB RAM minimum
- A stable internet connection
- A GitHub account (optional, for pulling pre-built images)

## Installation (5 minutes)

### 1. Clone the Repository

```bash
git clone https://github.com/Stevy2191/Sentinel.git
cd Sentinel
```

### 2. Configure Environment

```bash
cp .env.example .env
# Edit .env with your settings
nano .env
```

Key settings to configure:

- `DB_PASSWORD` — set a strong database password
- `JWT_SECRET` — a random secret of at least 32 characters (`openssl rand
  -base64 48`) that signs login sessions. If left unset, Sentinel generates a
  new one on every restart, which logs everyone out each time the container
  restarts — set it explicitly for anything beyond a quick local test.
- `FRONTEND_PORT` — host port for the web UI (default `3000`; change if it's in use)
- `BACKEND_PORT` — host port for the API (default `3001`)
- `TIMEZONE` — your timezone (e.g. `America/Chicago`)
- `ENVIRONMENT` — `production` or `development`
- `REGISTRATION_ENABLED` — `false` (default) closes self-registration after the
  first admin account is created; an admin can re-open it under **Security**
- `ADMINER_PORT` / `COMPOSE_PROFILES` — the optional Adminer database UI. Keep
  `COMPOSE_PROFILES=adminer` to run it (on `ADMINER_PORT`, default `8080`); set
  `COMPOSE_PROFILES=` (empty) to skip it. `install.sh` prompts for this.
- `SMTP_*` (optional) — system email for user invitations and scheduled reports,
  and the seed for an email alert channel. Other alert channels are added in the
  app; see [Setting Up Notifications](#setting-up-notifications-optional)

### 3. Start Sentinel

```bash
# Build the images locally and start everything
docker compose up -d --build
```

Once pre-built images are published to the registry, you can pull instead of
building:

```bash
docker compose pull
docker compose up -d
```

Sentinel will:

- Create and initialize the database (migrations run automatically on startup)
- Start the monitoring loop
- Serve the web UI

### 4. Access Sentinel

Open your browser and go to: **http://localhost:3000**

You'll land on the sign-in screen. Since no account exists yet, click through
to **register** and create the first account — it always succeeds regardless
of the `REGISTRATION_ENABLED` setting, and becomes the instance's first admin.
After that, self-registration is closed by default (`REGISTRATION_ENABLED=false`):
new users are added by an admin invitation (**Settings -> Users**) unless an
admin explicitly re-opens self-registration under **Settings -> Security**.

> Sentinel requires sign-in for the admin UI, but has no additional
> network-level access control of its own. **Do not expose it directly to the
> public internet** without a reverse proxy / VPN in front of it, since a
> compromised or weak admin password is then your only line of defense. (The
> public status pages under `/public/status/...` are the only pages meant to
> be shared with the outside world.)

## Your First Monitor (2 minutes)

1. **Click "Monitors"** in the sidebar
2. **Click "Create New Monitor"**
3. **Fill in the form:**
   - Name: `My Website`
   - Type: HTTP
   - URL: `https://example.com`
   - Check interval: 60 seconds
   - Timeout: 10 seconds
4. **Click "Create Monitor"**

Sentinel starts checking your site on its interval. Open the **Dashboard** to see
live status, and the monitor's detail page for uptime, response time, and
incident history.

## Setting Up Notifications (Optional)

Alert channels are configured in the app, under **Settings -> Notifications**.
You can add as many as you like, including several of the same type — separate
ntfy topics for different teams, say, or one Slack channel for production and
another for staging.

1. Open **Settings -> Notifications** and click **Add Channel**
2. Pick the type and fill in its details:

   | Channel  | What you need                                              |
   | -------- | ---------------------------------------------------------- |
   | Email    | SMTP host, port, username, password, and a from address     |
   | Slack    | An [incoming webhook](https://api.slack.com/messaging/webhooks) URL |
   | Discord  | A channel webhook URL                                       |
   | Telegram | A bot token and a chat ID                                   |
   | ntfy     | A topic, and a server URL if self-hosting                   |
   | Webhook  | Any URL that accepts a POST                                 |

3. Save, then click **Test** to send yourself a test alert

Each monitor then chooses which of those channels it uses, in its own
**Notifications** section — so a noisy staging check need not wake anyone.
A monitor with notifications switched off still records incidents; it just does
not alert.

Two channels cannot point at the same destination. Adding one that duplicates
an existing channel is refused, naming the one already there, because the two
would deliver every alert twice.

### A note for older installs

Channels used to be configured with environment variables instead
(`SLACK_WEBHOOK_URL`, `NTFY_TOPIC`, and so on). Those are still read on first
run and imported as ordinary channels named "... (from env)", so upgrading
changes nothing and you can edit or delete them like any other.

They are no longer the documented route: a single variable per type cannot
describe more than one channel of that type, and setting one in `.env` and then
adding the same channel in the app left two channels on one destination.

`SMTP_*` is the exception and stays in `.env`. Beyond seeding an email channel,
Sentinel uses it directly for system email — user invitations and scheduled
report delivery — which is not tied to any alert channel.

## Viewing Reports

1. Go to **Reports**
2. Choose a date range (last 7 / 30 / 90 days, or custom)
3. Explore the two tabs:
   - **Timeline** — uptime and response-time trends for a single monitor
   - **Summary** — compare uptime across all your monitors

Great for SLA tracking and performance analysis. You can export either view as
CSV.

## Public Status Page

Share your system status publicly without exposing the admin UI:

1. Go to **Status Pages** → **Create New Status Page**
2. Fill in a slug, name, description, and theme color
3. Open the page's **Manage** view and **Add Monitor** for each service to show
4. Make sure it's **Published**
5. Share the public URL with your users

Public URL: `http://localhost:3000/public/status/{page-slug}`

Unpublished pages return "not available", so drafts stay private.

## Monitoring a Server (Optional)

Beyond HTTP/TCP/ping/DNS/webhook checks, Sentinel can install a small agent on
a host to report its own system metrics (CPU, memory, disk, Docker containers):

1. Go to **Server Monitoring** → **Add Server Agent**
2. Copy the generated install command and run it on the target host
3. The agent registers itself and starts reporting; its page shows live
   system stats alongside any monitors you point at it

## SSL Certificate & Domain Monitoring (Optional)

Under **SSL & Domains**, add a hostname to track its certificate's expiry and
validity, or a domain to track its registration/expiry via RDAP — useful for
catching a lapsed certificate or an about-to-expire domain before it takes a
service down.

## Updating Sentinel

To update once new images are published:

```bash
cd Sentinel
docker compose pull
docker compose up -d
```

Your data is preserved (it lives in the `postgres_data` volume). If you build
from source instead, use `docker compose up -d --build` after pulling the latest
code with `git pull`.

## Next Steps

- **Deploy on another server** — see the [Deployment guide](README.md#deployment-docker-compose)
- **Explore the REST API** to build integrations (base URL `/api/v1`)
- Review the environment reference in [`.env.example`](.env.example)

## Need Help?

- Search [GitHub Issues](https://github.com/Stevy2191/Sentinel/issues)
- Open a new issue with your question or bug report

## Key Concepts

- **Monitor** — a service being watched (HTTP, TCP, ping, DNS, or webhook)
- **Check** — a single test execution and its result
- **Incident** — a downtime period (opened when a monitor goes offline, closed on recovery)
- **Report** — historical uptime and response-time analysis
- **Status Page** — a public-facing dashboard of selected monitors
- **Notification** — an alert sent when a monitor changes state (email, Slack, …)
- **Server Agent** — a small binary installed on a host that reports its
  system metrics (CPU, memory, disk, Docker containers) back to Sentinel

Happy monitoring! 🎉

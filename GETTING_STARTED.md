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

> **No login required.** Sentinel does not currently include built-in
> authentication — the UI opens directly. **Do not expose it directly to the
> public internet.** Run it on a private network, or place it behind a reverse
> proxy / VPN that provides authentication. (The public status pages under
> `/public/status/...` are the only pages meant to be shared.)

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

Happy monitoring! 🎉

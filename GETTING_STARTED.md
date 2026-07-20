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
- Notification channels (optional): `SMTP_*`, `SLACK_WEBHOOK_URL`, etc. — see
  [Setting Up Notifications](#setting-up-notifications-optional) below

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

Sentinel can alert you via **email, Slack, Discord, Telegram, ntfy, or custom
webhooks**.

> **Channels are configured with environment variables**, not in the web UI.
> Set the relevant variables in your `.env` file, then restart the backend
> (`docker compose up -d`). Configured channels then appear as **Enabled** on the
> **Notifications** page, where you can send a **Test** alert.

### Email Alerts

1. In `.env`, set your SMTP details:
   ```env
   SMTP_HOST=smtp.gmail.com
   SMTP_PORT=587
   SMTP_USER=your-email@gmail.com
   SMTP_PASSWORD=your-app-password   # an app password, not your login password
   SMTP_FROM=alerts@example.com
   ```
2. Apply the change: `docker compose up -d`
3. Open the **Notifications** page — "Email" should now show **Enabled**
4. Click **Test** to send yourself a test alert
5. When a monitor goes down, you'll get an email automatically

### Slack Alerts

1. Create an [incoming webhook](https://api.slack.com/messaging/webhooks) in your
   Slack workspace and copy its URL
2. In `.env`, set `SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...`
3. Apply: `docker compose up -d`
4. Open the **Notifications** page and click **Test** on Slack

The same pattern applies to the other channels:

| Channel  | Environment variable(s)                        |
| -------- | ---------------------------------------------- |
| Discord  | `DISCORD_WEBHOOK_URL`                           |
| Telegram | `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`        |
| ntfy     | `NTFY_URL` (default `https://ntfy.sh`), `NTFY_TOPIC` |
| Webhook  | `WEBHOOK_URL`                                   |

When a monitor transitions to **down** (or recovers), Sentinel sends an alert to
every configured channel automatically.

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

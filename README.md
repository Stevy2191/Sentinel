# Sentinel

**Advanced Uptime Monitoring with Reporting**

Sentinel is a lightweight, self-hosted uptime monitoring system with a focus on
advanced historical reporting and analytics. Built for DevOps teams and small
businesses, it delivers the uptime insights you need while keeping resource
consumption minimal thanks to a compiled **Go** backend.

---

## Key Features

- 📊 **Advanced historical reporting** with custom date ranges
- ✅ **Uptime percentage tracking** and SLA compliance monitoring
- 🔔 **Multi-channel notifications** — Email, ntfy, Slack, Discord, Telegram, and custom webhooks
- 🌐 **Public shareable status pages** for your users and stakeholders
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
cd sentinel

# 2. Run the installer
./install.sh
```

Then **open [http://localhost](http://localhost)** in your browser and complete
the setup wizard.

---

## Installation

For detailed installation instructions — including manual setup, environment
configuration, and production deployment — see
**[docs/INSTALLATION.md](docs/INSTALLATION.md)**.

---

## API Documentation

Sentinel exposes a RESTful API for managing monitors, retrieving reports, and
integrating with your existing tooling. See **[docs/API.md](docs/API.md)** for
the full reference.

---

## Notifications

Sentinel supports Email, ntfy, Slack, Discord, Telegram, and custom webhooks.
For configuration details for each channel, see
**[docs/NOTIFICATIONS.md](docs/NOTIFICATIONS.md)**.

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

## Contributing

Contributions are welcome! Please read **[CONTRIBUTING.md](CONTRIBUTING.md)**
before opening an issue or pull request.

---

## License

Sentinel is licensed under the **MIT License**. See the [LICENSE](LICENSE) file
for details.

---

Maintained by **Stevy2191**.

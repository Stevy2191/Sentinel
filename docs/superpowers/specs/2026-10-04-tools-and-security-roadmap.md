# Tools and security roadmap

Status: roadmap, agreed with the owner on 2026-10-04. Each phase below gets
its own brainstorm, spec and plan before any code is written; nothing here is
designed yet.

## Purpose

Sentinel monitors hosts, servers and the network, but it cannot yet *ask*
anything of them on demand or show what they expose. This track adds
interactive network tools, nmap, attack-surface scanning and traffic
analysis. Some of it is network-wide (flows, scans of subnets); some of it is
about servers (tools run from an agent, what a server listens on, its
traffic). It runs alongside the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`), so its phases are numbered
**S1–S6** to stay distinct from network phases 0–7.

What the owner asked for, and the answers that shaped it:

- Tools against a host: ping, traceroute and port scan, plus full nmap.
- Attack surface scanning: open ports, exposed services and a vulnerability
  overview.
- Live traffic analysis, meaning all three of: flow analysis from switches
  and routers, per-server traffic from the agents, and packet capture.
- Tools run from **both** the Sentinel server and any server agent.

## Guardrails (every phase)

These tools can probe and capture, so they share one set of rules:

- **Who:** admins only, or a new "network tools" permission granted
  explicitly; never the default for ordinary users.
- **Where:** a target allowlist of subnets and hosts. Anything outside it is
  refused, so Sentinel can never be pointed at networks the owner does not
  run.
- **How often:** per-user and per-target rate limits, and a cap on concurrent
  runs.
- **Record:** every run is an audit entry: who, what tool, which target, from
  where (Sentinel or which agent), when, and the outcome.
- **Agents opt in:** an agent runs tools only when its server has been
  enabled for it.

## Phases

### S1 — Network tools

- On-demand **ping**, **traceroute** with per-hop loss and latency (MTR
  style), **DNS lookup**, and a **TCP port check / quick port scan**.
- Run from the Sentinel server or from any enabled server agent ("traceroute
  from the file server").
- Results stream live to the browser while the tool runs.
- **Agent job channel.** Agents only push today; nothing can ask an agent to
  run something. S1 adds a pull-based channel: the agent collects pending tool
  jobs on its heartbeat and posts results back. No inbound port opens on any
  server.
- **Streaming.** Network phase 6 plans Sentinel's first push channel (SSE).
  Whichever of S1 and phase 6 comes first builds it; the other reuses it.

Depends on: nothing.

### S2 — nmap

- nmap bundled in the backend image; used on agents where nmap is installed.
- **Scan profiles:** quick (top ports), full TCP, service and version
  detection, OS detection, and NSE script categories (safe, default, vuln).
- **Raw mode** for admins: custom nmap arguments, checked against a guarded
  list so a scan cannot write files, run arbitrary scripts or escape the
  allowlist.
- Results parsed from nmap's XML output into hosts, ports and services, kept
  as history, with a diff between two runs.

Depends on: S1 (job channel, streaming, guardrails).

### S3 — Attack surface

- **Scheduled scans** of defined targets: site subnets, network devices,
  monitor hosts, and the owner's public IP addresses.
- **Inventory:** per host, the open ports and exposed services (product and
  version), and when each was first and last seen.
- **Change alerts:** a new open port, a new service or a changed version
  raises an incident through the existing notification channels.
- **Vulnerability overview:** detected products and versions are matched
  against a locally synced CVE database (NVD and/or OSV feeds, refreshed on a
  schedule), so a scan never needs internet access at run time. Shown per
  host and as a whole-estate summary by severity.
- **Inside vs outside view:** agents report what each server is actually
  listening on (port and process). Compared with the outside scan, this tells
  "exposed" apart from "listening but firewalled".

Depends on: S2.

### S4 — Flow analysis

- Sentinel becomes a **NetFlow v5/v9, IPFIX and sFlow collector** (a UDP
  listener).
- Views: **top talkers**, protocols and applications, **who talks to whom**,
  per site and per interface, both live (the last minutes) and historical.
- Flows are tied to the existing devices and ports (by exporter address and
  interface index), and feed new dashboard widgets and reports.
- Needs flow export switched on in the switches, routers or firewalls.

Depends on: network phase 2 (done). Independent of S1–S3.

### S5 — Per-server traffic

- Agents report **connections and traffic per process and remote endpoint**,
  shown live on the server's page, with a short history.
- Linux and Windows need different collection methods (kernel connection
  tables and per-process counters on each), chosen in the S5 design.

Depends on: S1 (the job channel drives the live view).

### S6 — Packet capture

- **On-demand, bounded capture** (duration, size and a BPF filter) from the
  Sentinel host or an enabled agent.
- A **protocol breakdown** of the capture, and a **.pcap download** for
  Wireshark.
- **Network-wide capture** needs a mirror/SPAN port wired to the Sentinel host
  (or a capture agent); the phase documents how.
- Captures hold payloads: admin-only, shown with a privacy notice, kept only
  briefly, and audited.

Depends on: S1. Last in the track because it is the heaviest and the most
sensitive.

## Order

S1 → S2 → S3 form one chain. S4 can go at any point. S5 follows S1. S6 comes
last. Where the track sits against network phases 6 and 7 is the owner's
call, made when the next phase is chosen.

## Risks and open questions

- **nmap licence.** nmap is distributed under the NPSL, which has
  redistribution terms of its own. Bundling it in the published Docker image
  next to Sentinel's AGPL-3.0 code must be checked before S2 (the fallback is
  installing nmap at deploy time rather than shipping it).
- **Container privileges.** SYN scans and OS detection need raw sockets
  (`NET_RAW`, which the backend already has); capture needs `NET_ADMIN` and
  possibly host networking. Each phase states exactly what it adds to
  `docker-compose.yml`.
- **Storage.** Flows (S4) and captures (S6) can be large; both need
  retention and rollups designed up front, like the metrics store.
- **CVE data.** Which feed, how often it syncs, how large it is, and how
  product versions map to CVEs reliably (CPE matching is imperfect, so the
  overview must say "possible" rather than "confirmed").
- **Agent footprint.** Tools and traffic collection must stay light on the
  servers the agents run on, and be off unless enabled.

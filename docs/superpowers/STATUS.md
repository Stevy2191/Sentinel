# Where Sentinel stands

Updated 2026-10-07. Read this first when picking the work up on another machine.

## Branches

- **`main`** — what runs in production. Behind `dev`: none of the network-monitoring work below is on `main` yet.
- **`dev`** — the integration branch, pushed to GitHub. Everything below lives here. **Work from `dev`** (or a `feature/*` branch made from it).
- Feature work happens on `feature/*` branches in a git worktree and is fast-forwarded into `dev` when done and tested. Merging to `main` (production) is a separate, deliberate step the owner asks for.

## Network monitoring roadmap

Roadmap: `docs/superpowers/specs/2026-09-28-network-monitoring-roadmap.md`. Each phase has a spec in `docs/superpowers/specs/` and a plan in `docs/superpowers/plans/`.

| Phase | What | State |
|---|---|---|
| 0 | Groundwork: TimescaleDB, sites, sharing | Done (`dev`) |
| 1 | SNMP foundation: credentials, devices, inventory, polling | Done (`dev`); follow-ups in `plans/2026-09-29-network-phase1-followups.md` |
| 2 | Ports and bandwidth: metrics store, faceplate, port events and alerts, roles, site traffic | Done (`dev`); follow-ups in `plans/2026-09-30-network-phase2-followups.md` |
| — | UPS monitoring (UPS-MIB readings, Power panel, on-battery/low-battery/high-load alerts) | Done (`dev`); spec `specs/2026-10-01-ups-monitoring-design.md` |
| 3 | MIB library and custom metrics: upload/browse/test-walk MIBs, profiles, Cisco switch health, Health section, metric-rule alerts | Done (`dev`, 2026-10-02); follow-ups in `plans/2026-10-02-network-phase3-followups.md` |
| 4 | Custom dashboards: grid editor, 11 widget types (incl. site power), sharing, fullscreen wall display, admin public links | Done (`dev`, 2026-10-03); follow-ups in `plans/2026-10-02-network-phase4-followups.md` |
| 5 | Metric reports: a Metrics report type (ports, port roles, devices, sites), exact 95th percentiles, totals, previous-period comparison, scheduled and shared like the other reports | Done (`dev`, 2026-10-05); follow-ups in `plans/2026-10-03-network-phase5-followups.md` |
| 6 | Live site maps (LLDP, SSE push) — needs phase 4 | **In progress**, in pieces: A back navigation (done), B find a device and switch maps (done, `dev`), B2 Cisco (built on `feature/cisco-l2-b2`, not yet on `dev`), C site maps (done, `dev`; then bundles and automatic port roles, done), D org map |
| 7 | UniFi controller source | **Next** |

Phase 7 is next; phase 6 is unblocked (it needed phase 4, which is done) and can follow. Where both sit against the tools and security track below is the owner's call.

## Tools and security track

Roadmap: `docs/superpowers/specs/2026-10-04-tools-and-security-roadmap.md` (agreed 2026-10-04; S1 designed in `specs/2026-10-05-tools-s1-network-tools-design.md` and built). Phases are numbered S1–S6 to stay distinct from the network phases. Every phase is admin-only (or a granted permission), rate limited and audited; port checks (and later scans) are limited to an allowlist of target subnets, while ping, traceroute and DNS may reach any address that is not always blocked.

| Phase | What | State |
|---|---|---|
| S1 | Network tools: ping, traceroute (MTR-style), DNS lookup, port check/quick scan — from Sentinel or any server agent, streamed live | Done (`dev`, 2026-10-07); follow-ups in `plans/2026-10-05-tools-s1-followups.md` |
| S2 | nmap: bundled scan profiles (quick, full TCP, service/version, OS, NSE categories), guarded raw mode, parsed results, history and diff — needs S1 | Not started; unblocked by S1 |
| S3 | Attack surface: scheduled scans, open ports and exposed services per host, change alerts, CVE-based vulnerability overview, inside (agent) vs outside view — needs S2 | Not started |
| S4 | Flow analysis: NetFlow/IPFIX/sFlow collector, top talkers, protocols, who-talks-to-whom per site and interface — needs network phase 2 only | Not started |
| S5 | Per-server traffic: connections and traffic per process from the agents, live on the server page — needs S1 | Not started; unblocked by S1 |
| S6 | Packet capture: bounded on-demand capture, protocol breakdown, .pcap download — needs S1; last | Not started; unblocked by S1 |

S1 → S2 → S3 is one chain; S4 can go at any point; S5 follows S1; S6 is last. Where the track sits against network phases 6 and 7 is the owner's call.

## UX reorganization

Spec: `docs/superpowers/specs/2026-10-07-ux-reorganization-design.md` (agreed 2026-10-07). Four pieces, in this order; each gets its own plan.

| Piece | What | State |
|---|---|---|
| 1 | Navigation and Settings: one grouped sidebar (Network folded in, Dashboards and Status Pages as one entry), one Settings area with a section menu, old addresses redirect | Done (`dev`); plan `plans/2026-10-07-ux-reorg-piece1-navigation-settings.md` |
| 2 | Combined Monitoring list: uptime checks, servers and network devices on one page, a section per type, shared filters and one "+ Add"; monitors and servers gain an optional site | Done (`dev`); plan `plans/2026-10-07-ux-piece2-monitoring-list.md`; follow-ups `plans/2026-10-07-ux-piece2-followups.md` |
| 3 | Site profiles: networks, ISPs and circuits (optionally tied to a port, with live usage) and notes per site; two-column site page with its devices, servers and uptime checks | Done (`dev`); plan `plans/2026-10-08-ux-piece3-site-profiles.md`; follow-ups `plans/2026-10-08-ux-piece3-followups.md` |
| 4 | Dashboards and status pages merged: status pages become publishable dashboards | Not started |

## Faceplate layouts per model

Spec `specs/2026-10-18-faceplate-model-layouts-design.md`, plan `plans/2026-10-18-faceplate-model-layouts.md`. The EdgeSwitch 16 XG is drawn as the real switch from a built-in table (`portmon/models.go`, on `dev`). On `feature/faceplate-layout-editor`, admins can save a layout for any model: **Fix layout** on a device's faceplate, and Settings → Faceplate layouts. Migration `062_faceplate_layouts.sql`.

## Realistic faceplates

Spec `specs/2026-10-18-realistic-faceplates-design.md` (approved mockups in `specs/assets/2026-10-18-realistic-faceplates/`), plan `plans/2026-10-18-realistic-faceplates.md`. The UDM-SE, USW-Pro-48-PoE and ES-16-XG draw as their real front panels (Realistic | Simple on the device page; always realistic on dashboards), with the UDM-SE drive bay light from UCD-DISKIO-MIB (bay disk `sdb`). A device Sentinel can't hear from goes dark with a badge. UniFi consoles now number ports from 1 (`portmon/numbering.go`). Second batch (2026-10-18): the EdgeSwitch 8 150W, 24 250W, 48 500W (newer look) and 24 500W, 48 750W (older look), matched on the product name in sysDescr, since all report bare models like `UBNT-ES24`. The ES-48-750W's SFP+ pair is 49/50 (confirmed by the owner, 2026-10-18). Cisco: the Catalyst 4500-X-16 is drawn (module slot 2 empty or the 8-port SFP+ module; drawings now declare their module slots). Waiting on better photos: C9300-24P, WS-C2960CX-8PC-L, WS-C2960X-24PS-L, WS-C2960X-48FPD-L, WS-C2960X-48FPS-L (images in the owner's /home/sysadmin/images); their real port names are recorded in `backend/internal/portmon/drawings.go` (B2, 2026-10-22). More models: add a drawing in `frontend/src/components/network/drawings/` and a line in `backend/internal/portmon/drawings.go`. PoE on UniFi switches waits for network phase 7 (the switch reports no PoE over SNMP).

## Find a device and switch maps (phase 6 piece B)

Spec `specs/2026-10-18-find-device-switch-maps-design.md`, plan `plans/2026-10-18-find-device-switch-maps.md`. Every 5 minutes, each up device's MAC table (Q-BRIDGE, else dot1d), LLDP neighbours and ARP table are read into the `l2` schema (migration `063_l2.sql`; left out of config backups, kept 30 days). Each MAC is placed when read: on an edge port (wired), through an AP (wireless), behind a link port, or only in a router's ARP table. The top bar's **Find a device** searches name, IP, MAC (any notation) or maker; switch and router pages have a **Connected** section (Map | List) and open a device's record from `?endpoint=<mac>`. Users' names for devices are in `endpoint_names` (backed up, audited as `endpoint_named`). Makers come from the IEEE OUI list built in (`backend/internal/l2/oui.tsv.gz`).

**B2 (Cisco), on `feature/cisco-l2-b2`:** spec `specs/2026-10-22-cisco-l2-b2-design.md` (with what a read-only walk of LAB-C3850, BR-CORE-C9300 and BR-ACC-2960X showed on 2026-10-21), plan `plans/2026-10-22-cisco-l2-b2.md`. A Cisco's Q-BRIDGE is empty and its default context is VLAN 1's table only, so each operational VLAN (CISCO-VTP-MIB) is now read in its own context (`community@vlan`, or the v3 context `vlan-N`, built from the docs and untested: there is no v3 Cisco); a VLAN that fails keeps its rows, and when none can be read the default context is read as before. CDP: names match with or without the domain (CDP sends `HQ-CORE-C9300`, Sentinel knows `HQ-CORE-C9300.corp.example`), a reason shows the far port as that switch names it ("port Te1/1/6"), capabilities are read (a CDP switch is a link, a phone a phone), a port where CDP from several devices crossed a switch that doesn't speak it is behind, never edge, UniFi devices (all "UBNT") are told apart by address, and an IP device ID keeps its whole name. Port-channels (ifType 53, like SVIs) count as LAGs by their description, for membership (static and PAgP channels from ifStack) and names; one with a link member is a link, so MACs learned on it are placed beyond it. Port-channels' stats collection is unchanged (a follow-up if wanted). No migration. The 8-MAC "behind" threshold and the 5-minute interval are still first guesses, to tune on work data.

## Monitoring as one list

Spec `specs/2026-10-19-monitoring-one-list-design.md`, plan `plans/2026-10-19-monitoring-one-list.md`. The Monitoring page is one list of every uptime check, server and device, with no sections. Filters (search; Kind as a tree; Status, Site and Tags, each multi-select; Monitor group) combine freely, show counts, and live in the address (`kind`, `status`, `site`, `tags`, `group`, `q`, `sort`, `by`). Old `show=`, `type=` and `device_type=` links still land right. Sort by column (Down first by default); Group by None, Site, Kind or Monitor group, remembered per browser. The model is `frontend/src/utils/monitoringList.ts`. Health sorts each kind worst first (slowest checks, busiest servers, devices failing or most recently restarted); Uptime sorts by percentage; Name has an Address sort beside it. Server metrics are polled once for the page. Each row's name is a link: Tab reaches it, Enter opens it.

## Site maps (phase 6 piece C)

Spec `specs/2026-10-19-site-maps-design.md` (approved mockup in `specs/assets/2026-10-19-site-maps/`), plan `plans/2026-10-19-site-maps.md`, follow-ups `plans/2026-10-19-site-maps-followups.md`. Each site has a live map of its **routers and switches only** (the owner: "just a network structure topology … I don't need to see end points"), at `/network/sites/:id/map`, a card on the site page, a "Show on site map" link on router and switch pages, a wall display, a read-only public link (admins; stand-in ids, no addresses) and a `site_map` dashboard widget (Simple view). Links come from hand-drawn links, ports' Network link settings, LLDP (`internal/sitemap`) and MAC-table guesses (piece B's placement; dashed with a "?" until kept). While a map is watched, the ports at link ends are read every 10 s (`services/link_poller.go`) and pushed over SSE; nothing extra is stored. Migration `064_site_maps.sql`.

## Site map bundles and automatic port roles

Spec `specs/2026-10-19-site-map-bundles-design.md` (mockup in `specs/assets/2026-10-19-site-map-bundles/`), plan `plans/2026-10-19-site-map-bundles.md`. From a site map at work:
- **Bundles.** Two or more cables between the same two devices (ports at both ends, not guesses) are drawn as one bundle: two rails, a bar across the member ports, and a badge ("LAG · 2 × 10G", "2 cables · 2 × 1G", amber with "1 of 2 down"). Clicking it lists every cable. A LAG's name comes from the switch (dot3ad). Hiding a bundle hides each cable. The API sends `members` and `lag`; traffic stays keyed by each cable and is added up in the browser (`services/site_map_bundles.go`, `utils/siteMap.ts` `bundleState`).
- **The map.** The first switch under a distribution switch drops straight down. Idle is now under 1 Mb/s (was under 1%, which hid up to 100 Mb/s on a 10G link). Zoom is calmer: a trackpad's two fingers pan and its pinch zooms smoothly, and a mouse notch is about 10%. The camera no longer re-fits when the server re-sends the map or a panel opens. A router's WAN ports get a violet "Internet" tag with live traffic.
- **Ports marked automatically** (`internal/portroles`, run after each 5-minute L2 poll). The poll now also reads CDP, LAG membership, and routers' default route and addresses. A port whose LLDP or CDP neighbour is one of your routers or switches (or a LAG member with one, or a hand-drawn map link's end) becomes a network link. A router's port that the default route goes out of, that has a public address, or that is named `wan1` becomes WAN. Each change gets a reason ("Found by LLDP: … port 49"). A port you set is never touched (`role_source = 'user'`, back-filled for every existing non-endpoint role). The port page's "Connects to" has a first option, Automatic, to hand a port back. Drawings frame network-link ports sky blue and WAN ports violet.
- **Side effects, no code needed:** WAN ports' traffic reads Download/Upload the right way round, reports group ports by role, and endpoint placement skips network-link ports, all for ports nobody had marked by hand.
- **Home-lab dry run** (a copy of the sandbox DB, before deploying): the back-fill marked Quantum-Gate eth10 and eth8 and QuantumLink 0/51 as yours, and the rules would change nothing. The other LLDP neighbours are an AP, a NAS and a PC, which stay endpoints.
- Migration `065_site_map_bundles.sql`.

## To be checked by the owner

### B2 Cisco (at work, on the work install, once `feature/cisco-l2-b2` is deployed)

1. Within 5 minutes of the deploy, BR-CORE-C9300's Connected map lists endpoints on all its VLANs, not only VLAN 1. Search a PC's MAC: its VLAN shows.
2. HQ-ACC1-2960X's, HQ-ACC2-2960X's and HQ-ACC3-2960X's Gi1/0/25 and HQ-ACC4-2960X's Gi1/0/48 are framed sky blue, "Found by CDP: HQ-CORE-C9300.corp.example port Te1/1/…" (or "Gi1/0/24" for the last).
3. On the SO site map, BR-CORE-C9300 ↔ BR-ACC-2960X is one bundle, "LAG · 2 × 10G", Po1 at both ends.
4. The C3850's Te1/1/4 no longer holds the phones as plugged in there: they show as behind it, or on the switch they're really on.
5. A Cisco port with two UniFi APs behind it lists both in the port's neighbours.
6. Port pages for BR-CORE-C9300's Te1/1/1–2 still say what you set by hand (they're yours; B2 never changes them).

Also: the C3850's hostname was never set (its sysName is `Switch.corp.example`). Setting it keeps a CDP neighbour called just "Switch" from ever matching it.

### Site map bundles and automatic port roles (on the dev stack, port 3200; most of it at work)

1. At work, the site map draws CORE-ES48-A ↔ CORE-ES48-B, CORE-ES48-A ↔ core-es48-c and LAB-ES48 ↔ LAB-USW48 each as one bundle with a "LAG · 2 × …" badge. Click one: the LAG names at both ends and a row per cable.
2. Pull one cable of a LAG (or wait for one to drop): the badge turns amber, "1 of 2 down", and the panel says what it's running on.
3. The USW under LAB-ES48 drops straight down from it, not round the left lane.
4. Zoom in with the trackpad and move around: two fingers pan, a pinch zooms, and nothing jumps back after a few minutes or when you open a panel.
5. The FortiGate shows an "Internet" tag with live ↓/↑. Hover it for why it's WAN.
6. Within 5 minutes of the deploy, ports between switches are framed sky blue, with "Found by LLDP: …" on hover. At home nothing changes: your three hand-set ports stay yours.
7. On a port page, "Connects to" → Automatic → Save hands it back to the rules.

### Site maps (on the dev stack, port 3200)

1. S6 Node's site page shows the Map card; Open map shows Quantum-Gate above QuantumLink, linked port 11 ↔ 51, with traffic moving.
2. Hover the link: "QuantumLink port 51 ↔ Quantum-Gate eth10 · ↓ … ↑ … · 10 Gb"; click it for both ports and the last hour.
3. Edit: drag QuantumLink, reload, and it stays where you put it; Reset layout puts it back.
4. Share → create a public link, open it in a private window: the map, live, with no addresses.
5. At work: the EdgeSwitches and Cisco switches form a tree (router, core, distribution columns); a switch without LLDP shows as a dashed "?" guess you can Keep.


### Monitoring as one list (on the dev stack, port 3200)

1. Monitoring shows your checks, servers and devices in one list, Down first.
2. Kind → Devices → Switch, then Status → Down: the pills read what's applied, and Clear all resets.
3. Group by Site, leave the page and come back: still grouped.
4. An old bookmark such as `/uptime` or `/network/devices` opens the list on that kind.
5. On your phone: rows are two-line cards, and the filter menus fit the screen.

### Find a device and switch maps (on the dev stack, port 3200)

1. Search "KitchenAP", then a phone's IP. The phone shows "via KitchenAP, QuantumLink port …".
2. QuantumLink's Connected map shows the NAS, both APs and the wired devices, and no Quantum-Gate uplink.
3. Name a device, reload, and search for that name.
4. Unplug something, and after the next poll it's faded with "last seen".
5. At work: the EdgeSwitches' Connected maps fill in (expected: the same Q-BRIDGE table as the UniFi switch).

### Realistic faceplates (on the dev stack, port 3200)

1. Quantum-Gate looks like `udm-se.html`. Hovering port 9 shows its popover, and clicking a jack selects it.
2. The drive bay light is steady white or flickering with the drive in. With the drive pulled, within two polls it goes off, and the tooltip says "empty".
3. QuantumLink looks like `usw-pro-48-poe.html`, with the PoE LEDs unlit.
4. Realistic | Simple switches between the drawing and today's faceplate, and the choice survives a reload.
5. A dashboard port grid widget for QuantumLink shows the whole drawing, scaled to fit.
6. At work, the ES-16-XG looks like `es-16-xg.html`. The 10G ports light white and the 1G ports light green.
7. Pause Quantum-Gate in Sentinel. Its drawing goes dark with "Paused", hovering a port shows "Last known", and Simple still shows the last known colours. Resume it, and it lights again after the next poll.
8. At work: if any UniFi console had Edit details faceplate settings or a saved layout made before the port renumbering, re-check them. Their port numbers are now one higher.


### Faceplate layouts per model (on the dev stack, port 3200, with QuantumLink, the US-48 Pro)

1. With no saved layout, the faceplate looks exactly as before.
2. Fix layout opens with the current drawing as groups. Change ports 49–52 to one row, and the preview updates. Save. The device page shows the new drawing, and so does a dashboard port grid widget for that device.
3. Remove a group: Save turns off and "Not placed: …" lists the ports.
4. Set the model in Edit details to something else: the device goes back to the automatic drawing. Set it back, and it uses the saved layout again.
5. Settings → Faceplate layouts lists the saved layout with 1 device, plus the built-in UBNT-ES16XG entry. Delete the saved one, and the US-48 Pro goes back to the automatic drawing.
6. A non-admin sees no Fix layout, no Settings section, and a 403 from each route.


### UX reorganization piece 1 (on the dev stack)

1. The sidebar reads Overview, Dashboards, then Monitor (Uptime, Servers, Devices, Sites, SSL & Domains), Respond (Incidents, Reports) and Tools (Network Tools, MIB Browser); opening a device or site no longer swaps the menu.
2. Each entry stays highlighted on its child pages: a monitor, a server, a device's port, a site, an incident, a report, a tool run, a status page's editor.
3. Dashboards shows Dashboards / Status pages tabs; "+ New" creates a dashboard or a status page, from either tab.
4. Every Settings section opens at its own address and works as before: save a General setting, add a notification channel, view notification history and retry a failed one, invite a user, download a backup, add an SNMP credential, open a device profile, upload a MIB, change a polling threshold, edit the network tools allowlist.
5. Old addresses land in the new place: `/admin/users`, `/notifications`, `/network/credentials`, `/network/profiles` (and a profile's page), `/network/mibs`, `/network/settings`, `/settings?tab=nettools`. The MIB browser's "add as metric" still opens the profile picker with the OID filled in.
6. As a non-admin, the Settings menu shows only Notifications → History and About, and an admin-only address (e.g. `/settings/users`) sends you to About.
7. At phone width, the sidebar drawer works and the Settings section select switches sections.

Tools S1 (after updating the work install from `dev`; Claude had no browser, so none of these has been run in one):
1. With the allowlist empty, a ping to any address (e.g. 8.8.8.8) runs, while the Ports tab shows "No port-check targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools" and a port check is refused with "… is not on the network tools allowlist". A ping to 169.254.169.254 is refused with "… is an address network tools never contact".
2. In Settings → Network tools, add the office subnet (e.g. a 10.x /16). A line such as 10.0.0.0/7 is named with "too broad: use /8 or narrower" and nothing is saved.
3. From the Sentinel server: ping a host on the list (replies arrive about one a second and the chart and tiles fill in), traceroute it, look up a name through the system resolver and through a named DNS server on the list, and check ports with "common".
4. Enable a Linux agent: re-run its install with "Enable network tools on this server" ticked (or add `ENABLE_TOOLS=true` to `/etc/sentinel/agent.conf` and restart it), then Edit → Allow network tools. Within a minute it can be chosen under "Run from"; run each tool from it.
5. A Windows agent, if one is to hand: the same as 4, and its traceroute shows the hops.
6. Through Caddy, watch a 10-round traceroute: the table updates round by round, not all at the end.
7. Cancel a run mid-way, once from the Sentinel server and once from an agent: it shows Cancelled within a second, and the agent stops.
8. Stop an agent's service in the middle of a traceroute: by the deadline plus 15 s the run shows Timed out and the page no longer says Running.
9. Grant a non-admin "Network tools" on the Users page: they see Network Tools in the menu, can run tools and see everyone's runs, cannot open Settings → Network tools, and get no Cancel on someone else's running run.
10. An agent that has not been updated says "This agent's version can't run tools — update it" in its Edit form and is disabled under "Run from".
11. `GET /api/v1/audit-log?resource_type=tool_run` lists `tool_run_started` and `tool_run_finished` entries, and a `tool_run_refused` for an off-list target.
12. Windows agent, ping and traceroute (it uses IcmpSendEcho): replies arrive, and a traceroute shows the intermediate hops (time-exceeded replies), not only the destination.
13. Windows agent, DNS lookup with no named server: it goes through the system resolver, which picks the adapter's DNS server (the first up adapter, not by metric); the answer matches `nslookup` on that machine.
14. Windows agent, TCP scan of a host with one closed port and one open port: the closed port shows `closed`, not `filtered` (the refused-connection path retries after an RST; a 1.5 s timeout could misreport it).
15. In the agent's Edit form, ticking "Allow network tools" survives until Save: wait over 15 s (the agent list re-polls) before pressing Save, and the tick must still be there. A pre-existing re-seed of the open modal on each poll is being triaged and may reset it.

Phase 5 (after updating the work install from `dev`; Claude had no browser, so none of these has been run in one, and 6-11 come from the code reviews):
1. Create a Metrics report for the two WAN ports, period last month; the PDF shows 95th in/out, the billable 95th and the total moved, and the numbers look right against the ISP's portal.
2. Create a Metrics report for a whole site with a schedule; the email arrives with the "ports · billable 95th · moved" summary line.
3. A Port roles scope picks up a port given the Uplink role after the report was created (run it again).
4. Create a report for a scope larger than 500 ports (if one exists) or check the wizard's size line on the biggest site; the PDF says how many were left out.
5. The wizard: Type → Scope → Metrics → Period; Uptime and Incident reports still build exactly as before.
6. In the wizard on a slow network, the size line shows "Sizing the scope…" and the port picker never shows another device's ports.
7. A member with access to one site sees only that site's devices and ports in the pickers.
8. A hidden or removed subject (e.g. a port deleted after it was picked) shows the red "A chosen port, device or site is not available" message, and Next stays disabled.
9. On the Metrics step, no more than 10 metrics can be chosen; changing the scope before editing the list brings in the new scope's defaults, and after editing keeps your list (naming any metric the new scope dropped); the PDF follows the chosen order.
10. Leaving the wizard while a report generates does not pull you back to it when the report finishes.
11. In the browser's devtools, `GET /reports` carries `scope_data` and the share-link response does not.

Phase 4 (after updating the work install from `dev`; the browser and visual checks were not done by Claude, so 5-7 are yours):
1. Create a site dashboard with the standard widgets; every widget shows data within a minute.
2. Leave it fullscreen on a screen for a day; it is still updating the next morning (no Stale markers).
3. As an admin, create a public link, open it in a private window, then turn the link off; the page says the link is no longer available.
4. Stop the backend for a few minutes; the widgets show Stale, then recover on their own.
5. In the editor, add, drag and resize widgets and save. Opening the editor on a dashboard without changes must not show "Discard your changes?" on Cancel.
6. Open a public link signed out in a private window, on a phone-width screen as well. Widgets stack and no link into Sentinel is clickable.
7. Every widget type renders on a site dashboard, and the settings forms save without errors.

Phase 3 (not yet confirmed on real equipment; verified against the SNMP simulator and in the sandbox):
1. MIB library lists 14 built-in modules as Ready; upload the 8 Cisco files linked on that page (needed only for names in the browser).
2. MIB browser → test-walk a 3850 (e.g. the ENTITY-SENSOR or ENVMON tables) shows real rows.
3. A 3850 and a 4500-X show the Health section within two minutes: CPU per switch, temperatures with sensible values, fans and power supplies with states.
4. Copy the starter profile, add a metric from the browser with a preview, save, and see it on the device page (a copy starts with no match prefixes — attach it or add prefixes).

### UX reorganization piece 2 (on the dev stack)

1. The sidebar shows Monitoring, Sites, SSL & Domains under Monitor; Monitoring stays highlighted on a monitor, a server, a device and a port.
2. `/monitoring` shows Uptime checks, Servers and Devices; each collapses and stays collapsed after a reload.
3. All monitors appear (more than 50 if you have them).
4. The type chips, status, site and group filters, and search narrow the sections; picking a group hides Servers and Devices; the address changes with each filter and Back restores the previous view.
5. "Down" in the summary strip filters to everything down.
6. Give a monitor and a server a site; both show it, and the site filter finds them with that site's devices. Delete a test site: its monitor and server stay, with no site.
7. As a non-admin, the Site select in a monitor's form lists only your sites; an empty Servers or Devices section is not shown.
8. `/uptime`, `/uptime?type=http`, `/servers` and `/network/devices` land on the right filtered view; the old bookmarks for monitor, server and device pages still work.
9. + Add offers every create action from the three old pages, with the admin-only ones hidden from non-admins.

### UX reorganization piece 3 (on the dev stack)

1. A site's page shows two columns (one on a phone): circuits, networks, notes and sharing on the left; on the right, tabs for Devices, Servers, Uptime checks and Traffic & ports, each with its count and a red "N down" (Traffic & ports: amber "N problems"). Only one tab shows at a time, `?tab=` in the address reopens it, and the site's dashboards sit on a line under the header.
2. Add a network with a host address (10.20.0.5/24): it is saved as 10.20.0.0/24. Adding it again is refused; a gateway outside it is refused.
3. Add a circuit tied to the firewall's WAN port: the card shows current in/out against its speed and links to the port; unplug or disable the port and the card says "Port down".
4. Edit the notes; reload; they are kept with their line breaks.
5. Scan subnet offers the saved networks.
6. Give a server and a monitor this site (from their edit forms): they appear in the site's Servers and Uptime checks lists.
7. The header's "watched · down" matches what Monitoring shows for the site, and "Open in Monitoring" lands on it filtered.
8. As an editable sharer, add and edit a circuit; as a read-only sharer, the add and edit actions are absent and the profile is readable.
9. The sites list shows each site's devices, servers, checks and down count.

## How to resume

1. `git fetch && git checkout dev` (the repo's `CLAUDE.md` has the commands, conventions and gotchas).
2. Check this file's "Next" row and the latest follow-ups doc.
3. For a new phase: brainstorm → write the spec → write the plan → execute task by task with review, on a `feature/*` branch made from `dev`.

## Owner-specific environment (home server only)

The owner's home server also has a sandbox instance (`/srv/docker/sentinel-dev`, ports 3200/3201) and the live instance; neither exists at work. At work, use `backend/scripts/test-db.sh` (Docker) and the simulator in `deploy/snmpsim` for testing.

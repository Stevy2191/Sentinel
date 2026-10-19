# Monitoring as one list — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Monitoring page's three sections with one sortable, groupable list of every uptime check, server and device, narrowed by free, combinable filters kept in the address.

**Architecture:** A new pure model, `utils/monitoringList.ts`, turns the three loaded lists into one row type. It also owns the filters, the address, the counts, sorting, grouping and the filter pills. Three small row adapters feed one shared row layout inside `MonitoringList`, and `ListFilterBar` draws the filters from the model's counts. `Monitoring.tsx` wires it together and the old section components are deleted. There is no backend change.

**Tech Stack:** React 18 + TypeScript, Tailwind (slate design system), react-router 6, lucide-react.

**Spec:** `docs/superpowers/specs/2026-10-19-monitoring-one-list-design.md`. The approved mockup is `.superpowers/brainstorm/3161371-1792410710/content/monitoring-one-list.html`, which is not committed.

## Global Constraints

- No backend change. The page keeps loading `useAllMonitors`, `useAgents` and `useDevices` and filtering in the browser.
- The detail pages (monitor, server, device) don't change, apart from where their back links point (`monitoringPath`).
- **Filter logic:** filters combine with AND, and the values within one filter combine with OR.
- **Group and tags:** a monitor group or tag filter narrows the list to uptime checks, and the bar says so: "uptime checks only — group and tag filters apply to them alone".
- **Address parameters:**
  - `kind`, `status`, `site` and `tags` take comma-separated values.
  - `group`, `q`, `sort` and `by` take one value each.
  - Old `show=`, `type=` and `device_type=` addresses still land on the matching view.
- **Default view:** the sort is Down first and Group by is None. Group by is remembered in this browser.
- **Copy, exact:**
  - "Nothing matches these filters."
  - "Clear all"
  - "Clear filters", the button in the empty state
  - "<Source> couldn't be loaded" with a Retry button, where Source is one of Uptime checks, Servers or Devices
  - The "nothing watched yet" message is today's text.
- **Design system:** slate/Tailwind, the `rd-select` and `rd-input` classes, the `btn-secondary` class, and the 8 accents in `utils/colors.ts`. Never put `*/` inside a CSS comment.
- **The frontend gate must pass:**
  `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- **No unit-test runner:** the frontend has none, so the pure model is checked by a throwaway esbuild assertion script kept in the scratchpad. It is not committed, which is the established pattern. `monitoringList.ts` may import only types from `@/`, plus relative value imports that are free of `import.meta`, so the script can bundle it for Node.

## Review Focus

1. **A bookmarked site or group id that no longer exists.** Its pill reads "Site: Unknown site" or "Group: Unknown group", and its menu lists that value so it can be unticked. Task 1 tests the pill text, and Task 4 checks the menu.
2. **A group or tag filter while Kind is set to Servers or Devices only.** The result is the empty state with the scope pill visible, never a blank page. Task 1 test.
3. **Search, then Back.** The debounced search must not undo Back, as today. A Task 4 harness check covers it.
4. **One source fails, for example devices return a 500.** The other rows still show, with "Devices couldn't be loaded" and Retry above them. A Task 4 harness check covers it.
5. **A monitor type the frontend doesn't list (a future type).** It is still shown, labelled with its upper-cased type plus " check", and counted under Uptime checks. Task 1 test.

---

### Task 1: The list model (`utils/monitoringList.ts`)

**Files:**
- Create: `frontend/src/utils/monitoringList.ts`
- Test, throwaway and not committed: `$S/mlcheck/check.ts`, where `$S` is the scratchpad

**Interfaces:**
- **Consumes, from `utils/monitoringView.ts` (unchanged):** `ViewStatus`, `VIEW_STATUS_LABEL`, `monitorState`, `serverState`, `deviceState` and `type Section` (`'uptime'|'servers'|'devices'`).
- **Consumes, from `utils/devices.ts`:** `deviceType` and `vendorModel`.
- **Consumes, as types:** `Monitor`, `MonitorType` and `MonitorGroup` from `@/types`; `Agent`; `Device` and `DeviceType`, plus the `DEVICE_TYPE_LABEL` value. That value lives in `@/hooks/useDevices`, which is not import-safe, so the model copies the six labels itself.
- **Produces, all exported:**

```ts
export type KindParent = Section
export type KindValue = KindParent | MonitorType | DeviceType
export const CHECK_LEAVES: readonly MonitorType[]      // ['http','dns','ping','tcp','webhook']
export const DEVICE_LEAVES: readonly DeviceType[]      // ['switch','router','access_point','nvr','ups','other']
export const KIND_PARENT_LABEL: Record<KindParent, string> // Uptime checks / Servers / Devices
export function kindValueLabel(v: KindValue): string   // 'Uptime checks', 'HTTP', 'Switch', 'Access point'

interface RowBase { key: string; name: string; line: string; siteId: string | null; siteName: string | null; status: ViewStatus; seenAt: string | null }
export type ListRow =
  | (RowBase & { kind: 'uptime'; leaf: string; groupId: string | null; tags: string[]; monitor: Monitor })
  | (RowBase & { kind: 'servers'; leaf: null; groupId: null; tags: []; agent: Agent })
  | (RowBase & { kind: 'devices'; leaf: DeviceType; groupId: null; tags: []; device: Device })
export function toRows(monitors: Monitor[], agents: Agent[], devices: Device[]): ListRow[]
export function kindLabel(r: ListRow): string          // 'HTTP check', 'Server', 'Switch'

export interface ListFilters { q: string; kinds: KindValue[]; statuses: ViewStatus[]; sites: string[]; group: string | null; tags: string[] }
export const NO_FILTERS: ListFilters
export type SortKey = 'status' | 'name' | 'kind' | 'site' | 'seen'
export interface ListSort { key: SortKey; desc: boolean }
export const DEFAULT_SORT: ListSort                    // { key: 'status', desc: false } = Down first
export type GroupBy = 'none' | 'site' | 'kind' | 'group'
export interface ListView { filters: ListFilters; sort: ListSort; by: GroupBy | null } // by null = not in the address
export function parseListParams(p: URLSearchParams): ListView
export function buildListParams(v: ListView): URLSearchParams
export function filtersActive(f: ListFilters): boolean
export function uptimeOnly(f: ListFilters): boolean    // group set or tags non-empty
export type Facet = 'kind' | 'status' | 'site' | 'group' | 'tags'
export function filterRows(rows: ListRow[], f: ListFilters, skip?: Facet): ListRow[]
export interface FacetCounts { kind: Record<string, number>; status: Record<string, number>; site: Record<string, number>; group: Record<string, number>; tags: Record<string, number> }
export function facetCounts(rows: ListRow[], f: ListFilters): FacetCounts
export function sortRows(rows: ListRow[], s: ListSort): ListRow[]
export function nextSort(cur: ListSort, key: SortKey): ListSort
export interface RowGroup { key: string; label: string; rows: ListRow[]; down: number; monitorGroup: MonitorGroup | null }
export function groupRows(rows: ListRow[], by: Exclude<GroupBy, 'none'>, ctx: { groups: MonitorGroup[]; includeEmptyGroups: boolean }): RowGroup[]
export interface FilterPill { id: string; label: string; remove: (f: ListFilters) => ListFilters }
export function filterPills(f: ListFilters, names: { site: (id: string) => string | null; group: (id: string) => string | null }): FilterPill[]
export function siteFilterOptions(sites: { id: string; name: string }[], rows: ListRow[]): { id: string; name: string }[]
export function legacyMonitoringTarget(location: { search: string; hash: string }, show: KindParent): string
```

**Rules the tests pin.** The implementer writes the bodies.

- **Row key:** `'m:'+id`, `'s:'+id` or `'d:'+id`.
- **Second line (`line`):**
  - monitor: `url`
  - server: `hostname ?? agent_id`, then ` · os_version` when present
  - device: `host` (plus `:port` when port is not 161), then ` · vendorModel` when present
- **Last seen (`seenAt`):** a monitor's `last_check_at`, a server's `last_heartbeat`, a device's `last_seen_at`.
- **Monitor `leaf`:** the monitor's raw `type`. A leaf outside `CHECK_LEAVES` is still shown, matched only by the `uptime` parent, and labelled `TYPE.toUpperCase()+' check'`.
- **Kind match:** a row matches when `kinds` is empty, contains its parent, or contains its leaf.
- **Search:** case-insensitive, over name, line, site name and `kindLabel`. Servers also match on `agent_id` and `ip_address`.
- **Site:** `'none'` matches rows with no site id.
- **Group:** `'ungrouped'` matches checks with no `group_id`. A group or tag filter excludes servers and devices.
- **Tags:** any of the chosen tags matches.
- **`filterRows(…, skip)`** ignores that one facet. **`facetCounts`** counts each facet over `filterRows(rows, f, thatFacet)`:
  - `kind` holds a count for every parent and every leaf. A leaf counts only when the row's kind is the leaf's parent; this matters for `other`, which is a device type.
  - `status` is keyed by ViewStatus, `site` by site id or `'none'`, `group` by group id or `'ungrouped'`, and `tags` by tag.
- **Urgency order:** down 0, error 1, maintenance 2, pending 3, up 4, paused 5. `status` sorts by urgency.
  - `name` sorts with `localeCompare` and `sensitivity: 'base'`.
  - `kind` sorts by `kindLabel`.
  - `site` sorts by site name, with no site last whichever way the sort runs.
  - `seen` sorts by time, with never-seen rows last.
  - Ties always fall back to name A–Z.
- **`nextSort`:**
  - The same key flips `desc`.
  - A new key starts with `desc = (key === 'seen')`, so newest is first.
- **Address `sort`:**
  - `sort` holds the key, prefixed with `-` for descending, for example `sort=-seen`.
  - It is omitted when the sort equals DEFAULT_SORT.
- **Address `by`:**
  - `by` is written only for `site`, `kind` or `group`.
  - Parse accepts `none` too, and anything else gives null.
- **Parsing:**
  - When `kind` is present, the legacy keys are ignored.
  - Otherwise `show=X` gives `[X]`. Then `type=T` replaces `uptime` with `T` (or adds T), and `device_type=D` does the same for `devices`.
  - Unknown values in `kind` and `status` are dropped.
  - Site and tag values are trimmed, and empty ones are dropped.
- **`groupRows`:**
  - **site:** headings A–Z by label, with "No site" last.
  - **kind:** headings in the order Uptime checks, Servers, Devices.
  - **group:**
    - `ctx.groups` in their given order; empty groups appear only when `includeEmptyGroups` is set.
    - then "Ungrouped"
    - then "Other (servers and devices)"
    - A monitor-group heading carries its `MonitorGroup`.
  - **All:** `down` counts the rows whose status is down, and a heading with no rows is dropped (the empty-group case above aside). Rows keep the order they came in.
- **Pill labels:**
  - `Search: “q”`
  - `Kind: Uptime checks`, `Kind: HTTP checks`, `Kind: Switch`, `Kind: Servers`
  - `Status: Down`
  - `Site: <name>`, `Site: No site` or `Site: Unknown site`
  - `Group: <name>`, `Group: Ungrouped` or `Group: Unknown group`
  - `Tag: vpn`
  - One pill per value, in that order. Each `remove` drops only its own value.
- **`siteFilterOptions`:** the user's sites plus any other site named on a row, A–Z.
- **`legacyMonitoringTarget`:** merges `show` into the old query, parses it, rebuilds it, and keeps the hash. For example `{search:'?type=http', hash:''}` with `'uptime'` gives `/monitoring?kind=http`.

- [ ] **Step 1: Write the failing check script** at `$S/mlcheck/check.ts`.

It imports from `@/utils/monitoringList` and builds fixtures with small `mon()`, `srv()` and `dev()` factories. The fixtures:

| Row | Kind | Site | Group | Tags | Status |
|---|---|---|---|---|---|
| m1 "Jellyfin" | http | — | — | — | up |
| m2 "prox-a" | ping | — | — | — | up |
| m3 "Courthouse portal" | http | s2 "Work" | g1 | public | offline |
| m4 "Clerk VPN" | tcp | s2 | g1 | vpn | up |
| m5 "example.gov DNS" | dns | s2 | — | — | disabled |
| m6 "Future" | `'grpc'`, cast | — | — | — | up |

| Row | Kind | Site | Status |
|---|---|---|---|
| a1 "Claude Dev" | server | s1 "S6 Node" | active |
| d1 "QuantumLink" | switch | s1 | up |
| d2 "Annex-SW" | switch | s2 | down |
| d3 "UNAS-Pro" | `device_type: null`, `device_type_detected: 'other'` | s1 | up |

The assertions, all of which use `deepEqual` on the row names:

```ts
const rows = toRows(mons, agents, devs)
const names = (rs: ListRow[]) => rs.map((r) => r.name)
const f = (over: Partial<ListFilters>) => ({ ...NO_FILTERS, ...over })
eq(names(filterRows(rows, f({ kinds: ['uptime'] }))).length, 6)                       // parent = every check, incl. unknown type
eq(names(filterRows(rows, f({ kinds: ['http'] }))), ['Jellyfin', 'Courthouse portal'])
eq(names(filterRows(rows, f({ kinds: ['uptime', 'switch'] }))).length, 8)
eq(names(filterRows(rows, f({ kinds: ['other'] }))), ['UNAS-Pro'])
eq(names(filterRows(rows, f({ statuses: ['down', 'pending'], sites: ['s1', 's2'] }))), ['Courthouse portal', 'Annex-SW'])
eq(names(filterRows(rows, f({ group: 'g1' }))), ['Courthouse portal', 'Clerk VPN'])
eq(names(filterRows(rows, f({ group: 'ungrouped' }))), ['Jellyfin', 'prox-a', 'example.gov DNS', 'Future'])
eq(names(filterRows(rows, f({ tags: ['vpn', 'public'] }))), ['Courthouse portal', 'Clerk VPN'])
eq(filterRows(rows, f({ kinds: ['servers'], group: 'g1' })), [])                      // Review Focus 2
eq(names(filterRows(rows, f({ q: 'work' }))).length, 4)                                // site name
eq(names(filterRows(rows, f({ q: 'switch' }))), ['QuantumLink', 'Annex-SW'])         // kind label
eq(names(filterRows(rows, f({ sites: ['none'] }))), ['Jellyfin', 'prox-a', 'Future'])
eq(kindLabel(rows.find((r) => r.name === 'Future')!), 'GRPC check')                   // Review Focus 5
eq(kindLabel(rows.find((r) => r.name === 'Claude Dev')!), 'Server')
const c = facetCounts(rows, f({ statuses: ['down'] }))
eq([c.kind.uptime, c.kind.http, c.kind.devices, c.kind.switch, c.kind.servers], [1, 1, 1, 1, 0])
eq(facetCounts(rows, f({ kinds: ['servers'] })).kind.devices, 3)                     // own facet ignored
eq(facetCounts(rows, f({ kinds: ['servers'] })).status.up, 1)                         // others respected
eq(names(sortRows(rows, DEFAULT_SORT)).slice(0, 2), ['Annex-SW', 'Courthouse portal'])
eq(names(sortRows(rows, DEFAULT_SORT)).at(-1), 'example.gov DNS')                     // paused last
eq(nextSort(DEFAULT_SORT, 'status'), { key: 'status', desc: true })
eq(nextSort(DEFAULT_SORT, 'seen'), { key: 'seen', desc: true })
eq(nextSort(DEFAULT_SORT, 'name'), { key: 'name', desc: false })
const p = (s: string) => parseListParams(new URLSearchParams(s))
eq(p('show=devices').filters.kinds, ['devices'])
eq(p('show=uptime&type=http').filters.kinds, ['http'])
eq(p('type=http').filters.kinds, ['http'])
eq(p('show=devices&device_type=switch').filters.kinds, ['switch'])
eq(p('kind=servers&show=devices').filters.kinds, ['servers'])
eq(p('status=down,bogus&site=a, none,').filters, f({ statuses: ['down'], sites: ['a', 'none'] }))
eq(p('sort=-seen&by=site').sort, { key: 'seen', desc: true })
eq(p('by=none').by, 'none'); eq(p('by=bogus').by, null)
eq(buildListParams({ filters: f({ kinds: ['http', 'switch'], statuses: ['down'] }), sort: DEFAULT_SORT, by: 'none' }).toString(), 'kind=http%2Cswitch&status=down')
eq(buildListParams(p('kind=uptime&sort=name&by=kind&q=x')).toString(), 'q=x&kind=uptime&sort=name&by=kind')
eq(legacyMonitoringTarget({ search: '?type=http', hash: '#x' }, 'uptime'), '/monitoring?kind=http#x')
eq(legacyMonitoringTarget({ search: '', hash: '' }, 'devices'), '/monitoring?kind=devices')
const g = groupRows(sortRows(rows, DEFAULT_SORT), 'group', { groups: [grp('g1', 'Courthouse'), grp('g2', 'Empty')], includeEmptyGroups: false })
eq(g.map((x) => [x.label, x.rows.length, x.down]), [['Courthouse', 2, 1], ['Ungrouped', 4, 0], ['Other (servers and devices)', 4, 1]])
eq(groupRows(rows, 'group', { groups: [grp('g1', 'Courthouse'), grp('g2', 'Empty')], includeEmptyGroups: true }).map((x) => x.label)[1], 'Empty')
eq(groupRows(rows, 'site', { groups: [], includeEmptyGroups: false }).map((x) => x.label), ['S6 Node', 'Work', 'No site'])
eq(groupRows(rows, 'kind', { groups: [], includeEmptyGroups: false }).map((x) => x.label), ['Uptime checks', 'Servers', 'Devices'])
const pills = filterPills(f({ q: ' x ', kinds: ['uptime', 'http', 'switch'], statuses: ['down'], sites: ['s1', 'gone', 'none'], group: 'old', tags: ['vpn'] }),
  { site: (id) => (id === 's1' ? 'S6 Node' : null), group: () => null })
eq(pills.map((x) => x.label), ['Search: “x”', 'Kind: Uptime checks', 'Kind: HTTP checks', 'Kind: Switch', 'Status: Down',
  'Site: S6 Node', 'Site: Unknown site', 'Site: No site', 'Group: Unknown group', 'Tag: vpn'])           // Review Focus 1
eq(pills[3].remove(f({ kinds: ['uptime', 'switch'] })).kinds, ['uptime'])
eq(uptimeOnly(f({ tags: ['vpn'] })), true); eq(filtersActive(NO_FILTERS), false)
eq(siteFilterOptions([{ id: 's1', name: 'S6 Node' }], rows).map((s) => s.name), ['S6 Node', 'Work'])
```

`eq` throws with both values printed. The script prints `ALL OK` at the end.

- [ ] **Step 2: Run the check and watch it fail.**

Run, from the worktree root, with `CHECK_DIR=$S/mlcheck`:

`docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app:ro -v "$CHECK_DIR":/check -w /app node:20-alpine sh -c "node_modules/.bin/esbuild /check/check.ts --bundle --platform=node --format=esm --jsx=automatic --tsconfig=/app/tsconfig.json --log-level=warning --outfile=/tmp/check.mjs && node /tmp/check.mjs"`

This needs `frontend/node_modules`; run the gate's `npm ci` first if it's missing.

Expected: FAIL, with esbuild reporting `Could not resolve "@/utils/monitoringList"`.

- [ ] **Step 3: Implement `frontend/src/utils/monitoringList.ts`** to the Interfaces and rules above. Put a header comment saying it may hold only type imports from `@/`.

- [ ] **Step 4: Run the check again.** Expected: `ALL OK`.

- [ ] **Step 5: Run the frontend gate.** Expected: exit 0.

- [ ] **Step 6: Commit.** `git add frontend/src/utils/monitoringList.ts && git commit -m "feat(monitoring): one-list model — rows, free filters, counts, sort, grouping"`

### Task 2: The list and filter components

**Files:**
- Create in `frontend/src/components/monitoring/`:
  - `FilterMenu.tsx`
  - `ListFilterBar.tsx`
  - `StatusPill.tsx`
  - `ListRowLayout.tsx`
  - `MonitorListRow.tsx`
  - `ServerListRow.tsx`
  - `DeviceListRow.tsx`
  - `MonitoringList.tsx`
  - `UnregisterServerDialog.tsx`

**Interfaces:**
- **Consumes, from Task 1:** `ListRow`, `ListFilters`, `ListSort`, `SortKey`, `GroupBy`, `RowGroup`, `facetCounts`, `filterPills`, `uptimeOnly`, `kindLabel`, `kindValueLabel`, `CHECK_LEAVES`, `DEVICE_LEAVES`, `KIND_PARENT_LABEL`, `nextSort`.
- **Consumes, existing:**
  - `MonitorRowActions`, `useMonitorUptime`
  - `Sparkline`, `hourlySparklinePoints`, `uptimeColor`
  - `formatLastResponseTime`
  - `monitorAccess`, `badgeToneClass`
  - `useAgentStatus`, `useDebounced`, `useRememberedToggles`
  - `VIEW_STATUS_LABEL`
- **Produces:**

```ts
// FilterMenu.tsx: a button ("Kind", or "Kind · 2" when set) opening a checkbox (multi) or radio (single) list.
export interface FilterOption { value: string; label: string; n?: number; child?: boolean; separatorBefore?: boolean }
export default function FilterMenu(p: { label: string; options: FilterOption[]; selected: string[]; multi: boolean; onChange: (next: string[]) => void; hint?: string }): JSX.Element
// StatusPill.tsx
export default function StatusPill(p: { status: ViewStatus; detail?: string }): JSX.Element
// ListFilterBar.tsx
export default function ListFilterBar(p: {
  rows: ListRow[]; filters: ListFilters; onChange: (next: ListFilters, replace?: boolean) => void
  sites: { id: string; name: string }[]; groups: MonitorGroup[]; by: GroupBy; onBy: (by: GroupBy) => void
}): JSX.Element
// ListRowLayout.tsx: one <tr>: 8 desktop cells (md and up) plus one phone card cell (below md).
export default function ListRowLayout(p: {
  kind: ListRow['kind']; name: ReactNode; line: string; kindText: string; site: string | null; status: ReactNode
  health: ReactNode; healthText: string; uptime: ReactNode; seen: string; actions: ReactNode; onOpen: () => void
}): JSX.Element
// One row per kind, each rendering ListRowLayout.
export default function MonitorListRow(p: { row: Extract<ListRow, { kind: 'uptime' }>; uptime24h: number | null; ownerUsername?: string; refreshKey: unknown; onChanged: () => void; push: Push }): JSX.Element
export default function ServerListRow(p: { row: Extract<ListRow, { kind: 'servers' }>; isAdmin: boolean; onInstructions: () => void; onDelete: () => void }): JSX.Element
export default function DeviceListRow(p: { row: Extract<ListRow, { kind: 'devices' }>; isAdmin: boolean; onEdit: () => void }): JSX.Element
// MonitoringList.tsx
export default function MonitoringList(p: {
  rows: ListRow[]                 // already filtered and sorted
  groups: RowGroup[] | null       // null = flat list
  by: GroupBy; sort: ListSort; onSort: (key: SortKey) => void
  isAdmin: boolean; uptimeById: Map<string, number>; usernameFor: (id: string | null | undefined) => string | undefined
  refreshKey: unknown; push: Push; onMonitorsChanged: () => void
  onServerInstructions: (a: Agent) => void; onServerDelete: (a: Agent) => void
  onDeviceEdit: (d: Device) => void; onEditGroup: (g: MonitorGroup) => void
}): JSX.Element
// UnregisterServerDialog.tsx: today's confirm dialog, moved out of ServersSection unchanged in copy.
export default function UnregisterServerDialog(p: { agent: Agent; busy: boolean; onCancel: () => void; onConfirm: () => void }): JSX.Element
```

(`Push` = `(msg: string, type?: 'success' | 'error' | 'info') => void`.)

**What the pieces must do:**

- **FilterMenu:**
  - It closes on an outside click or Escape. The button carries `aria-haspopup` and `aria-expanded`.
  - A value with `n === 0` that isn't selected is drawn dimmed but stays clickable.
  - `hint` is a small first line, used for "Uptime checks only".
- **ListFilterBar:**
  - **Search:** a search box, debounced 300 ms. It writes with `replace` and follows outside changes to `filters.q`, as `MonitoringToolbar` does today, including the comment about Back.
  - **Kind:** a tree in this order: "Uptime checks", then the CHECK_LEAVES as children labelled HTTP, DNS, Ping, TCP and Webhook, then a separator and "Servers", then a separator and "Devices" with the DEVICE_LEAVES as children.
  - **Status:** all six statuses.
  - **Site:** the site options plus "No site", and any selected id not among them, as "Unknown site".
  - **Monitor group:** single choice, with hint "Uptime checks only", offering All, each group, Ungrouped, and a selected unknown id as "Unknown group". It is shown when there are groups or one is selected.
  - **Tags:** the tags of every loaded check plus the selected ones, A–Z, with hint "Uptime checks only". It is shown when any tag exists or one is selected.
  - **Counts:** every option shows its count from `facetCounts(rows, filters)`.
  - **Group by:** a `rd-select` with None, Site, Kind and Monitor group (`aria-label="Group by"`).
  - **The pill row** holds `filterPills` pills, each with an × labelled `Remove <label>`. When `uptimeOnly`, it adds the scope pill "uptime checks only — group and tag filters apply to them alone". "Clear all" appears when any pill exists and resets to `NO_FILTERS`.
- **ListRowLayout:**
  - **The name cell** holds a small icon by kind: lucide `Activity` for uptime, `Server` for servers, `Network` for devices. The name sits next to it, with the muted second line below.
  - **On phones** (below md), the row is one cell. The top line has the icon, name and status; the second line reads `kindText · site or "No site" · healthText · seen`.
  - **Clicking the row** calls `onOpen`. Clicks in the actions cell stop propagation.
- **MonitorListRow:** today's `MonitorRow` cells, moved:
  - access badge, owner, response-time colour, the 24 h sparkline and %, the "Last 24 hours" caption, `MonitorRowActions`
  - Last seen is shown as a relative time ("40s ago").
  - It opens `/monitors/:id`.
- **ServerListRow:**
  - **Health:** `CPU n% · Mem n% · Disk n%`, each coloured by today's `usageClass`, from `useAgentStatus(agent_id, 20000)`. It shows "—" until loaded.
  - **Uptime:** `up 12 d` (days, or hours below a day) from `uptime_seconds`.
  - **Actions** (admins only): install instructions and unregister.
  - It opens `/servers/:agent_id`.
- **DeviceListRow:**
  - **Health:** `up N d` from `sys_uptime_seconds`. When the device is down or in error, it shows `status_detail` instead, falling back to "not responding".
  - **Uptime:** the 30-day availability as `99.98%` with a thin bar and a muted "30 d", or "—".
  - **Actions:** an Edit (pencil) button for admins.
  - It opens `/network/devices/:id`.
- **Seen times on every row** use one shared relative-time formatter: `just now` under 60 s, then `Ns`, `Nm`, `Nh` and `Nd ago`, and `never`. It lives in `ListRowLayout.tsx` as an exported `ago(iso)`.
- **MonitoringList:** one table in today's capped, sticky-header box (`max-h-[65vh] overflow-auto`, `thead sticky top-0`).
  - **Headers:** Name, Kind, Site, Status, Health, Uptime, Last seen, and an empty Actions header.
  - **Sorting:** Name, Kind, Site, Status and Last seen are sort buttons with `aria-sort`. When Status is the key and ascending, it shows a "Down first" title.
  - **Group headings:** when `groups` is set, each one is a full-width row with a collapse chevron, the label, the count, an "N down" chip, and (for a monitor group) its `group_uptime`% and an "Edit group" button.
  - **Collapse state** is remembered with `useRememberedToggles('sentinel:monitoringListGroups')`, keyed `${by}:${group.key}`. A collapsed group renders no rows, so rows that fetch their own data stay quiet.
  - **An empty monitor group** (shown when nothing narrows the list) gets today's line: "No monitors in this group yet — assign one from a row's Group dropdown."

- [ ] **Step 1: Write the components** to the contracts above, moving code out of `MonitorTable.tsx`, `ServerRow.tsx` and `ServersSection.tsx` rather than rewriting it. Don't delete those files yet; Task 3 does that.

- [ ] **Step 2: Run the frontend gate.** Expected: exit 0. ESLint may flag the unused new files only if they aren't exported; they all have default exports.

- [ ] **Step 3: Commit.** `git commit -m "feat(monitoring): one-list table, rows per kind, filter bar with counts and pills"`

These are presentational components with no logic beyond Task 1's model. Their behaviour is proven by Task 4's harness checks, which run against the wired page.

### Task 3: Wire the page; old links; remove the sections

**Files:**
- Rewrite: `frontend/src/pages/Monitoring.tsx`
- Modify:
  - `frontend/src/components/monitoring/SummaryStrip.tsx`
  - `frontend/src/components/MonitoringRedirect.tsx`
  - `frontend/src/utils/monitoringView.ts`
- Delete:
  - `frontend/src/components/monitoring/UptimeSection.tsx`, `ServersSection.tsx`, `DevicesSection.tsx`, `SectionShell.tsx`, `MonitoringToolbar.tsx`, `ServerRow.tsx`, `EmptyLine.tsx`
  - `frontend/src/components/MonitorTable.tsx`
  - Delete each only if nothing else imports it. Check with `grep -rn` first; `DeviceTable` stays because SiteDetail uses it.

**Interfaces:**
- **Consumes:** everything Task 1 and Task 2 produce.
- **Produces:**
  - `monitoringPath(show?: Section): string`, which now returns `/monitoring?kind=<show>`, or `/monitoring`.
  - `MonitoringRedirect` now imports `legacyMonitoringTarget` from `@/utils/monitoringList`.

**The page:**

- **The view:**
  - `view = parseListParams(searchParams)`.
  - `by = view.by ?? stored`, where `stored` is read once from localStorage key `sentinel:monitoringGroupBy` inside try/catch (default `'none'`).
  - `onBy` writes the stored value (try/catch) and the address.
  - `setFilters(next, replace)` and `onSort(key)` write `buildListParams` and keep the other parts of the view. A filter change pushes a history entry, and typing replaces it.
- **The data:**
  - `rows = toRows(monitors, agents, devices)`
  - `shown = sortRows(filterRows(rows, filters), sort)`
  - `grouped = by === 'none' ? null : groupRows(shown, by, { groups, includeEmptyGroups: !filtersActive(filters) })`
- **The header, Add menu, modals, refresh button and 30 s group refresh** stay as today. Drop the unused 30-day summary: the header stats are gone, per the spec's "What goes away".
- **Device modal:**
  - When adding, `DeviceFormModal` gets `siteId` when exactly one real site is filtered.
  - Device edit opens `DeviceFormModal initial={device}`, which refetches devices on save.
  - Server instructions and unregister keep today's `useAgentActions` logic, moved here from ServersSection, with `UnregisterServerDialog`.
- **The summary strip:**
  - Down and Paused set `statuses: [s]` and keep the other filters.
  - Watching clears `statuses`. Make Watching a button.
  - The `missing` prop goes.
- **Failed sources:** above the list, for each source that errored, a red line reads "<Source> couldn't be loaded" with the error text and a Retry button. Retry calls that source's refetch.
- **Loading:** three pulsing skeleton rows until all three sources have settled (loaded or errored).
- **Empty states:**
  - **Nothing watched:** when everything has loaded, none failed and there are no rows, show today's message.
  - **Nothing matches:** when rows exist but `shown` is empty, show "Nothing matches these filters." with a "Clear filters" button.
- **The header subtitle** stays.

- [ ] **Step 1: Rewrite `Monitoring.tsx` and edit `SummaryStrip`, `MonitoringRedirect` and `monitoringView.ts`:**
  - In `monitoringView.ts`, `monitoringPath` writes `kind=`.
  - Remove the exports nothing uses any more. Check each with grep: `SECTION_CHIP`, `MonitoringFilters`, `NO_FILTERS`, `parseMonitoringParams`, `buildMonitoringParams`, `filtersNarrow`, `visibleSections`, `ownFiltersSet`, `sectionVisible`, `filterMonitors`, `filterServers`, `filterDevicesView`, `statusOptions`, `siteFilterOptions`, `unknownOption`, `MONITOR_SORTS`, `MonitorSortKey`, `sortMonitors`, `legacyMonitoringTarget`.
  - Keep the states, the labels, `summaryCounts`, `Section`, `SECTIONS`, `SECTION_LABEL` and `monitoringPath`.
  - Update the file's header comment.

- [ ] **Step 2: Delete the old components** listed above, after grepping each for importers.

- [ ] **Step 3: Re-run the Task 1 check script**, which `legacyMonitoringTarget` now feeds through `MonitoringRedirect`. Then run the frontend gate. Expected: `ALL OK`, then exit 0.

- [ ] **Step 4: Commit.** `git commit -m "feat(monitoring): one list replaces the three sections; old links land on kind="`

### Task 4: Headless checks, then ship

**Files:**
- **Harness, throwaway and in the scratchpad:**
  - `$S/ml-harness/` is a copy of `frontend/`, via rsync with `node_modules` copied whole.
  - Its entry is `src/harness-monitoring.tsx`, mounted from `index.html`.
  - It uses an axios adapter, as `$S/fd-harness/src/harness-connected.tsx` does.
- **Script:** `$S/pw/monitoring.js`

**What the harness provides:**
- **Data:** the Task 1 fixture data, served by the adapter for `/monitors` (or whatever path `useAllMonitors` calls), `/monitor-groups`, `/agents`, `/devices` and `/sites`, plus the per-row uptime and agent-status calls.
- **Auth:** an admin user through the real `AuthProvider`, or a minimal context provider if it needs a session call. Read `AuthContext` to choose.
- **A failure switch:** `?fail=devices` in the page's own address makes `/devices` return a 500.
- **Routes:** `/monitoring` renders the page, and `/uptime` renders `<MonitoringRedirect show="uptime" />`.

- [ ] **Step 1: Write `$S/pw/monitoring.js`.** It uses the `check(what, got, want)` style of `connected.js` and runs these checks:
  - **Mixed list:** `/monitoring` shows 10 rows. The first two names are Annex-SW and Courthouse portal, and the last is example.gov DNS.
  - **Down at two sites:** `/monitoring?status=down&site=s1,s2` shows those 2 rows, with the pills "Status: Down", "Site: S6 Node" and "Site: Work".
  - **HTTP only:** open Kind and tick HTTP. Expect 2 rows; the URL has `kind=http`; the Kind menu shows "Switch" with count 2 when the filter is cleared.
  - **Group by site:** choosing Site in Group by gives the headings "S6 Node", "Work" and "No site", and reloading keeps them (remembered).
  - **Collapse:** clicking the "Work" heading hides its rows.
  - **Group filter:** `?group=g1` shows 2 rows and the scope pill.
  - **Review Focus 1:** `?group=gone` shows the pill "Group: Unknown group", and the Monitor group menu lists "Unknown group".
  - **Old link:** `/monitoring?show=devices` shows 3 device rows and the Kind button reads "Kind · 1".
  - **Old redirect:** `/uptime?type=http` lands on `/monitoring?kind=http`.
  - **Search and Back (Review Focus 3):** type "prox" and wait 500 ms; the URL has `q=prox`. Tick Status Up, go Back, and expect the URL to be `q=prox` without status. The text box shows "prox".
  - **Failed source (Review Focus 4):** `?fail=devices` shows 7 rows and the text "Devices couldn't be loaded" with Retry.
  - **Phone:** at 430 px width, no horizontal scroll (`scrollWidth <= innerWidth`), and the first row's card text contains "Switch · Work".
  - **No errors:** no page errors and no console errors.
  - **Screenshots:** `ml-all.png`, `ml-filtered.png`, `ml-grouped.png` and `ml-phone.png`.

- [ ] **Step 2: Run the script** against the vite dev server for the harness. Use node:20-alpine on port 8790 and the Playwright container from the Global Constraints' style of command. Expected: every line PASS, exit 0. Look at the four screenshots against the mockup.

- [ ] **Step 3: Fix any failures**, test-first where the cause is in the model, by adding the case to the check script. Commit each fix.

- [ ] **Step 4: Final whole-branch review**, as executing-plans requires, then one fix pass.

- [ ] **Step 5: Ship.**
  1. Rebase onto `origin/dev`, run the gate, and fast-forward-push to `dev`.
  2. In `/srv/docker/sentinel-dev`, run `git pull --ff-only && docker compose up -d --build`.
  3. Confirm `/monitoring` serves (HTTP 200 from port 3200).
  4. Add a line about this change to `docs/superpowers/STATUS.md` in the same push.
  5. Stop the harness container and the mockup server.

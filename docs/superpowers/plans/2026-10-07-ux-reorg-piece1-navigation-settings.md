# UX Reorganization, Piece 1: Navigation and Settings — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One grouped sidebar (Network folded in, Dashboards and Status Pages as one entry) and one Settings area with its own menu, with every moved address redirecting to its new place.

**Architecture:** Frontend only. Two dependency-free modules describe the structure — `utils/navigation.ts` (sidebar groups, which entry is active for a path, how a moved address maps to its new one) and `utils/settingsNav.ts` (the Settings sections, who sees them, legacy `?tab=` mapping). `Layout.tsx` renders the sidebar from the first; a new `pages/settings/SettingsLayout.tsx` renders the Settings menu from the second and nests every section as a child route. The old `pages/Settings.tsx` is split into section components; existing pages (Users, notification history, credentials, profiles, MIB library, network settings) move under `/settings/...` unchanged except for their heading size.

**Tech Stack:** React 18 + TypeScript + Vite + Tailwind + react-router-dom v6.

**Spec:** `docs/superpowers/specs/2026-10-07-ux-reorganization-design.md` (section "Piece 1: navigation and Settings").

## Global Constraints

- No backend, API or database change. API paths in hooks (`/network/settings`, `/network/profiles`, `/network/mibs`, …) are API routes and stay as they are; only browser routes move.
- Frontend rules: colours from `src/utils/colors.ts` or the existing slate/Tailwind classes (no hand-written hex, no `dark:` variants); component files export only components (react-refresh lint) — helpers go in `src/utils/` or hooks; no `window.confirm`/`alert`; guard async results against changed inputs.
- Sidebar labels, groups and order exactly: Overview · Dashboards · **Monitor**: Uptime, Servers, Devices, Sites, SSL & Domains · **Respond**: Incidents, Reports · **Tools**: Network Tools, MIB Browser; footer: your name (to `/profile`), Settings, Log out.
- Settings sections, addresses and visibility exactly as the spec's table (General `/settings/general`; Notifications → Channels `/settings/notifications`, History `/settings/notifications/history`; Users & access `/settings/users`; Backups `/settings/backups`; SNMP credentials `/settings/network/credentials`; Device profiles `/settings/network/profiles` and `/:id`; MIB library `/settings/network/mibs`; Polling & thresholds `/settings/network/polling`; Network tools `/settings/network-tools`; About `/settings/about`). Only History and About are shown to non-admins.
- Every old address in the spec's redirect table redirects with `replace`, keeping the rest of the path, the query and the hash.
- Frontend gate (from the repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`. Never run npm as root.
- Throwaway helper checks (Tasks 1–2): a `check.ts` in a temp directory outside the repo, bundled with the esbuild Vite already installs and run with Node in the same container: `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -v "$CHECK":/check -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx esbuild /check/check.ts --bundle --platform=node --outfile=/tmp/check.cjs --log-level=warning && node /tmp/check.cjs"` where `$CHECK` is the temp directory. The check imports the module by absolute path (`/app/src/utils/navigation.ts`), so the modules under test must not import through the `@/` alias.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A non-admin following an old admin link** (e.g. `/network/credentials` from an old bookmark) must land on Settings → About, not on an empty admin page or an error. Pinned by Task 2's `adminOnly` guard check and Task 3's redirect into the guarded section.
2. **A moved address with a query string must still land right**: the MIB test-walk's "add as metric" link (`/network/profiles?new=1&oid=…`) keeps its query through the redirect, and an old `/settings?tab=nettools` link opens Network tools (the tab value is translated, not dropped). Pinned by Task 1's `movedPath` check and Task 2's `legacySettingsTarget` check.
3. **Detail pages highlight their entry**: a monitor (`/monitors/:id`) → Uptime; a status page's editor (`/status-pages/x/edit`) → Dashboards; a device's port → Devices; a profile's page (`/settings/network/profiles/:id`) → Device profiles in the Settings menu, not "nothing". Pinned by Task 1's `isNavActive` table and Task 2's `sectionForPath` table.
4. **Notifications → History must not light up Channels too** (one address is a prefix of the other). Pinned by Task 2's `sectionForPath` check and the `end` flag on the Channels menu link.
5. **Unknown or old settings addresses never show a blank page**: `/settings/whatever` → the user's settings home; `/settings/security` still → `/profile`. Pinned by Task 2's routes and `settingsHome` check.

## File structure

- Create `frontend/src/utils/navigation.ts` — sidebar model (`NAV`, `SETTINGS_NAV`), `isNavActive`, `movedPath`.
- Create `frontend/src/utils/settingsNav.ts` — `SETTINGS_SECTIONS`, `visibleSections`, `sectionForPath`, `settingsHome`, `legacySettingsTarget`.
- Create `frontend/src/components/LegacyRedirect.tsx` — redirect a moved address keeping path tail, query and hash.
- Create `frontend/src/components/DashboardsHeader.tsx` — the shared Dashboards / Status pages title, tabs and "+ New" menu.
- Create `frontend/src/pages/settings/SettingsLayout.tsx`, `SettingsIndex.tsx`, `GeneralSettings.tsx`, `BackupsSettings.tsx`, `AboutSettings.tsx`, `NetworkToolsSection.tsx`.
- Delete `frontend/src/pages/Settings.tsx` (its content is split into the section files).
- Modify `frontend/src/components/Layout.tsx`, `frontend/src/App.tsx`, `frontend/src/pages/dashboards/Dashboards.tsx`, `frontend/src/pages/StatusPages.tsx`, `frontend/src/pages/AdminUsers.tsx`, `frontend/src/pages/Notifications.tsx`, `frontend/src/pages/network/{Credentials,Profiles,ProfileDetail,MibLibrary,NetworkSettings}.tsx`, `frontend/src/pages/NetworkTools.tsx`, `frontend/src/components/NotificationsSection.tsx`, `frontend/src/components/settings/NetToolsSettings.tsx`, `frontend/src/components/network/TestWalkPanel.tsx`.
- Modify `docs/superpowers/STATUS.md`.

## Plan rulings

1. **Section headings.** A page that moves into Settings keeps its own heading text and description (so its action buttons stay where they are), demoted from page-title size (`h1`, `text-4xl`) to section size (`h2 className="text-2xl font-light text-white"`). The Settings layout shows the one big "Settings" title. New section components follow the same pattern.
2. **Toasts.** Each section that needs toasts owns its `useToasts()` and `<Toaster>` (the old Settings page shared one); no outlet context is needed.
3. **The Settings guard** only redirects known admin-only sections for non-admins; unknown `/settings/*` addresses are handled by a catch-all child route that goes to the user's settings home, and `/settings/security` stays a child route redirecting to `/profile`.
4. **"+ New → Dashboard" from the Status pages tab** navigates to `/dashboards?new=1`, which opens the existing new-dashboard dialog.

---

### Task 1: The grouped sidebar

**Files:**
- Create: `frontend/src/utils/navigation.ts`
- Modify: `frontend/src/components/Layout.tsx`
- Test: throwaway `check.ts` (not committed) + the frontend gate

**Interfaces:**
- Produces: `NavItem`, `NavGroup`, `NAV: NavGroup[]`, `SETTINGS_NAV: NavItem`, `isNavActive(pathname: string, item: NavItem): boolean`, `movedPath(location: { pathname: string; search: string; hash: string }, from: string, to: string): string` (Task 3's `LegacyRedirect` uses `movedPath`).

- [ ] **Step 1: Write the throwaway check (it fails: the module does not exist yet)**

Make a temp directory outside the repo (`CHECK=$(mktemp -d)`) and write `$CHECK/check.ts`:

```ts
import { NAV, SETTINGS_NAV, isNavActive, movedPath } from '/app/src/utils/navigation.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

const items = NAV.flatMap((g) => g.items)
const byLabel = (label: string) => {
  const it = items.find((i) => i.label === label)
  if (!it) throw new Error(`no nav item ${label}`)
  return it
}
const activeFor = (path: string) => items.filter((i) => isNavActive(path, i)).map((i) => i.label)

expect('groups', NAV.map((g) => g.label), [null, 'Monitor', 'Respond', 'Tools'])
expect('labels', items.map((i) => i.label), [
  'Overview', 'Dashboards', 'Uptime', 'Servers', 'Devices', 'Sites', 'SSL & Domains',
  'Incidents', 'Reports', 'Network Tools', 'MIB Browser',
])
expect('net tools flag', items.filter((i) => i.netTools).map((i) => i.label), ['Network Tools'])

const cases: [string, string[]][] = [
  ['/', ['Overview']],
  ['/dashboards', ['Dashboards']],
  ['/dashboards/abc/edit', ['Dashboards']],
  ['/status-pages', ['Dashboards']],
  ['/status-pages/main/edit', ['Dashboards']],
  ['/uptime', ['Uptime']],
  ['/monitors/123', ['Uptime']],
  ['/monitors/create', ['Uptime']],
  ['/servers/agent_1a2b3c4d5e', ['Servers']],
  ['/network/devices/9/ports/3', ['Devices']],
  ['/network/sites/4', ['Sites']],
  ['/ssl', ['SSL & Domains']],
  ['/incidents/7', ['Incidents']],
  ['/reports/analytics', ['Reports']],
  ['/tools/runs/1', ['Network Tools']],
  ['/network/mibs/browse', ['MIB Browser']],
  ['/settings/general', []],
  ['/profile', []],
  ['/uptime/', ['Uptime']],
  ['/uptimeX', []],
]
for (const [path, want] of cases) expect(`active ${path}`, activeFor(path), want)
expect('settings active', isNavActive('/settings/network/profiles/3', SETTINGS_NAV), true)
expect('settings not active', isNavActive('/servers', SETTINGS_NAV), false)
expect('overview exact', isNavActive('/x', byLabel('Overview')), false)

expect('moved id', movedPath({ pathname: '/network/profiles/12', search: '', hash: '' }, '/network/profiles', '/settings/network/profiles'), '/settings/network/profiles/12')
expect('moved query', movedPath({ pathname: '/network/profiles', search: '?new=1&oid=1.3.6', hash: '#m' }, '/network/profiles', '/settings/network/profiles'), '/settings/network/profiles?new=1&oid=1.3.6#m')
expect('moved exact', movedPath({ pathname: '/admin/users', search: '', hash: '' }, '/admin/users', '/settings/users'), '/settings/users')

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('navigation: all checks passed')
```

- [ ] **Step 2: Run it to see it fail**

Run (repo root): the throwaway check command from the Global Constraints with `CHECK` set.
Expected: esbuild fails with `Could not resolve "/app/src/utils/navigation.ts"`.

- [ ] **Step 3: Write `frontend/src/utils/navigation.ts`**

```ts
// The sidebar's structure, kept apart from Layout.tsx so it can be checked on
// its own and so the Settings layout and redirects share one idea of "where a
// page lives". Deliberately free of `@/` imports.

/** One sidebar entry. `match` lists the paths it stands for: the entry is
 *  highlighted on each of them and on their child pages. */
export interface NavItem {
  to: string
  label: string
  match: string[]
  /** Highlight only on exactly these paths, not their children (Overview). */
  exact?: boolean
  /** Shown only to admins and to users granted network tools. */
  netTools?: boolean
}

/** A labelled group of entries; `label: null` is the ungrouped top. */
export interface NavGroup {
  label: string | null
  items: NavItem[]
}

export const NAV: NavGroup[] = [
  {
    label: null,
    items: [
      { to: '/', label: 'Overview', match: ['/'], exact: true },
      // Status pages share the entry: piece 4 of the reorganization turns them
      // into dashboards that can be published.
      { to: '/dashboards', label: 'Dashboards', match: ['/dashboards', '/status-pages'] },
    ],
  },
  {
    label: 'Monitor',
    items: [
      { to: '/uptime', label: 'Uptime', match: ['/uptime', '/monitors'] },
      { to: '/servers', label: 'Servers', match: ['/servers'] },
      { to: '/network/devices', label: 'Devices', match: ['/network/devices'] },
      { to: '/network/sites', label: 'Sites', match: ['/network/sites'] },
      { to: '/ssl', label: 'SSL & Domains', match: ['/ssl'] },
    ],
  },
  {
    label: 'Respond',
    items: [
      { to: '/incidents', label: 'Incidents', match: ['/incidents'] },
      { to: '/reports', label: 'Reports', match: ['/reports'] },
    ],
  },
  {
    label: 'Tools',
    items: [
      { to: '/tools', label: 'Network Tools', match: ['/tools'], netTools: true },
      { to: '/network/mibs/browse', label: 'MIB Browser', match: ['/network/mibs/browse'] },
    ],
  },
]

/** Settings sits in the sidebar footer and is highlighted on every settings page. */
export const SETTINGS_NAV: NavItem = { to: '/settings', label: 'Settings', match: ['/settings'] }

/** isNavActive reports whether item is the sidebar entry for pathname. A
 *  trailing slash is ignored; a prefix only counts at a path boundary, so
 *  /uptimeX does not light up Uptime. */
export function isNavActive(pathname: string, item: NavItem): boolean {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  return item.match.some((m) => (item.exact ? path === m : path === m || path.startsWith(`${m}/`)))
}

/** movedPath is where a moved address now lives: `from` is replaced by `to`,
 *  and the rest of the path (an id), the query and the hash are kept. */
export function movedPath(
  location: { pathname: string; search: string; hash: string },
  from: string,
  to: string
): string {
  const rest = location.pathname.startsWith(from) ? location.pathname.slice(from.length) : ''
  return `${to}${rest}${location.search}${location.hash}`
}
```

- [ ] **Step 4: Run the check to see it pass**

Run: the same throwaway command.
Expected: `navigation: all checks passed`.

- [ ] **Step 5: Rewrite the sidebar in `frontend/src/components/Layout.tsx`**

Make these edits:

1. Imports. Replace

```tsx
import { Menu, X, RefreshCw, Settings } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig } from '@/context/AppConfigContext'
import { canUseNetTools } from '@/utils/netTools'
```

with

```tsx
import { Menu, X, RefreshCw } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig } from '@/context/AppConfigContext'
import { canUseNetTools } from '@/utils/netTools'
import { NAV, SETTINGS_NAV, isNavActive } from '@/utils/navigation'
```

2. Delete everything from the comment `// Order matters: the four things Sentinel watches sit together, then what it` down to and including the whole `inNetworkSection` function (the `nav` array, the `networkNav` comment and array, and `inNetworkSection`).

3. Replace

```tsx
function navClass({ isActive }: { isActive: boolean }) {
  return `rd-nav ${isActive ? 'active' : ''}`
}
```

with

```tsx
function navClass(active: boolean) {
  return `rd-nav ${active ? 'active' : ''}`
}
```

4. In `SidebarBody`, delete the line `  const network = inNetworkSection(pathname)`.

5. Replace the wordmark's mode line

```tsx
        <div className="mt-2 text-xs text-slate-400">{network ? 'Network Monitoring' : 'Uptime Monitor'}</div>
```

with nothing (delete the line).

6. Replace the whole `<nav className="flex-1 space-y-1 overflow-y-auto p-4"> … </nav>` element (the `network ? (...) : (...)` block) with

```tsx
      <nav className="flex-1 overflow-y-auto p-4">
        {NAV.map((group) => {
          const items = group.items.filter((item) => !item.netTools || canUseNetTools(currentUser))
          if (items.length === 0) return null
          return (
            <div key={group.label ?? 'top'} className="space-y-1">
              {group.label && (
                <div className="px-4 pb-1 pt-5 text-[11px] font-semibold uppercase tracking-widest text-slate-500">
                  {group.label}
                </div>
              )}
              {items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  onClick={onNavigate}
                  className={() => navClass(isNavActive(pathname, item))}
                >
                  {item.label}
                </NavLink>
              ))}
            </div>
          )
        })}
      </nav>
```

7. In the user footer, delete the Users button block

```tsx
          {currentUser?.is_admin && (
            <button
              className="w-full rounded-lg px-4 py-2 text-left text-xs text-slate-400 transition hover:bg-white/5 hover:text-slate-300"
              onClick={() => go('/admin/users')}
            >
              Users
            </button>
          )}
```

and replace the Settings button

```tsx
          <button
            className="w-full rounded-lg px-4 py-2 text-left text-xs text-slate-400 transition hover:bg-white/5 hover:text-slate-300"
            onClick={() => go('/settings')}
          >
            Settings
          </button>
```

with

```tsx
          <button
            className={`w-full rounded-lg px-4 py-2 text-left text-xs transition hover:bg-white/5 ${
              isNavActive(pathname, SETTINGS_NAV) ? 'bg-white/5 text-white' : 'text-slate-400 hover:text-slate-300'
            }`}
            onClick={() => go('/settings')}
          >
            Settings
          </button>
```

8. In `Layout`'s top bar, delete the Settings gear button

```tsx
            <button
              className="text-slate-400 transition hover:text-slate-300"
              onClick={() => navigate('/settings')}
              aria-label="Settings"
            >
              <Settings className="h-5 w-5" />
            </button>
```

If `navigate` in `Layout` is now unused, also delete `const navigate = useNavigate()` from `Layout` (keep the one in `SidebarBody`). Update the top-bar comment `{/* Top bar: greeting on the left, refresh and settings on the right. */}` to `{/* Top bar: greeting on the left, refresh on the right. */}`.

- [ ] **Step 6: Run the frontend gate**

Run (repo root): the frontend gate from the Global Constraints.
Expected: exits 0 with no output.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/utils/navigation.ts frontend/src/components/Layout.tsx
git commit -m "feat(ux): one grouped sidebar with Network folded in

The sidebar is now Overview, Dashboards and the Monitor, Respond and Tools
groups; Network's pages no longer swap the whole menu. Users leaves the
footer (it moves into Settings), the top bar's duplicate Settings gear goes,
and every entry stays highlighted on its child pages.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The Settings area and the sections split from Settings.tsx

**Files:**
- Create: `frontend/src/utils/settingsNav.ts`
- Create: `frontend/src/pages/settings/SettingsLayout.tsx`, `SettingsIndex.tsx`, `GeneralSettings.tsx`, `BackupsSettings.tsx`, `AboutSettings.tsx`, `NetworkToolsSection.tsx`
- Delete: `frontend/src/pages/Settings.tsx`
- Modify: `frontend/src/App.tsx`, `frontend/src/pages/NetworkTools.tsx`, `frontend/src/components/NotificationsSection.tsx`, `frontend/src/pages/Notifications.tsx` (one link only)
- Test: throwaway `check.ts` + the frontend gate

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `SettingsSection`, `SETTINGS_SECTIONS`, `visibleSections(isAdmin: boolean): SettingsSection[]`, `sectionForPath(pathname: string): SettingsSection | undefined`, `settingsHome(isAdmin: boolean): string`, `legacySettingsTarget(tab: string | null, isAdmin: boolean): string`; the `/settings` layout route with child routes `index`, `general`, `notifications`, `backups`, `network-tools`, `about`, `security`, `*` (Task 3 adds `users`, `notifications/history` and `network/*` children to the same layout route).

- [ ] **Step 1: Write the throwaway check (fails: module missing)**

`$CHECK/check.ts` (a fresh temp directory):

```ts
import {
  SETTINGS_SECTIONS, visibleSections, sectionForPath, settingsHome, legacySettingsTarget,
} from '/app/src/utils/settingsNav.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

expect('paths', SETTINGS_SECTIONS.map((s) => s.path), [
  '/settings/general', '/settings/notifications', '/settings/notifications/history', '/settings/users',
  '/settings/backups', '/settings/network/credentials', '/settings/network/profiles', '/settings/network/mibs',
  '/settings/network/polling', '/settings/network-tools', '/settings/about',
])
expect('admin sees all', visibleSections(true).length, 11)
expect('member sees', visibleSections(false).map((s) => s.key), ['notification-history', 'about'])

const at = (p: string) => sectionForPath(p)?.key ?? null
expect('history not channels', at('/settings/notifications/history'), 'notification-history')
expect('channels', at('/settings/notifications'), 'notifications')
expect('profile detail', at('/settings/network/profiles/12'), 'profiles')
expect('trailing slash', at('/settings/general/'), 'general')
expect('unknown', at('/settings/whatever'), null)
expect('security not a section', at('/settings/security'), null)
expect('root', at('/settings'), null)
expect('network-tools vs network', at('/settings/network-tools'), 'network-tools')

expect('home admin', settingsHome(true), '/settings/general')
expect('home member', settingsHome(false), '/settings/about')
expect('tab nettools', legacySettingsTarget('nettools', true), '/settings/network-tools')
expect('tab system', legacySettingsTarget('system', true), '/settings/general')
expect('tab notifications', legacySettingsTarget('notifications', true), '/settings/notifications')
expect('tab about member', legacySettingsTarget('about', false), '/settings/about')
expect('tab system member', legacySettingsTarget('system', false), '/settings/about')
expect('no tab admin', legacySettingsTarget(null, true), '/settings/general')
expect('bad tab', legacySettingsTarget('toString', true), '/settings/general')

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('settingsNav: all checks passed')
```

- [ ] **Step 2: Run it to see it fail**

Run: the throwaway check command.
Expected: `Could not resolve "/app/src/utils/settingsNav.ts"`.

- [ ] **Step 3: Write `frontend/src/utils/settingsNav.ts`**

```ts
// The Settings area's sections: where each lives, which menu group it sits in
// and who may see it. Deliberately free of `@/` imports so it can be checked
// on its own.

export type SettingsGroup = 'Sentinel' | 'Network' | 'Help'

export interface SettingsSection {
  key: string
  label: string
  path: string
  group: SettingsGroup
  /** Shown only to admins (their APIs are admin-only too). */
  adminOnly: boolean
  /** A menu heading the section sits under (Notifications → Channels). */
  parent?: string
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'general', label: 'General', path: '/settings/general', group: 'Sentinel', adminOnly: true },
  { key: 'notifications', label: 'Channels', parent: 'Notifications', path: '/settings/notifications', group: 'Sentinel', adminOnly: true },
  { key: 'notification-history', label: 'History', parent: 'Notifications', path: '/settings/notifications/history', group: 'Sentinel', adminOnly: false },
  { key: 'users', label: 'Users & access', path: '/settings/users', group: 'Sentinel', adminOnly: true },
  { key: 'backups', label: 'Backups', path: '/settings/backups', group: 'Sentinel', adminOnly: true },
  { key: 'credentials', label: 'SNMP credentials', path: '/settings/network/credentials', group: 'Network', adminOnly: true },
  { key: 'profiles', label: 'Device profiles', path: '/settings/network/profiles', group: 'Network', adminOnly: true },
  { key: 'mibs', label: 'MIB library', path: '/settings/network/mibs', group: 'Network', adminOnly: true },
  { key: 'polling', label: 'Polling & thresholds', path: '/settings/network/polling', group: 'Network', adminOnly: true },
  { key: 'network-tools', label: 'Network tools', path: '/settings/network-tools', group: 'Network', adminOnly: true },
  { key: 'about', label: 'About', path: '/settings/about', group: 'Help', adminOnly: false },
]

/** visibleSections lists the sections this user may open, in menu order. */
export function visibleSections(isAdmin: boolean): SettingsSection[] {
  return SETTINGS_SECTIONS.filter((s) => isAdmin || !s.adminOnly)
}

/** sectionForPath is the section an address belongs to: the longest section
 *  path that equals it or is a parent of it (a profile's page belongs to
 *  Device profiles; History is not taken for Channels). Undefined for
 *  /settings itself and for addresses that are not a section. */
export function sectionForPath(pathname: string): SettingsSection | undefined {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  let best: SettingsSection | undefined
  for (const s of SETTINGS_SECTIONS) {
    if ((path === s.path || path.startsWith(`${s.path}/`)) && (!best || s.path.length > best.path.length)) {
      best = s
    }
  }
  return best
}

/** settingsHome is where /settings opens for this user. */
export function settingsHome(isAdmin: boolean): string {
  return isAdmin ? '/settings/general' : '/settings/about'
}

// The old Settings page's ?tab= values (links and bookmarks still carry them).
const LEGACY_TABS: Record<string, string> = {
  system: '/settings/general',
  notifications: '/settings/notifications',
  nettools: '/settings/network-tools',
  about: '/settings/about',
}

/** legacySettingsTarget maps an old ?tab= value to its section, falling back
 *  to the user's settings home for a missing, unknown or forbidden tab. */
export function legacySettingsTarget(tab: string | null, isAdmin: boolean): string {
  const target = tab && Object.prototype.hasOwnProperty.call(LEGACY_TABS, tab) ? LEGACY_TABS[tab] : null
  if (!target) return settingsHome(isAdmin)
  const section = sectionForPath(target)
  if (section && section.adminOnly && !isAdmin) return settingsHome(isAdmin)
  return target
}
```

- [ ] **Step 4: Run the check to see it pass**

Expected: `settingsNav: all checks passed`.

- [ ] **Step 5: Write `frontend/src/pages/settings/SettingsLayout.tsx`**

```tsx
import { Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig } from '@/context/AppConfigContext'
import { sectionForPath, visibleSections, type SettingsGroup, type SettingsSection } from '@/utils/settingsNav'

const GROUPS: SettingsGroup[] = ['Sentinel', 'Network', 'Help']

function linkClass({ isActive }: { isActive: boolean }) {
  return `block rounded-md px-3 py-1.5 text-sm transition ${
    isActive ? 'bg-white/10 text-white' : 'text-slate-300 hover:bg-white/5 hover:text-white'
  }`
}

/** SettingsMenu is the left-hand menu: groups, with Notifications' two pages
 *  under one heading. Channels uses `end` so History does not light it too. */
function SettingsMenu({ sections }: { sections: SettingsSection[] }) {
  return (
    <nav className="space-y-4" aria-label="Settings sections">
      {GROUPS.map((group) => {
        const inGroup = sections.filter((s) => s.group === group)
        if (inGroup.length === 0) return null
        return (
          <div key={group} className="space-y-0.5">
            <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-widest text-slate-500">{group}</div>
            {inGroup.map((s, i) => {
              const heading = s.parent && inGroup[i - 1]?.parent !== s.parent
              return (
                <div key={s.key}>
                  {heading && <div className="px-3 pb-0.5 pt-1 text-sm text-slate-400">{s.parent}</div>}
                  <NavLink to={s.path} end={s.key === 'notifications'} className={linkClass}>
                    <span className={s.parent ? 'pl-3' : ''}>{s.label}</span>
                  </NavLink>
                </div>
              )
            })}
          </div>
        )
      })}
    </nav>
  )
}

/** SettingsLayout is the one Settings area: the menu on the left (a select on
 *  narrow screens) and the chosen section on the right. A non-admin who opens
 *  an admin section (an old bookmark, a shared link) is sent to About; the API
 *  refuses them anyway, so this is tidiness, not security. */
export default function SettingsLayout() {
  const { currentUser } = useAuthContext()
  const { appName } = useAppConfig()
  const isAdmin = currentUser?.is_admin ?? false
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const section = sectionForPath(pathname)
  const sections = visibleSections(isAdmin)

  if (section && section.adminOnly && !isAdmin) {
    return <Navigate to="/settings/about" replace />
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Settings</h1>
        <p className="text-sm text-slate-400">{isAdmin ? `Configure this ${appName} instance` : 'Instance information'}</p>
      </div>

      <label className="block md:hidden">
        <span className="sr-only">Settings section</span>
        <select className="w-full" value={section?.path ?? ''} onChange={(e) => navigate(e.target.value)}>
          {section === undefined && <option value="">Choose a section…</option>}
          {GROUPS.map((group) => {
            const inGroup = sections.filter((s) => s.group === group)
            if (inGroup.length === 0) return null
            return (
              <optgroup key={group} label={group}>
                {inGroup.map((s) => (
                  <option key={s.key} value={s.path}>
                    {s.parent ? `${s.parent} — ${s.label}` : s.label}
                  </option>
                ))}
              </optgroup>
            )
          })}
        </select>
      </label>

      <div className="md:flex md:gap-8">
        <aside className="hidden w-56 shrink-0 md:block">
          <SettingsMenu sections={sections} />
        </aside>
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 6: Write `frontend/src/pages/settings/SettingsIndex.tsx`**

```tsx
import { Navigate, useSearchParams } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { legacySettingsTarget } from '@/utils/settingsNav'

/** SettingsIndex answers /settings and any unknown /settings/* address: the
 *  old ?tab= value if there is one, otherwise the user's settings home. */
export default function SettingsIndex() {
  const { currentUser } = useAuthContext()
  const [searchParams] = useSearchParams()
  return <Navigate to={legacySettingsTarget(searchParams.get('tab'), currentUser?.is_admin ?? false)} replace />
}
```

- [ ] **Step 7: Write `frontend/src/pages/settings/GeneralSettings.tsx`**

This is the old Settings → System tab without the backup card, under a section heading. The Users link points at the new address.

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import api from '@/services/api'
import { useToasts, Toaster } from '@/components/Toast'
import SettingsCard from '@/components/SettingsCard'
import ReconnectAgent from '@/components/ReconnectAgent'
import TimezoneSelector from '@/components/TimezoneSelector'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig, DEFAULT_APP_NAME } from '@/context/AppConfigContext'
import { useIncidentRetention } from '@/hooks/useIncidents'

// Mirrors the bounds the backend enforces (models.Min/MaxCheckIntervalSeconds),
// which are themselves the monitor validator's bounds — so a value accepted
// here is always a value a monitor may actually hold.
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MIN_CHECK_RETENTION = 1
const MAX_CHECK_RETENTION = 3650
const MAX_APP_NAME = 40
const MIN_SLA_TARGET = 0
const MAX_SLA_TARGET = 100

/** One address's reachability result from the server-side probe. */
interface UrlProbe {
  url: string
  reachable: boolean
  status?: number
  error?: string
  source: string
}

interface SystemSettings {
  app_name: string
  base_url: string
  default_check_interval: number
  /** How long individual check results are kept. */
  check_retention_days: number
  /** Where agent install commands download from. Empty means derive it. */
  sentinel_external_url: string
  /** Where agents report metrics. Empty means use the external URL. */
  sentinel_internal_url: string
  /** IANA zone that reports are rendered in and the UI displays times in. */
  report_timezone: string
  /** Uptime percentage a monitor is held to when it has no override of its own. */
  default_sla_target: number
}

/**
 * Reads the API's error message, falling back to something actionable.
 *
 * The API has two error shapes: respondError sends `error` as a plain string,
 * respondAuthError sends it as `{code, message}`. Handling only one of them
 * silently swallows half the validation messages and shows the fallback
 * instead, so both are read here.
 */
function apiMessage(err: unknown, fallback: string): string {
  const body = (err as { response?: { data?: { error?: unknown } } })?.response?.data?.error
  if (typeof body === 'string' && body.trim()) return body
  if (body && typeof body === 'object') {
    const msg = (body as { message?: unknown }).message
    if (typeof msg === 'string' && msg.trim()) return msg
  }
  return fallback
}

/** Settings → General: the instance-wide system settings. */
export default function GeneralSettings() {
  const { toasts, push } = useToasts()
  const { currentUser } = useAuthContext()
  const { refresh: refreshAppConfig } = useAppConfig()
  const isAdmin = currentUser?.is_admin ?? false

  // ---- Incident retention (server-side, admin-only) ----
  const { days: retentionDays, bounds: retentionBounds, save: saveRetention } = useIncidentRetention()
  const [retention, setRetention] = useState<number | null>(null)
  const [retentionSaving, setRetentionSaving] = useState(false)
  // Seeded from the server once it answers, then owned by the field so typing
  // is not overwritten by a later render.
  useEffect(() => {
    if (retentionDays != null) setRetention((r) => r ?? retentionDays)
  }, [retentionDays])

  const retentionValid =
    retention != null && retention >= retentionBounds.min && retention <= retentionBounds.max

  const persistRetention = async () => {
    if (retention == null || !retentionValid) return
    setRetentionSaving(true)
    try {
      await saveRetention(retention)
      push(`Incident history kept for ${retention} days`, 'success')
    } catch (err) {
      push(apiMessage(err, 'Could not update retention'), 'error')
    } finally {
      setRetentionSaving(false)
    }
  }

  // ---- System (server-side, admin-only) ----
  const [system, setSystem] = useState<SystemSettings | null>(null)
  const [systemLoading, setSystemLoading] = useState(isAdmin)
  const [systemSaving, setSystemSaving] = useState(false)
  const [systemError, setSystemError] = useState<string | null>(null)

  useEffect(() => {
    if (!isAdmin) return
    let active = true
    setSystemLoading(true)
    api
      .get<{ data: SystemSettings }>('/settings')
      .then((r) => {
        if (!active) return
        setSystem({
          app_name: r.data.data.app_name || DEFAULT_APP_NAME,
          base_url: r.data.data.base_url ?? '',
          default_check_interval: r.data.data.default_check_interval,
          check_retention_days: r.data.data.check_retention_days,
          sentinel_external_url: r.data.data.sentinel_external_url ?? '',
          sentinel_internal_url: r.data.data.sentinel_internal_url ?? '',
          report_timezone: r.data.data.report_timezone || 'UTC',
          default_sla_target: r.data.data.default_sla_target,
        })
        setSystemError(null)
      })
      .catch((err) => active && setSystemError(apiMessage(err, 'Could not load system settings')))
      .finally(() => active && setSystemLoading(false))
    return () => {
      active = false
    }
  }, [isAdmin])

  const saveSystem = async () => {
    if (!system) return
    setSystemSaving(true)
    try {
      await api.patch('/settings/system', {
        app_name: system.app_name.trim(),
        base_url: system.base_url.trim(),
        default_check_interval: system.default_check_interval,
        check_retention_days: system.check_retention_days,
        sentinel_external_url: system.sentinel_external_url.trim(),
        sentinel_internal_url: system.sentinel_internal_url.trim(),
        report_timezone: system.report_timezone,
        default_sla_target: system.default_sla_target,
      })
      // Re-read the public config so the sidebar, sign-in screen and browser tab
      // pick up a renamed instance without a reload.
      await refreshAppConfig()
      setSystemError(null)
      push('System settings saved', 'success')
    } catch (err) {
      const msg = apiMessage(err, 'Could not save system settings')
      setSystemError(msg)
      push(msg, 'error')
    } finally {
      setSystemSaving(false)
    }
  }

  const intervalValid =
    Number.isFinite(system?.default_check_interval) &&
    (system?.default_check_interval ?? 0) >= MIN_INTERVAL &&
    (system?.default_check_interval ?? 0) <= MAX_INTERVAL
  const slaValid =
    Number.isFinite(system?.default_sla_target) &&
    (system?.default_sla_target ?? -1) >= MIN_SLA_TARGET &&
    (system?.default_sla_target ?? -1) <= MAX_SLA_TARGET
  const checkRetentionValid =
    !system ||
    (Number.isFinite(system.check_retention_days) &&
      system.check_retention_days >= MIN_CHECK_RETENTION &&
      system.check_retention_days <= MAX_CHECK_RETENTION)
  // Validated the same way the server does, so the button does not offer to
  // save something that will be refused.
  const urlOK = (v: string) => {
    const raw = v.trim().replace(/\/+$/, '')
    if (raw === '') return true
    try {
      const u = new URL(raw)
      return (u.protocol === 'http:' || u.protocol === 'https:') && !!u.host && (u.pathname === '' || u.pathname === '/')
    } catch {
      return false
    }
  }
  const urlsValid =
    !system || (urlOK(system.sentinel_external_url) && urlOK(system.sentinel_internal_url))

  const [urlTesting, setUrlTesting] = useState(false)
  const [urlTest, setUrlTest] = useState<{
    external: UrlProbe
    internal: UrlProbe
  } | null>(null)

  const testURLs = async () => {
    if (!system) return
    setUrlTesting(true)
    try {
      const res = await api.post<{ data: { external: UrlProbe; internal: UrlProbe } }>(
        '/agents/urls/test',
        {
          // The values on screen, not the saved ones, so a URL can be checked
          // before it is committed.
          external_url: system.sentinel_external_url.trim(),
          internal_url: system.sentinel_internal_url.trim(),
        },
      )
      setUrlTest(res.data.data)
    } catch (err) {
      push(apiMessage(err, 'Could not test the URLs'), 'error')
    } finally {
      setUrlTesting(false)
    }
  }

  const nameValid = !!system && system.app_name.trim().length > 0 && system.app_name.trim().length <= MAX_APP_NAME

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h2 className="text-2xl font-light text-white">General</h2>
        <p className="mt-1 text-sm text-slate-400">
          These apply to the whole instance and to everyone who uses it, not just to you.
        </p>
      </div>

      {systemError && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
          {systemError}
        </div>
      )}

      {systemLoading || !system ? (
        <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading system settings…
        </div>
      ) : (
        <>
          <SettingsCard
            title="Application Name"
            description="Shown in the sidebar, on the sign-in screen, in the browser tab and in outgoing email."
          >
            <input
              value={system.app_name}
              maxLength={MAX_APP_NAME}
              onChange={(e) => setSystem({ ...system, app_name: e.target.value })}
              placeholder={DEFAULT_APP_NAME}
              aria-label="Application name"
              className="w-full"
            />
            {!nameValid && (
              <p className="text-xs text-red-400">
                A name is required, up to {MAX_APP_NAME} characters.
              </p>
            )}
          </SettingsCard>

          <SettingsCard
            title="Public URL"
            description="The address people reach this instance at. Every link in outgoing email is built from it, so it must be what a recipient can open — not what the server sees."
          >
            <input
              value={system.base_url}
              onChange={(e) => setSystem({ ...system, base_url: e.target.value })}
              placeholder="https://sentinel.example.com"
              inputMode="url"
              aria-label="Public URL"
              className="w-full"
            />
            <p className="text-xs text-slate-500">
              Leave empty to omit links from email rather than send broken ones.
            </p>
          </SettingsCard>

          <SettingsCard
            title="Default Check Interval"
            description="How often a newly created monitor checks, in seconds. Existing monitors keep their own interval."
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min={MIN_INTERVAL}
                max={MAX_INTERVAL}
                value={system.default_check_interval}
                onChange={(e) =>
                  setSystem({ ...system, default_check_interval: Number(e.target.value) })
                }
                aria-label="Default check interval in seconds"
                className="w-32"
              />
              <span className="text-sm text-slate-400">seconds</span>
            </div>
            {!intervalValid && (
              <p className="text-xs text-red-400">
                Must be between {MIN_INTERVAL} and {MAX_INTERVAL} seconds.
              </p>
            )}
          </SettingsCard>

          <SettingsCard
            title="Default SLA Target"
            description="The uptime percentage a monitor is held to when it has no target of its own. Reports compare against this unless a monitor overrides it."
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min={MIN_SLA_TARGET}
                max={MAX_SLA_TARGET}
                step={0.1}
                value={system.default_sla_target}
                onChange={(e) =>
                  setSystem({ ...system, default_sla_target: Number(e.target.value) })
                }
                aria-label="Default SLA target percentage"
                className="w-32"
              />
              <span className="text-sm text-slate-400">%</span>
            </div>
            {!slaValid && (
              <p className="text-xs text-red-400">
                Must be between {MIN_SLA_TARGET} and {MAX_SLA_TARGET}.
              </p>
            )}
          </SettingsCard>

          <SettingsCard
            title="Sentinel URLs"
            description="How monitored servers reach Sentinel. Leave both empty unless a reverse proxy makes the address a browser uses different from the one an agent can reach."
          >
            <label htmlFor="external-url" className="block text-sm font-medium text-white">
              External URL — where install scripts download from
            </label>
            <input
              id="external-url"
              type="text"
              value={system.sentinel_external_url}
              onChange={(e) => setSystem({ ...system, sentinel_external_url: e.target.value })}
              placeholder={`auto: ${window.location.origin}`}
              className="w-full"
            />
            <label htmlFor="internal-url" className="mt-3 block text-sm font-medium text-white">
              Internal URL — where agents report metrics
            </label>
            <input
              id="internal-url"
              type="text"
              value={system.sentinel_internal_url}
              onChange={(e) => setSystem({ ...system, sentinel_internal_url: e.target.value })}
              placeholder="same as the external URL"
              className="w-full"
            />
            <p className={`text-xs ${urlsValid ? 'text-slate-500' : 'text-red-400'}`}>
              {urlsValid
                ? 'Empty means Sentinel works the address out from your browser, which is right when there is no proxy. Include the scheme and port but no path, e.g. http://10.1.20.10:3001.'
                : 'Each URL must include a scheme and host and no path, e.g. https://sentinel.example.com or http://10.1.20.10:3001.'}
            </p>

            <div className="flex flex-wrap items-center gap-2 pt-1">
              <button
                className="btn-secondary !py-1"
                disabled={urlTesting || !urlsValid}
                onClick={() => void testURLs()}
              >
                {urlTesting ? 'Testing…' : 'Test both URLs'}
              </button>
              <span className="text-xs text-slate-500">
                Checked from the Sentinel server, which is what an agent has to reach
              </span>
            </div>

            {urlTest && (
              <div className="space-y-1">
                {(['external', 'internal'] as const).map((k) => {
                  const probe = urlTest[k]
                  return (
                    <p
                      key={k}
                      className={`text-xs ${probe.reachable ? 'text-emerald-400' : 'text-amber-300'}`}
                    >
                      {probe.reachable ? '✓' : '✕'} {k}: {probe.url || '(none)'} —{' '}
                      {probe.reachable ? `responded ${probe.status}` : probe.error}
                      <span className="text-slate-600"> ({probe.source})</span>
                    </p>
                  )
                })}
              </div>
            )}
          </SettingsCard>

          {/* Instance-wide rather than per-user: a rendered report is a file
              that gets emailed, downloaded and shared, so the times inside it
              must mean the same thing to everyone who opens it rather than
              depending on who pressed Generate. */}
          <SettingsCard
            title="Timezone"
            description="The zone reports are written in, and that times are displayed in across Sentinel."
          >
            <TimezoneSelector
              value={system.report_timezone}
              onChange={(tz) => setSystem({ ...system, report_timezone: tz })}
            />
            <p className="text-xs text-slate-500">
              Applies to generated PDFs, scheduled report emails and every timestamp on
              screen. Takes effect on the next report — nothing needs restarting.
            </p>
          </SettingsCard>

          <SettingsCard
            title="Data Retention"
            description="Old history is deleted automatically. The purge runs nightly at 2 AM."
          >
            {/* Two windows, because a check is a measurement and an
                incident is a conclusion drawn from many of them. Checks
                arrive one per monitor per interval, so they are far more
                numerous and rarely worth keeping as long. */}
            <label htmlFor="check-retention-days" className="block text-sm font-medium text-white">
              Keep check history for
            </label>
            <div className="flex items-center gap-2">
              <input
                id="check-retention-days"
                type="number"
                min={MIN_CHECK_RETENTION}
                max={MAX_CHECK_RETENTION}
                value={system.check_retention_days}
                onChange={(e) =>
                  setSystem({ ...system, check_retention_days: Number(e.target.value) })
                }
                aria-label="Check retention in days"
                className="w-32"
              />
              <span className="text-sm text-slate-400">days</span>
            </div>
            <p className={`mb-4 mt-1 text-xs ${checkRetentionValid ? 'text-slate-500' : 'text-red-400'}`}>
              {checkRetentionValid
                ? 'Every check is recorded, so this is the largest table by far — one monitor checked each minute adds over half a million rows a year. Response-time graphs and reports only reach back this far. Saved with the other system settings.'
                : `Must be between ${MIN_CHECK_RETENTION} and ${MAX_CHECK_RETENTION} days.`}
            </p>

            <label htmlFor="retention-days" className="block text-sm font-medium text-white">
              Keep incident history for
            </label>
            <div className="flex items-center gap-2">
              <input
                id="retention-days"
                type="number"
                min={retentionBounds.min}
                max={retentionBounds.max}
                value={retention ?? ''}
                onChange={(e) => setRetention(Number(e.target.value))}
                aria-label="Incident retention in days"
                className="w-32"
              />
              <span className="text-sm text-slate-400">days</span>
              <button
                className="btn-secondary !py-1"
                disabled={!retentionValid || retentionSaving || retention === retentionDays}
                onClick={() => void persistRetention()}
              >
                {retentionSaving ? 'Saving…' : 'Save'}
              </button>
            </div>
            {!retentionValid && retention != null && (
              <p className="text-xs text-red-400">
                Must be between {retentionBounds.min} and {retentionBounds.max} days.
              </p>
            )}
            {/* An ongoing incident is never purged however old it is: it is
                still happening, and is the one most likely to be looked at. */}
            <p className="text-xs text-slate-500">
              Only resolved incidents are removed. An ongoing incident is kept until it ends.
            </p>
          </SettingsCard>

          <ReconnectAgent push={push} />

          <div className="flex items-center justify-between border-t border-white/10 pt-4">
            <p className="text-xs text-slate-500">
              Who may create an account is under{' '}
              <Link className="text-primary-400 hover:underline" to="/settings/users">
                Users &amp; access
              </Link>
              .
            </p>
            <button
              className="btn-primary"
              disabled={systemSaving || !nameValid || !intervalValid || !checkRetentionValid || !urlsValid || !slaValid}
              onClick={() => void saveSystem()}
            >
              {systemSaving ? 'Saving…' : 'Save System Settings'}
            </button>
          </div>
        </>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
```

- [ ] **Step 8: Write `frontend/src/pages/settings/BackupsSettings.tsx`**

```tsx
import BackupRestore from '@/components/BackupRestore'
import { useToasts, Toaster } from '@/components/Toast'

/** Settings → Backups: download, schedule and restore database backups. */
export default function BackupsSettings() {
  const { toasts, push } = useToasts()
  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h2 className="text-2xl font-light text-white">Backups</h2>
        <p className="mt-1 text-sm text-slate-400">Copies of Sentinel&apos;s database, and restoring from one.</p>
      </div>
      <BackupRestore push={push} />
      <Toaster toasts={toasts} />
    </div>
  )
}
```

- [ ] **Step 9: Write `frontend/src/pages/settings/AboutSettings.tsx`**

```tsx
import { ExternalLink } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import { useAppConfig } from '@/context/AppConfigContext'
import { useSystemVersion } from '@/hooks/useSystemVersion'

const GITHUB_URL = 'https://github.com/Stevy2191/Sentinel'

/** True for a version CI actually tagged, e.g. "0.1.0" — false for "dev" or a branch name. */
const isReleasedVersion = (v: string) => /^\d+\.\d+\.\d+/.test(v)

/** Settings → About: what this instance is running, for everyone. */
export default function AboutSettings() {
  const { appName } = useAppConfig()
  const version = useSystemVersion()
  return (
    <div className="max-w-3xl space-y-6">
      <h2 className="text-2xl font-light text-white">About</h2>
      <SettingsCard title="Application">
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-slate-500">Name</dt>
            <dd className="font-medium">{appName}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Version</dt>
            <dd className="font-medium">
              {version === null ? (
                '…'
              ) : isReleasedVersion(version) ? (
                <a
                  className="hover:underline"
                  href={`${GITHUB_URL}/releases/tag/v${version}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  Sentinel v{version}
                </a>
              ) : (
                `Sentinel (development build)`
              )}
            </dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">License</dt>
            <dd className="font-medium">
              <a
                className="hover:underline"
                href={`${GITHUB_URL}/blob/main/LICENSE`}
                target="_blank"
                rel="noreferrer"
              >
                AGPL-3.0
              </a>
            </dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Frontend</dt>
            <dd className="font-medium">React + TypeScript + Vite</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Database</dt>
            <dd className="font-medium">PostgreSQL</dd>
          </div>
        </dl>
      </SettingsCard>
      <SettingsCard title="Links">
        <div className="flex flex-wrap gap-2">
          <a className="btn-secondary" href={`${GITHUB_URL}#readme`} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> Documentation
          </a>
          <a className="btn-secondary" href={GITHUB_URL} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> GitHub
          </a>
          <a className="btn-secondary" href={`${GITHUB_URL}/issues`} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> Report Issue
          </a>
        </div>
      </SettingsCard>
    </div>
  )
}
```

- [ ] **Step 10: Write `frontend/src/pages/settings/NetworkToolsSection.tsx`**

```tsx
import NetToolsSettings from '@/components/settings/NetToolsSettings'
import { useToasts, Toaster } from '@/components/Toast'

/** Settings → Network tools: the port-check allowlist, the Sentinel-server
 *  switch and how long run history is kept. */
export default function NetworkToolsSection() {
  const { toasts, push } = useToasts()
  return (
    <div className="max-w-3xl space-y-6">
      <h2 className="text-2xl font-light text-white">Network tools</h2>
      <NetToolsSettings push={push} />
      <Toaster toasts={toasts} />
    </div>
  )
}
```

- [ ] **Step 11: Route the Settings area in `frontend/src/App.tsx` and delete `pages/Settings.tsx`**

1. Replace the lazy import line `const Settings = lazy(() => import('@/pages/Settings'))` with

```tsx
const SettingsLayout = lazy(() => import('@/pages/settings/SettingsLayout'))
const SettingsIndex = lazy(() => import('@/pages/settings/SettingsIndex'))
const GeneralSettings = lazy(() => import('@/pages/settings/GeneralSettings'))
const BackupsSettings = lazy(() => import('@/pages/settings/BackupsSettings'))
const AboutSettings = lazy(() => import('@/pages/settings/AboutSettings'))
const NetworkToolsSection = lazy(() => import('@/pages/settings/NetworkToolsSection'))
const NotificationSettings = lazy(() => import('@/pages/NotificationSettings'))
```

2. Replace

```tsx
              <Route path="/settings" element={<Settings />} />
```

with

```tsx
              {/* One Settings area: the layout holds the section menu, each
                  section is a child route at its own address. */}
              <Route path="/settings" element={<SettingsLayout />}>
                <Route index element={<SettingsIndex />} />
                <Route path="general" element={<GeneralSettings />} />
                <Route path="notifications" element={<NotificationSettings />} />
                <Route path="backups" element={<BackupsSettings />} />
                <Route path="network-tools" element={<NetworkToolsSection />} />
                <Route path="about" element={<AboutSettings />} />
                {/* The profile page used to be Settings → Security. */}
                <Route path="security" element={<Navigate to="/profile" replace />} />
                <Route path="*" element={<SettingsIndex />} />
              </Route>
```

3. Delete the old top-level redirect and its comment (it is now the `security` child above):

```tsx
              {/* The profile page used to be Settings → Security. Kept as a
                  redirect so existing links and bookmarks still land. */}
              <Route path="/settings/security" element={<Navigate to="/profile" replace />} />
```

4. Delete the file: `git rm frontend/src/pages/Settings.tsx`.

- [ ] **Step 12: Point the in-app links at the new sections**

1. `frontend/src/pages/NetworkTools.tsx`: replace `to="/settings?tab=nettools"` with `to="/settings/network-tools"`.
2. `frontend/src/components/NotificationsSection.tsx`: in the "No notification channels configured" link, replace `to="/settings"` with `to="/settings/notifications"`.
3. `frontend/src/pages/Notifications.tsx`: replace `onConfigure={() => navigate('/settings#notifications')}` with `onConfigure={() => navigate('/settings/notifications')}`.

- [ ] **Step 13: Run the frontend gate**

Expected: exits 0 with no output.

- [ ] **Step 14: Commit**

```bash
git add frontend/src/utils/settingsNav.ts frontend/src/pages/settings frontend/src/App.tsx \
  frontend/src/pages/NetworkTools.tsx frontend/src/components/NotificationsSection.tsx frontend/src/pages/Notifications.tsx
git commit -m "feat(ux): one Settings area with a section menu

Settings becomes a layout with a menu (a select on phones) and each section
at its own address: General, Notifications channels, Backups, Network tools
and About come from the old tabbed page, which is split up and removed.
/settings opens the user's home section, old ?tab= links land on the
matching section, unknown addresses never show a blank page, and a
non-admin who opens an admin section is sent to About.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Move Users, notification history and the network setup pages into Settings

**Files:**
- Create: `frontend/src/components/LegacyRedirect.tsx`
- Modify: `frontend/src/App.tsx`, `frontend/src/pages/AdminUsers.tsx`, `frontend/src/pages/Notifications.tsx`, `frontend/src/pages/network/Credentials.tsx`, `frontend/src/pages/network/Profiles.tsx`, `frontend/src/pages/network/ProfileDetail.tsx`, `frontend/src/pages/network/MibLibrary.tsx`, `frontend/src/pages/network/NetworkSettings.tsx`, `frontend/src/components/network/TestWalkPanel.tsx`, `frontend/src/components/settings/NetToolsSettings.tsx`
- Test: the frontend gate (and Task 1's `movedPath` check covers the redirect mapping)

**Interfaces:**
- Consumes: Task 1 `movedPath(location, from, to)`; Task 2's `/settings` layout route (children are added to it).
- Produces: child routes `users`, `notifications/history`, `network/credentials`, `network/profiles`, `network/profiles/:id`, `network/mibs`, `network/polling`; redirects for every old address in the spec's table.

- [ ] **Step 1: Write `frontend/src/components/LegacyRedirect.tsx`**

```tsx
import { Navigate, useLocation } from 'react-router-dom'
import { movedPath } from '@/utils/navigation'

/** LegacyRedirect sends an address that moved (`from`) to its new home
 *  (`to`), keeping the rest of the path, the query and the hash, so old
 *  bookmarks and links keep working. */
export default function LegacyRedirect({ from, to }: { from: string; to: string }) {
  const location = useLocation()
  return <Navigate to={movedPath(location, from, to)} replace />
}
```

- [ ] **Step 2: Add the moved pages to the Settings layout and redirect the old addresses (`frontend/src/App.tsx`)**

1. Add the import next to the other eager imports:

```tsx
import LegacyRedirect from '@/components/LegacyRedirect'
```

2. Inside the `/settings` layout route from Task 2, after `<Route path="notifications" element={<NotificationSettings />} />`, add:

```tsx
                <Route path="notifications/history" element={<Notifications />} />
                <Route path="users" element={<AdminUsers />} />
                <Route path="network/credentials" element={<Credentials />} />
                <Route path="network/profiles" element={<Profiles />} />
                <Route path="network/profiles/:id" element={<ProfileDetail />} />
                <Route path="network/mibs" element={<MibLibrary />} />
                <Route path="network/polling" element={<NetworkSettings />} />
```

3. Replace these old routes

```tsx
              <Route path="/network/credentials" element={<Credentials />} />
              <Route path="/network/settings" element={<NetworkSettings />} />
              <Route path="/network/mibs" element={<MibLibrary />} />
```

with

```tsx
              {/* Network's setup pages moved into Settings; the old addresses
                  redirect so bookmarks and links still land. */}
              <Route path="/network/credentials" element={<LegacyRedirect from="/network/credentials" to="/settings/network/credentials" />} />
              <Route path="/network/settings" element={<LegacyRedirect from="/network/settings" to="/settings/network/polling" />} />
              <Route path="/network/mibs" element={<LegacyRedirect from="/network/mibs" to="/settings/network/mibs" />} />
```

4. Replace

```tsx
              <Route path="/network/profiles" element={<Profiles />} />
              <Route path="/network/profiles/:id" element={<ProfileDetail />} />
```

with

```tsx
              <Route path="/network/profiles/*" element={<LegacyRedirect from="/network/profiles" to="/settings/network/profiles" />} />
```

5. Replace

```tsx
              <Route path="/notifications" element={<Notifications />} />
```

with

```tsx
              <Route path="/notifications" element={<LegacyRedirect from="/notifications" to="/settings/notifications/history" />} />
```

6. Replace

```tsx
              {/* Admin-only page; AdminUsers itself redirects non-admins to /dashboard. */}
              <Route path="/admin/users" element={<AdminUsers />} />
```

with

```tsx
              {/* Users moved into Settings → Users & access. */}
              <Route path="/admin/users" element={<LegacyRedirect from="/admin/users" to="/settings/users" />} />
```

7. Leave `<Route path="/network/mibs/browse" element={<MibBrowser />} />` as it is (the MIB Browser keeps its address).

- [ ] **Step 3: Demote the moved pages' titles to section headings (plan ruling 1)**

Make exactly these replacements:

1. `frontend/src/pages/AdminUsers.tsx`: `<h1 className="vs-title text-4xl">Users</h1>` → `<h2 className="text-2xl font-light text-white">Users &amp; access</h2>`.
2. `frontend/src/pages/Notifications.tsx`: replace

```tsx
        <h1 className="text-2xl font-bold">Notifications</h1>
        <p className="text-sm text-slate-400">
          Configure notification channels and view alert history
        </p>
```

with

```tsx
        <h2 className="text-2xl font-light text-white">Notification history</h2>
        <p className="text-sm text-slate-400">
          The channels in use, this week&apos;s deliveries and every alert sent
        </p>
```

3. `frontend/src/pages/network/Credentials.tsx`: `<h1 className="text-4xl font-light text-white">Credentials</h1>` → `<h2 className="text-2xl font-light text-white">SNMP credentials</h2>`.
4. `frontend/src/pages/network/Profiles.tsx`: `<h1 className="text-4xl font-light text-white">Profiles</h1>` → `<h2 className="text-2xl font-light text-white">Device profiles</h2>`, and `<h1 className="text-4xl font-light text-white">Add this as a metric to which profile?</h1>` → `<h2 className="text-2xl font-light text-white">Add this as a metric to which profile?</h2>`.
5. `frontend/src/pages/network/ProfileDetail.tsx`: `<h1 className="text-4xl font-light text-white">{profile.name}</h1>` → `<h2 className="text-2xl font-light text-white">{profile.name}</h2>`.
6. `frontend/src/pages/network/MibLibrary.tsx`: `<h1 className="text-4xl font-light text-white">MIB library</h1>` → `<h2 className="text-2xl font-light text-white">MIB library</h2>`.
7. `frontend/src/pages/network/NetworkSettings.tsx`: `<h1 className="text-4xl font-light text-white">Network settings</h1>` → `<h2 className="text-2xl font-light text-white">Polling &amp; thresholds</h2>`.

- [ ] **Step 4: Point the moved pages' own links at the new addresses**

These are browser links (`navigate`/`Link`), not API calls; leave every `api.` path untouched.

1. `frontend/src/pages/network/Profiles.tsx`: replace every `/network/profiles` with `/settings/network/profiles` (5 places: two `navigate(\`/network/profiles/${…}\`)`, the `?addOid=` navigate, the back `Link to="/network/profiles"` and the row `Link to={\`/network/profiles/${p.id}\`}`).
2. `frontend/src/pages/network/ProfileDetail.tsx`: replace both `to="/network/profiles"` with `to="/settings/network/profiles"`.
3. `frontend/src/components/network/TestWalkPanel.tsx`: replace `` navigate(`/network/profiles?new=1&oid=… `` with `` navigate(`/settings/network/profiles?new=1&oid=… `` (only the path prefix changes).
4. `frontend/src/components/settings/NetToolsSettings.tsx`: replace `to="/admin/users"` with `to="/settings/users"`.

Then confirm nothing still links to an old address (expected: no output):

Run (repo root): `grep -rn "to=\"/admin/users\"\|'/admin/users'\|navigate(\`/network/profiles\|to=\"/network/profiles\|to={\`/network/profiles\|/settings?tab=\|/settings#" frontend/src --include=*.ts --include=*.tsx`

- [ ] **Step 5: Run the frontend gate**

Expected: exits 0 with no output.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/LegacyRedirect.tsx frontend/src/App.tsx frontend/src/pages/AdminUsers.tsx \
  frontend/src/pages/Notifications.tsx frontend/src/pages/network/Credentials.tsx frontend/src/pages/network/Profiles.tsx \
  frontend/src/pages/network/ProfileDetail.tsx frontend/src/pages/network/MibLibrary.tsx \
  frontend/src/pages/network/NetworkSettings.tsx frontend/src/components/network/TestWalkPanel.tsx \
  frontend/src/components/settings/NetToolsSettings.tsx
git commit -m "feat(ux): users, notification history and network setup live in Settings

Users & access, Notifications history, SNMP credentials, Device profiles,
the MIB library and Polling & thresholds move under /settings with
section-sized headings. Every old address (/admin/users, /notifications,
/network/credentials, /network/profiles/..., /network/mibs,
/network/settings) redirects to its new place, keeping ids and queries.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Dashboards and Status pages as one entry

**Files:**
- Create: `frontend/src/components/DashboardsHeader.tsx`
- Modify: `frontend/src/pages/dashboards/Dashboards.tsx`, `frontend/src/pages/StatusPages.tsx`
- Test: the frontend gate

**Interfaces:**
- Produces: `DashboardsHeader({ active: 'dashboards' | 'status'; onNewDashboard?: () => void })`; `/dashboards?new=1` opens the new-dashboard dialog (plan ruling 4).

- [ ] **Step 1: Write `frontend/src/components/DashboardsHeader.tsx`**

```tsx
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ChevronDown, Plus } from 'lucide-react'

type Tab = 'dashboards' | 'status'

const TABS: { key: Tab; to: string; label: string }[] = [
  { key: 'dashboards', to: '/dashboards', label: 'Dashboards' },
  { key: 'status', to: '/status-pages', label: 'Status pages' },
]

/** DashboardsHeader is the shared top of the Dashboards and Status pages
 *  lists: one sidebar entry, two tabs, and one "+ New" that makes either. A
 *  new dashboard from the Status pages tab goes to /dashboards?new=1, which
 *  opens the new-dashboard dialog there. */
export default function DashboardsHeader({ active, onNewDashboard }: { active: Tab; onNewDashboard?: () => void }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  const newDashboard = () => {
    setOpen(false)
    if (onNewDashboard) onNewDashboard()
    else navigate('/dashboards?new=1')
  }
  const newStatusPage = () => {
    setOpen(false)
    navigate('/status-pages/create')
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Dashboards</h1>
          <p className="mt-2 text-slate-400">
            Your own views, site views, screens for the wall, and public status pages.
          </p>
        </div>
        <div
          className="relative"
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setOpen(false)
          }}
        >
          <button
            className="btn-primary flex items-center gap-2"
            aria-haspopup="menu"
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
          >
            <Plus className="h-4 w-4" /> New <ChevronDown className="h-4 w-4" />
          </button>
          {open && (
            <div
              role="menu"
              className="absolute right-0 z-20 mt-2 w-48 overflow-hidden rounded-lg border border-white/10 bg-slate-900 shadow-xl"
            >
              <button
                role="menuitem"
                className="block w-full px-4 py-2 text-left text-sm text-slate-200 hover:bg-white/5"
                onClick={newDashboard}
              >
                Dashboard
              </button>
              <button
                role="menuitem"
                className="block w-full px-4 py-2 text-left text-sm text-slate-200 hover:bg-white/5"
                onClick={newStatusPage}
              >
                Status page
              </button>
            </div>
          )}
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        {TABS.map((t) => (
          <Link
            key={t.key}
            to={t.to}
            aria-current={active === t.key ? 'page' : undefined}
            className={`rounded-md px-4 py-2 text-sm font-medium ${
              active === t.key ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300 hover:bg-white/10'
            }`}
          >
            {t.label}
          </Link>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Use it on the Dashboards list (`frontend/src/pages/dashboards/Dashboards.tsx`)**

1. Replace the imports

```tsx
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Globe2, LayoutDashboard, Loader2, Plus } from 'lucide-react'
```

with

```tsx
import { useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Globe2, LayoutDashboard, Loader2 } from 'lucide-react'
import DashboardsHeader from '@/components/DashboardsHeader'
```

2. Replace `  const [creating, setCreating] = useState(false)` with

```tsx
  const [searchParams, setSearchParams] = useSearchParams()
  // ?new=1 (from the Status pages tab's "+ New → Dashboard") opens the dialog.
  const [creating, setCreating] = useState(() => searchParams.get('new') === '1')
  const closeCreate = () => {
    setCreating(false)
    if (searchParams.has('new')) setSearchParams({}, { replace: true })
  }
```

3. Replace the header block

```tsx
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Dashboards</h1>
          <p className="mt-2 text-slate-400">Your own views, site views for everyone at a site, and screens for the wall.</p>
        </div>
        <button className="btn-primary flex items-center gap-2" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" /> New dashboard
        </button>
      </div>
```

with

```tsx
      <DashboardsHeader active="dashboards" onNewDashboard={() => setCreating(true)} />
```

4. Replace `<NewDashboardModal onClose={() => setCreating(false)} onCreated={(d) => navigate(\`/dashboards/${d.id}/edit\`)} />` with `<NewDashboardModal onClose={closeCreate} onCreated={(d) => navigate(\`/dashboards/${d.id}/edit\`)} />`.

- [ ] **Step 3: Use it on the Status pages list (`frontend/src/pages/StatusPages.tsx`)**

1. Add `import DashboardsHeader from '@/components/DashboardsHeader'` with the other component imports.
2. In `StatusPageList`, replace the header block

```tsx
      <div className="flex items-center justify-between">
        <div>
          <h1 className="vs-title text-4xl">Status pages</h1>
          <p className="text-sm text-slate-400">
            Create and manage public status dashboards
          </p>
        </div>
        <button className="btn-primary" onClick={() => navigate('/status-pages/create')}>
          <Plus className="h-4 w-4" /> Create New Status Page
        </button>
      </div>
```

with

```tsx
      <DashboardsHeader active="status" />
```

3. If `Plus` is no longer used anywhere in `StatusPages.tsx`, remove it from the `lucide-react` import (eslint reports an unused import otherwise). Keep `navigate` in `StatusPageList` if it is still used by the row buttons (it is: detail/edit).

- [ ] **Step 4: Run the frontend gate**

Expected: exits 0 with no output.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/DashboardsHeader.tsx frontend/src/pages/dashboards/Dashboards.tsx frontend/src/pages/StatusPages.tsx
git commit -m "feat(ux): Dashboards and Status pages share one entry

Both lists open under the Dashboards sidebar entry with Dashboards / Status
pages tabs and one \"+ New\" that creates either; \"+ New → Dashboard\" from the
Status pages tab opens the new-dashboard dialog via /dashboards?new=1.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Status doc and final checks

**Files:**
- Modify: `docs/superpowers/STATUS.md`

**Interfaces:** none.

- [ ] **Step 1: Bring STATUS.md up to date**

1. In the network roadmap table, replace the phase 5 state `Done (\`feature/network-phase5\`, awaiting merge to \`dev\`); follow-ups in \`plans/2026-10-03-network-phase5-followups.md\`` with `Done (\`dev\`, 2026-10-05); follow-ups in \`plans/2026-10-03-network-phase5-followups.md\``.
2. In the tools and security table, replace the S1 state `Done (\`feature/tools-s1\`, awaiting merge to \`dev\`); follow-ups in \`plans/2026-10-05-tools-s1-followups.md\`` with `Done (\`dev\`, 2026-10-07); follow-ups in \`plans/2026-10-05-tools-s1-followups.md\``.
3. Insert this section immediately before the line `## To be checked by the owner`:

```markdown
## UX reorganization

Spec: `docs/superpowers/specs/2026-10-07-ux-reorganization-design.md` (agreed 2026-10-07). Four pieces, in this order; each gets its own plan.

| Piece | What | State |
|---|---|---|
| 1 | Navigation and Settings: one grouped sidebar (Network folded in, Dashboards and Status Pages as one entry), one Settings area with a section menu, old addresses redirect | Done (`feature/ux-piece1`, awaiting merge to `dev`); plan `plans/2026-10-07-ux-reorg-piece1-navigation-settings.md` |
| 2 | Combined Monitoring list: uptime checks, network devices and server agents on one page with filters and one "+ Add" | Not started |
| 3 | Site profiles: address, network information, ISPs and circuits per site | Not started |
| 4 | Dashboards and status pages merged: status pages become publishable dashboards | Not started |
```

4. Immediately after the line `## To be checked by the owner`, insert:

```markdown
### UX reorganization piece 1 (on the dev stack)

1. The sidebar reads Overview, Dashboards, then Monitor (Uptime, Servers, Devices, Sites, SSL & Domains), Respond (Incidents, Reports) and Tools (Network Tools, MIB Browser); opening a device or site no longer swaps the menu.
2. Each entry stays highlighted on its child pages: a monitor, a server, a device's port, a site, an incident, a report, a tool run, a status page's editor.
3. Dashboards shows Dashboards / Status pages tabs; "+ New" creates a dashboard or a status page, from either tab.
4. Every Settings section opens at its own address and works as before: save a General setting, add a notification channel, view notification history and retry a failed one, invite a user, download a backup, add an SNMP credential, open a device profile, upload a MIB, change a polling threshold, edit the network tools allowlist.
5. Old addresses land in the new place: `/admin/users`, `/notifications`, `/network/credentials`, `/network/profiles` (and a profile's page), `/network/mibs`, `/network/settings`, `/settings?tab=nettools`. The MIB browser's "add as metric" still opens the profile picker with the OID filled in.
6. As a non-admin, the Settings menu shows only Notifications → History and About, and an admin-only address (e.g. `/settings/users`) sends you to About.
7. At phone width, the sidebar drawer works and the Settings section select switches sections.
```

5. Change the `Updated` date at the top to `Updated 2026-10-07.` if it is not already.

- [ ] **Step 2: Run the frontend gate once more on the whole branch**

Expected: exits 0 with no output.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/STATUS.md
git commit -m "docs(ux): record piece 1 and its owner checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

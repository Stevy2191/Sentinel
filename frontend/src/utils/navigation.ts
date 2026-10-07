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

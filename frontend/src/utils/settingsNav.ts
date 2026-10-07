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

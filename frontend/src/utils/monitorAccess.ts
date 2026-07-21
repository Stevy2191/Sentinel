import type { Monitor } from '@/types'

export type AccessPermission = 'owner' | 'admin' | 'editable' | 'readonly'

export interface MonitorAccess {
  permission: AccessPermission
  isOwner: boolean
  canEdit: boolean
  canDelete: boolean
  // Badge shown on non-owned monitors (undefined when the current user owns it).
  badge?: { label: string; tone: 'readonly' | 'editable' | 'admin' }
}

/**
 * monitorAccess derives the current user's capabilities from the backend's
 * is_owner/permission fields. Defaults to full ownership when those fields are
 * absent (e.g. older responses), so nothing breaks.
 */
export function monitorAccess(m: Pick<Monitor, 'is_owner' | 'permission'>): MonitorAccess {
  const permission = (m.permission ?? 'owner') as AccessPermission
  const isOwner = m.is_owner ?? true
  const canEdit = isOwner || permission === 'editable' || permission === 'admin'
  const canDelete = isOwner || permission === 'admin'

  let badge: MonitorAccess['badge']
  if (!isOwner) {
    if (permission === 'editable') badge = { label: 'Can edit', tone: 'editable' }
    else if (permission === 'admin') badge = { label: 'Admin', tone: 'admin' }
    else badge = { label: 'Read-only', tone: 'readonly' }
  }

  return { permission, isOwner, canEdit, canDelete, badge }
}

// Tailwind classes for a permission badge tone.
export const badgeToneClass: Record<'readonly' | 'editable' | 'admin', string> = {
  readonly: 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300',
  editable: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
  admin: 'bg-info-100 text-info-700 dark:bg-info-900/40 dark:text-info-300',
}

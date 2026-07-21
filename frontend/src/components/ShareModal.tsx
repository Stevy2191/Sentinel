import { useMemo, useState } from 'react'
import { X, Trash2, Loader2, Share2 } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useUsers } from '@/hooks/useUsers'
import {
  useMonitorShares,
  useShareMonitor,
  useUpdateMonitorShare,
  useRevokeMonitorShare,
  type SharePermission,
} from '@/hooks/useMonitorSharing'

const selectCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  monitorId: string
  onClose: () => void
  onChanged?: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

function permLabel(p: SharePermission) {
  return p === 'editable' ? 'Can edit' : 'Read-only'
}

export default function ShareModal({ monitorId, onClose, onChanged, push }: Props) {
  const { currentUser } = useAuthContext()
  const { users } = useUsers()
  const { shares, loading: sharesLoading, refetch } = useMonitorShares(monitorId)
  const { share, loading: sharing } = useShareMonitor()
  const { update } = useUpdateMonitorShare()
  const { revoke } = useRevokeMonitorShare()

  const [selectedUserId, setSelectedUserId] = useState('')
  const [permission, setPermission] = useState<SharePermission>('readonly')
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null)
  const [busyUser, setBusyUser] = useState<string | null>(null)

  // Users who can still be shared with: not the owner (self) and not already shared.
  const availableUsers = useMemo(() => {
    const sharedIds = new Set(shares.map((s) => s.shared_with_user_id))
    return users.filter((u) => u.id !== currentUser?.user_id && !sharedIds.has(u.id))
  }, [users, shares, currentUser])

  const afterChange = async () => {
    await refetch()
    onChanged?.()
  }

  const handleShare = async () => {
    if (!selectedUserId) {
      push('Select a user to share with', 'error')
      return
    }
    try {
      await share(monitorId, selectedUserId, permission)
      push('Monitor shared', 'success')
      setSelectedUserId('')
      setPermission('readonly')
      await afterChange()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to share', 'error')
    }
  }

  const handleUpdate = async (userId: string, next: SharePermission) => {
    setBusyUser(userId)
    try {
      await update(monitorId, userId, next)
      push('Permission updated', 'success')
      await afterChange()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to update', 'error')
    } finally {
      setBusyUser(null)
    }
  }

  const handleRevoke = async (userId: string) => {
    setBusyUser(userId)
    try {
      await revoke(monitorId, userId)
      push('Access revoked', 'success')
      setConfirmRevoke(null)
      await afterChange()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to revoke', 'error')
    } finally {
      setBusyUser(null)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="card flex max-h-[85vh] w-full max-w-md flex-col p-6"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="mb-4 flex items-start justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-lg font-semibold">
              <Share2 className="h-5 w-5 text-primary-600" /> Share Monitor
            </h2>
            <p className="text-sm text-neutral-500 dark:text-neutral-400">Control who can access this monitor</p>
          </div>
          <button className="text-neutral-400 hover:text-neutral-600" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Grant access */}
        <div className="mb-4 space-y-3 border-b border-neutral-200 pb-4 dark:border-neutral-800">
          <h3 className="text-sm font-semibold">Grant access</h3>
          <select className={selectCls} value={selectedUserId} onChange={(e) => setSelectedUserId(e.target.value)}>
            <option value="">Select user to share with…</option>
            {availableUsers.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
                {u.email ? ` (${u.email})` : ''}
              </option>
            ))}
          </select>
          {availableUsers.length === 0 && (
            <p className="text-xs text-neutral-400">No other users available to share with.</p>
          )}

          <div className="space-y-1.5">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="permission"
                checked={permission === 'readonly'}
                onChange={() => setPermission('readonly')}
              />
              Read-only <span className="text-xs text-neutral-400">(view &amp; alerts)</span>
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="permission"
                checked={permission === 'editable'}
                onChange={() => setPermission('editable')}
              />
              Can edit <span className="text-xs text-neutral-400">(modify, pause, maintenance)</span>
            </label>
          </div>

          <button
            className="btn-primary w-full"
            disabled={!selectedUserId || sharing}
            onClick={() => void handleShare()}
          >
            {sharing ? 'Sharing…' : 'Share'}
          </button>
        </div>

        {/* Current access */}
        <div className="flex-1 overflow-y-auto">
          <h3 className="mb-2 text-sm font-semibold">Current access</h3>
          {sharesLoading ? (
            <div className="flex justify-center py-4 text-neutral-400">
              <Loader2 className="h-5 w-5 animate-spin" />
            </div>
          ) : shares.length === 0 ? (
            <p className="text-sm text-neutral-400">No one else has access yet.</p>
          ) : (
            <div className="space-y-2">
              {shares.map((s) => (
                <div key={s.shared_with_user_id} className="rounded-md border border-neutral-200 p-3 dark:border-neutral-800">
                  <div className="font-medium">{s.username}</div>
                  {s.email && <div className="text-xs text-neutral-400">{s.email}</div>}
                  {confirmRevoke === s.shared_with_user_id ? (
                    <div className="mt-2 flex items-center justify-between gap-2">
                      <span className="text-xs text-neutral-500 dark:text-neutral-400">Remove {s.username}?</span>
                      <div className="flex gap-1.5">
                        <button className="btn-secondary !py-1" onClick={() => setConfirmRevoke(null)}>
                          Cancel
                        </button>
                        <button
                          className="btn bg-error-600 !py-1 text-white hover:bg-error-700"
                          disabled={busyUser === s.shared_with_user_id}
                          onClick={() => void handleRevoke(s.shared_with_user_id)}
                        >
                          Remove
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-2 flex items-center gap-2">
                      <select
                        className="flex-1 rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-800"
                        value={s.permission}
                        disabled={busyUser === s.shared_with_user_id}
                        onChange={(e) => void handleUpdate(s.shared_with_user_id, e.target.value as SharePermission)}
                        aria-label={`Permission for ${s.username}`}
                      >
                        <option value="readonly">{permLabel('readonly')}</option>
                        <option value="editable">{permLabel('editable')}</option>
                      </select>
                      <button
                        className="btn-secondary !px-2 !py-1 text-error-600"
                        title={`Revoke ${s.username}`}
                        onClick={() => setConfirmRevoke(s.shared_with_user_id)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

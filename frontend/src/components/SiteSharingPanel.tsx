import { useMemo, useState } from 'react'
import { Loader2, Share2, Trash2 } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useUsers } from '@/hooks/useUsers'
import { useSiteShares, type SitePermission } from '@/hooks/useSites'
import type { ApiError } from '@/services/api'

const selectCls =
  'rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500'

function permLabel(p: SitePermission) {
  return p === 'editable' ? 'Can edit' : 'Read-only'
}

/** Admin-only: who else can see this site. Inline on the site page rather than
 *  a modal, since sharing is part of what a site is. */
export default function SiteSharingPanel({ siteId }: { siteId: string }) {
  const { currentUser } = useAuthContext()
  const { users } = useUsers()
  const { shares, loading, share, unshare } = useSiteShares(siteId, true)
  const [userId, setUserId] = useState('')
  const [permission, setPermission] = useState<SitePermission>('readonly')
  const [busyUser, setBusyUser] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Not yourself (an admin already sees every site) and not anyone already listed.
  const available = useMemo(() => {
    const shared = new Set(shares.map((s) => s.shared_with_user_id))
    return users.filter((u) => u.id !== currentUser?.user_id && !shared.has(u.id))
  }, [users, shares, currentUser])

  const attempt = async (who: string, fn: () => Promise<void>) => {
    setBusyUser(who)
    setError(null)
    try {
      await fn()
    } catch (err) {
      setError((err as ApiError).message || 'Something went wrong')
    } finally {
      setBusyUser(null)
    }
  }

  return (
    <div className="card space-y-4 p-6">
      <div>
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <Share2 className="h-5 w-5 text-primary-400" /> Sharing
        </h2>
        <p className="text-sm text-slate-400">
          Admins see every site. Share it with anyone else who needs it. Read-only lets them view it; can edit also lets
          them change what is in it, but not rename, delete or reshare it.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        <select className={`${selectCls} min-w-[12rem] flex-1`} value={userId} onChange={(e) => setUserId(e.target.value)}>
          <option value="">Select a user…</option>
          {available.map((u) => (
            <option key={u.id} value={u.id}>
              {u.username}
              {u.email ? ` (${u.email})` : ''}
            </option>
          ))}
        </select>
        <select className={selectCls} value={permission} onChange={(e) => setPermission(e.target.value as SitePermission)}>
          <option value="readonly">{permLabel('readonly')}</option>
          <option value="editable">{permLabel('editable')}</option>
        </select>
        <button
          className="btn-primary"
          disabled={!userId || busyUser !== null}
          onClick={() =>
            void attempt(userId, async () => {
              await share(userId, permission)
              setUserId('')
              setPermission('readonly')
            })
          }
        >
          Share
        </button>
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <div className="flex justify-center py-4 text-slate-400">
          <Loader2 className="h-5 w-5 animate-spin" />
        </div>
      ) : shares.length === 0 ? (
        <p className="text-sm text-slate-400">Not shared with anyone yet.</p>
      ) : (
        <div className="divide-y divide-white/10">
          {shares.map((s) => (
            <div key={s.shared_with_user_id} className="flex flex-wrap items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium text-white">{s.username}</div>
                {s.email && <div className="truncate text-xs text-slate-400">{s.email}</div>}
              </div>
              <select
                className={selectCls}
                value={s.permission}
                disabled={busyUser === s.shared_with_user_id}
                aria-label={`Permission for ${s.username}`}
                onChange={(e) =>
                  void attempt(s.shared_with_user_id, () => share(s.shared_with_user_id, e.target.value as SitePermission))
                }
              >
                <option value="readonly">{permLabel('readonly')}</option>
                <option value="editable">{permLabel('editable')}</option>
              </select>
              <button
                className="btn-secondary !px-2 !py-1 text-red-400"
                title={`Remove ${s.username}`}
                disabled={busyUser === s.shared_with_user_id}
                onClick={() => void attempt(s.shared_with_user_id, () => unshare(s.shared_with_user_id))}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

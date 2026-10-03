import { useEffect, useMemo, useRef, useState } from 'react'
import { Trash2, X } from 'lucide-react'
import { useUsers } from '@/hooks/useUsers'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { Dashboard, DashboardShare } from '@/types/dashboards'
import { inputCls } from '@/utils/dashboards'

/** Who a personal dashboard is shared with. A site dashboard follows its site's sharing instead. */
export default function DashboardSharingPanel({ dashboard, onClose }: { dashboard: Dashboard; onClose: () => void }) {
  const actions = useDashboardActions()
  const { users } = useUsers()
  const [shares, setShares] = useState<DashboardShare[] | null>(null)
  const [userId, setUserId] = useState('')
  const [permission, setPermission] = useState<'readonly' | 'editable'>('readonly')
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    actions
      .listShares(dashboard.id)
      .then((s) => {
        if (!cancelled) setShares(s)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Could not load the shares')
      })
    return () => {
      cancelled = true
    }
  }, [actions, dashboard.id, version])

  const available = useMemo(
    () => users.filter((u) => u.id !== dashboard.owner_id && !shares?.some((s) => s.user_id === u.id)),
    [users, shares, dashboard.owner_id],
  )

  const run = async (fn: () => Promise<void>) => {
    setError(null)
    try {
      await fn()
      if (mounted.current) setVersion((v) => v + 1)
    } catch (err) {
      if (mounted.current) setError((err as ApiError).message || 'That did not work')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Share “{dashboard.name}”</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">People see only the widgets whose devices and monitors they can already see.</p>
        <div className="flex gap-2">
          <select className={inputCls} value={userId} onChange={(e) => setUserId(e.target.value)}>
            <option value="">Choose a person…</option>
            {available.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
              </option>
            ))}
          </select>
          <select className={`${inputCls} w-36`} value={permission} onChange={(e) => setPermission(e.target.value as 'readonly' | 'editable')}>
            <option value="readonly">Can view</option>
            <option value="editable">Can edit</option>
          </select>
          <button className="btn-primary" disabled={!userId} onClick={() => void run(async () => { await actions.share(dashboard.id, userId, permission); setUserId('') })}>
            Share
          </button>
        </div>
        {error && <p className="text-sm text-red-400">{error}</p>}
        <ul className="divide-y divide-white/10">
          {(shares ?? []).map((s) => (
            <li key={s.user_id} className="flex items-center justify-between gap-2 py-2 text-sm">
              <span className="text-slate-200">{s.username}</span>
              <span className="flex items-center gap-3">
                <select
                  className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-xs"
                  value={s.permission}
                  onChange={(e) => void run(() => actions.share(dashboard.id, s.user_id, e.target.value as 'readonly' | 'editable'))}
                >
                  <option value="readonly">Can view</option>
                  <option value="editable">Can edit</option>
                </select>
                <button className="text-slate-400 hover:text-red-400" aria-label={`Stop sharing with ${s.username}`} onClick={() => void run(() => actions.unshare(dashboard.id, s.user_id))}>
                  <Trash2 className="h-4 w-4" />
                </button>
              </span>
            </li>
          ))}
          {shares?.length === 0 && <li className="py-2 text-sm text-slate-500">Not shared with anyone yet.</li>}
        </ul>
      </div>
    </div>
  )
}

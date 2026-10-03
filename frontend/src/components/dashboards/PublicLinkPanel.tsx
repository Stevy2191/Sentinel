import { useEffect, useRef, useState } from 'react'
import { Copy, X } from 'lucide-react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { PublicLinkInfo } from '@/types/dashboards'
import { inputCls, widgetInfo } from '@/utils/dashboards'

interface Props {
  dashboardId: string
  onClose: () => void
  /** The dashboard's published state changed: refetch it. */
  onChanged: () => void
}

/** Admin only: the dashboard's public link, for outsiders and wall displays. */
export default function PublicLinkPanel({ dashboardId, onClose, onChanged }: Props) {
  const actions = useDashboardActions()
  const [info, setInfo] = useState<PublicLinkInfo | null>(null)
  const [confirm, setConfirm] = useState<'regenerate' | 'revoke' | null>(null)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const mounted = useRef(true)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    actions
      .getPublicLink(dashboardId)
      .then((i) => {
        if (!cancelled) setInfo(i)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Could not load the link')
      })
    return () => {
      cancelled = true
    }
  }, [actions, dashboardId, version])

  const url = info?.link ? `${window.location.origin}/public/dashboards/${info.link.token}` : null

  const run = async (fn: () => Promise<unknown>) => {
    setError(null)
    setConfirm(null)
    setCopied(false)
    try {
      await fn()
      if (mounted.current) setVersion((v) => v + 1)
      onChanged()
    } catch (err) {
      if (mounted.current) setError((err as ApiError).message || 'That did not work')
    }
  }

  // navigator.clipboard exists only in a secure context. Without it, or when the write
  // is refused, select the link so it can be copied by hand.
  const copy = async (text: string) => {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(text)
      if (mounted.current) setCopied(true)
    } catch {
      inputRef.current?.select()
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Public link</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">
          Anyone with the link sees this dashboard without signing in, so a TV can show it for days. IP addresses, hostnames,
          monitor URLs, error details and notes are left out, and nothing on it links into Sentinel. While it has a link, only
          admins can change or delete the dashboard.
        </p>
        {info && info.broad_widgets.length > 0 && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">
            These widgets show everything rather than chosen items, so the link will also show things added later:
            <ul className="mt-1 list-disc pl-5">
              {info.broad_widgets.map((w) => (
                <li key={w.id}>{w.title || widgetInfo(w.type).label}</li>
              ))}
            </ul>
          </div>
        )}
        {error && <p className="text-sm text-red-400">{error}</p>}
        {!info ? null : url ? (
          <>
            <div className="flex gap-2">
              <input ref={inputRef} className={inputCls} readOnly value={url} onFocus={(e) => e.target.select()} />
              <button
                className="btn-secondary flex items-center gap-1"
                onClick={() => void copy(url)}
              >
                <Copy className="h-4 w-4" /> {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            {confirm ? (
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <span className="text-amber-300">
                  {confirm === 'revoke' ? 'Turn the link off? Screens using it go blank.' : 'Make a new link? The current one stops working at once.'}
                </span>
                <button className="btn-secondary" onClick={() => setConfirm(null)}>
                  Keep it
                </button>
                <button
                  className="btn bg-red-600 text-white hover:bg-red-700"
                  onClick={() => void run(() => (confirm === 'revoke' ? actions.revokePublicLink(dashboardId) : actions.createPublicLink(dashboardId)))}
                >
                  {confirm === 'revoke' ? 'Turn off' : 'Make a new link'}
                </button>
              </div>
            ) : (
              <div className="flex gap-2">
                <button className="btn-secondary" onClick={() => setConfirm('regenerate')}>
                  New link
                </button>
                <button className="btn-secondary text-red-400" onClick={() => setConfirm('revoke')}>
                  Turn off
                </button>
              </div>
            )}
          </>
        ) : (
          <button className="btn-primary" onClick={() => void run(() => actions.createPublicLink(dashboardId))}>
            Create a public link
          </button>
        )}
      </div>
    </div>
  )
}

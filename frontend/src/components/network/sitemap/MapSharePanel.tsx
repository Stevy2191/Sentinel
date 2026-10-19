import { useState } from 'react'
import { Copy, X } from 'lucide-react'
import { useMapPublicLink } from '@/hooks/useSiteMap'

/** Admin only: the map's read-only public link, for outsiders and wall
 *  displays. It shows names, statuses and traffic; no addresses. */
export default function MapSharePanel({ siteId, onClose }: { siteId: string; onClose: () => void }) {
  const link = useMapPublicLink(siteId)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const url = link.link ? `${window.location.origin}/public/maps/${link.link.token}` : null
  const act = async (f: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await f()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg space-y-4 p-6" role="dialog" aria-label="Public link" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Public link</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">
          Anyone with the link sees this map read-only, live: the routers' and switches' names, their state and the traffic on their links. No
          addresses, MACs or serial numbers.
        </p>
        {error && <p className="text-sm text-red-400">{error}</p>}
        {link.loading ? (
          <p className="text-sm text-slate-400">Loading…</p>
        ) : url ? (
          <div className="space-y-3">
            <div className="flex gap-2">
              <input className="rd-input flex-1 font-mono text-xs" readOnly value={url} onFocus={(e) => e.currentTarget.select()} aria-label="Public link" />
              <button
                type="button"
                className="btn-secondary flex items-center gap-1.5"
                onClick={() => {
                  void navigator.clipboard?.writeText(url).then(() => setCopied(true))
                }}
              >
                <Copy className="h-4 w-4" /> {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            {confirm ? (
              <div className="flex items-center gap-2 text-sm text-slate-300">
                Turn the link off? Anyone using it loses the map at once.
                <button type="button" className="rounded bg-red-600 px-2 py-1 text-white hover:bg-red-500" disabled={busy} onClick={() => void act(link.revoke)}>
                  Turn off
                </button>
                <button type="button" className="text-slate-400 hover:text-white" onClick={() => setConfirm(false)}>
                  Cancel
                </button>
              </div>
            ) : (
              <div className="flex gap-2">
                <button type="button" className="btn-secondary" disabled={busy} onClick={() => void act(link.create)}>
                  New link
                </button>
                <button type="button" className="btn-secondary" onClick={() => setConfirm(true)}>
                  Turn off
                </button>
              </div>
            )}
          </div>
        ) : (
          <button type="button" className="btn-primary" disabled={busy} onClick={() => void act(link.create)}>
            Create public link
          </button>
        )}
      </div>
    </div>
  )
}

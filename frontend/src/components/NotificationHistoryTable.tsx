import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowUpDown, Info, RotateCcw, Loader2 } from 'lucide-react'
import type { NotificationHistoryItem, NotificationStatus } from '@/types'
import { formatDatetime } from '@/utils/formatters'

type SortKey = 'monitor' | 'channel' | 'status' | 'created_at'

const statusBadge: Record<NotificationStatus, string> = {
  sent: 'bg-emerald-500/20 text-emerald-400',
  failed: 'bg-red-500/20 text-red-400',
  pending: 'bg-amber-500/20 text-amber-400',
}

interface Props {
  items: NotificationHistoryItem[]
  loading: boolean
  error?: string | null
  onRetry?: (item: NotificationHistoryItem) => void
  retryingId?: string | null
  onReload?: () => void
}

export default function NotificationHistoryTable({
  items,
  loading,
  error,
  onRetry,
  retryingId,
  onReload,
}: Props) {
  const [sortKey, setSortKey] = useState<SortKey>('created_at')
  const [sortAsc, setSortAsc] = useState(false)
  const [details, setDetails] = useState<NotificationHistoryItem | null>(null)

  const sorted = useMemo(() => {
    return [...items].sort((a, b) => {
      let cmp = 0
      if (sortKey === 'monitor') cmp = (a.monitor_name ?? '').localeCompare(b.monitor_name ?? '')
      else if (sortKey === 'channel') cmp = a.channel.localeCompare(b.channel)
      else if (sortKey === 'status') cmp = a.status.localeCompare(b.status)
      else cmp = a.created_at.localeCompare(b.created_at)
      return sortAsc ? cmp : -cmp
    })
  }, [items, sortKey, sortAsc])

  const toggle = (k: SortKey) => {
    if (sortKey === k) setSortAsc((s) => !s)
    else {
      setSortKey(k)
      setSortAsc(false)
    }
  }

  const Th = ({ k, label }: { k: SortKey; label: string }) => (
    <th className="px-4 py-3 font-medium">
      <button className="flex items-center gap-1" onClick={() => toggle(k)}>
        {label} <ArrowUpDown className="h-3 w-3" />
      </button>
    </th>
  )

  if (error) {
    return (
      <div className="card flex items-center justify-between border-error-300 p-4">
        <span className="text-red-400">{error}</span>
        {onReload && (
          <button className="btn-secondary" onClick={onReload}>
            Retry
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="card overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-white/10 text-slate-400">
            <tr>
              <Th k="monitor" label="Monitor" />
              <Th k="channel" label="Channel" />
              <Th k="status" label="Status" />
              <Th k="created_at" label="Sent at" />
              <th className="px-4 py-3 font-medium">Error</th>
              <th className="px-4 py-3 text-right font-medium">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {loading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <tr key={i} className="animate-pulse">
                  <td colSpan={6} className="px-4 py-4">
                    <div className="h-4 rounded bg-white/10" />
                  </td>
                </tr>
              ))
            ) : sorted.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-slate-500">
                  No notifications.
                </td>
              </tr>
            ) : (
              sorted.map((n, i) => (
                <tr key={n.id} className={i % 2 ? 'bg-white/5' : ''}>
                  <td className="px-4 py-3">
                    <Link to={`/monitors/${n.monitor_id}`} className="text-primary-400 hover:underline">
                      {n.monitor_name || n.monitor_id.slice(0, 8)}
                    </Link>
                  </td>
                  <td className="px-4 py-3 capitalize text-slate-300">
                    {n.channel}
                  </td>
                  <td className="px-4 py-3">
                    <span className={`rounded-md px-2 py-1 text-xs font-medium ${statusBadge[n.status]}`}>
                      {n.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-slate-500">
                    {n.sent_at ? formatDatetime(n.sent_at) : formatDatetime(n.created_at)}
                  </td>
                  <td className="max-w-[220px] truncate px-4 py-3 text-slate-500" title={n.error_message || ''}>
                    {n.error_message || '—'}
                  </td>
                  <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
                    <div className="flex justify-end gap-1">
                      <button
                        className="btn-secondary !px-2 !py-1"
                        title="Details"
                        onClick={() => setDetails(n)}
                      >
                        <Info className="h-4 w-4" />
                      </button>
                      {n.status === 'failed' && onRetry && (
                        <button
                          className="btn !px-2 !py-1 border border-amber-500/30 text-amber-400 hover:bg-amber-500/10 disabled:opacity-50"
                          title="Retry"
                          disabled={retryingId === n.id}
                          onClick={() => onRetry(n)}
                        >
                          {retryingId === n.id ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <RotateCcw className="h-4 w-4" />
                          )}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {details && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={() => setDetails(null)}
        >
          <div className="card w-full max-w-lg p-6" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold">Notification details</h3>
            <dl className="mt-4 space-y-2 text-sm">
              <div className="flex justify-between gap-4">
                <dt className="text-slate-500">Monitor</dt>
                <dd className="font-medium">{details.monitor_name || details.monitor_id}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="text-slate-500">Channel</dt>
                <dd className="font-medium capitalize">{details.channel}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="text-slate-500">Status</dt>
                <dd className="font-medium">{details.status}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="text-slate-500">Created</dt>
                <dd className="font-medium">{formatDatetime(details.created_at)}</dd>
              </div>
              {details.sent_at && (
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Sent</dt>
                  <dd className="font-medium">{formatDatetime(details.sent_at)}</dd>
                </div>
              )}
              {details.error_message && (
                <div>
                  <dt className="text-slate-500">Error</dt>
                  <dd className="mt-1 break-words rounded-lg bg-white/5 p-3 text-xs">
                    {details.error_message}
                  </dd>
                </div>
              )}
            </dl>
            <div className="mt-6 flex justify-end">
              <button className="btn-secondary" onClick={() => setDetails(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

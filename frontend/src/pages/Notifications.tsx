import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Bell, CheckCircle2, XCircle } from 'lucide-react'
import api, { type ApiError } from '@/services/api'
import { useNotificationChannels, useNotificationHistory } from '@/hooks/useNotifications'
import { useToasts, Toaster } from '@/components/Toast'
import NotificationChannelCard from '@/components/NotificationChannelCard'
import NotificationHistoryTable from '@/components/NotificationHistoryTable'
import type { NotificationHistoryItem } from '@/types'

const PAGE_SIZE = 50

const RANGE_PRESETS: { label: string; days: number }[] = [
  { label: 'Last 24h', days: 1 },
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
]

function isoDaysAgo(days: number): string {
  return new Date(Date.now() - days * 86400_000).toISOString()
}

export default function Notifications() {
  const navigate = useNavigate()
  const { toasts, push } = useToasts()

  // ---- Channels ----
  const { channels, loading: chLoading, error: chError } = useNotificationChannels()
  const [testing, setTesting] = useState<string | null>(null)

  const handleTest = async (name: string) => {
    setTesting(name)
    try {
      await api.post(`/notifications/test/${name}`)
      push(`Test notification sent to ${name}`, 'success')
    } catch (err) {
      push((err as ApiError).message || 'Test failed', 'error')
    } finally {
      setTesting(null)
    }
  }

  // ---- Summary (fixed last 7 days) ----
  const weekAgo = useMemo(() => isoDaysAgo(7), [])
  const { total: weekTotal } = useNotificationHistory({ limit: 1, start: weekAgo })
  const { total: weekSent } = useNotificationHistory({ limit: 1, status: 'sent', start: weekAgo })
  const { total: weekFailed } = useNotificationHistory({ limit: 1, status: 'failed', start: weekAgo })

  // ---- History (filtered + paginated) ----
  const [draftStatus, setDraftStatus] = useState('')
  const [draftDays, setDraftDays] = useState(7)
  const [applied, setApplied] = useState({ status: '', start: isoDaysAgo(7), end: new Date().toISOString() })
  const [page, setPage] = useState(1)

  const { history, total, loading, error, refetch } = useNotificationHistory({
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    status: applied.status || undefined,
    start: applied.start,
    end: applied.end,
  })

  const applyFilters = () => {
    setApplied({ status: draftStatus, start: isoDaysAgo(draftDays), end: new Date().toISOString() })
    setPage(1)
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const handleRetry = (_item: NotificationHistoryItem) => {
    push('Retry is not available yet', 'info')
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold">Notifications</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Configure notification channels and view alert history
        </p>
      </div>

      {/* Channels */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Active Channels</h2>
        {chError ? (
          <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
            {chError.message}
          </div>
        ) : chLoading ? (
          <div className="text-neutral-500">Loading channels…</div>
        ) : channels.length === 0 ? (
          <div className="card p-8 text-center text-neutral-500">
            No notification channels configured
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {channels.map((ch) => (
              <NotificationChannelCard
                key={ch.name}
                channel={ch}
                testing={testing === ch.name}
                onTest={handleTest}
                onConfigure={() => navigate('/settings#notifications')}
              />
            ))}
          </div>
        )}
      </section>

      {/* History */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Recent Alerts</h2>

        {/* Summary stats (last 7 days) */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="card flex items-center gap-4 p-5">
            <div className="rounded-lg bg-info-100 p-3 text-info-600 dark:bg-info-900/40 dark:text-info-400">
              <Bell className="h-6 w-6" />
            </div>
            <div>
              <div className="text-2xl font-bold">{weekTotal}</div>
              <div className="text-sm text-neutral-500">Total (7 days)</div>
            </div>
          </div>
          <div className="card flex items-center gap-4 p-5">
            <div className="rounded-lg bg-emerald-100 p-3 text-emerald-600 dark:bg-emerald-900/40 dark:text-emerald-400">
              <CheckCircle2 className="h-6 w-6" />
            </div>
            <div>
              <div className="text-2xl font-bold text-emerald-600 dark:text-emerald-400">{weekSent}</div>
              <div className="text-sm text-neutral-500">Sent</div>
            </div>
          </div>
          <div className="card flex items-center gap-4 p-5">
            <div className="rounded-lg bg-red-100 p-3 text-red-600 dark:bg-red-900/40 dark:text-red-400">
              <XCircle className="h-6 w-6" />
            </div>
            <div>
              <div className="text-2xl font-bold text-red-600 dark:text-red-400">{weekFailed}</div>
              <div className="text-sm text-neutral-500">Failed</div>
            </div>
          </div>
        </div>

        {/* Filters */}
        <div className="card flex flex-wrap items-end gap-4 p-4">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">Status</span>
            <select
              value={draftStatus}
              onChange={(e) => setDraftStatus(e.target.value)}
              className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
            >
              <option value="">All</option>
              <option value="sent">Sent</option>
              <option value="failed">Failed</option>
              <option value="pending">Pending</option>
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">Range</span>
            <select
              value={draftDays}
              onChange={(e) => setDraftDays(Number(e.target.value))}
              className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
            >
              {RANGE_PRESETS.map((r) => (
                <option key={r.days} value={r.days}>
                  {r.label}
                </option>
              ))}
            </select>
          </label>
          <button className="btn-primary" onClick={applyFilters}>
            Apply
          </button>
        </div>

        <NotificationHistoryTable
          items={history}
          loading={loading}
          error={error?.message ?? null}
          onRetry={handleRetry}
          onReload={() => void refetch()}
        />

        <div className="flex items-center justify-between text-sm text-neutral-500">
          <span>
            {total} total · page {page} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button className="btn-secondary" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              Previous
            </button>
            <button
              className="btn-secondary"
              disabled={page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        </div>
      </section>

      <Toaster toasts={toasts} />
    </div>
  )
}

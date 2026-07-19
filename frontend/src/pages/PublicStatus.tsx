import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import { ShieldCheck } from 'lucide-react'
import { formatRelative } from '@/utils/format'

interface PublicMonitor {
  id: string
  name: string
  group: string
  status: string
  last_check: string | null
  response_time_ms: number
  uptime: { last_7_days: number; last_30_days: number; last_90_days: number }
  recent_incidents: { start: string; end: string | null; duration_minutes: number }[]
}

interface PublicStatusData {
  page: { name: string; description: string; logo_url: string; theme_color: string; updated_at: string }
  monitors: PublicMonitor[]
  summary: { total_monitors: number; online: number; offline: number; last_updated: string }
}

export default function PublicStatus() {
  const { slug } = useParams<{ slug: string }>()
  const [data, setData] = useState<PublicStatusData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!slug) return
    axios
      .get<{ data: PublicStatusData }>(`/public/status/${slug}`)
      .then((res) => setData(res.data.data))
      .catch(() => setError('Status page not found'))
      .finally(() => setLoading(false))
  }, [slug])

  if (loading) {
    return <div className="p-10 text-center text-neutral-500">Loading…</div>
  }
  if (error || !data) {
    return <div className="p-10 text-center text-neutral-500">{error ?? 'Not found'}</div>
  }

  const allOperational = data.summary.offline === 0

  return (
    <div className="mx-auto max-w-3xl p-6">
      <header className="mb-8 text-center">
        <div className="mb-2 flex items-center justify-center gap-2">
          <ShieldCheck className="h-7 w-7 text-primary-600" />
          <h1 className="text-2xl font-bold">{data.page.name}</h1>
        </div>
        {data.page.description && (
          <p className="text-neutral-500 dark:text-neutral-400">{data.page.description}</p>
        )}
      </header>

      <div
        className={`mb-6 rounded-lg p-4 text-center font-medium ${
          allOperational
            ? 'bg-primary-100 text-primary-800 dark:bg-primary-900/40 dark:text-primary-200'
            : 'bg-error-100 text-error-800 dark:bg-error-900/40 dark:text-error-200'
        }`}
      >
        {allOperational
          ? 'All systems operational'
          : `${data.summary.offline} of ${data.summary.total_monitors} systems down`}
      </div>

      <div className="space-y-3">
        {data.monitors.map((m) => (
          <div key={m.id} className="card flex items-center justify-between p-4">
            <div>
              <div className="font-medium">{m.name}</div>
              {m.group && <div className="text-xs text-neutral-500">{m.group}</div>}
            </div>
            <div className="flex items-center gap-4">
              <span className="text-sm text-neutral-500">
                {m.uptime.last_30_days.toFixed(2)}% (30d)
              </span>
              <span
                className={`h-3 w-3 rounded-full ${
                  m.status === 'online'
                    ? 'bg-primary-500'
                    : m.status === 'offline'
                      ? 'bg-error-500'
                      : 'bg-neutral-400'
                }`}
                title={m.status}
              />
            </div>
          </div>
        ))}
      </div>

      <footer className="mt-8 text-center text-xs text-neutral-400">
        Last updated {formatRelative(data.summary.last_updated)} · Powered by Sentinel
      </footer>
    </div>
  )
}

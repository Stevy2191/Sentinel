import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'
import { formatDistanceToNow, parseISO } from 'date-fns'
import { usePublicStatusPage } from '@/hooks/usePublicStatus'
import { Sparkline } from '@/components/UptimeSparkline'
import type { MonitorStatus, PublicMonitor } from '@/types'

const DEFAULT_THEME = '#10b981'

function relative(iso: string | null | undefined): string {
  if (!iso) return 'never'
  try {
    return formatDistanceToNow(parseISO(iso), { addSuffix: true })
  } catch {
    return '—'
  }
}

function statusDotClass(status: MonitorStatus): string {
  if (status === 'online') return 'bg-emerald-500'
  if (status === 'offline') return 'bg-red-500 animate-pulse'
  return 'bg-slate-400'
}

// A stacked row rather than a card: this page is a quick health glance, not a
// dashboard, so a monitor is just its name, a status dot, and the same
// 24-hour bar strip the internal Uptime Monitoring table draws.
function MonitorRow({ m }: { m: PublicMonitor }) {
  return (
    <div className="flex items-center justify-between gap-4 p-4">
      <div className="flex min-w-0 items-center gap-2.5">
        <span className={`h-2 w-2 shrink-0 rounded-full ${statusDotClass(m.status)}`} />
        <span className="truncate font-medium">{m.name}</span>
      </div>
      <Sparkline data={m.hourly_data} className="h-6 w-40 shrink-0" />
    </div>
  )
}

function Skeleton() {
  return (
    <div className="mx-auto max-w-4xl space-y-6 p-6">
      <div className="h-8 w-1/3 animate-pulse rounded bg-white/10" />
      <div className="h-14 animate-pulse rounded-lg bg-white/10" />
      <div className="space-y-2 rounded-lg border border-white/10 p-2">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="h-12 animate-pulse rounded-lg bg-white/10" />
        ))}
      </div>
    </div>
  )
}

export default function PublicStatus() {
  const { slug } = useParams<{ slug: string }>()
  const { page, monitors, summary, loading, error } = usePublicStatusPage(slug)

  const groups = useMemo(() => {
    const map = new Map<string, PublicMonitor[]>()
    for (const m of monitors) {
      const key = m.group || ''
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(m)
    }
    return Array.from(map.entries())
  }, [monitors])

  const derived = useMemo(() => {
    const total = summary?.total_monitors ?? monitors.length
    const offline = summary?.offline ?? 0
    return { total, offline }
  }, [summary, monitors])

  if (loading) return <Skeleton />

  if (error || !page) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-6">
        <div className="text-center">
          <ShieldCheck className="mx-auto mb-3 h-10 w-10 text-slate-400" />
          <h1 className="text-xl font-semibold">This status page is not available</h1>
          <p className="mt-1 text-sm text-slate-500">
            It may be private or may not exist.
          </p>
        </div>
      </div>
    )
  }

  const theme = page.theme_color || DEFAULT_THEME
  const allOperational = derived.offline === 0

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950">
      <div className="h-1.5" style={{ backgroundColor: theme }} />
      <div className="mx-auto max-w-4xl space-y-8 p-6">
        {/* Branding */}
        <header className="text-center">
          <div className="mb-2 flex items-center justify-center gap-3">
            {page.logo_url ? (
              <img src={page.logo_url} alt="" className="h-10 w-10 rounded object-contain" />
            ) : (
              <ShieldCheck className="h-8 w-8" style={{ color: theme }} />
            )}
            <h1 className="text-3xl font-bold">{page.name}</h1>
          </div>
          {page.description && (
            <p className="text-slate-400">{page.description}</p>
          )}
          <p className="mt-1 text-xs text-slate-400">
            Last updated {relative(summary?.last_updated ?? page.updated_at)}
          </p>
        </header>

        {/* Overall banner */}
        <div
          className={`rounded-lg p-4 text-center font-medium ${
            allOperational
              ? 'bg-emerald-500/20 text-emerald-300'
              : 'bg-red-500/20 text-red-300'
          }`}
        >
          {allOperational
            ? 'All systems operational'
            : `${derived.offline} of ${derived.total} systems experiencing issues`}
        </div>

        {/* Monitors grouped, stacked */}
        {monitors.length === 0 ? (
          <div className="rounded-lg border border-white/10 p-10 text-center text-slate-400">
            No monitors on this status page yet.
          </div>
        ) : (
          groups.map(([groupName, groupMonitors]) => (
            <section key={groupName || 'ungrouped'} className="space-y-3">
              {groupName && <h2 className="text-lg font-semibold">{groupName}</h2>}
              <div className="rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
                <div className="divide-y divide-white/5">
                  {groupMonitors.map((m) => (
                    <MonitorRow key={m.id} m={m} />
                  ))}
                </div>
              </div>
            </section>
          ))
        )}

        {/* Footer */}
        <footer className="border-t border-white/10 pt-4 text-center text-xs text-slate-400">
          Powered by Sentinel · Last updated {relative(summary?.last_updated ?? page.updated_at)}
        </footer>
      </div>
    </div>
  )
}

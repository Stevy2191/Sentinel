import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

interface Props {
  title: string
  /** Rows after filtering. */
  count: number
  down: number
  /** Figures for the header, e.g. average response time. */
  summary?: ReactNode
  /** The section's own filters. */
  controls?: ReactNode
  collapsed: boolean
  onToggle: () => void
  /** True until the first load finishes. */
  loading: boolean
  error: string | null
  onRetry: () => void
  children: ReactNode
}

/** One Monitoring section: a collapsible header with its count, how many are
 *  down and its own figures and filters, then its rows. Collapsed sections do
 *  not render their rows, so rows that fetch their own data stay quiet. */
export default function SectionShell(p: Props) {
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <button className="flex items-center gap-2 text-left" onClick={p.onToggle} aria-expanded={!p.collapsed}>
          <ChevronDown className={`h-4 w-4 text-slate-400 transition-transform ${p.collapsed ? '-rotate-90' : ''}`} />
          <h2 className="text-xl font-light text-white">{p.title}</h2>
          <span className="text-sm tabular-nums text-slate-400">{p.loading ? '…' : p.count}</span>
          {p.down > 0 && (
            <span className="rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-400">
              {p.down} down
            </span>
          )}
        </button>
        {!p.collapsed && (p.summary || p.controls) && (
          <div className="flex flex-wrap items-center gap-3 text-sm text-slate-400">
            {p.summary}
            {p.controls}
          </div>
        )}
      </div>
      {!p.collapsed && (
        <>
          {p.error && (
            <div className="flex items-center justify-between rounded-lg border border-red-500/30 bg-red-500/10 p-3">
              <span className="text-sm text-red-400">{p.error}</span>
              <button className="btn-secondary" onClick={p.onRetry}>
                Retry
              </button>
            </div>
          )}
          {p.loading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="h-12 animate-pulse rounded-lg border border-white/10 bg-slate-800/40" />
              ))}
            </div>
          ) : (
            p.children
          )}
        </>
      )}
    </section>
  )
}

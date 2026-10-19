import type { SummaryCounts, ViewStatus } from '@/utils/monitoringView'

interface Props {
  counts: SummaryCounts
  /** The status filter, to show which figure is applied. */
  statuses: ViewStatus[]
  /** Down or Paused filters to that status; null (Watching) clears it. */
  onPick: (status: 'down' | 'paused' | null) => void
}

/** Watching · Down · Paused across everything loaded. Each one is a shortcut
 *  for the status filter, keeping the other filters. */
export default function SummaryStrip({ counts, statuses, onPick }: Props) {
  const only = (s: ViewStatus) => statuses.length === 1 && statuses[0] === s
  const cls = (on: boolean) => `transition hover:text-white ${on ? 'text-white underline underline-offset-4' : 'text-slate-400'}`
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1 rounded-lg border border-white/10 bg-slate-800/40 px-4 py-2.5 text-sm">
      <button className={cls(false)} onClick={() => onPick(null)} title="Show every status">
        Watching <span className="font-semibold tabular-nums text-white">{counts.watching}</span>
      </button>
      <button className={cls(only('down'))} onClick={() => onPick('down')} aria-pressed={only('down')}>
        Down{' '}
        <span className={`font-semibold tabular-nums ${counts.down > 0 ? 'text-red-400' : 'text-white'}`}>{counts.down}</span>
      </button>
      <button className={cls(only('paused'))} onClick={() => onPick('paused')} aria-pressed={only('paused')}>
        Paused <span className="font-semibold tabular-nums text-white">{counts.paused}</span>
      </button>
    </div>
  )
}

import type { SummaryCounts } from '@/utils/monitoringView'

interface Props {
  counts: SummaryCounts
  /** Labels of the sections that failed to load. */
  missing: string[]
  onPick: (status: 'down' | 'paused') => void
}

/** Watching · Down · Paused across everything loaded. Down and Paused set
 *  that status filter. */
export default function SummaryStrip({ counts, missing, onPick }: Props) {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1 rounded-lg border border-white/10 bg-slate-800/40 px-4 py-2.5 text-sm">
      <span className="text-slate-400">
        Watching <span className="font-semibold tabular-nums text-white">{counts.watching}</span>
      </span>
      <button className="text-slate-400 transition hover:text-white" onClick={() => onPick('down')}>
        Down{' '}
        <span className={`font-semibold tabular-nums ${counts.down > 0 ? 'text-red-400' : 'text-white'}`}>{counts.down}</span>
      </button>
      <button className="text-slate-400 transition hover:text-white" onClick={() => onPick('paused')}>
        Paused <span className="font-semibold tabular-nums text-white">{counts.paused}</span>
      </button>
      {missing.length > 0 && <span className="text-amber-300">{missing.join(', ')} couldn&rsquo;t load</span>}
    </div>
  )
}

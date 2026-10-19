import { VIEW_STATUS_LABEL, type ViewStatus } from '@/utils/monitoringView'

// Whole literal class strings per status - Tailwind only emits CSS for class
// names spelled out in the source.
const STYLE: Record<ViewStatus, { cls: string; dot: string }> = {
  up: { cls: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-400', dot: 'bg-emerald-400' },
  down: { cls: 'border-red-500/30 bg-red-500/15 text-red-400', dot: 'bg-red-400' },
  pending: { cls: 'border-slate-500/30 bg-slate-500/15 text-slate-300', dot: 'bg-slate-400' },
  paused: { cls: 'border-slate-500/30 bg-slate-500/10 text-slate-400', dot: 'bg-slate-500' },
  maintenance: { cls: 'border-yellow-500/30 bg-yellow-500/15 text-yellow-400', dot: 'bg-yellow-400' },
  error: { cls: 'border-amber-500/30 bg-amber-500/15 text-amber-400', dot: 'bg-amber-400' },
}

/** The one status pill every row on the Monitoring list shares. */
export default function StatusPill({ status, detail }: { status: ViewStatus; detail?: string }) {
  const s = STYLE[status]
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-0.5 text-xs font-medium ${s.cls}`}
      title={detail || undefined}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${s.dot}`} />
      {VIEW_STATUS_LABEL[status]}
    </span>
  )
}

import { ChevronDown, ChevronRight, Pencil } from 'lucide-react'

function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-primary-400'
  if (pct >= 80) return 'text-amber-400'
  return 'text-red-400'
}

interface Props {
  title: string
  color: string | null
  uptime: number | null
  count: number
  expanded: boolean
  onToggle: () => void
  onEdit?: () => void
  children: React.ReactNode
}

/** A collapsible dashboard section (a monitor group, or the "Ungrouped" bucket). */
export default function GroupSection({
  title,
  color,
  uptime,
  count,
  expanded,
  onToggle,
  onEdit,
  children,
}: Props) {
  return (
    <div className="space-y-2">
      <div
        className="flex items-center gap-3 rounded-lg border border-neutral-800 border-l-4 bg-neutral-900/60 px-3 py-2"
        style={{ borderLeftColor: color ?? '#7A8A94' }}
      >
        <button onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {expanded ? (
            <ChevronDown className="h-4 w-4 shrink-0 text-neutral-400" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0 text-neutral-400" />
          )}
          <span
            className="h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ backgroundColor: color ?? '#7A8A94', boxShadow: `0 0 8px ${color ?? '#7A8A94'}` }}
          />
          <span className="vs-title truncate text-sm" style={{ letterSpacing: '0.08em' }}>
            {title}
          </span>
          <span className="vs-readout shrink-0 rounded-full bg-neutral-800 px-2 py-0.5 text-xs text-neutral-300">
            {count}
          </span>
        </button>
        {uptime != null && (
          <span className={`vs-readout shrink-0 text-lg font-medium ${uptimeColor(uptime)}`}>{uptime.toFixed(1)}%</span>
        )}
        {onEdit && (
          <button
            onClick={onEdit}
            className="shrink-0 rounded p-1 text-neutral-400 hover:bg-neutral-200 hover:text-neutral-600 dark:hover:bg-neutral-700"
            title="Edit group"
          >
            <Pencil className="h-4 w-4" />
          </button>
        )}
      </div>
      {expanded && <div className="space-y-2 pl-1">{children}</div>}
    </div>
  )
}

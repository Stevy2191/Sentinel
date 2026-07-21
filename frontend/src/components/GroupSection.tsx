import { ChevronDown, ChevronRight, Pencil } from 'lucide-react'

function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-emerald-500'
  if (pct >= 80) return 'text-amber-500'
  return 'text-red-500'
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
        className="flex items-center gap-3 rounded-md border-l-4 bg-neutral-50 px-3 py-2 dark:bg-neutral-800/50"
        style={{ borderLeftColor: color ?? '#94a3b8' }}
      >
        <button onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {expanded ? (
            <ChevronDown className="h-4 w-4 shrink-0 text-neutral-400" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0 text-neutral-400" />
          )}
          <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: color ?? '#94a3b8' }} />
          <span className="truncate font-semibold">{title}</span>
          <span className="shrink-0 rounded-full bg-neutral-200 px-2 py-0.5 text-xs text-neutral-600 dark:bg-neutral-700 dark:text-neutral-300">
            {count}
          </span>
        </button>
        {uptime != null && (
          <span className={`shrink-0 text-lg font-bold ${uptimeColor(uptime)}`}>{uptime.toFixed(1)}%</span>
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

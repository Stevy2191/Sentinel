import type { ReactNode } from 'react'
import { GripVertical, Loader2, Settings2, Trash2 } from 'lucide-react'
import type { WidgetState } from '@/types/dashboards'

interface Props {
  title: string
  typeLabel: string
  state?: WidgetState
  hidden?: number
  stale?: boolean
  loading?: boolean
  error?: string | null
  editing?: boolean
  selected?: boolean
  /** Fullscreen and public: larger type for reading across a room. */
  display?: boolean
  onEdit?: () => void
  onRemove?: () => void
  children?: ReactNode
}

const STATE_TEXT: Record<Exclude<WidgetState, 'ok'>, string> = {
  no_access: "You don't have access to what this widget shows.",
  removed: 'What this widget showed has been removed.',
  no_data: 'No data yet.',
}

/** Every widget's frame: its title, the stale marker, and the states every
 *  widget shares, so renderers only ever draw real data. */
export default function WidgetFrame({ title, typeLabel, state, hidden = 0, stale, loading, error, editing, selected, display, onEdit, onRemove, children }: Props) {
  let body: ReactNode = children
  if (loading) body = <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
  else if (error) body = <p className="text-sm text-red-400">{error}</p>
  else if (state && state !== 'ok') body = <p className="text-sm text-slate-500">{STATE_TEXT[state]}</p>

  return (
    <div className={`card flex h-full flex-col overflow-hidden ${selected ? 'ring-2 ring-primary-500' : ''}`}>
      <div className={`flex items-center gap-2 border-b border-white/10 px-3 py-2 ${editing ? 'widget-drag-handle cursor-move' : ''}`}>
        {editing && <GripVertical className="h-4 w-4 shrink-0 text-slate-500" aria-hidden />}
        <h3 className={`min-w-0 flex-1 truncate font-medium text-slate-200 ${display ? 'text-lg' : 'text-sm'}`}>{title || typeLabel}</h3>
        {stale && (
          <span className="rounded-full border border-amber-500/30 bg-amber-500/15 px-2 py-0.5 text-xs text-amber-300" title="This data has not been refreshed for a while">
            Stale
          </span>
        )}
        {hidden > 0 && <span className="text-xs text-slate-500">{hidden} hidden</span>}
        {editing && (
          <>
            <button type="button" className="widget-no-drag text-slate-400 hover:text-white" onClick={onEdit} aria-label="Widget settings">
              <Settings2 className="h-4 w-4" />
            </button>
            <button type="button" className="widget-no-drag text-slate-400 hover:text-red-400" onClick={onRemove} aria-label="Remove widget">
              <Trash2 className="h-4 w-4" />
            </button>
          </>
        )}
      </div>
      <div className={`min-h-0 flex-1 overflow-auto p-3 ${display ? 'text-base' : 'text-sm'}`}>{body}</div>
    </div>
  )
}

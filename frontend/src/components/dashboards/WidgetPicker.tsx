import { X } from 'lucide-react'
import type { WidgetType } from '@/types/dashboards'
import { WIDGET_TYPES } from '@/utils/dashboards'

export default function WidgetPicker({ onPick, onClose }: { onPick: (t: WidgetType) => void; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-2xl space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Add a widget</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <ul className="grid gap-2 sm:grid-cols-2">
          {WIDGET_TYPES.map((w) => (
            <li key={w.type}>
              <button type="button" className="w-full rounded-lg border border-white/10 bg-slate-800/40 p-3 text-left transition hover:bg-slate-800/70" onClick={() => onPick(w.type)}>
                <p className="font-medium text-slate-100">{w.label}</p>
                <p className="text-sm text-slate-400">{w.description}</p>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

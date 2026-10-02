import { X } from 'lucide-react'
import type { DraftWidget } from '@/utils/dashboards'
import { inputCls, widgetInfo } from '@/utils/dashboards'
import WidgetSettings from '@/components/dashboards/WidgetSettings'
import WidgetPreview from '@/components/dashboards/WidgetPreview'

interface Props {
  dashboardId: string
  siteId: string | null
  widget: DraftWidget
  error?: string
  onChange: (patch: Partial<DraftWidget>) => void
  onClose: () => void
}

/** The side panel for one widget: its title, its type's settings and a live preview. */
export default function WidgetSettingsPanel({ dashboardId, siteId, widget, error, onChange, onClose }: Props) {
  return (
    <aside className="fixed inset-y-0 right-0 z-40 flex w-full max-w-md flex-col gap-4 overflow-y-auto border-l border-white/10 bg-slate-950 p-5 shadow-2xl">
      <div className="flex items-start justify-between">
        <h2 className="text-lg font-semibold">{widgetInfo(widget.type).label}</h2>
        <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close settings">
          <X className="h-5 w-5" />
        </button>
      </div>
      {error && <p className="rounded-md border border-red-500/30 bg-red-500/10 p-2 text-sm text-red-300">{error}</p>}
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Title</span>
        <input className={inputCls} maxLength={100} value={widget.title} placeholder={widgetInfo(widget.type).label} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <WidgetSettings type={widget.type} config={widget.config} siteId={siteId} onChange={(config) => onChange({ config })} />
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-slate-500">Preview</p>
        <WidgetPreview dashboardId={dashboardId} type={widget.type} title={widget.title} config={widget.config} />
      </div>
    </aside>
  )
}

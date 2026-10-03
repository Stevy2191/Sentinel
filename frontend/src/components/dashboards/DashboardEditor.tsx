import { useEffect, useMemo, useRef, useState } from 'react'
import { Plus, Save } from 'lucide-react'
import type { Layout } from 'react-grid-layout'
import { useDashboardActions, type SaveDashboardInput } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail, WidgetType } from '@/types/dashboards'
import { defaultConfig, inputCls, nextY, widgetInfo, widgetSummary, type DraftWidget } from '@/utils/dashboards'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetPicker from '@/components/dashboards/WidgetPicker'
import WidgetSettingsPanel from '@/components/dashboards/WidgetSettingsPanel'

interface Props {
  dashboard: DashboardDetail
  onSaved: (d: DashboardDetail) => void
  onCancel: () => void
}

function toDraft(d: DashboardDetail): DraftWidget[] {
  return d.widgets.map((w) => ({ key: w.id, id: w.id, type: w.type, title: w.title, config: w.config ?? {}, x: w.x, y: w.y, w: w.w, h: w.h }))
}

/** Edits a local draft of the whole dashboard; nothing is saved until Save. */
export default function DashboardEditor({ dashboard, onSaved, onCancel }: Props) {
  const { save } = useDashboardActions()
  const [name, setName] = useState(dashboard.name)
  const [description, setDescription] = useState(dashboard.description)
  const [widgets, setWidgets] = useState<DraftWidget[]>(() => toDraft(dashboard))
  const [selected, setSelected] = useState<string | null>(null)
  const [picking, setPicking] = useState(false)
  const [confirmCancel, setConfirmCancel] = useState(false)
  const [saving, setSaving] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [error, setError] = useState<{ key?: string; message: string } | null>(null)
  const counter = useRef(0)

  const initial = useMemo(() => JSON.stringify({ n: dashboard.name, d: dashboard.description, w: toDraft(dashboard) }), [dashboard])
  const dirty = JSON.stringify({ n: name, d: description, w: widgets }) !== initial

  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = '' // older browsers ask only when this is set
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const onLayoutChange = (layout: Layout) =>
    setWidgets((ws) =>
      ws.map((w) => {
        const l = layout.find((i) => i.i === w.key)
        return l && (l.x !== w.x || l.y !== w.y || l.w !== w.w || l.h !== w.h) ? { ...w, x: l.x, y: l.y, w: l.w, h: l.h } : w
      }),
    )

  const addWidget = (type: WidgetType) => {
    const info = widgetInfo(type)
    const key = `new-${counter.current++}`
    setWidgets((ws) => [...ws, { key, type, title: '', config: defaultConfig(type, dashboard.site_id), x: 0, y: nextY(ws), w: info.w, h: info.h }])
    setSelected(key)
    setPicking(false)
  }

  const patch = (key: string, p: Partial<DraftWidget>) => {
    setWidgets((ws) => ws.map((w) => (w.key === key ? { ...w, ...p } : w)))
    setError((e) => (e?.key === key ? null : e))
  }

  const submit = async () => {
    setSaving(true)
    setError(null)
    const input: SaveDashboardInput = {
      version: dashboard.version,
      name,
      description,
      widgets: widgets.map((w) => ({ id: w.id, type: w.type, title: w.title, config: w.config, x: w.x, y: w.y, w: w.w, h: w.h })),
    }
    try {
      onSaved(await save(dashboard.id, input))
    } catch (err) {
      const e = err as ApiError
      if (e.status === 409) {
        setConflict(true)
      } else {
        const index = e.details?.widget_index
        const w = typeof index === 'number' ? widgets[index] : undefined
        if (w) setSelected(w.key)
        setError({ key: w?.key, message: e.message || 'Could not save' })
      }
    } finally {
      setSaving(false)
    }
  }

  const current = widgets.find((w) => w.key === selected)
  const gridItems = widgets.map((w) => ({ ...w, id: w.key }))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <label className="min-w-64 flex-1 space-y-1">
          <span className="text-xs uppercase tracking-widest text-slate-500">Name</span>
          <input className={inputCls} value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="min-w-64 flex-1 space-y-1">
          <span className="text-xs uppercase tracking-widest text-slate-500">Description</span>
          <input className={inputCls} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <button className="btn-secondary flex items-center gap-2" onClick={() => setPicking(true)} disabled={widgets.length >= 50}>
          <Plus className="h-4 w-4" /> Add widget
        </button>
        {confirmCancel ? (
          <>
            <span className="text-sm text-amber-300">Discard your changes?</span>
            <button className="btn-secondary" onClick={() => setConfirmCancel(false)}>
              Keep editing
            </button>
            <button className="btn bg-red-600 text-white hover:bg-red-700" onClick={onCancel}>
              Discard
            </button>
          </>
        ) : (
          <button className="btn-secondary" onClick={() => (dirty ? setConfirmCancel(true) : onCancel())}>
            Cancel
          </button>
        )}
        <button className="btn-primary flex items-center gap-2" onClick={() => void submit()} disabled={saving || conflict || name.trim() === ''}>
          <Save className="h-4 w-4" /> {saving ? 'Saving…' : 'Save'}
        </button>
      </div>

      {conflict && (
        <div className="flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">
          Someone else saved this dashboard. Reload to see their changes; your edits will be lost.
          <button className="btn-secondary" onClick={() => window.location.reload()}>
            Reload
          </button>
        </div>
      )}
      {error && !error.key && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error.message}</div>}
      <p className="text-xs text-slate-500 lg:hidden">Editing works best on a wide screen.</p>

      {widgets.length === 0 ? (
        <p className="text-slate-500">No widgets yet. Choose Add widget.</p>
      ) : (
        <DashboardGrid
          items={gridItems}
          editing
          onLayoutChange={onLayoutChange}
          render={(w) => (
            <WidgetFrame
              title={w.title}
              typeLabel={widgetInfo(w.type).label}
              editing
              selected={w.key === selected}
              onEdit={() => setSelected(w.key)}
              onRemove={() => {
                setWidgets((ws) => ws.filter((x) => x.key !== w.key))
                if (selected === w.key) setSelected(null)
              }}
            >
              <p className="text-sm text-slate-400">{widgetSummary(w)}</p>
              {error?.key === w.key && <p className="mt-1 text-sm text-red-400">{error.message}</p>}
            </WidgetFrame>
          )}
        />
      )}

      {picking && <WidgetPicker onPick={addWidget} onClose={() => setPicking(false)} />}
      {current && (
        <WidgetSettingsPanel
          dashboardId={dashboard.id}
          siteId={dashboard.site_id}
          widget={current}
          error={error?.key === current.key ? error.message : undefined}
          onChange={(p) => patch(current.key, p)}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  )
}

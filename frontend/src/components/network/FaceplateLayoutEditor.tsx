import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, ArrowUp, Loader2, Plus, Trash2, X } from 'lucide-react'
import type { Device } from '@/hooks/useDevices'
import type { PortView } from '@/hooks/usePorts'
import type { ApiError } from '@/services/api'
import {
  deleteFaceplateLayout,
  previewFaceplate,
  saveFaceplateLayout,
  useFaceplateEditor,
  type FaceplatePreviewData,
  type LayoutGroup,
} from '@/hooks/useFaceplateLayouts'
import Faceplate from '@/components/network/Faceplate'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  device: Device
  ports: PortView[]
  onClose: () => void
  onSaved: () => void
}

/** Fix layout: an admin arranges a model's front panel as groups of ports,
 *  previewed live on this device, and saves it for every device of the
 *  model. */
export default function FaceplateLayoutEditor({ device, ports, onClose, onSaved }: Props) {
  const { data: editor, loading, error: loadError } = useFaceplateEditor(device.id)
  const [groups, setGroups] = useState<LayoutGroup[] | null>(null)
  const [preview, setPreview] = useState<FaceplatePreviewData | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [clearSettings, setClearSettings] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const seq = useRef(0)

  useEffect(() => {
    if (editor && groups === null) setGroups(editor.start)
  }, [editor, groups])

  // Redraw about 300 ms after the last change; a slower, older answer never
  // replaces a newer one, and the last preview stays while one is pending.
  useEffect(() => {
    if (!groups) return
    const mine = ++seq.current
    setPreviewing(true)
    const t = window.setTimeout(() => {
      previewFaceplate(device.id, groups)
        .then((p) => {
          if (mine !== seq.current) return
          setPreview(p)
          setPreviewError(null)
        })
        .catch((err) => {
          if (mine === seq.current) setPreviewError((err as ApiError).message || 'Preview failed')
        })
        .finally(() => {
          if (mine === seq.current) setPreviewing(false)
        })
    }, 300)
    return () => window.clearTimeout(t)
  }, [device.id, groups])

  const model = editor?.model || device.effective_model || ''
  const fits = !!preview && !previewError && preview.fit.missing.length === 0 && preview.fit.extra.length === 0
  const allPorts = useMemo(() => editor?.ports ?? [], [editor])

  const update = (i: number, patch: Partial<LayoutGroup>) =>
    setGroups((gs) =>
      (gs ?? []).map((g, k) => {
        if (k !== i) return g
        const next = { ...g, ...patch }
        if (next.rows === 2 && !next.numbering) next.numbering = 'alternate'
        return next
      })
    )
  const move = (i: number, by: number) =>
    setGroups((gs) => {
      const next = [...(gs ?? [])]
      const j = i + by
      if (j < 0 || j >= next.length) return next
      ;[next[i], next[j]] = [next[j], next[i]]
      return next
    })
  const remove = (i: number) => setGroups((gs) => (gs ?? []).filter((_, k) => k !== i))
  const add = () =>
    setGroups((gs) => {
      const cur = gs ?? []
      const placed = new Set(cur.flatMap((g) => Array.from({ length: Math.max(0, g.last - g.first + 1) }, (_, k) => g.first + k)))
      const unplaced = allPorts.filter((n) => !placed.has(n))
      const highest = allPorts.length ? Math.max(...allPorts) : 1
      const first = unplaced.length ? Math.min(...unplaced) : Math.min(highest, Math.max(0, ...cur.map((g) => g.last)) + 1)
      return [...cur, { first, last: Math.max(first, highest), kind: 'rj45', rows: 2, numbering: 'alternate' }]
    })

  const save = async () => {
    if (!groups) return
    setSaving(true)
    setSaveError(null)
    try {
      await saveFaceplateLayout(device.id, groups, !!editor?.device_settings && clearSettings)
      onSaved()
    } catch (err) {
      setSaveError((err as ApiError).message || 'Saving failed')
    } finally {
      setSaving(false)
    }
  }
  const removeSaved = async () => {
    if (!editor?.saved) return
    setSaving(true)
    setSaveError(null)
    try {
      await deleteFaceplateLayout(editor.saved.id)
      onSaved()
    } catch (err) {
      setSaveError((err as ApiError).message || 'Removing failed')
    } finally {
      setSaving(false)
    }
  }

  const fitLine = () => {
    if (previewError) return <p className="text-sm text-red-400">{previewError}</p>
    if (!preview) return <p className="text-sm text-slate-500">Drawing the preview…</p>
    const { missing, extra } = preview.fit
    if (missing.length === 0 && extra.length === 0) return <p className="text-sm text-emerald-400">All {allPorts.length} ports placed</p>
    return (
      <div className="space-y-0.5 text-sm text-amber-400">
        {missing.length > 0 && <p>Not placed: {missing.join(', ')}</p>}
        {extra.length > 0 && <p>Not on this device: {extra.join(', ')}</p>}
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card max-h-[90vh] w-full max-w-3xl space-y-4 overflow-y-auto p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold">
              Layout for <span className="text-white">{model}</span>
            </h2>
            {editor && (
              <p className="text-sm text-slate-400">
                Used by {editor.devices} device{editor.devices === 1 ? '' : 's'}. Groups run left to right, as on the real switch.
              </p>
            )}
          </div>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {loading && <p className="text-sm text-slate-400">Loading…</p>}
        {loadError && <p className="text-sm text-red-400">{loadError}</p>}

        {groups && (
          <>
            <div className="space-y-2">
              <div className="hidden grid-cols-[1fr_1fr_1fr_1.3fr_auto] gap-2 px-1 text-xs uppercase tracking-wide text-slate-500 sm:grid">
                <span>Ports</span>
                <span>Type</span>
                <span>Rows</span>
                <span>Numbering</span>
                <span className="w-24" />
              </div>
              {groups.map((g, i) => (
                <div key={i} className="grid grid-cols-2 items-center gap-2 rounded-md border border-white/5 p-2 sm:grid-cols-[1fr_1fr_1fr_1.3fr_auto] sm:border-0 sm:p-0">
                  <div className="col-span-2 flex items-center gap-1 sm:col-span-1">
                    <input
                      type="number"
                      min={1}
                      className={inputCls}
                      value={Number.isFinite(g.first) ? g.first : ''}
                      onChange={(e) => update(i, { first: e.target.valueAsNumber })}
                      aria-label={`Group ${i + 1} first port`}
                    />
                    <span className="text-slate-500">–</span>
                    <input
                      type="number"
                      min={1}
                      className={inputCls}
                      value={Number.isFinite(g.last) ? g.last : ''}
                      onChange={(e) => update(i, { last: e.target.valueAsNumber })}
                      aria-label={`Group ${i + 1} last port`}
                    />
                  </div>
                  <select className={inputCls} value={g.kind} onChange={(e) => update(i, { kind: e.target.value as LayoutGroup['kind'] })} aria-label={`Group ${i + 1} type`}>
                    <option value="sfp">SFP</option>
                    <option value="rj45">RJ45</option>
                  </select>
                  <select className={inputCls} value={g.rows} onChange={(e) => update(i, { rows: Number(e.target.value) as 1 | 2 })} aria-label={`Group ${i + 1} rows`}>
                    <option value={1}>1 row</option>
                    <option value={2}>2 rows</option>
                  </select>
                  {g.rows === 2 ? (
                    <select
                      className={inputCls}
                      value={g.numbering ?? 'alternate'}
                      onChange={(e) => update(i, { numbering: e.target.value as LayoutGroup['numbering'] })}
                      aria-label={`Group ${i + 1} numbering`}
                    >
                      <option value="alternate">Odd on top</option>
                      <option value="across">Across</option>
                    </select>
                  ) : (
                    <span className="hidden sm:block" />
                  )}
                  <div className="flex w-24 justify-end gap-1">
                    <button type="button" className="rounded p-1 text-slate-400 hover:text-white disabled:opacity-30" onClick={() => move(i, -1)} disabled={i === 0} aria-label={`Move group ${i + 1} earlier`}>
                      <ArrowUp className="h-4 w-4" />
                    </button>
                    <button type="button" className="rounded p-1 text-slate-400 hover:text-white disabled:opacity-30" onClick={() => move(i, 1)} disabled={i === groups.length - 1} aria-label={`Move group ${i + 1} later`}>
                      <ArrowDown className="h-4 w-4" />
                    </button>
                    <button type="button" className="rounded p-1 text-slate-400 hover:text-red-400" onClick={() => remove(i)} aria-label={`Remove group ${i + 1}`}>
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              ))}
              <button type="button" className="inline-flex items-center gap-1 text-sm text-primary-400 hover:underline" onClick={add}>
                <Plus className="h-4 w-4" /> Add group
              </button>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-slate-500">
                Preview {previewing && <Loader2 className="h-3 w-3 animate-spin" aria-label="Updating preview" />}
              </div>
              {preview && (
                <Faceplate faceplates={preview.faceplates} ports={ports} title={device.name} subtitle={model} selected={null} onSelect={() => {}} />
              )}
              {fitLine()}
            </div>

            {editor?.device_settings && (
              <label className="flex items-start gap-2 text-sm text-slate-300">
                <input type="checkbox" className="mt-0.5" checked={clearSettings} onChange={(e) => setClearSettings(e.target.checked)} />
                Also clear this device's own faceplate settings, which would otherwise override this layout
              </label>
            )}

            {saveError && <p className="text-sm text-red-400">{saveError}</p>}

            {confirmRemove ? (
              <div className="space-y-2 rounded-md border border-red-500/30 bg-red-500/10 p-3 text-sm text-slate-200">
                <p>Remove the saved layout for {model}? Devices of this model go back to the built-in or automatic drawing.</p>
                <div className="flex justify-end gap-2">
                  <button type="button" className="btn-secondary" onClick={() => setConfirmRemove(false)}>
                    Keep it
                  </button>
                  <button type="button" className="btn-primary bg-red-600 hover:bg-red-500" disabled={saving} onClick={() => void removeSaved()}>
                    Remove
                  </button>
                </div>
              </div>
            ) : (
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  {editor?.saved && (
                    <button type="button" className="text-sm text-red-400 hover:underline" onClick={() => setConfirmRemove(true)}>
                      Remove saved layout
                    </button>
                  )}
                </div>
                <div className="flex gap-2">
                  <button type="button" className="btn-secondary" onClick={onClose}>
                    Cancel
                  </button>
                  <button type="button" className="btn-primary" disabled={!fits || previewing || saving} onClick={() => void save()}>
                    Save for all {model} devices
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

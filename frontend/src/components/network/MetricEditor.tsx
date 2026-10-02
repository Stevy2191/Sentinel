import { useEffect, useRef, useState } from 'react'
import { X } from 'lucide-react'
import { useDevices } from '@/hooks/useDevices'
import { groupBySite } from '@/utils/devices'
import { mibSearch, type MibObject } from '@/hooks/useMibs'
import {
  createMetric,
  previewMetric,
  updateMetric,
  type LabelMode,
  type MetricKind,
  type MetricPreview,
  type MetricSource,
  type ProfileMetric,
  type RuleKind,
} from '@/hooks/useProfiles'
import { draftHash, emptyMetric, suggestKey } from '@/utils/metrics'
import type { ApiError } from '@/services/api'

/** A value that only updates `ms` after the last change, so the OID finder's
 *  search box doesn't fire a request per keystroke. Mirrors MibBrowser's own
 *  (unexported) copy - small enough that sharing it isn't worth a new file. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return debounced
}

/** The small inline search a "Find…" button opens: Task 12's mibSearch,
 *  picking a result fills the numeric OID into the field it was opened from. */
function OidFinder({ onPick }: { onPick: (oid: string) => void }) {
  const [q, setQ] = useState('')
  const debouncedQ = useDebounced(q, 300)
  const [results, setResults] = useState<MibObject[]>([])
  const [searching, setSearching] = useState(false)

  useEffect(() => {
    const query = debouncedQ.trim()
    if (query.length < 2) {
      setResults([])
      return
    }
    let cancelled = false
    setSearching(true)
    mibSearch(query)
      .then((r) => {
        if (!cancelled) setResults(r)
      })
      .finally(() => {
        if (!cancelled) setSearching(false)
      })
    return () => {
      cancelled = true
    }
  }, [debouncedQ])

  return (
    <div className="mt-2 space-y-2 rounded-lg border border-white/10 bg-slate-900/60 p-2">
      <input
        autoFocus
        className="rd-input w-full"
        placeholder="Search by name, OID or description…"
        value={q}
        onChange={(e) => setQ(e.target.value)}
      />
      {q.trim().length >= 2 && (
        <div className="max-h-40 divide-y divide-white/10 overflow-y-auto">
          {searching ? (
            <p className="p-2 text-xs text-slate-400">Searching…</p>
          ) : results.length === 0 ? (
            <p className="p-2 text-xs text-slate-500">No matches.</p>
          ) : (
            results.map((r) => (
              <button
                key={r.oid}
                type="button"
                className="block w-full p-2 text-left text-sm hover:bg-white/5"
                onClick={() => onPick(r.oid)}
              >
                <div className="text-slate-200">
                  {r.name} <span className="text-xs text-slate-500">({r.kind})</span>
                </div>
                <div className="font-mono text-xs text-slate-500">{r.oid}</div>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  )
}

/** A numeric-OID input with a "Find…" button that opens OidFinder below it. */
function OidField({ label, value, onChange, help }: { label: string; value: string; onChange: (v: string) => void; help?: string }) {
  const [finding, setFinding] = useState(false)
  return (
    <label className="block space-y-1">
      <span className="text-sm text-slate-300">{label}</span>
      <span className="flex items-center gap-2">
        <input
          className="rd-input flex-1 font-mono text-sm"
          placeholder="1.3.6.1.2.1…"
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <button type="button" className="btn-secondary shrink-0" onClick={() => setFinding((f) => !f)}>
          Find…
        </button>
      </span>
      {help && <span className="block text-xs text-slate-500">{help}</span>}
      {finding && (
        <OidFinder
          onPick={(oid) => {
            onChange(oid)
            setFinding(false)
          }}
        />
      )}
    </label>
  )
}

/** The list of named states for a status metric: value/name pairs from
 *  state_names, each with an OK checkbox driven by ok_states, plus a small
 *  form to add another pair. */
function StatusStatesEditor({
  stateNames,
  okStates,
  onChange,
}: {
  stateNames: Record<string, string> | null
  okStates: number[]
  onChange: (stateNames: Record<string, string>, okStates: number[]) => void
}) {
  const entries = Object.entries(stateNames ?? {})
    .map(([value, name]) => ({ value: Number(value), name }))
    .sort((a, b) => a.value - b.value)
  const [newValue, setNewValue] = useState('')
  const [newName, setNewName] = useState('')

  const renameState = (value: number, name: string) => onChange({ ...(stateNames ?? {}), [value]: name }, okStates)
  const toggleOk = (value: number, ok: boolean) =>
    onChange(stateNames ?? {}, ok ? [...okStates, value] : okStates.filter((v) => v !== value))
  const addState = () => {
    const value = Number(newValue)
    if (!Number.isInteger(value) || !newName.trim()) return
    onChange({ ...(stateNames ?? {}), [value]: newName.trim() }, okStates)
    setNewValue('')
    setNewName('')
  }

  return (
    <fieldset className="space-y-2 rounded-lg border border-white/10 p-3">
      <legend className="px-1 text-sm text-slate-300">States</legend>
      {entries.length === 0 && <p className="text-xs text-slate-500">No states yet — add the values this OID can report.</p>}
      {entries.map((s) => (
        <div key={s.value} className="flex items-center gap-2">
          <span className="w-10 shrink-0 text-right font-mono text-xs text-slate-400">{s.value}</span>
          <input className="rd-input flex-1" value={s.name} onChange={(e) => renameState(s.value, e.target.value)} />
          <label className="flex shrink-0 items-center gap-1.5 text-xs text-slate-400">
            <input type="checkbox" checked={okStates.includes(s.value)} onChange={(e) => toggleOk(s.value, e.target.checked)} />
            OK
          </label>
        </div>
      ))}
      <div className="flex items-center gap-2 pt-1">
        <input className="rd-input w-16" type="number" placeholder="#" value={newValue} onChange={(e) => setNewValue(e.target.value)} />
        <input className="rd-input flex-1" placeholder="Name" value={newName} onChange={(e) => setNewName(e.target.value)} />
        <button type="button" className="btn-secondary shrink-0" onClick={addState}>
          Add
        </button>
      </div>
    </fieldset>
  )
}

interface Props {
  profileId: string
  /** Null when adding a new metric; the metric being changed otherwise. */
  metric: ProfileMetric | null
  /** Pre-fills a new metric's draft (from metricFromMibObject). Ignored when
   *  editing an existing one. */
  seed?: Partial<ProfileMetric>
  onClose: () => void
  onSaved: () => void
}

/** A modal form over a ProfileMetric draft: what to poll, how to label and
 *  type it, its optional alert rule, and a live preview against a real
 *  device. Save stays disabled until a preview of the current values came
 *  back with at least one row - draftHash is compared against the hash taken
 *  at that preview, so any further edit disables it again. */
export default function MetricEditor({ profileId, metric, seed, onClose, onSaved }: Props) {
  const isNew = metric === null
  const [draft, setDraft] = useState<ProfileMetric>(() => ({ ...emptyMetric(), ...seed, ...(metric ?? {}) }))
  const [keyEdited, setKeyEdited] = useState(!isNew)
  const setField = (patch: Partial<ProfileMetric>) => setDraft((d) => ({ ...d, ...patch }))

  // While creating and the key hasn't been hand-edited, follow the name -
  // updated alongside it in the same state change rather than in a separate
  // effect, so there is no extra render and nothing to keep in sync.
  const setName = (name: string) => setDraft((d) => ({ ...d, name, key: isNew && !keyEdited ? suggestKey(name) : d.key }))

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { devices } = useDevices()
  const grouped = groupBySite(devices)
  const [deviceId, setDeviceId] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const [previewResult, setPreviewResult] = useState<MetricPreview | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [previewedHash, setPreviewedHash] = useState<string | null>(null)
  const requestRef = useRef(0)

  const runPreview = async () => {
    if (!deviceId) return
    const myRequest = ++requestRef.current
    setPreviewing(true)
    setPreviewError(null)
    try {
      const res = await previewMetric(deviceId, draft)
      if (requestRef.current !== myRequest) return
      if (res.ok && res.preview) {
        setPreviewResult(res.preview)
        if (res.preview.rows.length > 0) setPreviewedHash(draftHash(draft))
      } else {
        setPreviewResult(null)
        setPreviewError(res.error || 'The preview failed')
      }
    } catch (err) {
      if (requestRef.current !== myRequest) return
      setPreviewResult(null)
      setPreviewError((err as ApiError).message || 'The preview failed')
    } finally {
      if (requestRef.current === myRequest) setPreviewing(false)
    }
  }

  const canSave = !saving && previewedHash !== null && previewedHash === draftHash(draft)

  const save = async () => {
    setError(null)
    setSaving(true)
    try {
      if (isNew) {
        await createMetric(profileId, draft)
      } else {
        await updateMetric(draft.id, draft)
      }
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the metric')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-2xl space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{isNew ? 'Add metric' : 'Edit metric'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className="rd-input w-full" value={draft.name} onChange={(e) => setName(e.target.value)} required />
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Key</span>
          <input
            className="rd-input w-full font-mono disabled:opacity-60"
            value={draft.key}
            disabled={!isNew}
            onChange={(e) => {
              setKeyEdited(true)
              setField({ key: e.target.value })
            }}
            required
          />
          {!isNew && <span className="block text-xs text-slate-500">The key names the metric&apos;s history and cannot change.</span>}
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Source</span>
          <select className="rd-select w-full" value={draft.source} onChange={(e) => setField({ source: e.target.value as MetricSource })}>
            <option value="scalar">Scalar</option>
            <option value="column">Table column</option>
            <option value="used_free_pct">Used ÷ (used + free) %</option>
          </select>
        </label>

        {draft.source === 'used_free_pct' ? (
          <>
            <OidField label="Used OID" value={draft.oid} onChange={(v) => setField({ oid: v })} />
            <OidField label="Free OID" value={draft.oid2} onChange={(v) => setField({ oid2: v })} />
          </>
        ) : (
          <OidField label={draft.source === 'column' ? 'Column OID' : 'Value OID'} value={draft.oid} onChange={(v) => setField({ oid: v })} />
        )}

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Kind</span>
          <select
            className="rd-select w-full"
            value={draft.kind}
            onChange={(e) => {
              const kind = e.target.value as MetricKind
              setField({ kind, ...(kind !== 'status' && draft.rule_kind === 'not_ok' ? { rule_kind: '' as RuleKind } : {}) })
            }}
          >
            <option value="gauge">Gauge</option>
            <option value="counter">Counter (per-second rate)</option>
            <option value="status">Status</option>
          </select>
        </label>

        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Units</span>
            <input className="rd-input w-full" value={draft.units} onChange={(e) => setField({ units: e.target.value })} placeholder="%, Mbps, °C…" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Scale</span>
            <input
              className="rd-input w-full"
              type="number"
              step="any"
              value={draft.scale}
              onChange={(e) => setField({ scale: Number(e.target.value) })}
            />
          </label>
        </div>

        <details className="rounded-lg border border-white/10 p-3">
          <summary className="cursor-pointer text-sm text-slate-300">Advanced</summary>
          <div className="mt-3 space-y-3">
            <OidField
              label="Decimal places from column (OID)"
              value={draft.precision_oid}
              onChange={(v) => setField({ precision_oid: v })}
            />
            <OidField label="Keep only rows where column (OID)" value={draft.filter_oid} onChange={(v) => setField({ filter_oid: v })} />
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">…is one of (comma list)</span>
              <input
                className="rd-input w-full"
                value={draft.filter_values.join(', ')}
                onChange={(e) =>
                  setField({
                    filter_values: e.target.value
                      .split(',')
                      .map((s) => s.trim())
                      .filter(Boolean),
                  })
                }
                placeholder="1, 2, 3"
              />
            </label>
          </div>
        </details>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Label</span>
          <select className="rd-select w-full" value={draft.label_mode} onChange={(e) => setField({ label_mode: e.target.value as LabelMode })}>
            <option value="index">Row index</option>
            <option value="column">Column in this table</option>
            <option value="same_index">Column in another table with the same index</option>
            <option value="pointer">Through a pointer column</option>
          </select>
        </label>
        {(draft.label_mode === 'column' || draft.label_mode === 'same_index') && (
          <OidField label="Label column OID" value={draft.label_oid} onChange={(v) => setField({ label_oid: v })} />
        )}
        {draft.label_mode === 'pointer' && (
          <>
            <OidField label="Pointer column OID" value={draft.label_pointer_oid} onChange={(v) => setField({ label_pointer_oid: v })} />
            <OidField label="Target column OID" value={draft.label_target_oid} onChange={(v) => setField({ label_target_oid: v })} />
          </>
        )}

        {draft.kind === 'status' && (
          <StatusStatesEditor
            stateNames={draft.state_names}
            okStates={draft.ok_states}
            onChange={(state_names, ok_states) => setField({ state_names, ok_states })}
          />
        )}

        <fieldset className="space-y-3 rounded-lg border border-white/10 p-3">
          <legend className="px-1 text-sm text-slate-300">Alert rule</legend>
          <select
            className="rd-select w-full"
            value={draft.rule_kind}
            onChange={(e) => {
              const rule_kind = e.target.value as RuleKind
              setField({ rule_kind, ...(rule_kind === '' ? { rule_value: null } : {}) })
            }}
          >
            <option value="">None</option>
            <option value="above">Above</option>
            <option value="below">Below</option>
            {draft.kind === 'status' && <option value="not_ok">Not OK</option>}
          </select>
          {(draft.rule_kind === 'above' || draft.rule_kind === 'below') && (
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Value</span>
              <input
                className="rd-input w-full"
                type="number"
                step="any"
                value={draft.rule_value ?? ''}
                onChange={(e) => setField({ rule_value: e.target.value === '' ? null : Number(e.target.value) })}
              />
            </label>
          )}
          {draft.rule_kind !== '' && (
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Hold for (minutes)</span>
              <input
                className="rd-input w-full"
                type="number"
                min={0}
                max={1440}
                value={draft.rule_hold_minutes}
                onChange={(e) => setField({ rule_hold_minutes: Number(e.target.value) })}
              />
            </label>
          )}
          <label className="flex items-center gap-2 text-sm text-slate-300">
            <input type="checkbox" checked={draft.rule_enabled} onChange={(e) => setField({ rule_enabled: e.target.checked })} />
            Enabled
          </label>
        </fieldset>

        <section className="space-y-3 rounded-lg border border-white/10 p-3">
          <h3 className="text-sm font-medium text-slate-200">Preview</h3>
          <div className="flex flex-wrap items-center gap-2">
            <select className="rd-select" value={deviceId} onChange={(e) => setDeviceId(e.target.value)} aria-label="Device to preview on">
              <option value="">Choose a device…</option>
              {grouped.map((g) => (
                <optgroup key={g.site} label={g.site}>
                  {g.devices.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name} ({d.host})
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
            <button type="button" className="btn-secondary" disabled={!deviceId || previewing} onClick={() => void runPreview()}>
              {previewing ? 'Previewing…' : 'Preview'}
            </button>
          </div>

          {previewError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{previewError}</div>}

          {previewResult && (
            <div className="space-y-2">
              {Object.entries(previewResult.errors).map(([oid, msg]) => (
                <p key={oid} className="text-xs text-amber-300">
                  {oid}: {msg}
                </p>
              ))}
              {previewResult.rows.length === 0 ? (
                <p className="text-sm text-slate-500">No rows came back.</p>
              ) : (
                <div className="overflow-x-auto rounded-lg border border-white/10">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                        <th className="px-3 py-2 font-medium">Label</th>
                        <th className="px-3 py-2 font-medium">Value</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-white/5">
                      {previewResult.rows.map((r) => (
                        <tr key={r.instance}>
                          <td className="px-3 py-2 text-slate-200">{r.label}</td>
                          <td className="px-3 py-2 text-slate-200">
                            {draft.kind === 'status' ? r.state || String(r.value) : `${r.value}${draft.units ? ` ${draft.units}` : ''}`}
                            {r.violates && (
                              <span className="ml-2 rounded-full border border-red-500/30 bg-red-500/15 px-2 py-0.5 text-xs text-red-300">
                                would alert
                              </span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {previewResult.raw_counter && (
                <p className="text-xs text-slate-500">Counters show raw values here; history stores per-second rates.</p>
              )}
            </div>
          )}
        </section>

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={!canSave} title={canSave ? undefined : 'Preview the current values first'}>
            Save
          </button>
        </div>
      </form>
    </div>
  )
}

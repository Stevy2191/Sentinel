import { useMemo, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useDevices, type PortRole } from '@/hooks/useDevices'
import type { ScopePreviewState } from '@/hooks/useScopePreview'
import { useSites } from '@/hooks/useSites'
import PortPicker from '@/components/reports/PortPicker'
import { MAX_REPORT_SUBJECTS, type NetworkScopeType } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { PORT_ROLE_HINT } from '@/utils/network'
import {
  NETWORK_SCOPE_TABS,
  REPORT_ROLES,
  previewSizeLine,
  type NetworkScopeDraft,
  type ScopeChoice,
} from '@/utils/reportScope'

interface Props {
  value: NetworkScopeDraft
  onChange: (next: NetworkScopeDraft) => void
  /** The live preview of value, from useMetricsReportDraft. */
  preview: ScopePreviewState
}

interface Item {
  id: string
  name: string
  /** Shown beside the name, e.g. a device's site. */
  hint?: string
}

/** Sites or devices to tick, with a filter once the list is long. The dashboard
 *  pickers cap at 50 (the widget limit) and do not hand back names, so the
 *  report tabs use this instead. */
function Checklist({
  items,
  value,
  onChange,
  max,
  empty,
  loading,
}: {
  items: Item[]
  value: ScopeChoice[]
  onChange: (next: ScopeChoice[]) => void
  max?: number
  empty: string
  loading: boolean
}) {
  const [filter, setFilter] = useState('')
  const chosen = useMemo(() => new Set(value.map((v) => v.id)), [value])
  const q = filter.trim().toLowerCase()
  const shown = q
    ? items.filter((i) => i.name.toLowerCase().includes(q) || (i.hint ?? '').toLowerCase().includes(q))
    : items
  const full = max !== undefined && value.length >= max
  const toggle = (i: Item) => {
    if (chosen.has(i.id)) onChange(value.filter((v) => v.id !== i.id))
    else if (!full) onChange([...value, { id: i.id, name: i.name }])
  }
  return (
    <div className="space-y-2">
      {items.length > 8 && (
        <input className={inputCls} placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
      )}
      <div className="max-h-56 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
        {items.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : empty}</p>}
        {shown.map((i) => (
          <label
            key={i.id}
            className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-sm text-slate-300 hover:bg-white/5"
          >
            <input
              type="checkbox"
              checked={chosen.has(i.id)}
              disabled={full && !chosen.has(i.id)}
              onChange={() => toggle(i)}
            />
            <span className="truncate">{i.name}</span>
            {i.hint && <span className="truncate text-xs text-slate-500">{i.hint}</span>}
          </label>
        ))}
      </div>
      <p className="text-xs text-slate-500">
        {value.length} selected{full && max !== undefined ? ` (at most ${max})` : ''}
      </p>
    </div>
  )
}

/** How big the scope is right now, or why it cannot be used. */
function SizeLine({ scopeType, preview }: { scopeType: NetworkScopeType; preview: ScopePreviewState }) {
  if (preview.error) {
    return (
      <p role="alert" className={`text-sm ${colors.error.text}`}>
        {preview.error}
      </p>
    )
  }
  if (preview.loading) {
    return (
      <p className="flex items-center gap-2 text-sm text-slate-400">
        <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden /> Sizing the scope…
      </p>
    )
  }
  if (!preview.data) return null
  const nothing = (scopeType === 'devices' ? preview.data.devices : preview.data.ports) === 0
  return (
    <p className={`text-sm ${preview.data.capped || nothing ? colors.warning.text : 'text-slate-300'}`}>
      {previewSizeLine(scopeType, preview.data)}
    </p>
  )
}

/**
 * What a Metrics report covers, in four tabs: chosen ports, port roles at
 * sites, devices, or whole sites. Lists hold only what the user can see (the
 * API filters by site access). Under the tabs, a live line sizes the scope.
 */
export default function MetricsScopePicker({ value, onChange, preview }: Props) {
  const { sites, loading: sitesLoading, error: sitesError } = useSites()
  const { devices, loading: devicesLoading, error: devicesError } = useDevices()
  const siteItems = useMemo<Item[]>(() => sites.map((s) => ({ id: s.id, name: s.name })), [sites])
  const deviceItems = useMemo<Item[]>(
    () => devices.map((d) => ({ id: d.id, name: d.name, hint: d.site_name })),
    [devices],
  )
  const set = (patch: Partial<NetworkScopeDraft>) => onChange({ ...value, ...patch })
  const toggleRole = (role: PortRole) =>
    set({ roles: value.roles.includes(role) ? value.roles.filter((r) => r !== role) : [...value.roles, role] })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-1">
        {NETWORK_SCOPE_TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            aria-pressed={value.scopeType === t.value}
            onClick={() => set({ scopeType: t.value })}
            className={`rounded-lg px-3 py-1.5 text-sm transition ${
              value.scopeType === t.value ? 'bg-primary-500/15 text-white' : 'text-slate-400 hover:text-white'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {value.scopeType === 'ports' && (
        <PortPicker
          devices={devices}
          devicesLoading={devicesLoading}
          value={value.ports}
          onChange={(ports) => set({ ports })}
        />
      )}

      {value.scopeType === 'port_roles' && (
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-sm text-slate-300">Ports with these roles</p>
            {REPORT_ROLES.map((r) => (
              <label key={r.value} className="flex items-start gap-2 text-sm text-slate-300">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={value.roles.includes(r.value)}
                  onChange={() => toggleRole(r.value)}
                />
                <span>
                  {r.label} <span className="text-xs text-slate-500">{PORT_ROLE_HINT[r.value]}</span>
                </span>
              </label>
            ))}
          </div>
          <div className="space-y-1">
            <p className="text-sm text-slate-300">At these sites</p>
            <Checklist
              items={siteItems}
              value={value.sites}
              onChange={(s) => set({ sites: s })}
              empty={sitesError ?? 'No sites.'}
              loading={sitesLoading}
            />
          </div>
          <p className="text-xs text-slate-500">
            Worked out each time the report runs, so a port given one of these roles later is included.
          </p>
        </div>
      )}

      {value.scopeType === 'devices' && (
        <Checklist
          items={deviceItems}
          value={value.devices}
          onChange={(d) => set({ devices: d })}
          max={MAX_REPORT_SUBJECTS}
          empty={devicesError ?? 'No devices.'}
          loading={devicesLoading}
        />
      )}

      {value.scopeType === 'sites' && (
        <div className="space-y-1">
          <Checklist
            items={siteItems}
            value={value.sites}
            onChange={(s) => set({ sites: s })}
            empty={sitesError ?? 'No sites.'}
            loading={sitesLoading}
          />
          <p className="text-xs text-slate-500">
            Each site&apos;s totals, plus every port of every device there, worked out each time the report runs.
          </p>
        </div>
      )}

      <SizeLine scopeType={value.scopeType} preview={preview} />
    </div>
  )
}

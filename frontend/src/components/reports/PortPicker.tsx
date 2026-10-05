import { useMemo, useState } from 'react'
import { X } from 'lucide-react'
import type { Device, PortRole } from '@/hooks/useDevices'
import { usePortChoices, type PortView } from '@/hooks/usePorts'
import { MAX_REPORT_SUBJECTS } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { portChoiceName, type ScopeChoice } from '@/utils/reportScope'

interface Props {
  /** Devices the user can see, from the scope picker's useDevices. */
  devices: Device[]
  devicesLoading: boolean
  value: ScopeChoice[]
  onChange: (ports: ScopeChoice[]) => void
}

// Access is the default role, so only the other two are worth a tag.
const ROLE_TAG: Partial<Record<PortRole, string>> = { wan: 'WAN', uplink: 'Uplink' }

/**
 * Ports for a Metrics report: pick a device, tick its ports, then pick another
 * device and tick more. Everything ticked collects in one list, each port
 * named "device · port (alias)" the way the report names its rows.
 */
export default function PortPicker({ devices, devicesLoading, value, onChange }: Props) {
  const [deviceId, setDeviceId] = useState('')
  const [filter, setFilter] = useState('')
  const { ports, error } = usePortChoices(deviceId || undefined)
  const device = devices.find((d) => d.id === deviceId)
  const chosen = useMemo(() => new Set(value.map((p) => p.id)), [value])
  const full = value.length >= MAX_REPORT_SUBJECTS

  // Physical ports first, then VLANs, tunnels and the like; each group stays
  // in ifIndex order, which is how the API lists them.
  const shown = useMemo(() => {
    if (!ports) return []
    const q = filter.trim().toLowerCase()
    const matches = (p: PortView) => !q || [p.name, p.alias, p.label].some((s) => s.toLowerCase().includes(q))
    return [...ports.filter((p) => p.physical), ...ports.filter((p) => !p.physical)].filter(matches)
  }, [ports, filter])

  const choiceOf = (p: PortView): ScopeChoice => ({ id: p.id, name: portChoiceName(device?.name ?? 'Device', p) })
  const toggle = (p: PortView) => {
    if (chosen.has(p.id)) onChange(value.filter((v) => v.id !== p.id))
    else if (!full) onChange([...value, choiceOf(p)])
  }
  const tickAllShown = () => {
    const room = MAX_REPORT_SUBJECTS - value.length
    onChange([...value, ...shown.filter((p) => !chosen.has(p.id)).slice(0, room).map(choiceOf)])
  }

  return (
    <div className="space-y-3">
      <select
        className={inputCls}
        aria-label="Device"
        value={deviceId}
        onChange={(e) => {
          setDeviceId(e.target.value)
          setFilter('')
        }}
      >
        <option value="">{devicesLoading ? 'Loading…' : 'Choose a device to list its ports…'}</option>
        {devices.map((d) => (
          <option key={d.id} value={d.id}>
            {d.name}
            {d.site_name ? ` (${d.site_name})` : ''}
          </option>
        ))}
      </select>

      {deviceId && (
        <div className="space-y-2">
          <div className="flex gap-2">
            <input
              className={inputCls}
              placeholder="Filter by name or alias…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <button
              type="button"
              className="btn-secondary shrink-0"
              onClick={tickAllShown}
              disabled={full || shown.every((p) => chosen.has(p.id))}
            >
              Tick all shown
            </button>
          </div>
          <div className="max-h-56 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
            {error && <p className={`text-xs ${colors.error.text}`}>{error}</p>}
            {!error && ports === null && <p className="text-xs text-slate-500">Loading…</p>}
            {ports !== null && shown.length === 0 && (
              <p className="text-xs text-slate-500">
                {ports.length === 0 ? 'This device has no ports yet.' : 'No port matches.'}
              </p>
            )}
            {shown.map((p) => (
              <label
                key={p.id}
                className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-sm text-slate-300 hover:bg-white/5"
              >
                <input
                  type="checkbox"
                  checked={chosen.has(p.id)}
                  disabled={full && !chosen.has(p.id)}
                  onChange={() => toggle(p)}
                />
                <span className="truncate">{p.name || `Port ${p.label}`}</span>
                {p.alias && <span className="truncate text-xs text-slate-500">{p.alias}</span>}
                {ROLE_TAG[p.role] && (
                  <span className="ml-auto shrink-0 rounded bg-white/5 px-1.5 text-xs text-slate-400">
                    {ROLE_TAG[p.role]}
                  </span>
                )}
              </label>
            ))}
          </div>
        </div>
      )}

      <div className="space-y-1">
        <p className="text-sm text-slate-300">Chosen ports ({value.length})</p>
        {value.length === 0 ? (
          <p className="text-xs text-slate-500">
            Pick a device and tick its ports; pick another device to add more.
          </p>
        ) : (
          <ul className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
            {value.map((p) => (
              <li key={p.id} className="flex items-center gap-2 text-sm text-slate-300">
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                <button
                  type="button"
                  className="text-slate-500 hover:text-white"
                  aria-label={`Remove ${p.name}`}
                  onClick={() => onChange(value.filter((v) => v.id !== p.id))}
                >
                  <X className="h-4 w-4" />
                </button>
              </li>
            ))}
          </ul>
        )}
        {full && <p className={`text-xs ${colors.warning.text}`}>A report covers at most {MAX_REPORT_SUBJECTS} ports.</p>}
      </div>
    </div>
  )
}

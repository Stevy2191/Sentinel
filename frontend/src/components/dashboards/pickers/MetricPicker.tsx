import { useDeviceMetrics } from '@/hooks/useDashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  /** Metrics are listed from this device (the first one chosen). */
  deviceId?: string
  value: string[]
  onChange: (keys: string[]) => void
  single?: boolean
  /** Only port metrics (if_*), for site totals. */
  portOnly?: boolean
}

const PORT_METRICS = [
  { metric: 'if_in_bps', label: 'Traffic in', unit: 'bps' },
  { metric: 'if_out_bps', label: 'Traffic out', unit: 'bps' },
  { metric: 'if_in_util_pct', label: 'Busy in', unit: '%' },
  { metric: 'if_out_util_pct', label: 'Busy out', unit: '%' },
  { metric: 'if_in_errors_pm', label: 'Errors in', unit: 'per_min' },
  { metric: 'if_out_errors_pm', label: 'Errors out', unit: 'per_min' },
]

export default function MetricPicker({ deviceId, value, onChange, single, portOnly }: Props) {
  const { metrics, loading } = useDeviceMetrics(portOnly ? undefined : deviceId)
  const options = (portOnly ? PORT_METRICS : metrics).map((m) => ({ metric: m.metric, unit: m.unit, label: `${m.label}${m.unit ? ` (${m.unit})` : ''}` }))
  if (!portOnly && !deviceId) return <p className="text-xs text-slate-500">Choose a device first.</p>
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">{loading ? 'Loading…' : 'Choose a metric…'}</option>
        {options.map((o) => (
          <option key={o.metric} value={o.metric}>
            {o.label}
          </option>
        ))}
      </select>
    )
  }
  // One chart has one unit: once a metric is chosen, the others in a
  // different unit are greyed out (the server refuses mixing them).
  const unit = options.find((o) => value.includes(o.metric))?.unit
  const otherUnit = (o: { metric: string; unit: string }) => unit !== undefined && o.unit !== unit && !value.includes(o.metric)
  const toggle = (k: string) =>
    onChange(value.includes(k) ? value.filter((v) => v !== k) : value.length < 10 ? [...value, k] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {options.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'This device has no metrics yet.'}</p>}
      {options.some(otherUnit) && <p className="text-xs text-slate-500">One chart shows one unit: metrics in other units are greyed out.</p>}
      {options.map((o) => (
        <label key={o.metric} className={`flex items-center gap-2 text-sm ${otherUnit(o) ? 'text-slate-600' : 'text-slate-300'}`}>
          <input type="checkbox" checked={value.includes(o.metric)} disabled={otherUnit(o)} onChange={() => toggle(o.metric)} /> {o.label}
        </label>
      ))}
    </div>
  )
}

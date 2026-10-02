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
  { metric: 'if_in_bps', label: 'Traffic in' },
  { metric: 'if_out_bps', label: 'Traffic out' },
  { metric: 'if_in_util_pct', label: 'Busy in' },
  { metric: 'if_out_util_pct', label: 'Busy out' },
  { metric: 'if_in_errors_pm', label: 'Errors in' },
  { metric: 'if_out_errors_pm', label: 'Errors out' },
]

export default function MetricPicker({ deviceId, value, onChange, single, portOnly }: Props) {
  const { metrics, loading } = useDeviceMetrics(portOnly ? undefined : deviceId)
  const options = portOnly ? PORT_METRICS : metrics.map((m) => ({ metric: m.metric, label: `${m.label}${m.unit ? ` (${m.unit})` : ''}` }))
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
  const toggle = (k: string) =>
    onChange(value.includes(k) ? value.filter((v) => v !== k) : value.length < 10 ? [...value, k] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {options.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'This device has no metrics yet.'}</p>}
      {options.map((o) => (
        <label key={o.metric} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(o.metric)} onChange={() => toggle(o.metric)} /> {o.label}
        </label>
      ))}
    </div>
  )
}

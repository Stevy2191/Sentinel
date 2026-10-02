import { useDeviceMetrics } from '@/hooks/useDashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  deviceId?: string
  metric?: string
  value: string[]
  onChange: (v: string[]) => void
  single?: boolean
}

/** The instances (ports, sensor rows) of one metric on one device. Choosing
 *  none means the device's total. */
export default function InstancePicker({ deviceId, metric, value, onChange, single }: Props) {
  const { metrics } = useDeviceMetrics(deviceId)
  const instances = metrics.find((m) => m.metric === metric)?.instances ?? []
  if (!deviceId || !metric || instances.length <= 1) return null
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">Device total</option>
        {instances.map((i) => (
          <option key={i.instance} value={i.instance}>
            {i.label}
          </option>
        ))}
      </select>
    )
  }
  const toggle = (k: string) => onChange(value.includes(k) ? value.filter((v) => v !== k) : [...value, k])
  return (
    <div className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      <p className="text-xs text-slate-500">None chosen: the device total.</p>
      {instances.map((i) => (
        <label key={i.instance} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(i.instance)} onChange={() => toggle(i.instance)} /> {i.label}
        </label>
      ))}
    </div>
  )
}

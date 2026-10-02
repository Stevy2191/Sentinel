import { useDevices } from '@/hooks/useDevices'
import { inputCls } from '@/utils/dashboards'

interface Props {
  siteId?: string | null
  value: string[]
  onChange: (ids: string[]) => void
  single?: boolean
}

/** Devices the user can see, optionally one site's; a select for one, a
 *  checklist for several (at most 50, the widget limit). */
export default function DevicePicker({ siteId, value, onChange, single }: Props) {
  const { devices, loading } = useDevices(siteId ? { siteId } : {})
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">{loading ? 'Loading…' : 'Choose a device…'}</option>
        {devices.map((d) => (
          <option key={d.id} value={d.id}>
            {d.name} {siteId ? '' : `(${d.site_name})`}
          </option>
        ))}
      </select>
    )
  }
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {devices.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No devices.'}</p>}
      {devices.map((d) => (
        <label key={d.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(d.id)} onChange={() => toggle(d.id)} />
          {d.name} <span className="text-slate-500">{d.site_name}</span>
        </label>
      ))}
    </div>
  )
}

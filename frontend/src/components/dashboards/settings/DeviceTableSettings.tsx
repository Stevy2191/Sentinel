import { DEVICE_TYPE_LABEL, type DeviceType } from '@/hooks/useDevices'
import { str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'

const STATUSES = ['up', 'down', 'pending', 'paused', 'error']

export default function DeviceTableSettings({ config, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const toggle = (key: 'types' | 'statuses', v: string) => {
    const cur = strs(config, key)
    set({ [key]: cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v] })
  }
  return (
    <div className="space-y-3">
      <Field label="Site">
        <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
      </Field>
      <Field label="Only these types" hint="None ticked: every type.">
        <div className="flex flex-wrap gap-3">
          {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
            <label key={t} className="flex items-center gap-1.5 text-sm text-slate-300">
              <input type="checkbox" checked={strs(config, 'types').includes(t)} onChange={() => toggle('types', t)} /> {DEVICE_TYPE_LABEL[t]}
            </label>
          ))}
        </div>
      </Field>
      <Field label="Only these statuses" hint="None ticked: every status.">
        <div className="flex flex-wrap gap-3">
          {STATUSES.map((s) => (
            <label key={s} className="flex items-center gap-1.5 text-sm capitalize text-slate-300">
              <input type="checkbox" checked={strs(config, 'statuses').includes(s)} onChange={() => toggle('statuses', s)} /> {s}
            </label>
          ))}
        </div>
      </Field>
    </div>
  )
}

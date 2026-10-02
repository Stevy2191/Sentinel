import { inputCls, num, str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'

export default function EventLogSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const forDevice = 'device_id' in config && config.device_id !== undefined
  const device = str(config, 'device_id')
  return (
    <div className="space-y-3">
      <Field label="Events of">
        <select className={inputCls} value={forDevice ? 'device' : 'site'} onChange={(e) => (e.target.value === 'device' ? set({ device_id: '', site_id: undefined }) : set({ site_id: siteId, device_id: undefined }))}>
          <option value="site">A site</option>
          <option value="device">One device</option>
        </select>
      </Field>
      {forDevice ? (
        <Field label="Device">
          <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => set({ device_id: ids[0] ?? '' })} />
        </Field>
      ) : (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      )}
      <Field label="How many" hint="10 to 50.">
        <input className={inputCls} type="number" min={10} max={50} value={num(config, 'limit', 20)} onChange={(e) => set({ limit: Number(e.target.value) })} />
      </Field>
    </div>
  )
}

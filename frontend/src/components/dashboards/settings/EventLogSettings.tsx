import { useState } from 'react'
import { inputCls, num, str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'

export default function EventLogSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const [forDevice, setForDevice] = useState(typeof config.device_id === 'string' && config.device_id !== '')
  const device = str(config, 'device_id')
  return (
    <div className="space-y-3">
      <Field label="Events of">
        <select className={inputCls} value={forDevice ? 'device' : 'site'} onChange={(e) => {
            const toDevice = e.target.value === 'device'
            setForDevice(toDevice)
            set(toDevice ? { device_id: undefined, site_id: undefined } : { site_id: siteId ?? undefined, device_id: undefined })
          }}>
          <option value="site">A site</option>
          <option value="device">One device</option>
        </select>
      </Field>
      {forDevice ? (
        <Field label="Device">
          <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => set({ device_id: ids[0] })} />
        </Field>
      ) : (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id ?? undefined })} />
        </Field>
      )}
      <Field label="How many" hint="10 to 50.">
        <input className={inputCls} type="number" min={10} max={50} value={num(config, 'limit', 20)} onChange={(e) => set({ limit: Number(e.target.value) })} />
      </Field>
    </div>
  )
}

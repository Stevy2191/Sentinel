import { inputCls, num, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

export default function TopNSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const bySite = 'site_id' in config && config.site_id !== undefined
  return (
    <div className="space-y-3">
      <Field label="Ports of">
        <select className={inputCls} value={bySite ? 'site' : 'devices'} onChange={(e) => (e.target.value === 'site' ? set({ site_id: siteId, devices: [] }) : set({ site_id: undefined, devices: [] }))}>
          <option value="site">A site</option>
          <option value="devices">Chosen devices</option>
        </select>
      </Field>
      {bySite ? (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      ) : (
        <Field label="Devices">
          <DevicePicker siteId={siteId} value={strs(config, 'devices')} onChange={(ids) => set({ devices: ids })} />
        </Field>
      )}
      <Field label="Rank by">
        <select className={inputCls} value={str(config, 'measure') || 'traffic'} onChange={(e) => set({ measure: e.target.value })}>
          <option value="traffic">Traffic (average in + out)</option>
          <option value="utilisation">How busy (busier direction)</option>
          <option value="errors">Errors per minute</option>
        </select>
      </Field>
      <Field label="How many" hint="5 to 20.">
        <input className={inputCls} type="number" min={5} max={20} value={num(config, 'n', 10)} onChange={(e) => set({ n: Number(e.target.value) })} />
      </Field>
      <Field label="Time range">
        <RangeSelect value={str(config, 'range')} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}

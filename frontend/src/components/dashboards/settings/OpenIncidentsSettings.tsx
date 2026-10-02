import { inputCls, num, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MonitorPicker from '@/components/dashboards/pickers/MonitorPicker'

export default function OpenIncidentsSettings({ config, siteId, onChange }: SettingsProps) {
  const scope = str(config, 'scope') || 'all'
  const limit = num(config, 'limit', 20)
  const setScope = (s: string) => {
    if (s === 'site') onChange({ scope: s, site_id: siteId, limit })
    else if (s === 'devices') onChange({ scope: s, devices: [], limit })
    else if (s === 'monitors') onChange({ scope: s, monitors: [], limit })
    else onChange({ scope: 'all', limit })
  }
  return (
    <div className="space-y-3">
      <Field label="Incidents of" hint={scope === 'all' ? 'Everything the viewer can see. On a public link, everything — including what is added later.' : undefined}>
        <select className={inputCls} value={scope} onChange={(e) => setScope(e.target.value)}>
          <option value="all">Everything I can see</option>
          <option value="site">A site's devices</option>
          <option value="devices">Chosen devices</option>
          <option value="monitors">Chosen monitors</option>
        </select>
      </Field>
      {scope === 'site' && (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => onChange({ ...config, site_id: id })} />
        </Field>
      )}
      {scope === 'devices' && (
        <Field label="Devices">
          <DevicePicker siteId={siteId} value={strs(config, 'devices')} onChange={(ids) => onChange({ ...config, devices: ids })} />
        </Field>
      )}
      {scope === 'monitors' && (
        <Field label="Monitors">
          <MonitorPicker value={strs(config, 'monitors')} onChange={(ids) => onChange({ ...config, monitors: ids })} />
        </Field>
      )}
      <Field label="Show at most" hint="5 to 50.">
        <input className={inputCls} type="number" min={5} max={50} value={limit} onChange={(e) => onChange({ ...config, limit: Number(e.target.value) })} />
      </Field>
    </div>
  )
}

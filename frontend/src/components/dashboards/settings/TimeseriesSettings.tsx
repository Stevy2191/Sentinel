import { inputCls, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MetricPicker from '@/components/dashboards/pickers/MetricPicker'
import InstancePicker from '@/components/dashboards/pickers/InstancePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

export default function TimeseriesSettings({ config, siteId, onChange }: SettingsProps) {
  const source = str(config, 'source') || 'metrics'
  const range = str(config, 'range') || '24h'
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const devices = strs(config, 'devices')
  const metrics = strs(config, 'metrics')
  const siteTotal = source === 'metrics' && 'site_id' in config && config.site_id !== undefined

  return (
    <div className="space-y-3">
      <Field label="Show">
        <select
          className={inputCls}
          value={source}
          onChange={(e) =>
            onChange(
              e.target.value === 'site_traffic'
                ? { source: 'site_traffic', site_id: siteId, view: 'internet', range }
                : { source: 'metrics', metrics: [], devices: [], range },
            )
          }
        >
          <option value="metrics">Metrics from devices</option>
          <option value="site_traffic">A site's traffic</option>
        </select>
      </Field>

      {source === 'site_traffic' ? (
        <>
          <Field label="Site">
            <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
          </Field>
          <Field label="Traffic" hint="Internet traffic needs a port marked WAN at the site.">
            <select className={inputCls} value={str(config, 'view') || 'internet'} onChange={(e) => set({ view: e.target.value })}>
              <option value="internet">Internet (download and upload)</option>
              <option value="east_west">Inside the site</option>
            </select>
          </Field>
        </>
      ) : (
        <>
          <label className="flex items-center gap-2 text-sm text-slate-300">
            <input
              type="checkbox"
              checked={siteTotal}
              onChange={(e) =>
                e.target.checked
                  ? set({ site_id: siteId, devices: [], instances: [], metrics: metrics.filter((m) => m.startsWith('if_')) })
                  : set({ site_id: undefined })
              }
            />
            A whole site's total
          </label>
          {siteTotal ? (
            <Field label="Site">
              <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
            </Field>
          ) : (
            <Field label="Devices" hint="Up to 50.">
              <DevicePicker siteId={siteId} value={devices} onChange={(ids) => set({ devices: ids, instances: [] })} />
            </Field>
          )}
          <Field
            label="Metrics"
            hint={siteTotal ? "Site totals combine every device's physical ports: traffic and errors add up, percentages are averaged." : 'Up to 10, listed from the first device.'}
          >
            <MetricPicker deviceId={devices[0]} portOnly={siteTotal} value={metrics} onChange={(m) => set({ metrics: m, instances: [] })} />
          </Field>
          {!siteTotal && devices.length === 1 && metrics.length === 1 && (
            <Field label="Ports or rows">
              <InstancePicker deviceId={devices[0]} metric={metrics[0]} value={strs(config, 'instances')} onChange={(v) => set({ instances: v })} />
            </Field>
          )}
        </>
      )}
      <Field label="Time range">
        <RangeSelect value={range} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}

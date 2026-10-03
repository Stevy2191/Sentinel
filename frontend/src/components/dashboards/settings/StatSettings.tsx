import { inputCls, str, type SettingsProps } from '@/utils/dashboards'
import { colors } from '@/utils/colors'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MetricPicker from '@/components/dashboards/pickers/MetricPicker'
import InstancePicker from '@/components/dashboards/pickers/InstancePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

function numberOrUndefined(v: string): number | undefined {
  return v === '' || Number.isNaN(Number(v)) ? undefined : Number(v)
}

export default function StatSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const siteTotal = 'site_id' in config && config.site_id !== undefined
  const device = str(config, 'device_id')
  const metric = str(config, 'metric')
  const direction = str(config, 'direction') || 'above'
  const warn = typeof config.warn === 'number' ? config.warn : undefined
  const crit = typeof config.crit === 'number' ? config.crit : undefined
  // The server refuses a warning level that comes after the critical one.
  const misordered = warn !== undefined && crit !== undefined && (direction === 'above' ? warn > crit : warn < crit)

  return (
    <div className="space-y-3">
      <Field label="Of">
        <select
          className={inputCls}
          value={siteTotal ? 'site' : 'device'}
          onChange={(e) =>
            e.target.value === 'site'
              ? // A site total is of a port metric only, as on the timeseries form.
                set({ site_id: siteId, device_id: undefined, instance: undefined, metric: metric.startsWith('if_') ? metric : undefined })
              : set({ site_id: undefined })
          }
        >
          <option value="device">One device</option>
          <option value="site">A whole site's total</option>
        </select>
      </Field>
      {siteTotal ? (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      ) : (
        <Field label="Device">
          <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => set({ device_id: ids[0], instance: undefined })} />
        </Field>
      )}
      <Field label="Metric">
        <MetricPicker single deviceId={device || undefined} portOnly={siteTotal} value={metric ? [metric] : []} onChange={(m) => set({ metric: m[0], instance: undefined })} />
      </Field>
      {!siteTotal && (
        <InstancePicker single deviceId={device || undefined} metric={metric || undefined} value={str(config, 'instance') ? [str(config, 'instance')] : []} onChange={(v) => set({ instance: v[0] })} />
      )}
      <Field label="Show">
        <select className={inputCls} value={str(config, 'mode') || 'latest'} onChange={(e) => set({ mode: e.target.value })}>
          <option value="latest">The latest value</option>
          <option value="average">The average over the range</option>
        </select>
      </Field>
      <div className="grid grid-cols-3 gap-2">
        <Field label="Warning at">
          <input className={inputCls} type="number" value={config.warn === undefined ? '' : String(config.warn)} onChange={(e) => set({ warn: numberOrUndefined(e.target.value) })} />
        </Field>
        <Field label="Critical at">
          <input className={inputCls} type="number" value={config.crit === undefined ? '' : String(config.crit)} onChange={(e) => set({ crit: numberOrUndefined(e.target.value) })} />
        </Field>
        <Field label="When">
          <select className={inputCls} value={direction} onChange={(e) => set({ direction: e.target.value })}>
            <option value="above">Above</option>
            <option value="below">Below</option>
          </select>
        </Field>
      </div>
      {misordered && (
        <p className={`text-xs ${colors.warning.text}`}>
          {direction === 'above' ? 'The warning level must not be higher than the critical level.' : 'The warning level must not be lower than the critical level.'}
        </p>
      )}
      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input type="checkbox" checked={config.sparkline === true} onChange={(e) => set({ sparkline: e.target.checked })} /> Show a sparkline
      </label>
      <Field label="Time range">
        <RangeSelect value={str(config, 'range')} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}

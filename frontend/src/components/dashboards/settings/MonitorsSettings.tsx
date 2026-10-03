import { useAuthContext } from '@/context/AuthContext'
import { inputCls, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import MonitorPicker from '@/components/dashboards/pickers/MonitorPicker'
import AgentPicker from '@/components/dashboards/pickers/AgentPicker'

export default function MonitorsSettings({ config, onChange }: SettingsProps) {
  const { currentUser } = useAuthContext()
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const style = str(config, 'style') || 'list'
  return (
    <div className="space-y-3">
      <Field label="Monitors" hint="Up to 50.">
        <MonitorPicker value={strs(config, 'monitors')} onChange={(ids) => set({ monitors: ids })} />
      </Field>
      {currentUser?.is_admin && (
        <Field label="Servers" hint="Server agents are visible to admins only.">
          <AgentPicker value={strs(config, 'agents')} onChange={(ids) => set({ agents: ids })} />
        </Field>
      )}
      <Field label="Show as">
        <select className={inputCls} value={style} onChange={(e) => set({ style: e.target.value })}>
          <option value="list">A status list</option>
          <option value="bars">Uptime bars</option>
        </select>
      </Field>
      {style === 'bars' && (
        <Field label="Bars cover">
          <select className={inputCls} value={str(config, 'window') || '24h'} onChange={(e) => set({ window: e.target.value })}>
            <option value="24h">The last 24 hours (one bar an hour)</option>
            <option value="90d">The last 90 days (one bar a day)</option>
          </select>
        </Field>
      )}
    </div>
  )
}

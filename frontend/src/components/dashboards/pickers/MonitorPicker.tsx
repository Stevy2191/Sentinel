import { useMonitors } from '@/hooks/useMonitors'

export default function MonitorPicker({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { monitors, loading } = useMonitors({ page: 1, limit: 500 })
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {monitors.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No monitors.'}</p>}
      {monitors.map((m) => (
        <label key={m.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(m.id)} onChange={() => toggle(m.id)} /> {m.name}
        </label>
      ))}
    </div>
  )
}

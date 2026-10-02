import { useAgents } from '@/hooks/useAgents'

/** Server agents. Admin-only, like the agents themselves; members never see
 *  this picker. */
export default function AgentPicker({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { agents, loading } = useAgents(0)
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {agents.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No servers.'}</p>}
      {agents.map((a) => (
        <label key={a.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(a.id)} onChange={() => toggle(a.id)} /> {a.name}
        </label>
      ))}
    </div>
  )
}

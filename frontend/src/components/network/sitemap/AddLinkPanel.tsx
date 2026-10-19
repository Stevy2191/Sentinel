import { X } from 'lucide-react'
import { usePortChoices } from '@/hooks/usePorts'
import type { MapEndInput } from '@/hooks/useSiteMap'

interface Props {
  /** Changing a hand-drawn link's ports, rather than drawing a new one. */
  changing: boolean
  a: MapEndInput | null
  b: MapEndInput | null
  names: Map<string, string>
  busy: boolean
  error: string | null
  onChange: (a: MapEndInput | null, b: MapEndInput | null) => void
  onSave: () => void
  onCancel: () => void
}

function PortPick({ end, onPick }: { end: MapEndInput; onPick: (ix: number | null) => void }) {
  const { ports } = usePortChoices(end.device_id)
  return (
    <select
      className="rd-select w-full"
      aria-label="Port"
      value={end.if_index ?? ''}
      onChange={(e) => onPick(e.target.value === '' ? null : Number(e.target.value))}
    >
      <option value="">Port not known</option>
      {(ports ?? [])
        .filter((p) => p.physical)
        .map((p) => (
          <option key={p.if_index} value={p.if_index}>
            {p.label}
            {p.name && p.name !== p.label ? ` (${p.name})` : ''}
          </option>
        ))}
    </select>
  )
}

/** Drawing a link: click a device (or a port on its drawing), then the other
 *  one; each end's port can also be picked from a list. */
export default function AddLinkPanel(p: Props) {
  const step = !p.a ? 'a' : !p.b ? 'b' : 'done'
  return (
    <div className="space-y-3" aria-label="Add link">
      <div className="flex items-center justify-between">
        <h3 className="text-[15px] font-medium text-white">{p.changing ? 'Change ports' : 'Add link'}</h3>
        <button type="button" className="rounded p-1 text-slate-400 hover:text-white" onClick={p.onCancel} aria-label="Cancel">
          <X className="h-4 w-4" />
        </button>
      </div>
      {(['a', 'b'] as const).map((k) => {
        const end = p[k]
        return (
          <div key={k} className="space-y-1">
            <p className="text-xs text-slate-400">{k === 'a' ? 'One end' : 'Other end'}</p>
            {end ? (
              <>
                <p className="text-sm text-white">{p.names.get(end.device_id) ?? '?'}</p>
                <PortPick end={end} onPick={(ix) => p.onChange(k === 'a' ? { ...end, if_index: ix } : p.a, k === 'b' ? { ...end, if_index: ix } : p.b)} />
              </>
            ) : (
              <p className={`text-sm ${step === k ? 'text-sky-300' : 'text-slate-500'}`}>
                {step === k ? 'Click a switch on the map, or a port on its drawing' : 'Then the other one'}
              </p>
            )}
          </div>
        )
      })}
      {p.error && <p className="text-sm text-red-400">{p.error}</p>}
      <div className="flex gap-2">
        <button type="button" className="btn-primary" disabled={step !== 'done' || p.busy} onClick={p.onSave}>
          {p.changing ? 'Save' : 'Add link'}
        </button>
        <button type="button" className="btn-secondary" onClick={p.onCancel}>
          Cancel
        </button>
      </div>
    </div>
  )
}

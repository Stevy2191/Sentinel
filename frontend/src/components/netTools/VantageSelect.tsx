import { useId } from 'react'
import type { VantageView } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { vantageKey, vantageReasonText } from '@/utils/netTools'

interface Props {
  vantages: VantageView[]
  /** A vantageKey: "sentinel" or "agent:<agent_id>". */
  value: string
  onChange: (key: string) => void
  disabled?: boolean
}

/** "Run from": the Sentinel server and every agent. Agents that cannot run
 *  tools now stay listed, disabled, with the reason beside the name. */
export default function VantageSelect({ vantages, value, onChange, disabled }: Props) {
  const id = useId()
  const selected = vantages.find((v) => vantageKey(v) === value)

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-white">
        Run from
      </label>
      <select id={id} className="w-full" value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {!selected && <option value={value}>{value === 'sentinel' ? 'Sentinel server' : 'Unknown agent'}</option>}
        {vantages.map((v) => (
          <option key={vantageKey(v)} value={vantageKey(v)} disabled={!v.ready}>
            {v.ready ? v.name : `${v.name} — ${vantageReasonText(v.reason ?? 'offline')}`}
          </option>
        ))}
      </select>
      {selected && !selected.ready && (
        <p className={`mt-1 text-xs ${colors.warning.text}`}>
          {selected.name} can&apos;t run tools now: {vantageReasonText(selected.reason ?? 'offline')}
        </p>
      )}
      {!selected && vantages.length > 0 && (
        <p className={`mt-1 text-xs ${colors.warning.text}`}>That agent is not available. Choose another.</p>
      )}
    </div>
  )
}

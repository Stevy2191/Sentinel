import type { HealthRow } from '@/hooks/useDeviceHealth'
import { CHIP_TONE, chipTone, rowValueText } from '@/utils/health'

interface Props {
  row: HealthRow
  units: string
  open?: boolean
  /** Without it a numeric row is a plain tile (dashboards); with it, a
   *  button that opens the row's history chart (the device page). */
  onToggle?: () => void
}

/** One row's tile: a status row shows a coloured chip; a numeric row shows
 *  its value. A red ring marks a row an incident is open for; a row with an
 *  open incident but no live sample says "No reading". */
export default function HealthRowTile({ row, units, open = false, onToggle }: Props) {
  const ring = row.problem ? 'ring-2 ring-red-500/60' : ''
  if (row.no_reading) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <p className="mt-1 text-sm text-red-300">No reading</p>
      </div>
    )
  }
  if (row.state !== undefined) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <span className={`mt-1 inline-flex rounded-full border px-2 py-0.5 text-xs font-medium ${CHIP_TONE[chipTone(row)]}`}>{row.state}</span>
      </div>
    )
  }
  const body = (
    <>
      <p className="truncate text-xs text-slate-400">{row.label}</p>
      <p className="mt-1 text-sm tabular-nums text-white">{rowValueText(row, units)}</p>
    </>
  )
  if (!onToggle) return <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>{body}</div>
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 text-left transition hover:bg-slate-800/60 ${ring}`}
    >
      {body}
    </button>
  )
}

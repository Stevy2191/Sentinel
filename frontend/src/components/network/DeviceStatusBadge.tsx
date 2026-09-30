import type { DeviceStatus } from '@/hooks/useDevices'

const STYLE: Record<DeviceStatus, string> = {
  up: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-400',
  down: 'border-red-500/30 bg-red-500/15 text-red-400',
  pending: 'border-slate-500/30 bg-slate-500/15 text-slate-300',
  paused: 'border-slate-500/30 bg-slate-500/10 text-slate-400',
  error: 'border-amber-500/30 bg-amber-500/15 text-amber-400',
}
const LABEL: Record<DeviceStatus, string> = { up: 'Up', down: 'Down', pending: 'Pending', paused: 'Paused', error: 'Error' }

export default function DeviceStatusBadge({ status, detail }: { status: DeviceStatus; detail?: string }) {
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${STYLE[status]}`}
      title={detail || undefined}
    >
      {LABEL[status]}
    </span>
  )
}

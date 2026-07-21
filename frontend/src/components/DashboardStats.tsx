import { Activity, CheckCircle2, XCircle, Gauge } from 'lucide-react'
import { formatResponseTime } from '@/utils/formatters'

interface Props {
  total: number
  online: number
  offline: number
  avgResponseMs: number
}

function Stat({
  label,
  value,
  icon: Icon,
  tone,
}: {
  label: string
  value: string | number
  icon: typeof Activity
  tone: string
}) {
  return (
    <div className="card flex items-center gap-3 p-3">
      <div className={`rounded-md p-2 ${tone}`}>
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <div className="text-lg font-bold leading-tight">{value}</div>
        <div className="truncate text-xs text-neutral-500 dark:text-neutral-400">{label}</div>
      </div>
    </div>
  )
}

/** Compact, always-visible top stats row. */
export default function DashboardStats({ total, online, offline, avgResponseMs }: Props) {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Stat label="Total Monitors" value={total} icon={Activity} tone="bg-info-100 text-info-600 dark:bg-info-900/40 dark:text-info-400" />
      <Stat label="Online" value={online} icon={CheckCircle2} tone="bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-400" />
      <Stat label="Offline" value={offline} icon={XCircle} tone="bg-error-100 text-error-600 dark:bg-error-900/40 dark:text-error-400" />
      <Stat label="Avg Response" value={formatResponseTime(avgResponseMs)} icon={Gauge} tone="bg-warning-100 text-warning-600 dark:bg-warning-900/40 dark:text-warning-400" />
    </div>
  )
}

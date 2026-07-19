import { CheckCircle2, XCircle, Clock, X } from 'lucide-react'
import type { Check } from '@/types'
import { formatResponseTime } from '@/utils/formatters'

const config = {
  success: { icon: CheckCircle2, tone: 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-200', label: 'Success' },
  failed: { icon: XCircle, tone: 'border-red-300 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-900/30 dark:text-red-200', label: 'Failed' },
  timeout: { icon: Clock, tone: 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-200', label: 'Timeout' },
} as const

export default function TestResult({ check, onClose }: { check: Check; onClose?: () => void }) {
  const c = config[check.status] ?? config.failed
  const Icon = c.icon
  return (
    <div className={`flex items-start gap-3 rounded-lg border p-4 ${c.tone}`}>
      <Icon className="mt-0.5 h-5 w-5 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="font-semibold">Test {c.label}</div>
        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm">
          <span>Response: {formatResponseTime(check.response_time_ms)}</span>
          {check.status_code > 0 && <span>Status code: {check.status_code}</span>}
        </div>
        {check.error_message && (
          <div className="mt-1 break-words text-sm opacity-90">{check.error_message}</div>
        )}
      </div>
      {onClose && (
        <button onClick={onClose} className="shrink-0 opacity-70 hover:opacity-100" aria-label="Dismiss">
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  )
}

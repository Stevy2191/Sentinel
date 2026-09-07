import { CheckCircle2, XCircle, Clock, X } from 'lucide-react'
import type { Check } from '@/types'
import { formatResponseTime } from '@/utils/formatters'

const config = {
  success: { icon: CheckCircle2, tone: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300', label: 'Success' },
  failed: { icon: XCircle, tone: 'border-red-500/30 bg-red-500/10 text-red-300', label: 'Failed' },
  timeout: { icon: Clock, tone: 'border-amber-500/30 bg-amber-500/10 text-amber-300', label: 'Timeout' },
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

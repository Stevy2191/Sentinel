import { useEffect, useRef, useState } from 'react'
import { Trash2, Loader2 } from 'lucide-react'
import type { StatusPageMonitorView } from '@/types'
import { getStatusBgColor } from '@/utils/formatters'

interface Props {
  monitors: StatusPageMonitorView[]
  onRemove: (monitorId: string) => void
  // When provided, the position column becomes an editable (debounced) input.
  onUpdatePosition?: (monitorId: string, position: number) => Promise<void> | void
}

const DEBOUNCE_MS = 500

export default function MonitorList({ monitors, onRemove, onUpdatePosition }: Props) {
  const editable = Boolean(onUpdatePosition)
  const [values, setValues] = useState<Record<string, number>>({})
  const [savingId, setSavingId] = useState<string | null>(null)
  const timers = useRef<Record<string, number>>({})

  // Sync local input values whenever the monitor list changes (e.g. after a
  // save + refetch reorders rows).
  useEffect(() => {
    setValues(Object.fromEntries(monitors.map((m) => [m.id, m.position ?? 0])))
  }, [monitors])

  const handleChange = (monitorId: string, raw: string) => {
    const n = Number(raw)
    setValues((v) => ({ ...v, [monitorId]: n }))
    if (!onUpdatePosition) return
    window.clearTimeout(timers.current[monitorId])
    timers.current[monitorId] = window.setTimeout(async () => {
      if (!Number.isInteger(n) || n < 1) return
      setSavingId(monitorId)
      try {
        await onUpdatePosition(monitorId, n)
      } finally {
        setSavingId(null)
      }
    }, DEBOUNCE_MS)
  }

  if (monitors.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-neutral-500">
        No monitors on this page yet.
      </div>
    )
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-neutral-200 text-neutral-500 dark:border-neutral-800 dark:text-neutral-400">
          <tr>
            <th className="px-4 py-3 font-medium">Monitor</th>
            <th className="px-4 py-3 font-medium">Group</th>
            <th className="px-4 py-3 font-medium">Status</th>
            <th className="px-4 py-3 font-medium">Position</th>
            <th className="px-4 py-3 text-right font-medium">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-neutral-100 dark:divide-neutral-800">
          {monitors.map((m, i) => (
            <tr key={m.id} className={i % 2 ? 'bg-neutral-50/50 dark:bg-neutral-800/30' : ''}>
              <td className="px-4 py-3 font-medium">{m.name}</td>
              <td className="px-4 py-3 text-neutral-500">{m.group_name || '—'}</td>
              <td className="px-4 py-3">
                <span className={`rounded-md px-2 py-1 text-xs font-medium ${getStatusBgColor(m.status)}`}>
                  {m.status}
                </span>
              </td>
              <td className="px-4 py-3">
                {editable ? (
                  <div className="flex items-center gap-2">
                    <input
                      type="number"
                      min={1}
                      value={values[m.id] ?? m.position ?? 0}
                      onChange={(e) => handleChange(m.id, e.target.value)}
                      className="w-16 rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-800"
                    />
                    {savingId === m.id && (
                      <Loader2 className="h-4 w-4 animate-spin text-neutral-400" />
                    )}
                  </div>
                ) : (
                  <span className="text-neutral-500">{m.position ?? '—'}</span>
                )}
              </td>
              <td className="px-4 py-3">
                <div className="flex justify-end">
                  <button
                    className="btn-secondary !px-2 !py-1 text-error-600"
                    title="Remove from page"
                    onClick={() => onRemove(m.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

import { Trash2 } from 'lucide-react'
import type { StatusPageMonitorView } from '@/types'
import { getStatusBgColor } from '@/utils/formatters'

interface Props {
  monitors: StatusPageMonitorView[]
  onRemove: (monitorId: string) => void
}

export default function MonitorList({ monitors, onRemove }: Props) {
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
              <td className="px-4 py-3 text-neutral-500">{m.position ?? '—'}</td>
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

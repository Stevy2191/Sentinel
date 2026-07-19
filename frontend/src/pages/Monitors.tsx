import { useState } from 'react'
import { Plus, Pencil, Trash2, Play } from 'lucide-react'
import { useDeleteMonitor, useMonitors, useTestMonitor } from '@/hooks/useMonitors'
import { formatMs, formatRelative, statusBadge } from '@/utils/format'

const PAGE_SIZE = 10

export default function Monitors() {
  const { monitors, loading, error, refetch } = useMonitors()
  const { delete: deleteMonitor } = useDeleteMonitor()
  const { test } = useTestMonitor()
  const [page, setPage] = useState(1)

  const totalPages = Math.max(1, Math.ceil(monitors.length / PAGE_SIZE))
  const pageItems = monitors.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  async function handleDelete(id: string) {
    if (!window.confirm('Delete this monitor?')) return
    await deleteMonitor(id)
    await refetch()
  }

  async function handleTest(id: string) {
    await test(id)
    await refetch()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Monitors</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Manage your uptime checks
          </p>
        </div>
        <button className="btn-primary" disabled>
          <Plus className="h-4 w-4" />
          New Monitor
        </button>
      </div>

      {error && (
        <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
          {error.message}
        </div>
      )}

      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-200 text-neutral-500 dark:border-neutral-800 dark:text-neutral-400">
              <tr>
                <th className="px-4 py-3 font-medium">Name</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Response</th>
                <th className="px-4 py-3 font-medium">Last Check</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-100 dark:divide-neutral-800">
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-neutral-500">
                    Loading…
                  </td>
                </tr>
              ) : pageItems.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-neutral-500">
                    No monitors.
                  </td>
                </tr>
              ) : (
                pageItems.map((m) => (
                  <tr key={m.id}>
                    <td className="px-4 py-3">
                      <div className="font-medium">{m.name}</div>
                      <div className="text-xs text-neutral-500">{m.url}</div>
                    </td>
                    <td className="px-4 py-3 uppercase text-neutral-500">{m.type}</td>
                    <td className="px-4 py-3">
                      <span
                        className={`rounded-md px-2 py-1 text-xs font-medium ${statusBadge(
                          m.current_status
                        )}`}
                      >
                        {m.current_status}
                      </span>
                    </td>
                    <td className="px-4 py-3">{formatMs(m.last_response_time_ms)}</td>
                    <td className="px-4 py-3 text-neutral-500">
                      {formatRelative(m.last_check_at)}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <button
                          onClick={() => handleTest(m.id)}
                          className="btn-secondary !px-2 !py-1"
                          title="Test now"
                        >
                          <Play className="h-4 w-4" />
                        </button>
                        <button className="btn-secondary !px-2 !py-1" title="Edit" disabled>
                          <Pencil className="h-4 w-4" />
                        </button>
                        <button
                          onClick={() => handleDelete(m.id)}
                          className="btn-secondary !px-2 !py-1 text-error-600"
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="flex items-center justify-between text-sm text-neutral-500">
        <span>
          Page {page} of {totalPages}
        </span>
        <div className="flex gap-2">
          <button
            className="btn-secondary"
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
          >
            Previous
          </button>
          <button
            className="btn-secondary"
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </button>
        </div>
      </div>
    </div>
  )
}

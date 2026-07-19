import { useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, Incident } from '@/types'
import { formatDatetime, formatDuration } from '@/utils/formatters'

const PAGE_SIZE = 5

interface IncidentsResponse {
  incidents: Incident[]
  count: number
}

export default function IncidentList({ monitorId }: { monitorId: string }) {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)
  const [page, setPage] = useState(1)

  useEffect(() => {
    let active = true
    setLoading(true)
    api
      .get<ApiResponse<IncidentsResponse>>(`/monitors/${monitorId}/incidents`)
      .then(({ data }) => active && setIncidents(data.data.incidents ?? []))
      .catch((err) => active && setError(err as ApiError))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [monitorId])

  if (loading) return <div className="text-sm text-neutral-500">Loading incidents…</div>
  if (error) return <div className="text-sm text-error-600">{error.message}</div>
  if (incidents.length === 0)
    return <div className="text-sm text-neutral-500">No incidents in this period.</div>

  const totalPages = Math.max(1, Math.ceil(incidents.length / PAGE_SIZE))
  const pageItems = incidents.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div className="space-y-3">
      <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
        {pageItems.map((inc) => (
          <div key={inc.id} className="flex items-center justify-between gap-4 py-2 text-sm">
            <div className="min-w-0">
              <div className="font-medium">{formatDatetime(inc.start_time)}</div>
              <div className="text-xs text-neutral-500">
                {inc.end_time ? `Resolved ${formatDatetime(inc.end_time)}` : 'Ongoing'}
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-3">
              <span className="text-neutral-500">{formatDuration(inc.duration_seconds)}</span>
              {inc.severity && (
                <span className="rounded-md bg-neutral-100 px-2 py-0.5 text-xs uppercase text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
                  {inc.severity}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
      {totalPages > 1 && (
        <div className="flex items-center justify-between text-xs text-neutral-500">
          <span>
            Page {page} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button className="btn-secondary !py-1" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              Prev
            </button>
            <button
              className="btn-secondary !py-1"
              disabled={page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

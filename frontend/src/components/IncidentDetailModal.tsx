import { useEffect } from 'react'
import { X, Loader2, ExternalLink } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useIncidentDetail, formatDuration, type Incident } from '@/hooks/useIncidents'

const STATUS_STYLE: Record<Incident['status'], string> = {
  ongoing: 'border-red-500/30 bg-red-500/20 text-red-400',
  resolved: 'border-emerald-500/30 bg-emerald-500/20 text-emerald-400',
}

const TYPE_LABEL: Record<Incident['incident_type'], string> = {
  down: 'Down',
  timeout: 'Timeout',
  error: 'Error',
}

/** A check's colour in the timeline strip. */
const CHECK_TONE: Record<string, string> = {
  success: 'bg-emerald-500',
  failed: 'bg-red-500',
  timeout: 'bg-yellow-500',
}

function when(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString() : '—'
}

interface Props {
  incidentId: string | null
  onClose: () => void
}

/**
 * IncidentDetailModal shows one incident and the checks recorded while it was
 * open, so "it was down for two hours" can be read as the sequence of failures
 * that actually happened.
 */
export default function IncidentDetailModal({ incidentId, onClose }: Props) {
  const { detail, loading, error } = useIncidentDetail(incidentId)

  useEffect(() => {
    if (!incidentId) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [incidentId, onClose])

  if (!incidentId) return null

  const inc = detail?.incident
  const failed = detail?.checks.filter((c) => c.status !== 'success').length ?? 0

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="incident-detail-title"
        className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        <div className="flex shrink-0 items-start justify-between border-b border-white/10 p-6">
          <div className="min-w-0">
            <h2 id="incident-detail-title" className="truncate text-2xl font-light text-white">
              {inc?.monitor_name ?? 'Incident'}
            </h2>
            {inc && (
              <p className="mt-1 truncate text-sm text-slate-400" title={inc.monitor_url}>
                {inc.monitor_url}
              </p>
            )}
          </div>
          <button
            onClick={onClose}
            className="ml-4 shrink-0 rounded p-1 text-slate-400 transition hover:text-white"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          {loading ? (
            <div className="flex items-center gap-2 text-sm text-slate-400">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading incident…
            </div>
          ) : error || !inc ? (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">
              {error ?? 'Incident not found'}
            </div>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span
                  className={`rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[inc.status]}`}
                >
                  {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'}
                </span>
                <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-xs font-medium text-slate-300">
                  {TYPE_LABEL[inc.incident_type] ?? inc.incident_type}
                </span>
              </div>

              <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
                <div>
                  <dt className="text-xs uppercase tracking-widest text-slate-500">Started</dt>
                  <dd className="mt-0.5 text-sm text-slate-200">{when(inc.start_time)}</dd>
                </div>
                <div>
                  <dt className="text-xs uppercase tracking-widest text-slate-500">Ended</dt>
                  <dd className="mt-0.5 text-sm text-slate-200">
                    {inc.end_time ? when(inc.end_time) : 'Still ongoing'}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs uppercase tracking-widest text-slate-500">Duration</dt>
                  <dd
                    className={`mt-0.5 text-sm font-medium ${inc.status === 'ongoing' ? 'text-red-400' : 'text-slate-200'}`}
                  >
                    {formatDuration(inc.duration_seconds)}
                    {inc.status === 'ongoing' && ' and counting'}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs uppercase tracking-widest text-slate-500">
                    Checks during the incident
                  </dt>
                  <dd className="mt-0.5 text-sm text-slate-200">
                    {detail.total_checks}
                    {detail.total_checks > 0 && (
                      <span className="text-slate-500"> · {failed} failed</span>
                    )}
                  </dd>
                </div>
              </dl>

              {inc.root_cause && (
                <div>
                  <h3 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
                    Reason
                  </h3>
                  <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-sm text-slate-300">
                    {inc.root_cause}
                  </p>
                </div>
              )}

              <div>
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
                  Check timeline
                </h3>
                {detail.checks.length === 0 ? (
                  <p className="text-sm text-slate-500">
                    No checks were recorded while this incident was open.
                  </p>
                ) : (
                  <>
                    <div className="mb-3 flex flex-wrap gap-px">
                      {detail.checks.map((c) => (
                        <div
                          key={c.id}
                          className={`h-4 w-1.5 rounded-sm ${CHECK_TONE[c.status] ?? 'bg-slate-600'}`}
                          title={`${new Date(c.timestamp).toLocaleString()} · ${c.status}${
                            c.error_message ? ` · ${c.error_message}` : ''
                          }`}
                        />
                      ))}
                    </div>
                    {/* The strip gives the shape; the last few give the detail
                        without turning the modal into a log viewer. */}
                    <ul className="space-y-1 text-xs">
                      {detail.checks.slice(-5).map((c) => (
                        <li key={c.id} className="flex items-center gap-2 text-slate-400">
                          <span
                            className={`h-1.5 w-1.5 shrink-0 rounded-full ${CHECK_TONE[c.status] ?? 'bg-slate-600'}`}
                          />
                          <span className="shrink-0 tabular-nums">
                            {new Date(c.timestamp).toLocaleTimeString()}
                          </span>
                          <span className="truncate">
                            {c.error_message || `${c.status} · ${c.response_time_ms}ms`}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </>
                )}
              </div>

              {(inc.notes || inc.resolution_notes) && (
                <div>
                  <h3 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
                    Notes
                  </h3>
                  {inc.notes && <p className="text-sm text-slate-300">{inc.notes}</p>}
                  {inc.resolution_notes && (
                    <p className="mt-2 text-sm text-slate-400">
                      <span className="text-slate-500">Resolution: </span>
                      {inc.resolution_notes}
                    </p>
                  )}
                </div>
              )}
            </>
          )}
        </div>

        <div className="flex shrink-0 items-center justify-between gap-3 border-t border-white/10 p-6">
          {inc ? (
            <Link
              to={`/monitors/${inc.monitor_id}`}
              className="inline-flex items-center gap-1.5 text-sm text-emerald-400 underline-offset-2 hover:underline"
              onClick={onClose}
            >
              View monitor <ExternalLink className="h-3.5 w-3.5" />
            </Link>
          ) : (
            <span />
          )}
          <button className="btn-secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

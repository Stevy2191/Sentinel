import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, ExternalLink, Check, AlertTriangle } from 'lucide-react'
import IncidentThread from '@/components/IncidentThread'
import { useAppConfig } from '@/context/AppConfigContext'
import {
  useIncidentDetail,
  useUpdateIncident,
  formatDuration,
  type Incident,
} from '@/hooks/useIncidents'

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

/**
 * One incident in full: when it happened, what the checks recorded, who was
 * alerted, and the discussion around it.
 *
 * A page rather than a dialog because an incident is read, not glanced at — it
 * is linked to from a report or a chat message, it is scrolled through while
 * writing notes, and a modal that cannot be linked to or left open beside
 * something else gets in the way of all three.
 */
export default function IncidentDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { appName } = useAppConfig()

  const { detail, loading, error, reload } = useIncidentDetail(id)
  const { save, saving } = useUpdateIncident()

  const [notes, setNotes] = useState('')
  const [resolution, setResolution] = useState('')
  const [saveError, setSaveError] = useState<string | null>(null)
  const [justSaved, setJustSaved] = useState(false)

  const inc = detail?.incident

  // Seeded from whatever loaded, and keyed on the incident's id rather than the
  // object, so a background reload does not discard something half-typed.
  const loadedId = inc?.id
  useEffect(() => {
    if (!detail) return
    setNotes(detail.incident.notes ?? '')
    setResolution(detail.incident.resolution_notes ?? '')
    setSaveError(null)
    setJustSaved(false)
  }, [loadedId]) // eslint-disable-line react-hooks/exhaustive-deps

  // The tab is how someone finds this page again among several open incidents.
  useEffect(() => {
    if (!inc) return
    const previous = document.title
    document.title = `Incident · ${inc.monitor_name} · ${appName}`
    return () => {
      document.title = previous
    }
  }, [inc, appName])

  const dirty =
    !!detail &&
    (notes !== (detail.incident.notes ?? '') ||
      resolution !== (detail.incident.resolution_notes ?? ''))

  // Warn before a reload or tab close swallows unsaved notes. In a dialog this
  // was handled by refusing to close; a page can be navigated away from by the
  // browser itself.
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const persist = async () => {
    if (!detail || !dirty) return
    setSaveError(null)
    try {
      await save(detail.incident.id, { notes, resolution_notes: resolution })
      setJustSaved(true)
      reload()
    } catch (err) {
      const e = err as { status?: number; message?: string }
      setSaveError(
        e.status === 403
          ? 'You do not have permission to edit this monitor'
          : (e.message ?? 'Could not save the notes'),
      )
    }
  }

  // Where "back" goes. A caller can say where it came from; otherwise the
  // monitor is the sensible parent, since that is what the incident belongs to.
  const from = (location.state as { from?: string } | null)?.from
  const backTo = from ?? (inc ? `/monitors/${inc.monitor_id}` : '/incidents')
  const backLabel = from === '/incidents' ? 'Back to Incidents' : 'Back to Monitor'

  if (loading && !detail) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading incident…
      </div>
    )
  }

  if (error || !inc || !detail) {
    return (
      <div className="space-y-4">
        <button
          className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white"
          onClick={() => navigate('/incidents')}
        >
          <ArrowLeft className="h-4 w-4" /> Back to Incidents
        </button>
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-6 text-sm text-red-400">
          {error ?? 'That incident could not be found.'}
        </div>
      </div>
    )
  }

  const failed = detail.checks.filter((c) => c.status !== 'success').length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link
            to={backTo}
            className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" /> {backLabel}
          </Link>
          <nav aria-label="Breadcrumb" className="mt-1 text-xs text-slate-600">
            <Link to={`/monitors/${inc.monitor_id}`} className="transition hover:text-slate-400">
              {inc.monitor_name}
            </Link>
            <span className="px-1">›</span>
            <span className="text-slate-500">Incident</span>
          </nav>
        </div>
        <Link
          to={`/monitors/${inc.monitor_id}`}
          className="inline-flex items-center gap-1.5 text-sm text-emerald-400 underline-offset-2 hover:underline"
        >
          View monitor <ExternalLink className="h-3.5 w-3.5" />
        </Link>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(300px,1fr)]">
        <div className="min-w-0 space-y-6">
          <div>
            <h1 className="truncate text-2xl font-light text-white">{inc.monitor_name}</h1>
            <p className="truncate text-sm text-slate-500" title={inc.monitor_url}>
              {inc.monitor_url}
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span
                className={`rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[inc.status]}`}
              >
                {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'}
              </span>
              <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-xs font-medium text-slate-300">
                {TYPE_LABEL[inc.incident_type] ?? inc.incident_type}
              </span>
              {inc.severity && (
                <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-xs uppercase text-slate-300">
                  {inc.severity}
                </span>
              )}
            </div>
          </div>

          <dl className="grid gap-x-6 gap-y-3 rounded-lg border border-white/10 bg-slate-800/40 p-5 sm:grid-cols-2">
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
                className={`mt-0.5 text-sm font-medium ${
                  inc.status === 'ongoing' ? 'text-red-400' : 'text-slate-200'
                }`}
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
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
                Reason
              </h2>
              <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-sm text-slate-300">
                {inc.root_cause}
              </p>
            </section>
          )}

          {/* Who was actually told. An incident record that cannot answer "did
              anyone get paged" is missing what people check first after an
              outage nobody noticed. */}
          <section>
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Alerts sent
            </h2>
            {!detail.notifications || detail.notifications.length === 0 ? (
              <p className="text-sm text-slate-500">No alerts were sent for this incident.</p>
            ) : (
              <ul className="space-y-1 text-sm">
                {detail.notifications.map((n) => (
                  <li
                    key={n.id}
                    className="flex flex-wrap items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 px-3 py-2"
                  >
                    <span
                      className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                        n.status === 'sent' ? 'bg-emerald-500' : 'bg-red-500'
                      }`}
                    />
                    <span className="font-medium capitalize text-slate-200">{n.channel}</span>
                    <span className="text-xs text-slate-500">
                      {new Date(n.sent_at ?? n.created_at).toLocaleString()}
                    </span>
                    <span className="text-xs text-slate-400">{n.status}</span>
                    {n.error_message && (
                      <span className="w-full break-words text-xs text-red-400">
                        {n.error_message}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section>
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Check timeline
            </h2>
            {detail.checks.length === 0 ? (
              <p className="text-sm text-slate-500">
                No checks were recorded while this incident was open.
              </p>
            ) : (
              <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
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
                {/* The page has room for more of the sequence than the dialog
                    did, without becoming a log viewer. */}
                <ul className="space-y-1 text-xs">
                  {detail.checks.slice(-15).map((c) => (
                    <li key={c.id} className="flex items-center gap-2 text-slate-400">
                      <span
                        className={`h-1.5 w-1.5 shrink-0 rounded-full ${CHECK_TONE[c.status] ?? 'bg-slate-600'}`}
                      />
                      <span className="shrink-0 tabular-nums">
                        {new Date(c.timestamp).toLocaleTimeString()}
                      </span>
                      <span className="truncate">
                        {c.error_message ||
                          `${c.status}${c.response_time_ms ? ` · ${c.response_time_ms}ms` : ''}`}
                      </span>
                    </li>
                  ))}
                </ul>
                {detail.checks.length > 15 && (
                  <p className="mt-2 text-xs text-slate-600">
                    Showing the last 15 of {detail.checks.length}.
                  </p>
                )}
              </div>
            )}
          </section>

          <IncidentThread
            incidentId={inc.id}
            comments={detail.comments ?? []}
            onChanged={reload}
          />
        </div>

        <aside className="xl:sticky xl:top-6 xl:self-start">
          <div className="rounded-lg border border-white/10 bg-slate-800/40 p-5">
            <h2 className="mb-3 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Summary
            </h2>

            <label htmlFor="incident-notes" className="mb-1 block text-sm font-medium text-white">
              What happened
            </label>
            <textarea
              id="incident-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              rows={4}
              placeholder="Context worth keeping — what you found, what you ruled out…"
              className="w-full resize-none rounded-lg border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-white/30 focus:outline-none"
            />

            <label
              htmlFor="incident-resolution"
              className="mb-1 mt-4 block text-sm font-medium text-white"
            >
              How it was resolved
            </label>
            <textarea
              id="incident-resolution"
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
              rows={3}
              placeholder={
                inc.status === 'ongoing' ? 'Fill this in once it is fixed' : 'What actually fixed it'
              }
              className="w-full resize-none rounded-lg border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-white/30 focus:outline-none"
            />
            <p className="mt-1 text-xs text-slate-500">
              Both appear in reports. The reason above is what the check itself returned and is not
              editable.
            </p>

            {saveError && (
              <p className="mt-2 rounded-lg border border-red-500/30 bg-red-500/10 p-2 text-xs text-red-400">
                {saveError}
              </p>
            )}

            <div className="mt-4 flex items-center justify-between gap-2">
              {dirty ? (
                <span className="inline-flex items-center gap-1 text-xs text-amber-300">
                  <AlertTriangle className="h-3.5 w-3.5" /> Unsaved
                </span>
              ) : justSaved ? (
                <span className="inline-flex items-center gap-1 text-xs text-emerald-400">
                  <Check className="h-3.5 w-3.5" /> Saved
                </span>
              ) : (
                <span />
              )}
              <button
                className="btn-primary !py-1.5"
                disabled={!dirty || saving}
                onClick={() => void persist()}
              >
                {saving ? 'Saving…' : 'Save summary'}
              </button>
            </div>
          </div>
        </aside>
      </div>
    </div>
  )
}

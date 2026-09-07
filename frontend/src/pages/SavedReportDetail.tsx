import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  Clock,
  Download,
  History,
  Link2,
  Loader2,
  Pencil,
  Play,
  Plus,
  Trash2,
} from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import ScheduleManager from '@/components/ScheduleManager'
import EditScheduleModal from '@/components/EditScheduleModal'
import {
  downloadReportPDF,
  formatFileSize,
  useReportJobs,
  useReportSchedules,
  useSavedReports,
  useShareLinks,
} from '@/hooks/useReportBuilder'

/**
 * SavedReportDetail shows one report's generation history and its delivery
 * schedules.
 */
export default function SavedReportDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { toasts, push } = useToasts()

  const { reports, loading: reportsLoading, listReports, shareReport } = useSavedReports()
  const { links, listLinks, revokeLink } = useShareLinks(id)
  const { jobs, listJobs } = useReportJobs(id)
  const {
    schedules,
    listSchedules,
    deleteSchedule,
    runScheduleNow,
  } = useReportSchedules(id)

  const [showForm, setShowForm] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expiryDays, setExpiryDays] = useState(30)
  const [openJobId, setOpenJobId] = useState<string | null>(null)

  // The list must actually be fetched here; the hook holds per-instance state,
  // so relying on another page having loaded it would leave this one empty.
  useEffect(() => {
    listReports()
  }, [listReports])

  useEffect(() => {
    listSchedules()
  }, [listSchedules])

  useEffect(() => {
    listLinks()
  }, [listLinks])

  useEffect(() => {
    listJobs()
  }, [listJobs])

  const report = useMemo(() => reports.find((r) => r.id === id), [reports, id])
  // Resolved from the live list rather than copied into state, so the modal
  // always opens on current values.
  const editingSchedule = useMemo(
    () => schedules.find((s) => s.id === editingId),
    [schedules, editingId]
  )

  const handleDownload = async (url: string, generatedAt: string) => {
    setBusy(url)
    try {
      const stamp = new Date(generatedAt).toISOString().slice(0, 10)
      await downloadReportPDF(url, `${report?.name ?? 'report'}-${stamp}.pdf`)
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not download the report', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleRun = async (scheduleId: string) => {
    setBusy(scheduleId)
    try {
      const result = await runScheduleNow(scheduleId)
      push(`Report delivered to ${result.recipients} recipient(s)`, 'success')
      await listReports()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Delivery failed', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleCreateLink = async () => {
    if (!id) return
    setBusy('share')
    try {
      const result = await shareReport(id, expiryDays)
      const url = `${window.location.origin}${result.share_link}`
      try {
        await navigator.clipboard.writeText(url)
        push('Share link created and copied to the clipboard', 'success')
      } catch {
        // Clipboard access needs a secure context and can be denied; never
        // lose the link because of it.
        push(`Share link created: ${url}`, 'info')
      }
      await listLinks()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not create a share link', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleRevokeLink = async (shareId: string) => {
    setBusy(shareId)
    try {
      await revokeLink(shareId)
      push('Share link revoked', 'success')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not revoke the link', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleDeleteSchedule = async (scheduleId: string) => {
    setBusy(scheduleId)
    try {
      await deleteSchedule(scheduleId)
      push('Schedule deleted', 'success')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not delete the schedule', 'error')
    } finally {
      setBusy(null)
    }
  }

  if (reportsLoading && !report) {
    return (
      <div className="flex items-center gap-2 p-6" style={{ color: 'var(--vs-text-dim)' }}>
        <Loader2 className="h-4 w-4 animate-spin" /> Loading report…
      </div>
    )
  }

  if (!report) {
    return (
      <div className="rd-card p-8 text-center">
        <p className="font-medium">Report not found</p>
        <p className="mt-1 text-sm" style={{ color: 'var(--vs-text-dim)' }}>
          It may have been deleted, or you may not have access to it.
        </p>
        <button className="rd-btn rd-btn-secondary mt-4" onClick={() => navigate('/reports')}>
          Back to reports
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-5 pb-10">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="vs-title text-2xl">{report.name}</h1>
          <p className="mt-1 text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            {report.template_name} · {report.scope_type} · {report.time_range_days} day window
          </p>
        </div>
        <button className="rd-btn rd-btn-secondary" onClick={() => navigate('/reports')}>
          Back
        </button>
      </div>

      <section className="rd-card p-5">
        <h2 className="vs-eyebrow mb-3">Generated PDFs</h2>
        {report.generations.length === 0 ? (
          <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            Nothing generated yet.
          </p>
        ) : (
          <div className="space-y-2">
            {report.generations.map((gen) => (
              <div
                key={gen.id}
                className="flex items-center justify-between gap-3 rounded-md p-3"
                style={{ background: 'var(--vs-panel-2)' }}
              >
                <div>
                  <p className="text-sm font-medium">
                    {new Date(gen.generated_at).toLocaleString()}
                  </p>
                  {gen.file_size != null && (
                    <p className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                      {formatFileSize(gen.file_size)}
                    </p>
                  )}
                </div>
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => handleDownload(gen.download_url, gen.generated_at)}
                  disabled={busy === gen.download_url}
                >
                  <Download className="h-4 w-4" /> Download
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="rd-card p-5">
        <h2 className="vs-eyebrow mb-3 flex items-center gap-2">
          <History className="h-4 w-4" /> Render history
        </h2>
        {jobs.length === 0 ? (
          <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            No render attempts recorded.
          </p>
        ) : (
          <div className="space-y-2">
            {jobs.map((job) => {
              const failed = job.status === 'failed'
              const pending = job.status === 'queued' || job.status === 'running'
              const color = failed
                ? 'var(--vs-flat)'
                : pending
                  ? 'var(--vs-amber)'
                  : 'var(--vs-ecg)'
              return (
                <div
                  key={job.id}
                  className="rounded-md p-3"
                  style={{ background: 'var(--vs-panel-2)' }}
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      {failed && (
                        <AlertTriangle className="h-4 w-4" style={{ color: 'var(--vs-flat)' }} />
                      )}
                      <span className="text-sm font-medium capitalize" style={{ color }}>
                        {job.status}
                      </span>
                      <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        {new Date(job.created_at).toLocaleString()}
                        {job.attempts > 1 && ` · ${job.attempts} attempts`}
                      </span>
                    </div>
                    {failed && job.error && (
                      // Collapsed by default: the reason is usually long and
                      // only wanted when something has gone wrong.
                      <button
                        className="rd-btn rd-btn-secondary"
                        onClick={() => setOpenJobId(openJobId === job.id ? null : job.id)}
                        aria-expanded={openJobId === job.id}
                        aria-controls={`job-error-${job.id}`}
                      >
                        {openJobId === job.id ? (
                          <ChevronDown className="h-4 w-4" />
                        ) : (
                          <ChevronRight className="h-4 w-4" />
                        )}
                        Details
                      </button>
                    )}
                  </div>
                  {failed && job.error && openJobId === job.id && (
                    <pre
                      id={`job-error-${job.id}`}
                      className="mt-2 overflow-x-auto whitespace-pre-wrap break-words rounded p-2 font-mono text-xs"
                      style={{ background: 'var(--vs-bg)', color: 'var(--vs-text-dim)' }}
                    >
                      {job.error}
                    </pre>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </section>

      <section className="rd-card p-5">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 className="vs-eyebrow flex items-center gap-2">
            <Link2 className="h-4 w-4" /> Public share links
          </h2>
          <div className="flex items-center gap-2">
            <label className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              Expires in
            </label>
            <select
              className="rd-select"
              value={expiryDays}
              onChange={(e) => setExpiryDays(Number(e.target.value))}
              aria-label="Share link lifetime"
            >
              <option value={1}>1 day</option>
              <option value={7}>7 days</option>
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={0}>Never</option>
            </select>
            <button
              className="rd-btn rd-btn-primary"
              onClick={handleCreateLink}
              disabled={busy === 'share'}
            >
              <Plus className="h-4 w-4" /> Create link
            </button>
          </div>
        </div>

        {links.length === 0 ? (
          <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            No public links. Anyone with a link can read this report without signing in.
          </p>
        ) : (
          <div className="space-y-2">
            {links.map((link) => (
              <div
                key={link.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-md p-3"
                style={{ background: 'var(--vs-panel-2)' }}
              >
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs">
                    {`${window.location.origin}${link.share_link}`}
                  </p>
                  <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                    {link.expired ? (
                      <span style={{ color: 'var(--vs-flat)' }}>Expired</span>
                    ) : link.expires_at ? (
                      `Expires ${new Date(link.expires_at).toLocaleString()}`
                    ) : (
                      <span style={{ color: 'var(--vs-amber)' }}>Never expires</span>
                    )}
                    {` · created ${new Date(link.created_at).toLocaleDateString()}`}
                  </p>
                </div>
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => handleRevokeLink(link.id)}
                  disabled={busy === link.id}
                  title="Revoke this link"
                >
                  {busy === link.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Trash2 className="h-4 w-4" style={{ color: 'var(--vs-flat)' }} />
                  )}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="rd-card p-5">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="vs-eyebrow flex items-center gap-2">
            <Clock className="h-4 w-4" /> Delivery schedules
          </h2>
          <button className="rd-btn rd-btn-secondary" onClick={() => setShowForm((v) => !v)}>
            {showForm ? 'Cancel' : 'Add schedule'}
          </button>
        </div>

        {showForm && id && (
          <ScheduleManager
            reportId={id}
            onScheduleCreated={() => {
              setShowForm(false)
              listSchedules()
              push('Schedule created', 'success')
            }}
            onError={(m) => push(m, 'error')}
          />
        )}

        {schedules.length === 0 && !showForm && (
          <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            No schedules. Add one to have this report emailed automatically.
          </p>
        )}

        <div className="space-y-2">
          {schedules.map((s) => (
            <div
              key={s.id}
              className="flex flex-wrap items-start justify-between gap-3 rounded-md p-3"
              style={{ background: 'var(--vs-panel-2)' }}
            >
              <div className="min-w-0">
                <p className="text-sm font-medium capitalize">
                  {s.schedule_type}
                  {s.cron_expression && (
                    <span className="ml-2 font-mono text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                      {s.cron_expression}
                    </span>
                  )}
                  {!s.is_active && (
                    <span className="ml-2 text-xs" style={{ color: 'var(--vs-amber)' }}>
                      paused
                    </span>
                  )}
                </p>
                <p className="mt-1 break-words text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                  {s.email_recipients.join(', ')}
                </p>
                <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                  {s.next_run_at
                    ? `Next run ${new Date(s.next_run_at).toLocaleString()}`
                    : 'Not scheduled'}
                  {s.last_run_at &&
                    ` · last run ${new Date(s.last_run_at).toLocaleString()}`}
                </p>
              </div>

              <div className="flex shrink-0 gap-1">
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => handleRun(s.id)}
                  disabled={busy === s.id}
                  title="Generate and send now"
                >
                  {busy === s.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Play className="h-4 w-4" />
                  )}
                </button>
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => setEditingId(s.id)}
                  disabled={busy === s.id}
                  title="Edit this schedule"
                >
                  <Pencil className="h-4 w-4" />
                </button>
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => handleDeleteSchedule(s.id)}
                  disabled={busy === s.id}
                  title="Delete this schedule"
                >
                  <Trash2 className="h-4 w-4" style={{ color: 'var(--vs-flat)' }} />
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      {editingSchedule && id && (
        <EditScheduleModal
          schedule={editingSchedule}
          reportId={id}
          onClose={() => setEditingId(null)}
          onUpdated={() => {
            setEditingId(null)
            listSchedules()
            push('Schedule updated', 'success')
          }}
        />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Clock, Download, Loader2, Pencil, Play, Trash2 } from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import ScheduleManager from '@/components/ScheduleManager'
import EditScheduleModal from '@/components/EditScheduleModal'
import {
  downloadReportPDF,
  formatFileSize,
  useReportSchedules,
  useSavedReports,
} from '@/hooks/useReportBuilder'

/**
 * SavedReportDetail shows one report's generation history and its delivery
 * schedules.
 */
export default function SavedReportDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { toasts, push } = useToasts()

  const { reports, loading: reportsLoading, listReports } = useSavedReports()
  const {
    schedules,
    listSchedules,
    deleteSchedule,
    runScheduleNow,
  } = useReportSchedules(id)

  const [showForm, setShowForm] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  // The list must actually be fetched here; the hook holds per-instance state,
  // so relying on another page having loaded it would leave this one empty.
  useEffect(() => {
    listReports()
  }, [listReports])

  useEffect(() => {
    listSchedules()
  }, [listSchedules])

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
          <h1 className="vs-title text-2xl normal-case">{report.name}</h1>
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

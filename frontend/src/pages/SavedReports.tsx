import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { BarChart3, Download, FileText, Loader2, Plus, Share2, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useToasts, Toaster } from '@/components/Toast'
import ReportBuilderWizard from '@/components/ReportBuilderWizard'
import {
  downloadReportPDF,
  formatFileSize,
  useSavedReports,
} from '@/hooks/useReportBuilder'
import type { SavedReport } from '@/types/reports'

interface SavedReportsProps {
  /** 'list' is the hub; 'create' shows the builder wizard. */
  mode?: 'list' | 'create'
}

/**
 * SavedReports is the hub for saved report definitions: generate, download,
 * share, and delete. The live analytics view lives separately at
 * /reports/analytics.
 */
export default function SavedReports({ mode = 'list' }: SavedReportsProps) {
  const navigate = useNavigate()
  const { toasts, push } = useToasts()
  const { reports, loading, error, listReports, deleteReport, shareReport } = useSavedReports()
  const [busyId, setBusyId] = useState<string | null>(null)
  const [confirmId, setConfirmId] = useState<string | null>(null)

  useEffect(() => {
    if (mode === 'list') listReports()
  }, [mode, listReports])

  const handleShare = async (report: SavedReport) => {
    setBusyId(report.id)
    try {
      const result = await shareReport(report.id)
      const link = `${window.location.origin}/reports/share/${result.share_token}`
      // Clipboard access can be denied or unavailable outside a secure context;
      // show the link either way so it is never lost.
      try {
        await navigator.clipboard.writeText(link)
        push('Share link copied to the clipboard', 'success')
      } catch {
        push(`Share link: ${link}`, 'info')
      }
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not share the report', 'error')
    } finally {
      setBusyId(null)
    }
  }

  const handleDownload = async (report: SavedReport) => {
    const latest = report.generations[0]
    if (!latest) return
    setBusyId(report.id)
    try {
      await downloadReportPDF(latest.download_url, `${report.name}.pdf`)
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not download the report', 'error')
    } finally {
      setBusyId(null)
    }
  }

  const handleDelete = async (report: SavedReport) => {
    setBusyId(report.id)
    try {
      await deleteReport(report.id)
      push(`Deleted "${report.name}"`, 'success')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not delete the report', 'error')
    } finally {
      setBusyId(null)
      setConfirmId(null)
    }
  }

  if (mode === 'create') {
    return (
      <>
        <div className="mb-5 flex items-center justify-between">
          <h1 className="vs-title text-2xl">New report</h1>
          <button className="rd-btn rd-btn-secondary" onClick={() => navigate('/reports')}>
            Cancel
          </button>
        </div>
        <ReportBuilderWizard onError={(m) => push(m, 'error')} />
        <Toaster toasts={toasts} />
      </>
    )
  }

  return (
    <div className="space-y-5 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="vs-title flex items-center gap-2 text-2xl">
            <FileText className="h-6 w-6" style={{ color: 'var(--vs-cyan)' }} />
            Reports
          </h1>
          <p className="mt-1 text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            Generate, schedule, and share uptime reports.
          </p>
        </div>
        <div className="flex gap-2">
          <Link to="/reports/analytics" className="rd-btn rd-btn-secondary">
            <BarChart3 className="h-4 w-4" /> Live analytics
          </Link>
          <Link to="/reports/new" className="rd-btn rd-btn-primary">
            <Plus className="h-4 w-4" /> New report
          </Link>
        </div>
      </div>

      {loading && reports.length === 0 && (
        <div className="rd-card flex items-center gap-2 p-6" style={{ color: 'var(--vs-text-dim)' }}>
          <Loader2 className="h-4 w-4 animate-spin" /> Loading reports…
        </div>
      )}

      {error && (
        <div className="rd-card p-4 text-sm" style={{ color: 'var(--vs-flat)' }}>
          {error.message}
        </div>
      )}

      {!loading && !error && reports.length === 0 && (
        <div className="rd-card p-8 text-center">
          <FileText className="mx-auto mb-3 h-10 w-10" style={{ color: 'var(--vs-text-dim)' }} />
          <p className="font-medium">No reports yet</p>
          <p className="mt-1 text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            Build one to generate a PDF you can schedule or share.
          </p>
          <Link to="/reports/new" className="rd-btn rd-btn-primary mt-4 inline-flex">
            <Plus className="h-4 w-4" /> Create a report
          </Link>
        </div>
      )}

      <div className="grid gap-3">
        {reports.map((report) => (
          <div key={report.id} className="rd-card p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              {/* The card title is the only navigation target, so the action
                  buttons are not nested inside a link. */}
              <div className="min-w-0 flex-1">
                <Link
                  to={`/reports/${report.id}`}
                  className="text-lg font-semibold hover:underline"
                >
                  {report.name}
                </Link>
                <div
                  className="mt-1 flex flex-wrap items-center gap-3 text-xs"
                  style={{ color: 'var(--vs-text-dim)' }}
                >
                  <span>{report.time_range_days}d window</span>
                  <span className="capitalize">{report.scope_type}</span>
                  <span>{report.template_name}</span>
                  {report.last_generated && (
                    <span>
                      generated{' '}
                      {formatDistanceToNow(new Date(report.last_generated), { addSuffix: true })}
                    </span>
                  )}
                  <span>
                    {report.generations.length} generation
                    {report.generations.length === 1 ? '' : 's'}
                  </span>
                </div>
              </div>

              <div className="flex shrink-0 items-center gap-1">
                {report.generations.length > 0 && (
                  <button
                    className="rd-btn rd-btn-secondary"
                    onClick={() => handleDownload(report)}
                    disabled={busyId === report.id}
                    title={`Download latest (${formatFileSize(report.generations[0].file_size)})`}
                  >
                    <Download className="h-4 w-4" />
                  </button>
                )}
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => handleShare(report)}
                  disabled={busyId === report.id}
                  title="Create a public share link"
                >
                  <Share2 className="h-4 w-4" />
                </button>
                <button
                  className="rd-btn rd-btn-secondary"
                  onClick={() => setConfirmId(report.id)}
                  disabled={busyId === report.id}
                  title="Delete this report"
                >
                  <Trash2 className="h-4 w-4" style={{ color: 'var(--vs-flat)' }} />
                </button>
              </div>
            </div>

            {confirmId === report.id && (
              <div
                className="mt-3 rounded-md p-3 text-sm"
                style={{ background: 'var(--vs-panel-2)' }}
              >
                <p>
                  Delete <strong>{report.name}</strong>? Its schedules stop, and{' '}
                  {report.generations.length} generated PDF
                  {report.generations.length === 1 ? '' : 's'} will be removed from disk. This
                  cannot be undone.
                </p>
                <div className="mt-3 flex gap-2">
                  <button
                    className="rd-btn rd-btn-primary"
                    onClick={() => handleDelete(report)}
                    disabled={busyId === report.id}
                  >
                    Delete
                  </button>
                  <button className="rd-btn rd-btn-secondary" onClick={() => setConfirmId(null)}>
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>
        ))}
      </div>

      <Toaster toasts={toasts} />
    </div>
  )
}

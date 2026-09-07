import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Calendar, Download, FileText } from 'lucide-react'
import {
  downloadReportPDF,
  formatFileSize,
  usePublicReport,
} from '@/hooks/useReportBuilder'

/**
 * PublicReport renders a report shared by token. It is reachable without
 * signing in, so it is mounted outside RequireAuth and shows only the report's
 * name, period, and downloadable generations.
 */
export default function PublicReport() {
  const { token } = useParams<{ token: string }>()
  const { report, loading, error, load } = usePublicReport(token)

  useEffect(() => {
    load()
  }, [load])

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center" style={{ background: 'var(--vs-bg)' }}>
        <p style={{ color: 'var(--vs-text-dim)' }}>Loading report…</p>
      </div>
    )
  }

  if (error || !report) {
    return (
      <div className="flex min-h-screen items-center justify-center" style={{ background: 'var(--vs-bg)' }}>
        <div className="rd-card max-w-md p-8 text-center">
          <FileText className="mx-auto mb-3 h-10 w-10" style={{ color: 'var(--vs-text-dim)' }} />
          <p className="font-medium">Report unavailable</p>
          <p className="mt-1 text-sm" style={{ color: 'var(--vs-text-dim)' }}>
            This link may have been revoked, or it may never have been valid.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen p-6" style={{ background: 'var(--vs-bg)' }}>
      <div className="mx-auto max-w-3xl space-y-5">
        <header className="border-b pb-5" style={{ borderColor: 'var(--vs-line)' }}>
          <h1 className="vs-title text-2xl">{report.name}</h1>
          <div
            className="mt-2 flex flex-wrap items-center gap-3 text-sm"
            style={{ color: 'var(--vs-text-dim)' }}
          >
            <span className="flex items-center gap-1">
              <Calendar className="h-4 w-4" />
              {report.time_range_days} day window
            </span>
            <span className="capitalize">{report.scope_type}</span>
            <span>
              {report.last_generated
                ? `Generated ${new Date(report.last_generated).toLocaleString()}`
                : 'Not yet generated'}
            </span>
          </div>
        </header>

        <section className="space-y-2">
          <h2 className="vs-eyebrow">Available downloads</h2>
          {report.generations.length === 0 && (
            <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
              No versions of this report have been generated yet.
            </p>
          )}
          {report.generations.map((gen) => (
            <div key={gen.id} className="rd-card flex items-center justify-between gap-3 p-4">
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
                className="rd-btn rd-btn-primary"
                onClick={() =>
                  downloadReportPDF(
                    gen.download_url,
                    `${report.name}-${new Date(gen.generated_at).toISOString().slice(0, 10)}.pdf`
                  )
                }
              >
                <Download className="h-4 w-4" /> Download
              </button>
            </div>
          ))}
        </section>

        <footer
          className="border-t pt-5 text-center text-xs"
          style={{ borderColor: 'var(--vs-line)', color: 'var(--vs-text-dim)' }}
        >
          Shared report from Sentinel monitoring.
        </footer>
      </div>
    </div>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FileText, Loader2, Download, ExternalLink, Check, AlertTriangle } from 'lucide-react'
import {
  useSavedReports,
  useReportTemplates,
  waitForReportJob,
  downloadReportPDF,
} from '@/hooks/useReportBuilder'
import type { ReportTemplate } from '@/types/reports'
import type { Monitor } from '@/types'

/**
 * What each report section contains, in the reader's terms rather than the
 * template's. Templates are rows in the database, so a report type is described
 * by the sections it carries — a template added later needs no change here.
 */
const SECTION_LABEL: Record<string, string> = {
  sla_compliance: 'Uptime and SLA compliance',
  incident_summary: 'Incidents and downtime',
  charts: 'Response-time charts',
  custom: 'Custom notes',
}

function describe(template: ReportTemplate): string {
  const sections = template.sections ?? []
  if (sections.length === 0) return 'No sections'
  return sections.map((s) => SECTION_LABEL[s] ?? s.replace(/_/g, ' ')).join(' · ')
}

const PERIODS = [
  { days: 7, label: 'Last 7 days' },
  { days: 30, label: 'Last 30 days' },
  { days: 90, label: 'Last 90 days' },
] as const

/** The widest period the API accepts, matching time_range_days on the server. */
const MAX_DAYS = 365

type Phase =
  | { kind: 'form' }
  | { kind: 'working'; message: string }
  | { kind: 'done'; downloadURL: string | null; reportID: string }
  | { kind: 'error'; message: string; reportID?: string }

/**
 * Generates a report covering one monitor.
 *
 * The report builder already does all of this, but it starts from "which
 * monitors?" — a question already answered by being on a monitor's page. This
 * asks only what it cannot infer: which report, over what period. The result is
 * an ordinary saved report, so it appears under Reports and can be shared or
 * scheduled from there like any other.
 */
export default function GenerateReportModal({
  monitor,
  isOpen,
  onClose,
}: {
  monitor: Monitor
  isOpen: boolean
  onClose: () => void
}) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { templates, loading: templatesLoading, listTemplates } = useReportTemplates()

  const [templateID, setTemplateID] = useState('')
  const [days, setDays] = useState<number>(30)
  const [customDays, setCustomDays] = useState('')
  const [phase, setPhase] = useState<Phase>({ kind: 'form' })

  useEffect(() => {
    if (isOpen) void listTemplates()
  }, [isOpen, listTemplates])

  // Default to the template marked default, which is the broadest report.
  useEffect(() => {
    if (!templateID && templates.length > 0) {
      setTemplateID((templates.find((t) => t.is_default) ?? templates[0]).id)
    }
  }, [templates, templateID])

  // Reopening after a generation should start a fresh form rather than show the
  // previous result.
  useEffect(() => {
    if (isOpen) setPhase({ kind: 'form' })
  }, [isOpen])

  const usingCustom = customDays !== ''
  const effectiveDays = usingCustom ? Number(customDays) : days
  const daysValid =
    Number.isInteger(effectiveDays) && effectiveDays >= 1 && effectiveDays <= MAX_DAYS

  const template = useMemo(
    () => templates.find((t) => t.id === templateID),
    [templates, templateID],
  )

  const periodLabel = usingCustom
    ? `Last ${effectiveDays} day${effectiveDays === 1 ? '' : 's'}`
    : (PERIODS.find((p) => p.days === days)?.label ?? `Last ${days} days`)

  if (!isOpen) return null

  const generate = async () => {
    if (!template || !daysValid) return
    setPhase({ kind: 'working', message: 'Creating the report…' })
    let reportID: string | undefined
    try {
      const result = await createReport({
        name: `${monitor.name} — ${template.name}`,
        template_id: template.id,
        scope_type: 'monitors',
        scope_data: { monitor_ids: [monitor.id] },
        time_range_days: effectiveDays,
        custom_title: `${monitor.name}: ${template.name}`,
        custom_description: `${periodLabel} · ${monitor.url}`,
      })
      reportID = result.id

      setPhase({ kind: 'working', message: 'Queued…' })
      const job = await waitForReportJob(result.job_id, {
        onProgress: (j) =>
          setPhase({
            kind: 'working',
            message: j.status === 'running' ? 'Rendering…' : 'Queued…',
          }),
      })
      setPhase({ kind: 'done', downloadURL: job.download_url ?? null, reportID: result.id })
    } catch (err) {
      // A failed render still leaves the definition saved, so the report is
      // offered rather than lost — it can be re-run from its own page.
      setPhase({
        kind: 'error',
        message: (err as { message?: string }).message ?? 'Could not generate the report',
        reportID,
      })
    }
  }

  const download = async (url: string) => {
    const stamp = new Date().toISOString().slice(0, 10)
    await downloadReportPDF(url, `${monitor.name}-${stamp}.pdf`)
  }

  const working = phase.kind === 'working'

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !working && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Generate a report for ${monitor.name}`}
        className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-xl border border-white/10 bg-slate-900/95 p-6"
      >
        <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
          <FileText className="h-5 w-5" aria-hidden /> Generate Report
        </h3>
        <p className="mt-1 text-sm text-slate-400">
          Covering <span className="text-slate-200">{monitor.name}</span> only.
        </p>

        {phase.kind === 'form' && (
          <div className="mt-5 space-y-5">
            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Report type</legend>
              {templatesLoading && templates.length === 0 ? (
                <div className="flex items-center gap-2 text-sm text-slate-400">
                  <Loader2 className="h-4 w-4 animate-spin" /> Loading report types…
                </div>
              ) : templates.length === 0 ? (
                <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-300">
                  No report types are configured.
                </p>
              ) : (
                <div className="space-y-2">
                  {templates.map((t) => (
                    <label
                      key={t.id}
                      className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition ${
                        templateID === t.id
                          ? 'border-primary-500/60 bg-primary-500/10'
                          : 'border-white/10 bg-slate-800/40 hover:border-white/25'
                      }`}
                    >
                      <input
                        type="radio"
                        name="report-template"
                        className="mt-1"
                        checked={templateID === t.id}
                        onChange={() => setTemplateID(t.id)}
                      />
                      <span className="min-w-0">
                        <span className="block text-sm font-medium text-white">{t.name}</span>
                        <span className="block text-xs text-slate-400">{describe(t)}</span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </fieldset>

            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Period</legend>
              <div className="flex flex-wrap gap-2">
                {PERIODS.map((p) => (
                  <button
                    key={p.days}
                    type="button"
                    onClick={() => {
                      setDays(p.days)
                      setCustomDays('')
                    }}
                    className={`rounded-lg border px-3 py-1.5 text-sm transition ${
                      !usingCustom && days === p.days
                        ? 'border-primary-500/60 bg-primary-500/10 text-white'
                        : 'border-white/10 bg-slate-800/40 text-slate-300 hover:border-white/25'
                    }`}
                  >
                    {p.label}
                  </button>
                ))}
                <label className="flex items-center gap-2 text-sm text-slate-400">
                  <span>or</span>
                  <input
                    type="number"
                    min={1}
                    max={MAX_DAYS}
                    value={customDays}
                    onChange={(e) => setCustomDays(e.target.value)}
                    placeholder="days"
                    aria-label="Custom period in days"
                    className="w-24 rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-white placeholder-slate-500"
                  />
                </label>
              </div>
              {usingCustom && !daysValid && (
                <p className="mt-2 text-xs text-red-400">
                  Enter a whole number of days between 1 and {MAX_DAYS}.
                </p>
              )}
            </fieldset>

            <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              {template ? (
                <>
                  <span className="text-slate-200">{template.name}</span> for {monitor.name},{' '}
                  {periodLabel.toLowerCase()}. It is saved under Reports, where it can be shared or
                  scheduled.
                </>
              ) : (
                'Choose a report type.'
              )}
            </p>

            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>
                Cancel
              </button>
              <button
                className="btn-primary"
                disabled={!template || !daysValid}
                onClick={() => void generate()}
              >
                <FileText className="h-4 w-4" /> Generate
              </button>
            </div>
          </div>
        )}

        {phase.kind === 'working' && (
          <div className="mt-6 space-y-3">
            <div className="flex items-center gap-2 text-sm text-slate-300">
              <Loader2 className="h-4 w-4 animate-spin" /> {phase.message}
            </div>
            <p className="text-xs text-slate-500">
              Rendering happens on the server and keeps going if this is closed — the report appears
              under Reports either way.
            </p>
          </div>
        )}

        {phase.kind === 'done' && (
          <div className="mt-6 space-y-4">
            <div className="flex items-center gap-2 text-sm text-emerald-400">
              <Check className="h-4 w-4" /> Report ready.
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>
                Close
              </button>
              <button
                className="btn-secondary"
                onClick={() => navigate(`/reports/${phase.reportID}`)}
              >
                <ExternalLink className="h-4 w-4" /> Open in Reports
              </button>
              {phase.downloadURL && (
                <button className="btn-primary" onClick={() => void download(phase.downloadURL!)}>
                  <Download className="h-4 w-4" /> Download PDF
                </button>
              )}
            </div>
          </div>
        )}

        {phase.kind === 'error' && (
          <div className="mt-6 space-y-4">
            <div className="flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <span>{phase.message}</span>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button className="btn-secondary" onClick={() => setPhase({ kind: 'form' })}>
                Back
              </button>
              {phase.reportID && (
                <button
                  className="btn-secondary"
                  onClick={() => navigate(`/reports/${phase.reportID}`)}
                >
                  <ExternalLink className="h-4 w-4" /> Open in Reports
                </button>
              )}
              <button className="btn-primary" onClick={onClose}>
                Close
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FileText, Loader2, Download, ExternalLink, Check, AlertTriangle } from 'lucide-react'
import {
  useSavedReports,
  useReportTemplates,
  useMonitorTags,
  waitForReportJob,
  downloadReportPDF,
} from '@/hooks/useReportBuilder'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import type { ReportPeriod, ReportScopeType, ReportTemplate } from '@/types/reports'

/** What a report covers, when the caller already knows — a monitor's own page. */
export interface FixedScope {
  scope_type: ReportScopeType
  ids: string[]
  /** How to describe it in the dialog, e.g. the monitor's name. */
  label: string
}

/**
 * What each report section contains, in the reader's terms rather than the
 * template's. Templates are rows in the database, so a report type is described
 * by the sections it carries — one added later needs no change here.
 */
const SECTION_LABEL: Record<string, string> = {
  executive_summary: 'Headline figures vs the previous period',
  availability_breakdown: 'Uptime day by day',
  timeline: 'What happened, in order',
  performance: 'Response times',
  sla_compliance: 'Uptime and SLA compliance',
  incident_summary: 'Incidents and downtime',
  charts: 'Summary figures',
  custom: 'Custom notes',
}

function describe(template: ReportTemplate): string {
  const sections = template.sections ?? []
  if (sections.length === 0) return 'No sections'
  return sections.map((s) => SECTION_LABEL[s] ?? s.replace(/_/g, ' ')).join(' · ')
}

// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

const SCOPE_TABS: { value: ReportScopeType; label: string }[] = [
  { value: 'monitors', label: 'Monitors' },
  { value: 'groups', label: 'Groups' },
  { value: 'tags', label: 'Tags' },
  { value: 'types', label: 'Types' },
]

type Phase =
  | { kind: 'form' }
  | { kind: 'working'; message: string }
  | { kind: 'done'; downloadURL: string | null; reportID: string }
  | { kind: 'error'; message: string; reportID?: string }

/**
 * Generates a report in one dialog.
 *
 * The wizard asks four questions across four steps, which is right when a scope
 * needs assembling from several groups and a custom title. Most of the time the
 * question is "last month, these services, that report", and that fits in one
 * screen. Given a fixedScope — a monitor's own page — the scope picker is
 * dropped entirely, since it is already answered.
 */
export default function GenerateReportModal({
  isOpen,
  onClose,
  fixedScope,
}: {
  isOpen: boolean
  onClose: () => void
  fixedScope?: FixedScope
}) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { templates, loading: templatesLoading, listTemplates } = useReportTemplates()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [templateID, setTemplateID] = useState('')
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [scopeType, setScopeType] = useState<ReportScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [phase, setPhase] = useState<Phase>({ kind: 'form' })

  useEffect(() => {
    if (!isOpen) return
    void listTemplates()
    if (!fixedScope) void listTags()
  }, [isOpen, fixedScope, listTemplates, listTags])

  // Default to the template marked default, which is the broadest report.
  useEffect(() => {
    if (!templateID && templates.length > 0) {
      setTemplateID((templates.find((t) => t.is_default) ?? templates[0]).id)
    }
  }, [templates, templateID])

  // Reopening should start a fresh form rather than show the previous result.
  useEffect(() => {
    if (isOpen) {
      setPhase({ kind: 'form' })
      setSelection([])
    }
  }, [isOpen])

  // Options for the active scope tab. A tag is its own identity — there is no
  // separate id — so its value and label are the same string.
  const options = useMemo(() => {
    if (scopeType === 'monitors') return monitors.map((m) => ({ id: m.id, name: m.name }))
    if (scopeType === 'groups') return groups.map((g) => ({ id: g.id, name: g.name }))
    if (scopeType === 'types') {
      // Only types that have monitors. Offering PING with no ping monitors
      // builds a report that is empty for a reason the reader cannot see.
      const counts = new Map<string, number>()
      for (const m of monitors) {
        if (REPORTABLE_TYPES.includes(m.type)) counts.set(m.type, (counts.get(m.type) ?? 0) + 1)
      }
      return REPORTABLE_TYPES.filter((t) => counts.has(t)).map((t) => ({
        id: t,
        name: `${t.toUpperCase()} (${counts.get(t)})`,
      }))
    }
    return tags.map((t) => ({ id: t, name: t }))
  }, [scopeType, monitors, groups, tags])

  const optionsLoading =
    (scopeType === 'monitors' && monitorsLoading) ||
    (scopeType === 'groups' && groupsLoading) ||
    (scopeType === 'tags' && tagsLoading) ||
    (scopeType === 'types' && monitorsLoading)

  const template = useMemo(
    () => templates.find((t) => t.id === templateID),
    [templates, templateID],
  )

  const effectiveScope: FixedScope | null = fixedScope
    ? fixedScope
    : selection.length > 0
      ? {
          scope_type: scopeType,
          ids: selection,
          label:
            selection.length === 1
              ? (options.find((o) => o.id === selection[0])?.name ?? selection[0])
              : `${selection.length} ${scopeType}`,
        }
      : null

  const periodValid =
    period.period_kind !== 'custom' || (!!period.period_start && !!period.period_end)
  const canGenerate = !!template && !!effectiveScope && periodValid

  if (!isOpen) return null

  const scopeData = (scope: FixedScope) => {
    switch (scope.scope_type) {
      case 'monitors':
        return { monitor_ids: scope.ids }
      case 'groups':
        return { group_ids: scope.ids }
      case 'tags':
        return { tags: scope.ids }
      default:
        return { types: scope.ids }
    }
  }

  const generate = async () => {
    if (!template || !effectiveScope || !periodValid) return
    setPhase({ kind: 'working', message: 'Creating the report…' })
    let reportID: string | undefined
    try {
      const result = await createReport({
        name: `${effectiveScope.label} — ${template.name}`,
        template_id: template.id,
        scope_type: effectiveScope.scope_type,
        scope_data: scopeData(effectiveScope),
        ...period,
        custom_title: `${effectiveScope.label}: ${template.name}`,
        custom_description: describePeriod(period),
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
    await downloadReportPDF(url, `${effectiveScope?.label ?? 'report'}-${stamp}.pdf`)
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))

  const working = phase.kind === 'working'

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !working && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Generate a report"
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-white/10 bg-slate-900/95 p-6"
      >
        <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
          <FileText className="h-5 w-5" aria-hidden /> Generate Report
        </h3>
        {fixedScope && (
          <p className="mt-1 text-sm text-slate-400">
            Covering <span className="text-slate-200">{fixedScope.label}</span> only.
          </p>
        )}

        {phase.kind === 'form' && (
          <div className="mt-5 space-y-5">
            {!fixedScope && (
              <fieldset>
                <legend className="mb-2 text-sm font-medium text-white">What to cover</legend>
                <div className="mb-2 flex flex-wrap gap-1">
                  {SCOPE_TABS.map((t) => (
                    <button
                      key={t.value}
                      type="button"
                      onClick={() => {
                        setScopeType(t.value)
                        setSelection([])
                      }}
                      className={`rounded-lg px-3 py-1.5 text-sm transition ${
                        scopeType === t.value
                          ? 'bg-primary-500/15 text-white'
                          : 'text-slate-400 hover:text-white'
                      }`}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
                {optionsLoading ? (
                  <div className="flex items-center gap-2 text-sm text-slate-400">
                    <Loader2 className="h-4 w-4 animate-spin" /> Loading…
                  </div>
                ) : options.length === 0 ? (
                  <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-300">
                    No {scopeType} available.
                    {scopeType === 'tags' && ' Tag a monitor first to scope a report by tag.'}
                  </p>
                ) : (
                  <div className="max-h-40 space-y-1 overflow-y-auto rounded-lg border border-white/10 bg-slate-800/40 p-2">
                    {options.map((o) => (
                      <label
                        key={o.id}
                        className="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm text-slate-200 hover:bg-white/5"
                      >
                        <input
                          type="checkbox"
                          checked={selection.includes(o.id)}
                          onChange={() => toggle(o.id)}
                        />
                        {o.name}
                      </label>
                    ))}
                  </div>
                )}
              </fieldset>
            )}

            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Period</legend>
              <PeriodSelector value={period} onChange={setPeriod} />
            </fieldset>

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

            <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              {canGenerate && template && effectiveScope ? (
                <>
                  <span className="text-slate-200">{template.name}</span> for{' '}
                  <span className="text-slate-200">{effectiveScope.label}</span>,{' '}
                  {describePeriod(period).toLowerCase()}. Saved under Reports, where it can be
                  shared or scheduled.
                </>
              ) : !effectiveScope ? (
                'Choose at least one thing to report on.'
              ) : !periodValid ? (
                'Choose both dates for a custom period.'
              ) : (
                'Choose a report type.'
              )}
            </p>

            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>
                Cancel
              </button>
              <button className="btn-primary" disabled={!canGenerate} onClick={() => void generate()}>
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

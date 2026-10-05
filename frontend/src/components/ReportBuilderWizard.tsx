import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import {
  METRICS_WAIT_MS,
  useMonitorTags,
  useSavedReports,
  waitForReportJob,
} from '@/hooks/useReportBuilder'
import { useMetricsReportDraft } from '@/hooks/useMetricsReportDraft'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_BLURB, REPORT_TYPE_LABEL, REPORT_TYPES } from '@/types/reports'
import type { MonitorScopeType, ReportPeriod, ReportType } from '@/types/reports'
import { REPORTABLE_MONITOR_TYPES, describeScope, monitorScopeData } from '@/utils/reportScope'

type StepId = 'type' | 'scope' | 'metrics' | 'period' | 'details'

const STEP_TITLE: Record<StepId, string> = {
  type: 'Report Type',
  scope: 'Scope',
  metrics: 'Metrics',
  period: 'Period',
  details: 'Details',
}

// Uptime and Incident reports have no metrics to choose, so they skip that step.
const MONITOR_STEPS: StepId[] = ['type', 'scope', 'period', 'details']
const METRICS_STEPS: StepId[] = ['type', 'scope', 'metrics', 'period', 'details']

interface ReportBuilderWizardProps {
  onError?: (message: string) => void
}

/**
 * ReportBuilderWizard walks through defining a saved report: its type, what it
 * covers, the metrics (Metrics reports only), the period, and an optional
 * title and description. Generating it renders a PDF immediately.
 */
export default function ReportBuilderWizard({ onError }: ReportBuilderWizardProps) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [stepIndex, setStepIndex] = useState(0)
  const [generating, setGenerating] = useState(false)
  // Rendering is queued, so the button reflects the job's actual state rather
  // than a generic spinner.
  const [progress, setProgress] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [reportType, setReportType] = useState<ReportType>('uptime')
  const [scopeType, setScopeType] = useState<MonitorScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [customTitle, setCustomTitle] = useState('')
  const [customDescription, setCustomDescription] = useState('')

  const isMetrics = reportType === 'metrics'
  const draft = useMetricsReportDraft(isMetrics)
  // The type is only chosen on the first step, which both lists share, so
  // switching it never strands the wizard on a step the other list lacks.
  const steps = isMetrics ? METRICS_STEPS : MONITOR_STEPS
  const step = steps[stepIndex]

  // generate waits for the render: two minutes, or METRICS_WAIT_MS for a
  // Metrics report. If the user leaves the wizard meanwhile, the result must not pull them back to the report or
  // raise an error on a page they have left. Set again in the effect body so
  // StrictMode's mount, unmount, mount ends mounted.
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  // Tags are fetched once, not on every step change.
  useEffect(() => {
    listTags()
  }, [listTags])

  // Options for the active scope tab. Tags are their own identity - there is no
  // separate id - so the value and the label are the same string.
  const options = useMemo(() => {
    if (scopeType === 'monitors') return monitors.map((m) => ({ id: m.id, name: m.name }))
    if (scopeType === 'groups') return groups.map((g) => ({ id: g.id, name: g.name }))
    if (scopeType === 'types') {
      // Only types that have monitors. Offering "PING" with no ping monitors
      // would build a report that is empty for a reason the reader cannot see.
      const counts = new Map<string, number>()
      for (const m of monitors) {
        if (REPORTABLE_MONITOR_TYPES.includes(m.type)) counts.set(m.type, (counts.get(m.type) ?? 0) + 1)
      }
      return REPORTABLE_MONITOR_TYPES.filter((t) => counts.has(t)).map((t) => ({
        id: t,
        name: `${t.toUpperCase()} (${counts.get(t)} monitor${counts.get(t) === 1 ? '' : 's'})`,
      }))
    }
    return tags.map((t) => ({ id: t, name: t }))
  }, [scopeType, monitors, groups, tags])

  const optionsLoading =
    (scopeType === 'monitors' && monitorsLoading) ||
    (scopeType === 'groups' && groupsLoading) ||
    (scopeType === 'tags' && tagsLoading) ||
    (scopeType === 'types' && monitorsLoading)

  const changeScopeType = (type: MonitorScopeType) => {
    setScopeType(type)
    setSelection([])
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((s) => s !== id) : [...cur, id]))

  // Validation lives here so Next is disabled rather than failing on click.
  const stepError = useMemo(() => {
    if (step === 'scope') {
      if (!name.trim()) return 'Give the report a name'
      if (isMetrics) return draft.scopeError
      if (selection.length === 0) {
        return `Select at least one ${scopeType === 'types' ? 'monitor type' : scopeType.slice(0, -1)}`
      }
    }
    if (step === 'metrics') return draft.metricsError
    if (
      step === 'period' &&
      period.period_kind === 'custom' &&
      (!period.period_start || !period.period_end)
    ) {
      return 'Choose both a start and an end date'
    }
    return null
  }, [step, name, isMetrics, draft.scopeError, draft.metricsError, selection, scopeType, period])

  // A Metrics scope also waits for its preview, which checks every pick is
  // still available and lists the metrics the next step offers. The picker
  // shows its progress and any error itself.
  const waiting = step === 'scope' && isMetrics && !draft.scopeReady

  const generate = async () => {
    setGenerating(true)
    try {
      const result = await createReport({
        name: name.trim(),
        report_type: reportType,
        scope_type: isMetrics ? draft.scope.scopeType : scopeType,
        scope_data: isMetrics ? draft.payloadData : monitorScopeData(scopeType, selection),
        ...period,
        custom_title: customTitle.trim() || undefined,
        custom_description: customDescription.trim() || undefined,
      })
      // Left already: the report is saved and renders on the server anyway.
      if (!mounted.current) return

      // The report exists now; its first PDF is still rendering. Wait for the
      // job so the detail page does not open on an empty generation list.
      setProgress('Queued…')
      try {
        await waitForReportJob(result.job_id, {
          timeoutMs: isMetrics ? METRICS_WAIT_MS : undefined,
          onProgress: (job) => {
            if (mounted.current) setProgress(job.status === 'running' ? 'Rendering…' : 'Queued…')
          },
        })
      } catch (jobErr) {
        // The definition was saved even though the render failed, so send the
        // user to it rather than losing their work, and say what happened.
        if (mounted.current) {
          onError?.(
            (jobErr as { message?: string }).message ??
              'The report was saved but its PDF could not be generated'
          )
        }
      }
      if (mounted.current) navigate(`/reports/${result.id}`)
    } catch (err) {
      if (mounted.current) {
        onError?.((err as { message?: string }).message ?? 'Could not generate the report')
      }
    } finally {
      if (mounted.current) {
        setGenerating(false)
        setProgress(null)
      }
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5 pb-10">
      {/* Step indicator */}
      <div className="flex items-center">
        {steps.map((id, idx) => (
          <div key={id} className="flex flex-1 items-center">
            <div
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
              style={{
                background: stepIndex >= idx ? 'var(--vs-ecg)' : 'var(--vs-panel-2)',
                color: stepIndex >= idx ? 'var(--vs-bg)' : 'var(--vs-text-dim)',
              }}
            >
              {stepIndex > idx ? <Check className="h-4 w-4" /> : idx + 1}
            </div>
            <span
              className="ml-2 hidden text-sm sm:inline"
              style={{ color: stepIndex >= idx ? 'var(--vs-text)' : 'var(--vs-text-dim)' }}
            >
              {STEP_TITLE[id]}
            </span>
            {idx < steps.length - 1 && (
              <div
                className="mx-3 h-px flex-1"
                style={{ background: stepIndex > idx ? 'var(--vs-ecg)' : 'var(--vs-line)' }}
              />
            )}
          </div>
        ))}
      </div>

      {step === 'type' && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Report type</span>
          {REPORT_TYPES.map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setReportType(t)}
              className="w-full rounded-md p-4 text-left"
              style={{
                border: `1px solid ${reportType === t ? 'var(--vs-ecg)' : 'var(--vs-line)'}`,
              }}
            >
              <p className="font-medium">{REPORT_TYPE_LABEL[t]}</p>
              <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                {REPORT_TYPE_BLURB[t]}
              </p>
            </button>
          ))}
        </div>
      )}

      {step === 'scope' && (
        <div className="rd-card space-y-5 p-5">
          <div>
            <label className="mb-1 block text-sm font-medium">Report name</label>
            <input
              className="rd-input w-full"
              placeholder="Weekly service report"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div>
            <span className="vs-eyebrow mb-2 block">Scope</span>
            {isMetrics ? (
              <p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>
            ) : (
              <>
                <div className="mb-3 flex gap-1 border-b" style={{ borderColor: 'var(--vs-line)' }}>
                  {(['monitors', 'types', 'groups', 'tags'] as const).map((type) => (
                    <button
                      key={type}
                      type="button"
                      onClick={() => changeScopeType(type)}
                      className="px-3 py-2 text-sm font-medium capitalize"
                      style={{
                        color: scopeType === type ? 'var(--vs-ecg)' : 'var(--vs-text-dim)',
                        borderBottom:
                          scopeType === type ? '2px solid var(--vs-ecg)' : '2px solid transparent',
                      }}
                    >
                      {type}
                    </button>
                  ))}
                </div>

                <div className="max-h-64 space-y-1 overflow-y-auto">
                  {optionsLoading && (
                    <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                      Loading…
                    </p>
                  )}
                  {!optionsLoading && options.length === 0 && (
                    <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                      No {scopeType === 'types' ? 'monitor types' : scopeType} available.
                      {scopeType === 'tags' && ' Tag a monitor first to scope a report by tag.'}
                      {scopeType === 'types' && ' Create a monitor first.'}
                    </p>
                  )}
                  {!optionsLoading &&
                    options.map((o) => (
                      <label
                        key={o.id}
                        className="flex cursor-pointer items-center gap-3 rounded px-2 py-2 text-sm hover:bg-white/5"
                      >
                        <input
                          type="checkbox"
                          className="h-4 w-4 rounded"
                          checked={selection.includes(o.id)}
                          onChange={() => toggle(o.id)}
                        />
                        <span>{o.name}</span>
                      </label>
                    ))}
                </div>
                <p className="mt-2 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                  {selection.length} selected
                </p>
              </>
            )}
          </div>
        </div>
      )}

      {step === 'metrics' && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Metrics</span>
          <p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>
        </div>
      )}

      {step === 'period' && (
        <div className="rd-card space-y-5 p-5">
          <span className="vs-eyebrow block">Reporting period</span>
          {/* The same selector the quick dialog uses: the two paths must not
              offer different periods, or a report built one way cannot be
              reproduced the other. */}
          <PeriodSelector value={period} onChange={setPeriod} />
        </div>
      )}

      {step === 'details' && (
        <div className="rd-card space-y-4 p-5">
          <span className="vs-eyebrow block">Details (optional)</span>
          <div>
            <label className="mb-1 block text-sm font-medium">Title on the report</label>
            <input
              className="rd-input w-full"
              placeholder="Leave blank to use the report name"
              value={customTitle}
              onChange={(e) => setCustomTitle(e.target.value)}
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium">Description</label>
            <textarea
              className="rd-input h-24 w-full resize-none"
              placeholder="Context or notes for whoever reads this"
              value={customDescription}
              onChange={(e) => setCustomDescription(e.target.value)}
            />
          </div>

          <div
            className="rounded-md p-4 text-sm"
            style={{ background: 'var(--vs-panel-2)', color: 'var(--vs-text-dim)' }}
          >
            <p>
              <strong style={{ color: 'var(--vs-text)' }}>{name || 'Untitled'}</strong>
            </p>
            <p className="mt-1">
              {isMetrics
                ? describeScope(draft.scope.scopeType, draft.payloadData, draft.names)
                : `${selection.length} ${scopeType}`}{' '}
              · {describePeriod(period)} · {REPORT_TYPE_LABEL[reportType]}
            </p>
          </div>
        </div>
      )}

      {stepError && (
        <p className="text-sm" style={{ color: 'var(--vs-amber)' }}>
          {stepError}
        </p>
      )}

      <div className="flex items-center justify-between gap-3">
        <button
          type="button"
          className="rd-btn rd-btn-secondary"
          onClick={() => setStepIndex((i) => Math.max(0, i - 1))}
          disabled={stepIndex === 0 || generating}
        >
          <ChevronLeft className="h-4 w-4" /> Back
        </button>

        {stepIndex < steps.length - 1 ? (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={() => setStepIndex((i) => i + 1)}
            disabled={stepError !== null || waiting}
          >
            Next <ChevronRight className="h-4 w-4" />
          </button>
        ) : (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={generate}
            disabled={generating || stepError !== null}
          >
            {generating ? (
              <span className="flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" /> {progress ?? 'Saving…'}
              </span>
            ) : (
              'Generate report'
            )}
          </button>
        )}
      </div>
    </div>
  )
}

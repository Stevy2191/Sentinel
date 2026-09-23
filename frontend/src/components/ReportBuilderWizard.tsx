import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useMonitorTags, useSavedReports, waitForReportJob } from '@/hooks/useReportBuilder'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_LABEL } from '@/types/reports'
import type { ReportPeriod, ReportScopeType, ReportType } from '@/types/reports'

type WizardStep = 1 | 2 | 3 | 4

const STEPS = [
  { number: 1, title: 'Scope' },
  { number: 2, title: 'Period' },
  { number: 3, title: 'Report Type' },
  { number: 4, title: 'Details' },
] as const

// The check types a report may be scoped to, in the order the app shows them.
// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

interface ReportBuilderWizardProps {
  onError?: (message: string) => void
}

/**
 * ReportBuilderWizard walks through defining a saved report: what it covers,
 * over what period, of which type, with optional title and description.
 * Generating it renders a PDF immediately.
 */
export default function ReportBuilderWizard({ onError }: ReportBuilderWizardProps) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [step, setStep] = useState<WizardStep>(1)
  const [generating, setGenerating] = useState(false)
  // Rendering is queued, so the button reflects the job's actual state rather
  // than a generic spinner.
  const [progress, setProgress] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [scopeType, setScopeType] = useState<ReportScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [reportType, setReportType] = useState<ReportType>('uptime')
  const [customTitle, setCustomTitle] = useState('')
  const [customDescription, setCustomDescription] = useState('')

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
        if (REPORTABLE_TYPES.includes(m.type)) counts.set(m.type, (counts.get(m.type) ?? 0) + 1)
      }
      return REPORTABLE_TYPES.filter((t) => counts.has(t)).map((t) => ({
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

  const changeScopeType = (type: ReportScopeType) => {
    setScopeType(type)
    setSelection([])
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((s) => s !== id) : [...cur, id]))

  // Validation lives here so Next is disabled rather than failing on click.
  const stepError = useMemo(() => {
    if (step === 1) {
      if (!name.trim()) return 'Give the report a name'
      if (selection.length === 0) {
        return `Select at least one ${scopeType === 'types' ? 'monitor type' : scopeType.slice(0, -1)}`
      }
    }
    if (
      step === 2 &&
      period.period_kind === 'custom' &&
      (!period.period_start || !period.period_end)
    ) {
      return 'Choose both a start and an end date'
    }
    return null
  }, [step, name, selection, scopeType, period])

  const generate = async () => {
    setGenerating(true)
    try {
      const scopeData =
        scopeType === 'monitors'
          ? { monitor_ids: selection }
          : scopeType === 'tags'
            ? { tags: selection }
            : scopeType === 'types'
              ? { types: selection }
              : { group_ids: selection }

      const result = await createReport({
        name: name.trim(),
        report_type: reportType,
        scope_type: scopeType,
        scope_data: scopeData,
        ...period,
        custom_title: customTitle.trim() || undefined,
        custom_description: customDescription.trim() || undefined,
      })

      // The report exists now; its first PDF is still rendering. Wait for the
      // job so the detail page does not open on an empty generation list.
      setProgress('Queued…')
      try {
        await waitForReportJob(result.job_id, {
          onProgress: (job) =>
            setProgress(job.status === 'running' ? 'Rendering…' : 'Queued…'),
        })
      } catch (jobErr) {
        // The definition was saved even though the render failed, so send the
        // user to it rather than losing their work, and say what happened.
        onError?.(
          (jobErr as { message?: string }).message ??
            'The report was saved but its PDF could not be generated'
        )
      }
      navigate(`/reports/${result.id}`)
    } catch (err) {
      onError?.((err as { message?: string }).message ?? 'Could not generate the report')
    } finally {
      setGenerating(false)
      setProgress(null)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5 pb-10">
      {/* Step indicator */}
      <div className="flex items-center">
        {STEPS.map((s, idx) => (
          <div key={s.number} className="flex flex-1 items-center">
            <div
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
              style={{
                background: step >= s.number ? 'var(--vs-ecg)' : 'var(--vs-panel-2)',
                color: step >= s.number ? 'var(--vs-bg)' : 'var(--vs-text-dim)',
              }}
            >
              {step > s.number ? <Check className="h-4 w-4" /> : s.number}
            </div>
            <span
              className="ml-2 hidden text-sm sm:inline"
              style={{ color: step >= s.number ? 'var(--vs-text)' : 'var(--vs-text-dim)' }}
            >
              {s.title}
            </span>
            {idx < STEPS.length - 1 && (
              <div
                className="mx-3 h-px flex-1"
                style={{ background: step > s.number ? 'var(--vs-ecg)' : 'var(--vs-line)' }}
              />
            )}
          </div>
        ))}
      </div>

      {step === 1 && (
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
          </div>
        </div>
      )}

      {step === 2 && (
        <div className="rd-card space-y-5 p-5">
          <span className="vs-eyebrow block">Reporting period</span>
          {/* The same selector the quick dialog uses: the two paths must not
              offer different periods, or a report built one way cannot be
              reproduced the other. */}
          <PeriodSelector value={period} onChange={setPeriod} />
        </div>
      )}

      {step === 3 && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Report type</span>
          {(['uptime', 'incident'] as const).map((t) => (
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
                {t === 'uptime'
                  ? 'Uptime vs. SLA, with a cumulative-uptime graph and a per-monitor breakdown.'
                  : 'Every incident in scope, with root cause and resolution detail.'}
              </p>
            </button>
          ))}
        </div>
      )}

      {step === 4 && (
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
              {selection.length} {scopeType} · {describePeriod(period)} ·{' '}
              {REPORT_TYPE_LABEL[reportType]}
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
          onClick={() => setStep((s) => (s > 1 ? ((s - 1) as WizardStep) : s))}
          disabled={step === 1}
        >
          <ChevronLeft className="h-4 w-4" /> Back
        </button>

        {step < 4 ? (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={() => setStep((s) => (s + 1) as WizardStep)}
            disabled={stepError !== null}
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

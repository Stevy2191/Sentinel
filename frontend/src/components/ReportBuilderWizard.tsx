import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import {
  useMonitorTags,
  useReportTemplates,
  useSavedReports,
  waitForReportJob,
} from '@/hooks/useReportBuilder'
import type { ReportScopeType } from '@/types/reports'

type WizardStep = 1 | 2 | 3 | 4

const STEPS = [
  { number: 1, title: 'Scope' },
  { number: 2, title: 'Period' },
  { number: 3, title: 'Template' },
  { number: 4, title: 'Details' },
] as const

const RANGE_PRESETS = [
  { value: 1, label: '24 hours' },
  { value: 7, label: '7 days' },
  { value: 30, label: '30 days' },
  { value: 90, label: '90 days' },
]

interface ReportBuilderWizardProps {
  onError?: (message: string) => void
}

/**
 * ReportBuilderWizard walks through defining a saved report: what it covers,
 * over what period, in which template, with optional title and description.
 * Generating it renders a PDF immediately.
 */
export default function ReportBuilderWizard({ onError }: ReportBuilderWizardProps) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { templates, listTemplates } = useReportTemplates()
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
  const [timeRangeDays, setTimeRangeDays] = useState(7)
  const [templateId, setTemplateId] = useState('')
  const [customTitle, setCustomTitle] = useState('')
  const [customDescription, setCustomDescription] = useState('')

  // Templates and tags are fetched once, not on every step change.
  useEffect(() => {
    listTemplates()
    listTags()
  }, [listTemplates, listTags])

  // Preselect the default template so step 3 is not a dead end if the user
  // clicks straight through.
  useEffect(() => {
    if (!templateId && templates.length > 0) {
      setTemplateId((templates.find((t) => t.is_default) ?? templates[0]).id)
    }
  }, [templates, templateId])

  // Options for the active scope tab. Tags are their own identity - there is no
  // separate id - so the value and the label are the same string.
  const options = useMemo(() => {
    if (scopeType === 'monitors') return monitors.map((m) => ({ id: m.id, name: m.name }))
    if (scopeType === 'groups') return groups.map((g) => ({ id: g.id, name: g.name }))
    return tags.map((t) => ({ id: t, name: t }))
  }, [scopeType, monitors, groups, tags])

  const optionsLoading =
    (scopeType === 'monitors' && monitorsLoading) ||
    (scopeType === 'groups' && groupsLoading) ||
    (scopeType === 'tags' && tagsLoading)

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
      if (selection.length === 0) return `Select at least one ${scopeType.slice(0, -1)}`
    }
    if (step === 2 && (timeRangeDays < 1 || timeRangeDays > 365)) {
      return 'The period must be between 1 and 365 days'
    }
    if (step === 3 && !templateId) return 'Choose a template'
    return null
  }, [step, name, selection, scopeType, timeRangeDays, templateId])

  const generate = async () => {
    setGenerating(true)
    try {
      const scopeData =
        scopeType === 'monitors'
          ? { monitor_ids: selection }
          : scopeType === 'tags'
            ? { tags: selection }
            : { group_ids: selection }

      const result = await createReport({
        name: name.trim(),
        template_id: templateId,
        scope_type: scopeType,
        scope_data: scopeData,
        time_range_days: timeRangeDays,
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
              {(['monitors', 'tags', 'groups'] as const).map((type) => (
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
                  No {scopeType} available.
                  {scopeType === 'tags' && ' Tag a monitor first to scope a report by tag.'}
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
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {RANGE_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setTimeRangeDays(p.value)}
                className="rounded-md px-3 py-3 text-sm font-medium"
                style={{
                  border: `1px solid ${timeRangeDays === p.value ? 'var(--vs-ecg)' : 'var(--vs-line)'}`,
                  color: timeRangeDays === p.value ? 'var(--vs-ecg)' : 'var(--vs-text)',
                }}
              >
                {p.label}
              </button>
            ))}
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium">Or a custom number of days</label>
            <input
              type="number"
              min={1}
              max={365}
              className="rd-input w-full"
              value={timeRangeDays}
              onChange={(e) => setTimeRangeDays(Number(e.target.value))}
            />
            <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              1 to 365 days.
            </p>
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Template</span>
          {templates.length === 0 && (
            <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
              No templates found.
            </p>
          )}
          {templates.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTemplateId(t.id)}
              className="w-full rounded-md p-4 text-left"
              style={{
                border: `1px solid ${templateId === t.id ? 'var(--vs-ecg)' : 'var(--vs-line)'}`,
              }}
            >
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="font-medium">{t.name}</p>
                  <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                    Sections: {t.sections.join(', ').replace(/_/g, ' ')}
                  </p>
                </div>
                {t.is_default && <span className="vs-eyebrow">Default</span>}
              </div>
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
              {selection.length} {scopeType} · {timeRangeDays} day
              {timeRangeDays === 1 ? '' : 's'} ·{' '}
              {templates.find((t) => t.id === templateId)?.name ?? 'no template'}
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

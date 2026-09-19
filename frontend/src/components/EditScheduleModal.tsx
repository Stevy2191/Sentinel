import { useEffect, useRef, useState } from 'react'
import { Loader2, X } from 'lucide-react'
import { useReportSchedules } from '@/hooks/useReportBuilder'
import type { ReportSchedule, ScheduleType } from '@/types/reports'

interface EditScheduleModalProps {
  /** The schedule being edited, used in full so nothing is lost on save. */
  schedule: ReportSchedule
  reportId: string
  onClose: () => void
  onUpdated: () => void
}

// The times are chosen now, so the labels name the cadence rather than baking
// 08:00 into it.
const SCHEDULE_OPTIONS: { value: ScheduleType; label: string }[] = [
  { value: 'daily', label: 'Every day' },
  { value: 'weekly', label: 'Every week' },
  { value: 'monthly', label: 'Every month' },
  { value: 'quarterly', label: 'Every quarter' },
  { value: 'custom', label: 'Custom (cron)' },
]

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

/**
 * EditScheduleModal edits an existing delivery schedule.
 *
 * The backend PATCH replaces include_in_email wholesale when it is present, so
 * every field is seeded from the current schedule and sent back. Submitting a
 * partial object would silently reset the settings the form does not show -
 * editing a recipient list would, for instance, turn off the summary.
 */
export default function EditScheduleModal({
  schedule,
  reportId,
  onClose,
  onUpdated,
}: EditScheduleModalProps) {
  const { updateSchedule } = useReportSchedules(reportId)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  const [form, setForm] = useState({
    scheduleType: schedule.schedule_type,
    // Carried through so a custom cadence keeps its expression on save.
    cronExpression: schedule.cron_expression ?? '',
    // Existing schedules predate configurable times, so they read as 08:00 —
    // which is the hour they have always fired at.
    sendHour: schedule.send_hour ?? 8,
    sendMinute: schedule.send_minute ?? 0,
    dayOfWeek: schedule.day_of_week ?? 1,
    dayOfMonth: schedule.day_of_month ?? 1,
    recipients: schedule.email_recipients.join(', '),
    sendAsAttachment: schedule.send_as_attachment,
    includeLink: schedule.include_in_email?.include_link ?? false,
    includeSummary: schedule.include_in_email?.include_summary ?? false,
    isActive: schedule.is_active,
  })

  // Escape closes the dialog, and focus moves into it on open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    dialogRef.current?.querySelector<HTMLElement>('select, input')?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const recipientList = form.recipients
    .split(',')
    .map((e) => e.trim())
    .filter(Boolean)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    if (recipientList.length === 0) {
      setError('Enter at least one email address')
      return
    }
    if (form.scheduleType === 'custom' && !form.cronExpression.trim()) {
      setError('A custom schedule needs a cron expression')
      return
    }

    setSaving(true)
    try {
      await updateSchedule(schedule.id, {
        schedule_type: form.scheduleType,
        cron_expression:
          form.scheduleType === 'custom' ? form.cronExpression.trim() : undefined,
        send_hour: form.sendHour,
        send_minute: form.sendMinute,
        day_of_week: form.scheduleType === 'weekly' ? form.dayOfWeek : undefined,
        day_of_month:
          form.scheduleType === 'monthly' || form.scheduleType === 'quarterly'
            ? form.dayOfMonth
            : undefined,
        email_recipients: recipientList,
        send_as_attachment: form.sendAsAttachment,
        include_in_email: {
          include_link: form.includeLink,
          include_summary: form.includeSummary,
        },
        is_active: form.isActive,
      })
      onUpdated()
    } catch (err) {
      // The server validates recipients and cron expressions too; surface its
      // message rather than a generic one.
      setError((err as { message?: string }).message ?? 'Could not update the schedule')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onMouseDown={(e) => {
        // Close only on a click that starts on the backdrop, so a drag that
        // ends outside the dialog does not dismiss it.
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Edit delivery schedule"
        className="rd-card max-h-[90vh] w-full max-w-md overflow-y-auto p-5"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="vs-title text-lg">Edit schedule</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 hover:bg-white/10"
          >
            <X className="h-5 w-5" style={{ color: 'var(--vs-text-dim)' }} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div
              className="rounded-md p-3 text-sm"
              style={{ background: 'var(--vs-panel-2)', color: 'var(--vs-flat)' }}
            >
              {error}
            </div>
          )}

          <div>
            <label className="mb-1 block text-sm font-medium">Cadence</label>
            <select
              className="rd-select w-full"
              value={form.scheduleType}
              onChange={(e) =>
                setForm((f) => ({ ...f, scheduleType: e.target.value as ScheduleType }))
              }
            >
              {SCHEDULE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>

          {form.scheduleType === 'weekly' && (
            <div>
              <label className="mb-1 block text-sm font-medium">Day</label>
              <select
                className="rd-select w-full"
                value={form.dayOfWeek}
                onChange={(e) => setForm((f) => ({ ...f, dayOfWeek: Number(e.target.value) }))}
              >
                {WEEKDAYS.map((d, i) => (
                  <option key={d} value={i}>
                    {d}
                  </option>
                ))}
              </select>
            </div>
          )}

          {(form.scheduleType === 'monthly' || form.scheduleType === 'quarterly') && (
            <div>
              <label className="mb-1 block text-sm font-medium">Day of month</label>
              <select
                className="rd-select w-full"
                value={form.dayOfMonth}
                onChange={(e) => setForm((f) => ({ ...f, dayOfMonth: Number(e.target.value) }))}
              >
                {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
            </div>
          )}

          {form.scheduleType !== 'custom' && (
            <div>
              <label className="mb-1 block text-sm font-medium">Time</label>
              <input
                type="time"
                className="rd-input w-full"
                value={`${String(form.sendHour).padStart(2, '0')}:${String(form.sendMinute).padStart(2, '0')}`}
                onChange={(e) => {
                  const [h, m] = e.target.value.split(':').map(Number)
                  setForm((f) => ({ ...f, sendHour: h || 0, sendMinute: m || 0 }))
                }}
              />
              <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                In the instance timezone, set under Settings &rarr; System.
              </p>
            </div>
          )}

          {form.scheduleType === 'custom' && (
            <div>
              <label className="mb-1 block text-sm font-medium">Cron expression</label>
              <input
                className="rd-input w-full font-mono text-sm"
                placeholder="0 12 * * 1-5"
                value={form.cronExpression}
                onChange={(e) => setForm((f) => ({ ...f, cronExpression: e.target.value }))}
              />
              <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                minute hour day month weekday — or a descriptor such as @daily.
              </p>
            </div>
          )}

          <div>
            <label className="mb-1 block text-sm font-medium">Recipients</label>
            <textarea
              className="rd-input h-20 w-full resize-none"
              placeholder="ops@example.com, sre@example.com"
              value={form.recipients}
              onChange={(e) => setForm((f) => ({ ...f, recipients: e.target.value }))}
            />
            <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              Comma separated. {recipientList.length} recipient
              {recipientList.length === 1 ? '' : 's'}; up to 50.
            </p>
          </div>

          <div className="space-y-2">
            {(
              [
                ['sendAsAttachment', 'Attach the PDF'],
                ['includeSummary', 'Include a summary in the body'],
                ['includeLink', 'Include a share link'],
                ['isActive', 'Schedule is active'],
              ] as const
            ).map(([key, label]) => (
              <label key={key} className="flex cursor-pointer items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded"
                  checked={form[key]}
                  onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.checked }))}
                />
                <span>{label}</span>
              </label>
            ))}
          </div>

          {!form.isActive && (
            <p className="text-xs" style={{ color: 'var(--vs-amber)' }}>
              An inactive schedule keeps its settings but will not run.
            </p>
          )}

          <div className="flex gap-3 pt-2">
            <button type="button" className="rd-btn rd-btn-secondary flex-1" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="rd-btn rd-btn-primary flex-1" disabled={saving}>
              {saving ? (
                <span className="flex items-center justify-center gap-2">
                  <Loader2 className="h-4 w-4 animate-spin" /> Saving…
                </span>
              ) : (
                'Save changes'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

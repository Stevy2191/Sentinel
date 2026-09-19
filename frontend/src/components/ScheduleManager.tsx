import { useState } from 'react'
import { Loader2, Mail } from 'lucide-react'
import { useReportSchedules } from '@/hooks/useReportBuilder'
import type { ScheduleType } from '@/types/reports'

interface ScheduleManagerProps {
  reportId: string
  onScheduleCreated?: () => void
  onError?: (message: string) => void
}

// The times are chosen now, so the labels say the cadence rather than baking
// 08:00 into the name of it.
const SCHEDULE_OPTIONS: { value: ScheduleType; label: string }[] = [
  { value: 'daily', label: 'Every day' },
  { value: 'weekly', label: 'Every week' },
  { value: 'monthly', label: 'Every month' },
  { value: 'quarterly', label: 'Every quarter' },
  { value: 'custom', label: 'Custom (cron)' },
]

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

// Capped below 29 deliberately: a schedule set to the 31st would not fire in
// February, and a report that silently skips a month is worse than one that
// arrives on the 28th.
const MAX_DAY_OF_MONTH = 28

/**
 * ScheduleManager is the create form for a report's delivery schedule. It owns
 * only creation; listing and per-schedule actions live on the detail page.
 */
export default function ScheduleManager({
  reportId,
  onScheduleCreated,
  onError,
}: ScheduleManagerProps) {
  const { createSchedule } = useReportSchedules(reportId)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState({
    scheduleType: 'weekly' as ScheduleType,
    cronExpression: '',
    sendHour: 8,
    sendMinute: 0,
    dayOfWeek: 1,
    dayOfMonth: 1,
    recipients: '',
    sendAsAttachment: true,
    includeLink: false,
    includeSummary: true,
  })

  // Parsed here as well as on the server so the count shown to the user matches
  // what will actually be submitted.
  const recipientList = form.recipients
    .split(',')
    .map((e) => e.trim())
    .filter(Boolean)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (recipientList.length === 0) {
      onError?.('Enter at least one email address')
      return
    }
    if (form.scheduleType === 'custom' && !form.cronExpression.trim()) {
      onError?.('A custom schedule needs a cron expression')
      return
    }

    setSaving(true)
    try {
      await createSchedule({
        schedule_type: form.scheduleType,
        cron_expression:
          form.scheduleType === 'custom' ? form.cronExpression.trim() : undefined,
        send_hour: form.sendHour,
        send_minute: form.sendMinute,
        // Only sent where the cadence has a choice to make, so the server keeps
        // its own default for the rest rather than storing a meaningless day.
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
      })
      setForm({
        scheduleType: 'weekly',
        cronExpression: '',
        sendHour: 8,
        sendMinute: 0,
        dayOfWeek: 1,
        dayOfMonth: 1,
        recipients: '',
        sendAsAttachment: true,
        includeLink: false,
        includeSummary: true,
      })
      onScheduleCreated?.()
    } catch (err) {
      onError?.((err as { message?: string }).message ?? 'Could not create the schedule')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="rd-card mb-5 space-y-4 p-5">
      <div className="flex items-center gap-2">
        <Mail className="h-4 w-4" style={{ color: 'var(--vs-cyan)' }} />
        <span className="vs-eyebrow">New delivery schedule</span>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
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

        {/* The day and time only appear where the cadence has a choice to
            make, so the form never asks which weekday a monthly report goes
            out on. */}
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
              {Array.from({ length: MAX_DAY_OF_MONTH }, (_, i) => i + 1).map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              Up to the 28th, so the report still goes out in February.
            </p>
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
              placeholder="0 8 * * MON"
              value={form.cronExpression}
              onChange={(e) => setForm((f) => ({ ...f, cronExpression: e.target.value }))}
            />
            <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              Five fields, or a descriptor such as @daily.
            </p>
          </div>
        )}
      </div>

      <div>
        <label className="mb-1 block text-sm font-medium">Recipients</label>
        <input
          className="rd-input w-full"
          placeholder="ops@example.com, sre@example.com"
          value={form.recipients}
          onChange={(e) => setForm((f) => ({ ...f, recipients: e.target.value }))}
        />
        <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
          Comma separated. {recipientList.length} recipient
          {recipientList.length === 1 ? '' : 's'}; up to 50.
        </p>
      </div>

      <div className="flex flex-wrap gap-4">
        {(
          [
            ['sendAsAttachment', 'Attach the PDF'],
            ['includeSummary', 'Include a summary in the body'],
            ['includeLink', 'Include a share link'],
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

      {form.includeLink && (
        <p className="text-xs" style={{ color: 'var(--vs-amber)' }}>
          A link is only included if this report has already been shared — scheduled
          delivery never creates public access on its own.
        </p>
      )}

      <button type="submit" className="rd-btn rd-btn-primary" disabled={saving}>
        {saving ? (
          <span className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" /> Creating…
          </span>
        ) : (
          'Create schedule'
        )}
      </button>
    </form>
  )
}

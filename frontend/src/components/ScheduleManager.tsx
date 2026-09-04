import { useState } from 'react'
import { Loader2, Mail } from 'lucide-react'
import { useReportSchedules } from '@/hooks/useReportBuilder'
import type { ScheduleType } from '@/types/reports'

interface ScheduleManagerProps {
  reportId: string
  onScheduleCreated?: () => void
  onError?: (message: string) => void
}

const SCHEDULE_OPTIONS: { value: ScheduleType; label: string }[] = [
  { value: 'daily', label: 'Daily at 08:00' },
  { value: 'weekly', label: 'Weekly, Monday 08:00' },
  { value: 'monthly', label: 'Monthly, 1st at 08:00' },
  { value: 'custom', label: 'Custom (cron)' },
]

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

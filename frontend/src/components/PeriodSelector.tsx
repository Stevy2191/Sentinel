import type { ReportPeriod } from '@/types/reports'

/**
 * Calendar periods come first because they are what people ask for: "last
 * month" means a month, not the thirty days ending today. Rolling windows stay
 * for the cases with no calendar equivalent — "the last 24 hours" is not a
 * calendar unit, and a rolling window is what a dashboard-style check wants.
 */
const CALENDAR: { label: string; unit: ReportPeriod['period_unit']; offset: number }[] = [
  { label: 'This week', unit: 'week', offset: 0 },
  { label: 'Last week', unit: 'week', offset: 1 },
  { label: 'This month', unit: 'month', offset: 0 },
  { label: 'Last month', unit: 'month', offset: 1 },
  { label: 'This quarter', unit: 'quarter', offset: 0 },
  { label: 'Last quarter', unit: 'quarter', offset: 1 },
  { label: 'This year', unit: 'year', offset: 0 },
  { label: 'Last year', unit: 'year', offset: 1 },
]

const ROLLING = [
  { label: 'Last 24 hours', days: 1 },
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
  { label: 'Last 90 days', days: 90 },
]

/** The period a fresh report starts on. */
export const DEFAULT_PERIOD: ReportPeriod = {
  period_kind: 'calendar',
  period_unit: 'month',
  period_offset: 1,
}

export function describePeriod(p: ReportPeriod): string {
  if (p.period_kind === 'calendar') {
    const match = CALENDAR.find((c) => c.unit === p.period_unit && c.offset === p.period_offset)
    if (match) return match.label
    return `${p.period_offset} ${p.period_unit}s ago`
  }
  if (p.period_kind === 'custom') {
    return p.period_start && p.period_end
      ? `${p.period_start.slice(0, 10)} to ${p.period_end.slice(0, 10)}`
      : 'Custom range'
  }
  return ROLLING.find((r) => r.days === p.time_range_days)?.label ?? `Last ${p.time_range_days} days`
}

function Chip({
  label,
  active,
  onClick,
}: {
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-lg border px-3 py-1.5 text-sm transition ${
        active
          ? 'border-primary-500/60 bg-primary-500/10 text-white'
          : 'border-white/10 bg-slate-800/40 text-slate-300 hover:border-white/25'
      }`}
    >
      {label}
    </button>
  )
}

export default function PeriodSelector({
  value,
  onChange,
}: {
  value: ReportPeriod
  onChange: (p: ReportPeriod) => void
}) {
  const isCalendar = value.period_kind === 'calendar'
  const isCustom = value.period_kind === 'custom'

  // A datetime-local value, which the inputs need, from the stored ISO string.
  const localDate = (iso: string | undefined) => (iso ? iso.slice(0, 10) : '')

  return (
    <div className="space-y-3">
      <div>
        <p className="mb-1.5 text-xs font-medium uppercase tracking-wide text-slate-500">
          Calendar period
        </p>
        <div className="flex flex-wrap gap-2">
          {CALENDAR.map((c) => (
            <Chip
              key={`${c.unit}-${c.offset}`}
              label={c.label}
              active={isCalendar && value.period_unit === c.unit && value.period_offset === c.offset}
              onClick={() =>
                onChange({ period_kind: 'calendar', period_unit: c.unit, period_offset: c.offset })
              }
            />
          ))}
        </div>
      </div>

      <div>
        <p className="mb-1.5 text-xs font-medium uppercase tracking-wide text-slate-500">
          Rolling window
        </p>
        <div className="flex flex-wrap gap-2">
          {ROLLING.map((r) => (
            <Chip
              key={r.days}
              label={r.label}
              active={value.period_kind === 'rolling' && value.time_range_days === r.days}
              onClick={() => onChange({ period_kind: 'rolling', time_range_days: r.days })}
            />
          ))}
        </div>
      </div>

      <div>
        <p className="mb-1.5 text-xs font-medium uppercase tracking-wide text-slate-500">
          Exact dates
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <input
            type="date"
            aria-label="Period start"
            value={localDate(value.period_start)}
            onChange={(e) =>
              onChange({
                period_kind: 'custom',
                period_start: e.target.value ? `${e.target.value}T00:00:00Z` : undefined,
                period_end: value.period_end,
              })
            }
            className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-white"
          />
          <span className="text-sm text-slate-500">to</span>
          <input
            type="date"
            aria-label="Period end"
            value={localDate(value.period_end)}
            onChange={(e) =>
              onChange({
                period_kind: 'custom',
                period_start: value.period_start,
                // Inclusive of the chosen day: someone picking the 30th means
                // the whole of the 30th, not midnight at its start.
                period_end: e.target.value ? `${e.target.value}T23:59:59Z` : undefined,
              })
            }
            className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-white"
          />
        </div>
        {isCustom && (!value.period_start || !value.period_end) && (
          <p className="mt-1 text-xs text-amber-400">Choose both a start and an end date.</p>
        )}
      </div>
    </div>
  )
}

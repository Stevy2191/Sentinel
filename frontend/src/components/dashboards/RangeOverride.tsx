import { METRICS_RANGES, type MetricsRange } from '@/hooks/useMetrics'

/** The dashboard-wide range picker: overrides every time-based widget's
 *  saved range until the page is left. Never saved. */
export default function RangeOverride({ value, onChange }: { value: MetricsRange | ''; onChange: (v: MetricsRange | '') => void }) {
  return (
    <select
      className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-slate-200"
      value={value}
      onChange={(e) => onChange(e.target.value as MetricsRange | '')}
      aria-label="Time range"
    >
      <option value="">Saved ranges</option>
      {METRICS_RANGES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  )
}

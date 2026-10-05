import { ChevronDown, ChevronUp, X } from 'lucide-react'
import { MAX_REPORT_METRICS, type MetricChoice, type MetricSource } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { METRIC_SOURCE_LABEL, moveItem } from '@/utils/reportScope'

interface Props {
  /** What the scope offers, from the latest preview. */
  choices: MetricChoice[]
  /** Chosen keys in order; the first ranks the rows. */
  value: string[]
  onChange: (keys: string[]) => void
  /** Labels of metrics a scope change removed, for the note. */
  dropped: string[]
}

const GROUPS: { source: MetricSource; label: string }[] = [
  { source: 'builtin', label: 'Built-in' },
  { source: 'profile', label: 'From device profiles' },
  { source: 'custom', label: 'Custom (MIB)' },
]

const iconBtn = 'rounded p-1 text-slate-400 hover:bg-white/5 hover:text-white disabled:opacity-30 disabled:hover:bg-transparent'

/**
 * The Metrics step: an ordered list of up to 10 metrics, pre-filled with the
 * scope's defaults. The first ranks the rows. Choices are what the scope's
 * devices offer, grouped by where each metric is defined.
 */
export default function ReportMetricsPicker({ choices, value, onChange, dropped }: Props) {
  const byKey = new Map(choices.map((c) => [c.key, c]))
  const remaining = choices.filter((c) => !value.includes(c.key))
  const full = value.length >= MAX_REPORT_METRICS
  const addLabel = full
    ? `Up to ${MAX_REPORT_METRICS} metrics`
    : choices.length === 0
      ? 'This scope has no metrics yet'
      : remaining.length === 0
        ? 'Every metric is chosen'
        : 'Add a metric…'

  return (
    <div className="space-y-3">
      {dropped.length > 0 && (
        <p className={`rounded-md border p-2 text-xs ${colors.warning.border} ${colors.warning.bg} ${colors.warning.text}`}>
          Removed because the scope no longer offers {dropped.length === 1 ? 'it' : 'them'}: {dropped.join(', ')}.
        </p>
      )}

      {value.length === 0 ? (
        <p className="text-sm text-slate-400">No metrics chosen.</p>
      ) : (
        <ol className="space-y-1">
          {value.map((key, i) => {
            const c = byKey.get(key)
            const label = c?.label ?? key
            return (
              <li
                key={key}
                className="flex items-center gap-2 rounded-md border border-white/10 bg-slate-800/40 px-3 py-2 text-sm"
              >
                <span className="w-5 shrink-0 text-right tabular-nums text-slate-500">{i + 1}.</span>
                <span className="min-w-0 flex-1">
                  <span className="text-white">{label}</span>
                  {c && (
                    <span className="ml-2 text-xs text-slate-500">
                      {[c.unit, METRIC_SOURCE_LABEL[c.source]].filter(Boolean).join(' · ')}
                    </span>
                  )}
                  {i === 0 && (
                    <span className="ml-2 rounded bg-primary-500/15 px-1.5 py-0.5 text-xs text-white">
                      ranks the rows
                    </span>
                  )}
                </span>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Move ${label} up`}
                  disabled={i === 0}
                  onClick={() => onChange(moveItem(value, i, -1))}
                >
                  <ChevronUp className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Move ${label} down`}
                  disabled={i === value.length - 1}
                  onClick={() => onChange(moveItem(value, i, 1))}
                >
                  <ChevronDown className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Remove ${label}`}
                  onClick={() => onChange(value.filter((k) => k !== key))}
                >
                  <X className="h-4 w-4" />
                </button>
              </li>
            )
          })}
        </ol>
      )}

      <select
        className={inputCls}
        aria-label="Add a metric"
        value=""
        disabled={full || remaining.length === 0}
        onChange={(e) => e.target.value && onChange([...value, e.target.value])}
      >
        <option value="">{addLabel}</option>
        {GROUPS.map((g) => {
          const items = remaining.filter((c) => c.source === g.source)
          return items.length === 0 ? null : (
            <optgroup key={g.source} label={g.label}>
              {items.map((c) => (
                <option key={c.key} value={c.key}>
                  {c.label}
                  {c.unit ? ` (${c.unit})` : ''}
                </option>
              ))}
            </optgroup>
          )
        })}
      </select>

      <p className="text-xs text-slate-500">
        The first metric ranks the rows: it decides the busiest and the order of every table. In and out of the
        same measure (traffic, busy, errors) are reported side by side.
      </p>
    </div>
  )
}

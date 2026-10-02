import { METRICS_RANGES } from '@/hooks/useMetrics'
import { inputCls } from '@/utils/dashboards'

export default function RangeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <select className={inputCls} value={value || '24h'} onChange={(e) => onChange(e.target.value)}>
      {METRICS_RANGES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  )
}

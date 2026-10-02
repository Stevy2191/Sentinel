import { Link } from 'react-router-dom'
import type { TopNData } from '@/types/dashboards'
import { formatMetric } from '@/utils/dashboards'

export default function TopNWidget({ data, linkable }: { data: TopNData; linkable: boolean }) {
  const items = data.items ?? []
  const max = Math.max(1, ...items.map((i) => i.value))
  return (
    <ol className="space-y-2">
      {items.map((it, i) => {
        const label = `${it.device_name} · ${it.port_name}${it.alias ? ` (${it.alias})` : ''}`
        return (
          <li key={`${it.device_name}-${it.if_index}-${i}`} className="space-y-0.5">
            <div className="flex items-baseline justify-between gap-2">
              {linkable && it.device_id ? (
                <Link to={`/network/devices/${it.device_id}/ports/${it.if_index}`} className="truncate text-slate-200 hover:text-primary-400">
                  {label}
                </Link>
              ) : (
                <span className="truncate text-slate-200">{label}</span>
              )}
              <span className="shrink-0 tabular-nums text-slate-300">{formatMetric(it.value, data.unit)}</span>
            </div>
            <div className="h-1.5 rounded bg-slate-800">
              <div className="h-1.5 rounded bg-cyan-500/70" style={{ width: `${(it.value / max) * 100}%` }} />
            </div>
          </li>
        )
      })}
    </ol>
  )
}

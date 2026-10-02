import { useMemo } from 'react'
import { Area, AreaChart, ResponsiveContainer } from 'recharts'
import type { StatData } from '@/types/dashboards'
import { chartPalette, colors } from '@/utils/colors'
import { formatMetric } from '@/utils/dashboards'

const LEVEL = { ok: colors.operational.text, warn: colors.warning.text, crit: colors.error.text } as const

export default function StatWidget({ data }: { data: StatData }) {
  const spark = useMemo(() => (data.spark ?? []).map((p) => ({ t: new Date(p.t).getTime(), v: p.avg })), [data.spark])
  return (
    <div className="flex h-full flex-col justify-between gap-2">
      <p className="text-xs text-slate-400">
        {data.label}
        {data.mode === 'average' && ` · average over ${data.range}`}
      </p>
      <p className={`text-4xl font-light tabular-nums ${LEVEL[data.level] ?? LEVEL.ok}`}>{formatMetric(data.value, data.unit)}</p>
      {spark.length > 1 && (
        <div className="h-10">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={spark} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
              <Area type="monotone" dataKey="v" stroke={chartPalette[0]} fill={chartPalette[0]} fillOpacity={0.15} isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}

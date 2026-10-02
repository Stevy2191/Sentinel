import { useMemo } from 'react'
import { AreaSeriesChart } from '@/components/network/TrafficChart'
import type { TimeseriesData } from '@/types/dashboards'
import { chartPalette } from '@/utils/colors'
import { chartRows, unitOf } from '@/utils/dashboards'

const SHORT = new Set(['1h', '6h', '24h'])

export default function TimeseriesWidget({ data }: { data: TimeseriesData }) {
  const src = useMemo(() => data.lines ?? [], [data.lines])
  const rows = useMemo(() => chartRows(src), [src])
  const lines = useMemo(() => src.map((l, i) => ({ metric: l.key, label: l.label, colour: chartPalette[i % chartPalette.length] })), [src])
  const unit = src[0]?.unit ?? ''
  return (
    <AreaSeriesChart bare data={rows} lines={lines} unit={unitOf(unit)} unitLabel={unit} step={data.step_seconds} shortTicks={SHORT.has(data.range)} />
  )
}

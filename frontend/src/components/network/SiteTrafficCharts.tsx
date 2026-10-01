import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { METRICS_RANGES, type MetricsRange } from '@/hooks/useMetrics'
import { useSiteTraffic } from '@/hooks/usePorts'
import { AreaSeriesChart, type ChartLine } from '@/components/network/TrafficChart'

const NORTH_SOUTH_LINES: ChartLine[] = [
  { metric: 'in_bps', label: 'Download', colour: '#22d3ee' },
  { metric: 'out_bps', label: 'Upload', colour: '#a78bfa' },
]
const EAST_WEST_LINES: ChartLine[] = [{ metric: 'bps', label: 'Inside the site', colour: '#34d399' }]

const EMPTY = (
  <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
    No data in this range yet. Figures appear a minute or two after the first stats poll.
  </div>
)

/** Replaces the single "Site traffic" chart with two: the internet
 *  (north-south: download/upload over the site's WAN ports) and traffic that
 *  stays inside the site (east-west), sharing one range picker. Until a port
 *  is marked WAN, shows one explanatory card instead of empty charts. */
export default function SiteTrafficCharts({ siteId }: { siteId: string }) {
  const [range, setRange] = useState<MetricsRange>('24h')
  const { data, loading, error } = useSiteTraffic(siteId, range)

  const northSouth: Record<string, number>[] = (data?.north_south ?? []).map((p) => ({
    t: new Date(p.t).getTime(),
    in_bps: p.in_bps,
    out_bps: p.out_bps,
  }))
  const eastWest: Record<string, number>[] = (data?.east_west ?? []).map((p) => ({ t: new Date(p.t).getTime(), bps: p.bps }))

  const picker = (
    <select
      value={range}
      onChange={(e) => setRange(e.target.value as MetricsRange)}
      aria-label="Site traffic time range"
      className="cursor-pointer rounded-lg border border-white/10 bg-slate-800/50 px-3 py-1.5 text-sm text-white focus:border-white/30 focus:outline-none"
    >
      {METRICS_RANGES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">Site traffic</h2>
        {picker}
      </div>
      {loading && !data ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">{error}</div>
      ) : !data?.wan_configured ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
          Mark the port that faces the internet as WAN (on its port page) to see internet and east-west traffic.
        </div>
      ) : (
        <div className="space-y-6">
          <section>
            <h3 className="mb-3 text-sm font-medium text-slate-300">Internet (north-south)</h3>
            {northSouth.length === 0 ? EMPTY : <AreaSeriesChart data={northSouth} lines={NORTH_SOUTH_LINES} unit="bps" step={data.step_seconds} />}
          </section>
          <section>
            <h3 className="mb-3 text-sm font-medium text-slate-300">Inside the site (east-west)</h3>
            {eastWest.length === 0 ? EMPTY : <AreaSeriesChart data={eastWest} lines={EAST_WEST_LINES} unit="bps" step={data.step_seconds} />}
          </section>
        </div>
      )}
    </div>
  )
}

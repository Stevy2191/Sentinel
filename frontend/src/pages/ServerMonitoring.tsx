import { Plus, Server } from 'lucide-react'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import ShimmerStatCard from '@/components/ShimmerStatCard'

/**
 * Server monitoring needs an agent, a transport and a metrics table, none of
 * which exist yet. The page shows its shape so the feature is discoverable,
 * with every reading at zero and labelled "coming soon" — a table of invented
 * servers would be worse than an empty one.
 */
export default function ServerMonitoring() {
  const shimmer = useCardShimmer(['total', 'online', 'offline', 'warning'])

  const cards = [
    { id: 'total', title: 'Total Servers', tone: 'monitoring' as const },
    { id: 'online', title: 'Online Servers', tone: 'monitoring' as const },
    { id: 'offline', title: 'Offline Servers', tone: 'incidents' as const },
    { id: 'warning', title: 'Warning Servers', tone: 'agents' as const },
  ]

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Server Monitoring</h1>
          <p className="mt-2 text-sm text-slate-400">Monitor connected agents and servers</p>
        </div>
        <button
          className="btn-primary shrink-0 cursor-not-allowed opacity-60"
          disabled
          title="Server agents are coming in a future release"
        >
          <Plus className="h-4 w-4" /> Add Server Agent
        </button>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {cards.map((c) => (
          <ShimmerStatCard
            key={c.id}
            title={c.title}
            value={0}
            subtitle="coming soon"
            colorType={c.tone}
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, c.id)}
            onMouseEnter={() => shimmer.handleCardMouseEnter(c.id)}
            onMouseLeave={() => shimmer.handleCardMouseLeave(c.id)}
            showShimmer={shimmer.isShown(c.id)}
            shimmerStyle={shimmer.getShimmerStyle(c.id)}
          />
        ))}
      </div>

      <div className="flex min-h-64 flex-col items-center justify-center rounded-lg border border-white/10 bg-slate-800/40 p-12 text-center backdrop-blur-sm">
        <Server className="mb-4 h-12 w-12 text-slate-500" aria-hidden="true" />
        <h2 className="text-lg font-light text-white">No servers found</h2>
        <p className="mt-1 text-sm text-slate-400">
          Server and agent monitoring is coming in a future release.
        </p>
      </div>
    </div>
  )
}

import React from 'react'
import { Server } from 'lucide-react'

/**
 * Placeholder. Server metrics need an agent, a transport and a metrics table,
 * none of which exist yet, so this shows its state honestly instead of a table
 * of zeros.
 */
export const ServerMonitoring: React.FC = () => {
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">Instance Monitoring</h1>
        <p className="mt-2 text-sm text-slate-400">Monitor connected agents and servers</p>
      </div>

      <div className="flex min-h-96 items-center justify-center">
        <div className="relative overflow-hidden rounded-lg border border-amber-500/30 bg-gradient-to-br from-amber-600/15 to-slate-800/40 px-12 py-14 text-center backdrop-blur-sm">
          <div className="pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full bg-amber-500/10 blur-2xl" />
          <div className="relative z-10">
            <Server className="mx-auto mb-4 h-12 w-12 text-amber-400" aria-hidden="true" />
            <h2 className="mb-2 text-2xl font-light text-white">Coming Soon</h2>
            <p className="text-sm text-slate-400">
              Server and agent monitoring is coming in the next release
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export default ServerMonitoring

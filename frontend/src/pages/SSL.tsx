import React from 'react'
import { ShieldCheck } from 'lucide-react'

/**
 * Placeholder. There is no certificate backend yet: nothing in the API reads a
 * TLS handshake or stores an expiry, so this page deliberately shows no data
 * rather than empty tables that imply a feature exists.
 */
export const SSL: React.FC = () => {
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">SSL &amp; Domains</h1>
        <p className="mt-2 text-sm text-slate-400">Manage SSL certificates and domain monitoring</p>
      </div>

      <div className="flex min-h-96 items-center justify-center">
        <div className="relative overflow-hidden rounded-lg border border-emerald-500/30 bg-gradient-to-br from-emerald-600/15 to-slate-800/40 px-12 py-14 text-center backdrop-blur-sm">
          <div className="pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full bg-emerald-500/10 blur-2xl" />
          <div className="relative z-10">
            <ShieldCheck className="mx-auto mb-4 h-12 w-12 text-emerald-400" aria-hidden="true" />
            <h2 className="mb-2 text-2xl font-light text-white">Coming Soon</h2>
            <p className="text-sm text-slate-400">
              SSL certificate monitoring is coming in the next release
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export default SSL

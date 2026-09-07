import React from 'react'

/**
 * Colour-coded pill for a monitor's type, using the app's eight-colour scheme:
 * DNS purple, HTTP cyan, PING yellow, TCP orange.
 *
 * Classes are complete literal strings per type. Tailwind only emits CSS for
 * class names it finds spelled out in the source, so these cannot be built by
 * interpolating the type into a template.
 */
const typeMap: Record<string, string> = {
  dns: 'bg-purple-500/10 text-purple-400 border-purple-500/20',
  http: 'bg-cyan-500/10 text-cyan-400 border-cyan-500/20',
  ping: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20',
  tcp: 'bg-orange-500/10 text-orange-400 border-orange-500/20',
  webhook: 'bg-slate-500/10 text-slate-300 border-slate-500/20',
}

const FALLBACK = 'bg-slate-500/10 text-slate-300 border-slate-500/20'

export const MonitorTypeBadge: React.FC<{ type: string }> = ({ type }) => (
  <span
    className={`inline-block rounded border px-2 py-0.5 text-xs font-semibold uppercase tracking-wider ${
      typeMap[type?.toLowerCase()] ?? FALLBACK
    }`}
  >
    {type}
  </span>
)

export default MonitorTypeBadge

import React from 'react'

interface ShimmerTypeCardProps {
  /** Monitor type label, e.g. "DNS". */
  label: string
  count: number
  online: number
  colorType: 'dns' | 'http' | 'ping' | 'tcp'
  onMouseMove: (e: React.MouseEvent) => void
  onMouseEnter: () => void
  onMouseLeave: () => void
  showShimmer: boolean
  shimmerStyle: React.CSSProperties
  onClick?: () => void
}

// Same literal-class rule as ShimmerStatCard: one complete string per colour.
const colorMap = {
  dns: {
    bg: 'from-purple-600/15',
    text: 'text-purple-400',
    subtle: 'text-purple-400/70',
    border: 'border-purple-500/20',
    glow: 'bg-purple-500/10',
    glowHover: 'group-hover:bg-purple-500/20',
  },
  http: {
    bg: 'from-cyan-600/15',
    text: 'text-cyan-400',
    subtle: 'text-cyan-400/70',
    border: 'border-cyan-500/20',
    glow: 'bg-cyan-500/10',
    glowHover: 'group-hover:bg-cyan-500/20',
  },
  ping: {
    bg: 'from-yellow-600/15',
    text: 'text-yellow-400',
    subtle: 'text-yellow-400/70',
    border: 'border-yellow-500/20',
    glow: 'bg-yellow-500/10',
    glowHover: 'group-hover:bg-yellow-500/20',
  },
  tcp: {
    bg: 'from-orange-600/15',
    text: 'text-orange-400',
    subtle: 'text-orange-400/70',
    border: 'border-orange-500/20',
    glow: 'bg-orange-500/10',
    glowHover: 'group-hover:bg-orange-500/20',
  },
} as const

export const ShimmerTypeCard: React.FC<ShimmerTypeCardProps> = ({
  label,
  count,
  online,
  colorType,
  onMouseMove,
  onMouseEnter,
  onMouseLeave,
  showShimmer,
  shimmerStyle,
  onClick,
}) => {
  const c = colorMap[colorType]
  const allUp = count > 0 && online === count

  return (
    <button
      type="button"
      onClick={onClick}
      onMouseMove={onMouseMove}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      className={`group relative w-full overflow-hidden rounded-lg border ${c.border} bg-gradient-to-br ${c.bg} to-slate-800/40 p-5 text-left backdrop-blur-sm transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-white/40`}
    >
      <div
        className={`pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full ${c.glow} blur-2xl transition-all ${c.glowHover}`}
      />
      {showShimmer && (
        <div
          className="pointer-events-none absolute inset-0 rounded-lg transition-all duration-75"
          style={shimmerStyle}
        />
      )}

      <div className="relative z-10">
        <div className={`mb-3 text-xs font-semibold uppercase tracking-widest ${c.text}`}>{label}</div>
        <div className="flex items-end justify-between gap-3">
          <div className="text-3xl font-light text-white">{count}</div>
          <div className={`text-xs font-medium ${allUp ? 'text-emerald-400/80' : c.subtle}`}>
            {count === 0 ? 'none configured' : `${online}/${count} up`}
          </div>
        </div>
      </div>
    </button>
  )
}

export default ShimmerTypeCard

import React from 'react'

interface ShimmerStatCardProps {
  title: string
  value: string | number
  subtitle?: string
  colorType: 'responseTime' | 'incidents' | 'agents'
  onMouseMove: (e: React.MouseEvent) => void
  onMouseEnter: () => void
  onMouseLeave: () => void
  showShimmer: boolean
  shimmerStyle: React.CSSProperties
}

// Whole literal class strings per colour. Tailwind only generates CSS for class
// names it can find spelled out in the source, so these cannot be assembled
// from a key at runtime.
const colorMap = {
  responseTime: {
    bg: 'from-blue-600/15',
    text: 'text-blue-400',
    subtle: 'text-blue-400/70',
    border: 'border-blue-500/30',
    glow: 'bg-blue-500/10',
    glowHover: 'group-hover:bg-blue-500/20',
  },
  incidents: {
    bg: 'from-red-600/15',
    text: 'text-red-400',
    subtle: 'text-red-400/70',
    border: 'border-red-500/30',
    glow: 'bg-red-500/10',
    glowHover: 'group-hover:bg-red-500/20',
  },
  agents: {
    bg: 'from-amber-600/15',
    text: 'text-amber-400',
    subtle: 'text-amber-400/70',
    border: 'border-amber-500/30',
    glow: 'bg-amber-500/10',
    glowHover: 'group-hover:bg-amber-500/20',
  },
} as const

export const ShimmerStatCard: React.FC<ShimmerStatCardProps> = ({
  title,
  value,
  subtitle,
  colorType,
  onMouseMove,
  onMouseEnter,
  onMouseLeave,
  showShimmer,
  shimmerStyle,
}) => {
  const c = colorMap[colorType]

  return (
    <div
      className={`relative overflow-hidden rounded-lg border ${c.border} bg-gradient-to-br ${c.bg} to-slate-800/40 p-6 backdrop-blur-sm transition-all group cursor-default`}
      onMouseMove={onMouseMove}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      {/* Corner glow, brightening on hover. */}
      <div
        className={`pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full ${c.glow} blur-2xl transition-all ${c.glowHover}`}
      />
      <div className="pointer-events-none absolute inset-0 rounded-lg bg-gradient-to-b from-slate-500/5 via-transparent to-transparent" />

      {/* Cursor-following highlight, mounted only while hovered. */}
      {showShimmer && (
        <div
          className="pointer-events-none absolute inset-0 rounded-lg transition-all duration-75"
          style={shimmerStyle}
        />
      )}

      <div className="relative z-10">
        <div className={`mb-4 text-xs font-semibold uppercase tracking-widest ${c.text}`}>{title}</div>
        <div className="flex items-end justify-between gap-3">
          <div className="text-3xl font-light text-white">{value}</div>
          {subtitle && <div className={`text-xs font-medium ${c.subtle}`}>{subtitle}</div>}
        </div>
      </div>
    </div>
  )
}

export default ShimmerStatCard

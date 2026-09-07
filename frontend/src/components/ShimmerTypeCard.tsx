import React from 'react'
import { colors, type MonitorTypeKey } from '@/utils/colors'

interface ShimmerTypeCardProps {
  /** Monitor type label, e.g. "DNS". */
  label: string
  count: number
  online: number
  colorType: MonitorTypeKey
  onMouseMove: (e: React.MouseEvent) => void
  onMouseEnter: () => void
  onMouseLeave: () => void
  showShimmer: boolean
  shimmerStyle: React.CSSProperties
  onClick?: () => void
}


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
  const c = colors[colorType]
  const allUp = count > 0 && online === count

  return (
    <button
      type="button"
      onClick={onClick}
      onMouseMove={onMouseMove}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      className={`group relative w-full overflow-hidden rounded-lg border ${c.border} bg-gradient-to-br ${c.cardBg} to-slate-800/40 p-5 text-left backdrop-blur-sm transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-white/40`}
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

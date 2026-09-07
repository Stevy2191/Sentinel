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
  /** Marks the card as the active type filter. */
  active?: boolean
}

/**
 * ShimmerTypeCard — the reference's monitor-type tile: a flat tinted panel with
 * two stacked corner glows (the second fades in on hover), a cursor-following
 * highlight, and the passing count in the type's accent.
 */
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
  active = false,
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
      aria-pressed={active}
      className={`group relative flex w-full flex-col justify-between overflow-hidden rounded-lg border ${c.bg} ${
        active ? 'border-white/40' : c.border
      } p-4 text-left backdrop-blur-sm transition-all hover:border-opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-white/40`}
    >
      {/* Base corner glow. */}
      <div
        className="pointer-events-none absolute right-0 top-0 -mr-8 -mt-8 h-16 w-16 rounded-full blur-2xl transition-all"
        style={{ backgroundColor: c.glowRgba }}
      />
      {/* Brighter glow stacked on top, revealed on hover. */}
      <div
        className="pointer-events-none absolute right-0 top-0 -mr-8 -mt-8 h-16 w-16 rounded-full opacity-0 blur-2xl transition-all group-hover:opacity-100"
        style={{ backgroundColor: c.glowRgbaHover }}
      />

      {showShimmer && (
        <div
          className="pointer-events-none absolute inset-0 rounded-lg transition-all duration-75"
          style={shimmerStyle}
        />
      )}

      <div className="relative z-10">
        <div className={`mb-1 text-xs font-semibold uppercase tracking-wide ${c.text}`}>
          {label} Checks
        </div>
        <div className="text-sm font-medium text-white">
          {count === 0 ? 'None configured' : allUp ? 'All passing' : `${count - online} failing`}
        </div>
      </div>
      <div className={`relative z-10 mt-2 text-lg font-light ${c.text}`}>
        {online}/{count}
      </div>
    </button>
  )
}

export default ShimmerTypeCard

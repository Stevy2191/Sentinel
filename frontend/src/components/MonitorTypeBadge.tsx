import React from 'react'
import { getMonitorTypeColor } from '@/utils/colors'

/**
 * Colour-coded pill for a monitor's type: DNS purple, HTTP cyan, PING yellow,
 * TCP orange, neutral for webhook and anything unrecognised.
 *
 * The classes come from utils/colors, which stores each one as a complete
 * literal string. Tailwind only emits CSS for class names it finds spelled out
 * in the source, so a colour can be selected at runtime but never assembled.
 */
export const MonitorTypeBadge: React.FC<{ type: string }> = ({ type }) => {
  const c = getMonitorTypeColor(type)
  return (
    <span
      className={`inline-block rounded border px-2 py-0.5 text-xs font-semibold uppercase tracking-wider ${c.bg} ${c.text} ${c.border}`}
    >
      {type}
    </span>
  )
}

export default MonitorTypeBadge

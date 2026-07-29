import { useMemo } from 'react'

export type Vital = 'up' | 'degraded' | 'down' | 'idle'

const CHANNEL: Record<Vital, string> = {
  up: 'var(--vs-ecg)',
  degraded: 'var(--vs-amber)',
  down: 'var(--vs-flat)',
  idle: 'var(--vs-text-dim)',
}

// One ECG period (P wave · QRS complex · T wave) as offsets from the baseline,
// expressed as a fraction of the amplitude. A "down" trace is a near-flat line
// searching for a signal; "idle" is dead flat. This is the product's signature:
// a living service has a heartbeat, an outage flatlines.
type Pt = [x: number, amp: number] // amp: +up is a rise (toward top)
const BEAT: Pt[] = [
  [0, 0],
  [22, 0],
  [30, 0.16], // P
  [38, 0],
  [54, 0],
  [60, -0.14], // Q
  [66, 1.0], // R (spike)
  [72, -0.42], // S
  [80, 0],
  [98, 0],
  [110, 0.3], // T
  [122, 0],
  [160, 0],
]
const FLAT: Pt[] = [
  [0, 0],
  [120, 0],
  [126, 0.12], // faint blip — "still searching"
  [132, 0],
  [160, 0],
]

function buildPath(shape: Pt[], repeat: number, period: number, height: number): string {
  const mid = height / 2
  const amp = height * 0.36
  const parts: string[] = []
  for (let r = 0; r < repeat; r++) {
    const ox = r * period
    shape.forEach(([x, a], i) => {
      const px = (ox + x).toFixed(1)
      const py = (mid - a * amp).toFixed(1)
      parts.push(`${i === 0 && r === 0 ? 'M' : 'L'}${px} ${py}`)
    })
  }
  return parts.join(' ')
}

interface Props {
  status: Vital
  height?: number
  /** pixels of trace that scroll past per second (0 = static) */
  speed?: number
  strokeWidth?: number
  className?: string
  /** show the live sweep dot at the leading (right) edge */
  cursor?: boolean
}

/**
 * EcgTrace — an animated phosphor heartbeat. Scrolls right-to-left over the
 * screen graticule; the leading dot is the live cursor. Ambient motion is
 * frozen for `prefers-reduced-motion` (see index.css).
 */
export default function EcgTrace({
  status,
  height = 40,
  speed = 44,
  strokeWidth = 2,
  className,
  cursor = true,
}: Props) {
  const period = 160
  const repeat = 18
  const color = CHANNEL[status]
  const shape = status === 'up' || status === 'degraded' ? BEAT : FLAT
  const d = useMemo(() => buildPath(shape, repeat, period, height), [shape, height])
  // Degraded beats slower and weaker; down "searches" slowly.
  const pxPerSec = status === 'down' ? Math.min(speed, 22) : status === 'degraded' ? speed * 0.7 : speed
  const durationS = pxPerSec > 0 ? period / pxPerSec : 0

  return (
    <div
      className={`relative overflow-hidden ${className ?? ''}`}
      style={{ color }}
      aria-hidden
    >
      <svg width="100%" height={height} className="block">
        <g
          className={durationS > 0 ? 'vs-ecg-scroll' : undefined}
          style={
            { ['--vs-period' as string]: `${period}px`, animationDuration: `${durationS}s` } as React.CSSProperties
          }
        >
          <path
            d={d}
            fill="none"
            stroke="currentColor"
            strokeWidth={strokeWidth}
            strokeLinejoin="round"
            strokeLinecap="round"
            style={{ filter: 'drop-shadow(0 0 3px currentColor)' }}
          />
        </g>
      </svg>
      {cursor && (
        <span
          className="vs-pulse pointer-events-none absolute right-0 top-1/2 h-2 w-2 -translate-y-1/2 rounded-full"
          style={{ backgroundColor: color, boxShadow: `0 0 8px 1px ${color}` }}
        />
      )}
    </div>
  )
}

/** Map a monitor's status flags to a vital-sign channel. */
export function vitalOf(status: string | undefined, inMaintenance?: boolean): Vital {
  if (inMaintenance) return 'degraded'
  if (status === 'online') return 'up'
  if (status === 'offline') return 'down'
  return 'idle'
}

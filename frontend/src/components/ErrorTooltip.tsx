import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { AlertTriangle } from 'lucide-react'

interface Props {
  /** The full message. Nothing renders if this is empty. */
  message: string
  /** Screen-reader and native-tooltip label for the trigger. */
  label: string
}

/** Gap between the trigger and the bubble, in pixels. */
const OFFSET = 8
/** Keeps the bubble off the viewport edges. */
const MARGIN = 8

/**
 * A warning icon that reveals its message on hover, focus or tap.
 *
 * The bubble is rendered into document.body rather than beside the icon
 * because these live in a table inside an `overflow-x-auto` wrapper: a bubble
 * positioned within that wrapper is clipped at the edge, or widens the scroll
 * area and produces a horizontal scrollbar. Fixed positioning from the
 * trigger's own rectangle sidesteps both.
 *
 * The trigger is a real button so it is reachable by keyboard and usable on
 * touch, where hover does not exist. `title` is deliberately left on it as
 * well: if JavaScript positioning ever fails, the message is still readable.
 */
export default function ErrorTooltip({ message, label }: Props) {
  const triggerRef = useRef<HTMLButtonElement>(null)
  const bubbleRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)

  const place = useCallback(() => {
    const trigger = triggerRef.current
    const bubble = bubbleRef.current
    if (!trigger || !bubble) return

    const t = trigger.getBoundingClientRect()
    const b = bubble.getBoundingClientRect()

    // Above by default, below when there is not room above.
    let top = t.top - b.height - OFFSET
    if (top < MARGIN) top = t.bottom + OFFSET

    // Centred on the icon, then pulled back inside the viewport.
    let left = t.left + t.width / 2 - b.width / 2
    const maxLeft = window.innerWidth - b.width - MARGIN
    left = Math.max(MARGIN, Math.min(left, maxLeft))

    setPos({ top, left })
  }, [])

  // Measured after the bubble is in the DOM but before paint, so it never
  // appears at the wrong place first.
  useLayoutEffect(() => {
    if (open) place()
  }, [open, place])

  useEffect(() => {
    if (!open) return
    const close = () => setOpen(false)
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    // Scrolling or resizing moves the trigger out from under a fixed bubble,
    // so close rather than chase it.
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    document.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!message) return null

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={message}
        className="inline-flex cursor-help rounded text-amber-400 transition hover:text-amber-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400/60"
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onClick={(e) => {
          // Taps have no hover, so make the icon toggle the bubble.
          e.stopPropagation()
          setOpen((v) => !v)
        }}
      >
        <AlertTriangle className="h-4 w-4" aria-hidden />
      </button>

      {open &&
        createPortal(
          <div
            ref={bubbleRef}
            role="tooltip"
            className="pointer-events-none fixed z-[100] max-w-xs rounded-lg border border-white/10 bg-slate-800 px-3 py-2 text-sm text-white shadow-xl"
            style={{
              top: pos?.top ?? 0,
              left: pos?.left ?? 0,
              // Hidden until measured, so it cannot flash in the corner.
              visibility: pos ? 'visible' : 'hidden',
            }}
          >
            {message}
          </div>,
          document.body,
        )}
    </>
  )
}

/**
 * Drops the leading "domain: " the backend prefixes onto check errors. The
 * domain is already the first column, so repeating it in the bubble spends
 * width on something the reader can see.
 */
export function trimDomainPrefix(message: string, domain: string): string {
  const prefix = `${domain}: `
  return message.startsWith(prefix) ? message.slice(prefix.length) : message
}

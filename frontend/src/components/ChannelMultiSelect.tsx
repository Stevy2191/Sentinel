import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import { CHANNEL_META, type ChannelName } from '@/hooks/useNotificationConfig'

export interface SelectableChannel {
  id: string
  name: string
  channel: ChannelName
}

interface Props {
  channels: SelectableChannel[]
  /** Ids of the selected channels. */
  selected: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
  /** Labels the control for assistive technology. */
  label: string
}

/** Gap between the trigger and the panel, in pixels. */
const GAP = 4
/** Keeps the panel off the viewport edges. */
const MARGIN = 8

/** "Select channels" / "Slack — alerts" / "3 channels selected" */
function summarise(channels: SelectableChannel[], selected: string[]): string {
  if (selected.length === 0) return 'Select channels'
  if (selected.length === 1) {
    const one = channels.find((c) => c.id === selected[0])
    return one ? one.name : '1 channel selected'
  }
  return `${selected.length} channels selected`
}

/**
 * A multi-select dropdown for notification channels.
 *
 * The panel is portalled to the body and positioned fixed rather than
 * absolutely inside the field. These live in modals whose bodies scroll, and
 * an absolutely positioned panel is clipped at the modal's edge — a dropdown
 * near the bottom of the form would open into nothing.
 *
 * A native `<select multiple>` would avoid all of that, but it renders as a
 * scrolling list box that ignores the surrounding styling and requires
 * ctrl-click to select more than one, which is not discoverable.
 */
export default function ChannelMultiSelect({
  channels,
  selected,
  onChange,
  disabled = false,
  label,
}: Props) {
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number; width: number } | null>(null)

  const place = useCallback(() => {
    const trigger = triggerRef.current
    const panel = panelRef.current
    if (!trigger || !panel) return

    const t = trigger.getBoundingClientRect()
    const height = panel.offsetHeight

    // Below by default, above when there is not room below but there is above.
    let top = t.bottom + GAP
    if (top + height > window.innerHeight - MARGIN && t.top - height - GAP > MARGIN) {
      top = t.top - height - GAP
    }
    setPos({ top, left: t.left, width: t.width })
  }, [])

  useLayoutEffect(() => {
    if (open) place()
  }, [open, place, selected.length])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    const onPointer = (e: PointerEvent) => {
      const target = e.target as Node
      if (!triggerRef.current?.contains(target) && !panelRef.current?.contains(target)) {
        setOpen(false)
      }
    }
    // Closes rather than chases: the panel is fixed, so scrolling the modal
    // would otherwise leave it floating away from its field.
    const close = () => setOpen(false)
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onPointer)
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onPointer)
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
    }
  }, [open])

  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((c) => c !== id) : [...selected, id])

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={label}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-2 rounded-lg border border-white/10 bg-slate-800/50 px-4 py-2 text-left text-white transition hover:border-white/20 focus:border-white/30 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      >
        <span className={`truncate text-sm ${selected.length ? 'text-white' : 'text-slate-500'}`}>
          {summarise(channels, selected)}
        </span>
        <ChevronDown
          className={`h-4 w-4 shrink-0 text-slate-400 transition-transform ${open ? 'rotate-180' : ''}`}
          aria-hidden
        />
      </button>

      {open &&
        createPortal(
          <div
            ref={panelRef}
            role="listbox"
            aria-multiselectable="true"
            aria-label={label}
            className="fixed z-[100] max-h-64 overflow-y-auto rounded-lg border border-white/10 bg-slate-800 p-1 shadow-xl"
            style={{
              top: pos?.top ?? 0,
              left: pos?.left ?? 0,
              width: pos?.width ?? 'auto',
              visibility: pos ? 'visible' : 'hidden',
            }}
          >
            {channels.map((c) => {
              const on = selected.includes(c.id)
              return (
                <button
                  key={c.id}
                  type="button"
                  role="option"
                  aria-selected={on}
                  onClick={() => toggle(c.id)}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition hover:bg-white/5"
                >
                  <span
                    className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
                      on ? 'border-emerald-500 bg-emerald-500' : 'border-white/25'
                    }`}
                    aria-hidden
                  >
                    {on && <Check className="h-3 w-3 text-slate-900" />}
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-sm text-white">{c.name}</span>
                    <span className="block text-xs text-slate-500">
                      {CHANNEL_META[c.channel].label}
                    </span>
                  </span>
                </button>
              )
            })}
          </div>,
          document.body,
        )}
    </>
  )
}

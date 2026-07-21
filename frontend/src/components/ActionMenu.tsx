import { useEffect, useRef, useState, type ComponentType } from 'react'
import { MoreVertical } from 'lucide-react'

export interface ActionItem {
  key: string
  label: string
  icon: ComponentType<{ className?: string }>
  onClick: () => void
  danger?: boolean
  disabled?: boolean
}

/**
 * ActionMenu renders a "more" (⋮) button that opens a dropdown of actions. Used
 * on mobile where per-card action buttons don't fit; desktop shows the same
 * actions as inline buttons instead.
 */
export default function ActionMenu({ items, label = 'Actions' }: { items: ActionItem[]; label?: string }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="btn-secondary !px-2 !py-1"
      >
        <MoreVertical className="h-4 w-4" />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 z-30 mt-1 w-48 overflow-hidden rounded-md border border-neutral-200 bg-white py-1 shadow-card dark:border-neutral-700 dark:bg-neutral-800"
        >
          {items.map((item) => (
            <button
              key={item.key}
              role="menuitem"
              disabled={item.disabled}
              onClick={() => {
                setOpen(false)
                item.onClick()
              }}
              className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-neutral-100 disabled:opacity-50 dark:hover:bg-neutral-700 ${
                item.danger ? 'text-error-600' : ''
              }`}
            >
              <item.icon className="h-4 w-4" /> {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

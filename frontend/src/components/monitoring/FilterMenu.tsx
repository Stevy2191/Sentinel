import { useEffect, useId, useRef, useState } from 'react'
import { ChevronDown } from 'lucide-react'

export interface FilterOption {
  value: string
  label: string
  /** How many rows choosing it would show. */
  n?: number
  /** Indented under the option above (a check or device type). */
  child?: boolean
  separatorBefore?: boolean
}

interface Props {
  label: string
  options: FilterOption[]
  selected: string[]
  /** Checkboxes when true; otherwise one choice, '' meaning All. */
  multi: boolean
  onChange: (next: string[]) => void
  /** A small first line, e.g. "Uptime checks only". */
  hint?: string
}

/** A filter button that opens a list of choices with their counts. The button
 *  says how many are chosen ("Kind · 2"). Closes on an outside click or
 *  Escape. */
export default function FilterMenu({ label, options, selected, multi, onChange, hint }: Props) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const id = useId()

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const set = selected.length
  const toggle = (v: string) => {
    if (multi) onChange(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v])
    else onChange(v ? [v] : [])
  }

  return (
    <div className="relative" ref={box}>
      <button
        type="button"
        className={`flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm transition ${
          set ? 'border-primary-500/50 bg-primary-500/10 text-white' : 'border-white/10 bg-slate-800/60 text-slate-300 hover:text-white'
        }`}
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
      >
        {set ? `${label} · ${set}` : label}
        <ChevronDown className="h-3.5 w-3.5 text-slate-400" />
      </button>
      {open && (
        <div
          id={id}
          role="group"
          aria-label={label}
          className="absolute left-0 z-30 mt-1 max-h-[60vh] min-w-[220px] overflow-auto rounded-lg border border-white/10 bg-slate-900 p-1.5 shadow-xl"
        >
          {hint && <div className="px-2 pb-1 pt-0.5 text-xs text-slate-500">{hint}</div>}
          {!multi && (
            <Choice multi={false} name={id} label="All" checked={set === 0} onToggle={() => toggle('')} />
          )}
          {options.map((o) => (
            <div key={o.value}>
              {o.separatorBefore && <div className="my-1 border-t border-white/5" />}
              <Choice
                multi={multi}
                name={id}
                label={o.label}
                n={o.n}
                child={o.child}
                checked={selected.includes(o.value)}
                onToggle={() => toggle(o.value)}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function Choice(p: { multi: boolean; name: string; label: string; n?: number; child?: boolean; checked: boolean; onToggle: () => void }) {
  // An option that would show nothing is dimmed, not disabled: it can still
  // be chosen, and a chosen one must stay easy to untick.
  const dim = p.n === 0 && !p.checked
  return (
    <label
      className={`flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-white/5 ${p.child ? 'pl-7' : ''} ${
        dim ? 'text-slate-500' : 'text-slate-200'
      }`}
    >
      <input
        type={p.multi ? 'checkbox' : 'radio'}
        name={p.multi ? undefined : p.name}
        checked={p.checked}
        onChange={p.onToggle}
        className="accent-primary-500"
      />
      <span className="flex-1">{p.label}</span>
      {p.n !== undefined && <span className="tabular-nums text-xs text-slate-500">{p.n}</span>}
    </label>
  )
}

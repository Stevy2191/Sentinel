import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2, Search } from 'lucide-react'
import { endpointHref, endpointTitle, locationLine, useEndpointSearch, type Endpoint } from '@/hooks/useEndpoints'
import { useDismissOnOutsideClick } from '@/hooks/useDismissOnOutsideClick'
import { timeAgo } from '@/utils/network'
import { KIND_ICON } from '@/components/network/endpointKind'

const SHOWN = 8

/** The top bar's search: a name, IP or MAC (or part of one) finds the
 *  device, and choosing it opens its switch with the device highlighted. */
export default function FindDevice() {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const { results, loading, error, query } = useEndpointSearch(q)
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const listId = useId()
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismissOnOutsideClick(open, close)
  const shown = results.slice(0, SHOWN)
  const typed = q.trim()
  const settled = !loading && query === typed

  useEffect(() => setActive(0), [query])

  const go = (e: Endpoint) => {
    const href = endpointHref(e)
    if (!href) return
    setOpen(false)
    setQ('')
    inputRef.current?.blur()
    navigate(href)
  }

  const onKeyDown = (ev: React.KeyboardEvent<HTMLInputElement>) => {
    switch (ev.key) {
      case 'ArrowDown':
        ev.preventDefault()
        setOpen(true)
        setActive((a) => Math.min(a + 1, Math.max(shown.length - 1, 0)))
        break
      case 'ArrowUp':
        ev.preventDefault()
        setActive((a) => Math.max(a - 1, 0))
        break
      case 'Enter':
        // Not while the results are still the previous query's.
        if (settled && shown[active]) {
          ev.preventDefault()
          go(shown[active])
        }
        break
      case 'Escape':
        setOpen(false)
        inputRef.current?.blur()
        break
    }
  }

  return (
    <div ref={ref} className="relative w-full max-w-md">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" aria-hidden />
      <input
        ref={inputRef}
        type="search"
        value={q}
        onChange={(e) => {
          setQ(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
        placeholder="Find a device by name, IP or MAC"
        aria-label="Find a device by name, IP or MAC"
        role="combobox"
        aria-expanded={open && !!typed}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && shown[active] ? `${listId}-${active}` : undefined}
        className="w-full rounded-md border border-white/10 bg-slate-900/60 py-1.5 pl-9 pr-8 text-sm text-slate-200 placeholder-slate-500 focus:border-primary-500 focus:outline-none"
      />
      {loading && <Loader2 className="absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-slate-500" aria-hidden />}
      {open && typed && (
        <div className="absolute left-0 right-0 top-full z-40 mt-1 overflow-hidden rounded-lg border border-white/10 bg-slate-900 shadow-xl">
          {error && <p className="px-3 py-2 text-sm text-red-400">{error}</p>}
          {!error && settled && results.length === 0 && (
            <p className="px-3 py-2 text-sm text-slate-400">No device matches “{typed}”.</p>
          )}
          {!error && !settled && results.length === 0 && <p className="px-3 py-2 text-sm text-slate-500">Searching…</p>}
          {shown.length > 0 && (
            <ul id={listId} role="listbox" className="max-h-[70vh] overflow-y-auto py-1">
              {shown.map((e, i) => {
                const Icon = KIND_ICON[e.how === 'behind' ? 'behind' : e.kind]
                const facts = [e.name ? e.ips[0] : '', e.name || e.ips[0] ? e.mac : '', e.maker].filter(Boolean)
                return (
                  <li key={e.mac} id={`${listId}-${i}`} role="option" aria-selected={i === active}>
                    <button
                      type="button"
                      onMouseDown={(ev) => ev.preventDefault()}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => go(e)}
                      className={`flex w-full items-start gap-3 px-3 py-2 text-left ${i === active ? 'bg-white/5' : ''}`}
                    >
                      <Icon className="mt-0.5 h-4 w-4 shrink-0 text-slate-400" aria-hidden />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm text-slate-100">{endpointTitle(e)}</span>
                        {facts.length > 0 && <span className="block truncate text-xs text-slate-500">{facts.join(' · ')}</span>}
                        <span className="block truncate text-xs text-slate-400">{locationLine(e)}</span>
                      </span>
                      <span className="shrink-0 text-xs text-slate-500">{timeAgo(e.last_seen)}</span>
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
          {results.length > SHOWN && (
            <p className="border-t border-white/5 px-3 py-1.5 text-xs text-slate-500">
              Showing {SHOWN} of {results.length >= 50 ? '50+' : results.length}. Type more to narrow it down.
            </p>
          )}
        </div>
      )}
    </div>
  )
}

import { useState } from 'react'
import type { MapDevice, MapLink } from '@/types/siteMap'

interface Props {
  hiddenDevices: MapDevice[]
  hiddenLinks: MapLink[]
  candidates: MapDevice[]
  names: Map<string, string>
  busy: boolean
  onAddLink: () => void
  onShowDevice: (id: string) => void
  onShowLink: (key: string) => void
  onAddDevice: (id: string) => void
  onReset: () => void
}

/** The edit mode's bar: add a link, bring back what was hidden, put a
 *  device of another type on the map, or go back to the automatic layout. */
export default function EditBar(p: Props) {
  const [open, setOpen] = useState<'hidden' | 'add' | null>(null)
  const [confirmReset, setConfirmReset] = useState(false)
  const hiddenCount = p.hiddenDevices.length + p.hiddenLinks.length
  const linkName = (l: MapLink) => `${p.names.get(l.a.device_id) ?? '?'} ↔ ${p.names.get(l.b.device_id) ?? '?'}`
  return (
    <div className="relative flex flex-wrap items-center gap-2 rounded-lg border border-sky-700 bg-sky-950/30 px-3 py-2 text-sm" aria-label="Editing">
      <b className="font-medium text-white">Editing</b>
      <button type="button" className="btn-secondary" disabled={p.busy} onClick={p.onAddLink}>
        + Add link
      </button>
      <button type="button" className="btn-secondary" onClick={() => setOpen(open === 'hidden' ? null : 'hidden')} aria-expanded={open === 'hidden'}>
        Hidden ({hiddenCount})
      </button>
      {p.candidates.length > 0 && (
        <button type="button" className="btn-secondary" onClick={() => setOpen(open === 'add' ? null : 'add')} aria-expanded={open === 'add'}>
          Add a device
        </button>
      )}
      {confirmReset ? (
        <span className="flex items-center gap-2 text-slate-300">
          Go back to the automatic layout?
          <button
            type="button"
            className="rounded bg-red-600 px-2 py-1 text-white hover:bg-red-500"
            disabled={p.busy}
            onClick={() => {
              setConfirmReset(false)
              p.onReset()
            }}
          >
            Reset
          </button>
          <button type="button" className="text-slate-400 hover:text-white" onClick={() => setConfirmReset(false)}>
            Cancel
          </button>
        </span>
      ) : (
        <button type="button" className="btn-secondary" onClick={() => setConfirmReset(true)}>
          Reset layout
        </button>
      )}
      <span className="ml-auto text-xs text-sky-300">Drag switches to move them · changes save as you go</span>

      {open && (
        <div className="absolute left-3 top-full z-20 mt-1 w-80 rounded-lg border border-white/10 bg-slate-900 p-2 shadow-xl">
          {open === 'hidden' &&
            (hiddenCount === 0 ? (
              <p className="p-2 text-sm text-slate-400">Nothing is hidden.</p>
            ) : (
              <ul className="space-y-1">
                {p.hiddenDevices.map((d) => (
                  <li key={d.id} className="flex items-center justify-between gap-2 rounded px-2 py-1 hover:bg-white/5">
                    <span className="truncate">{d.name}</span>
                    <button type="button" className="text-xs text-sky-400 hover:underline" disabled={p.busy} onClick={() => p.onShowDevice(d.id)}>
                      Show
                    </button>
                  </li>
                ))}
                {p.hiddenLinks.map((l) => (
                  <li key={l.key} className="flex items-center justify-between gap-2 rounded px-2 py-1 hover:bg-white/5">
                    <span className="truncate">{linkName(l)}</span>
                    <button type="button" className="text-xs text-sky-400 hover:underline" disabled={p.busy} onClick={() => p.onShowLink(l.key)}>
                      Show
                    </button>
                  </li>
                ))}
              </ul>
            ))}
          {open === 'add' && (
            <ul className="space-y-1">
              <li className="px-2 pb-1 text-xs text-slate-500">Devices of other types at this site, such as a firewall filed as Other</li>
              {p.candidates.map((d) => (
                <li key={d.id} className="flex items-center justify-between gap-2 rounded px-2 py-1 hover:bg-white/5">
                  <span className="truncate">
                    {d.name} <span className="text-xs text-slate-500">{d.type}</span>
                  </span>
                  <button
                    type="button"
                    className="text-xs text-sky-400 hover:underline"
                    disabled={p.busy}
                    onClick={() => {
                      setOpen(null)
                      p.onAddDevice(d.id)
                    }}
                  >
                    Add
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

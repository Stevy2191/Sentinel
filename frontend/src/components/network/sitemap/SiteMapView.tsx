import { useEffect, useMemo, useState } from 'react'
import { Maximize2, Pencil, Share2 } from 'lucide-react'
import MapCanvas, { type MapSelection } from '@/components/network/sitemap/MapCanvas'
import LinkPanel from '@/components/network/sitemap/LinkPanel'
import DevicePanel from '@/components/network/sitemap/DevicePanel'
import EditBar from '@/components/network/sitemap/EditBar'
import AddLinkPanel from '@/components/network/sitemap/AddLinkPanel'
import { useSiteMapActions, type LiveState, type MapEndInput } from '@/hooks/useSiteMap'
import { useMapFolds, useMapView } from '@/hooks/useSiteMapPrefs'
import { NO_DATA_MS, ago } from '@/utils/siteMap'
import type { MapSnapshot, MapTraffic } from '@/types/siteMap'

function usePhone(): boolean {
  const q = '(max-width: 767px)'
  const [phone, setPhone] = useState(() => typeof window !== 'undefined' && window.matchMedia?.(q).matches)
  useEffect(() => {
    const m = window.matchMedia?.(q)
    if (!m) return
    const on = () => setPhone(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return !!phone
}

/** "Live", "Updated 2 min ago", "Reconnecting…": what the toolbar says. */
function liveText(live: LiveState, traffic: MapTraffic | null, now: number): { text: string; ok: boolean } {
  if (live.kind === 'reconnecting') return { text: 'Reconnecting…', ok: false }
  if (live.kind === 'stopped') return { text: live.error, ok: false }
  if (live.kind === 'connecting') return { text: 'Connecting…', ok: false }
  const at = traffic?.at ?? null
  if (!at || now - new Date(at).getTime() > NO_DATA_MS) return { text: ago(at, now), ok: false }
  return { text: 'Live', ok: true }
}

interface Props {
  map: MapSnapshot
  traffic: MapTraffic | null
  live: LiveState
  /** Where folds are remembered: the site id, or "public:<token>". */
  mapKey: string
  publicToken?: string
  initialDevice?: string | null
  onFullscreen?: () => void
  onShare?: () => void
  /** The canvas box's height (CSS). */
  height: string
}

type Adding = { changing: string | null; a: MapEndInput | null; b: MapEndInput | null; error: string | null }

/** The site map with its toolbar, edit bar, side panel and legend: the map
 *  page, its wall display and the public map all show this. */
export default function SiteMapView(p: Props) {
  const { map, traffic } = p
  const [view, setView] = useMapView()
  const [folds, toggleFold] = useMapFolds(p.mapKey)
  const phone = usePhone()
  const canEdit = map.can_edit && !p.publicToken
  const [editing, setEditing] = useState(false)
  const [selected, setSelected] = useState<MapSelection | null>(p.initialDevice ? { kind: 'device', id: p.initialDevice } : null)
  const [adding, setAdding] = useState<Adding | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const actions = useSiteMapActions(map.site_id)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [])
  useEffect(() => {
    if (phone) setEditing(false)
  }, [phone])

  const devices = useMemo(() => new Map(map.devices.map((d) => [d.id, d] as const)), [map.devices])
  const names = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of [...map.devices, ...map.hidden_devices, ...map.candidates]) m.set(d.id, d.name)
    return m
  }, [map])
  const selLink = selected?.kind === 'link' ? map.links.find((l) => l.key === selected.id) : undefined
  const selDevice = selected?.kind === 'device' ? devices.get(selected.id) : undefined

  const run = (f: Promise<void>, after?: () => void) => {
    setErr(null)
    f.then(after).catch((e: Error) => setErr(e.message))
  }
  const pick = (deviceId: string, ifIndex: number | null) => {
    setAdding((cur) => {
      if (!cur) return cur
      const end = { device_id: deviceId, if_index: ifIndex }
      if (!cur.a || cur.a.device_id === deviceId) return { ...cur, a: end, error: null }
      return { ...cur, b: end, error: null }
    })
  }
  const saveLink = () => {
    if (!adding?.a || !adding.b) return
    const f = adding.changing ? actions.updateLink(adding.changing, adding.a, adding.b) : actions.addLink(adding.a, adding.b)
    f.then(() => setAdding(null)).catch((e: Error) => setAdding((cur) => (cur ? { ...cur, error: e.message } : cur)))
  }

  const status = liveText(p.live, traffic, now)
  const down = map.devices.filter((d) => (traffic?.devices[d.id] ?? d.status) === 'down').length

  let side: React.ReactNode = null
  if (adding) {
    side = (
      <AddLinkPanel
        changing={!!adding.changing}
        a={adding.a}
        b={adding.b}
        names={names}
        busy={actions.busy}
        error={adding.error}
        onChange={(a, b) => setAdding((cur) => (cur ? { ...cur, a, b } : cur))}
        onSave={saveLink}
        onCancel={() => setAdding(null)}
      />
    )
  } else if (selLink) {
    side = (
      <LinkPanel
        link={selLink}
        devices={devices}
        rate={traffic?.links[selLink.key]}
        canEdit={canEdit}
        isPublic={!!p.publicToken}
        busy={actions.busy}
        onClose={() => setSelected(null)}
        onHide={() => run(actions.setLinkHidden(selLink.key, true), () => setSelected(null))}
        onKeep={() => run(actions.keep(selLink.key))}
        onDelete={() => selLink.manual_id && run(actions.deleteLink(selLink.manual_id), () => setSelected(null))}
        onChangePorts={() =>
          setAdding({
            changing: selLink.manual_id,
            a: { device_id: selLink.a.device_id, if_index: selLink.a.if_index },
            b: { device_id: selLink.b.device_id, if_index: selLink.b.if_index },
            error: null,
          })
        }
      />
    )
  } else if (selDevice) {
    side = (
      <DevicePanel
        device={selDevice}
        links={map.links.filter((l) => l.a.device_id === selDevice.id || l.b.device_id === selDevice.id)}
        devices={devices}
        canEdit={canEdit}
        isPublic={!!p.publicToken}
        busy={actions.busy}
        onClose={() => setSelected(null)}
        onHide={() => run(actions.setDevice(selDevice.id, { hidden: true }), () => setSelected(null))}
      />
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="mr-auto text-sm text-slate-400">
          {map.devices.length} routers and switches · {map.links.length} links
          {down > 0 && <span className="text-red-300"> · {down} down</span>}
        </span>
        <span className="flex items-center gap-1.5 text-xs text-slate-400" role="status" aria-live="polite">
          <span className={`h-2 w-2 rounded-full ${status.ok ? 'bg-emerald-500 shadow-[0_0_0_3px_rgba(34,197,94,.18)]' : 'bg-amber-400'}`} />
          {status.text}
        </span>
        <div className="inline-flex overflow-hidden rounded-lg border border-white/10" role="group" aria-label="View">
          {(['realistic', 'simple'] as const).map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={view === v}
              className={`px-3 py-1.5 text-sm ${view === v ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white'}`}
              onClick={() => setView(v)}
            >
              {v === 'realistic' ? 'Realistic' : 'Simple'}
            </button>
          ))}
        </div>
        {canEdit && !phone && (
          <button
            type="button"
            className={`btn-secondary flex items-center gap-1.5 ${editing ? 'border-sky-600 bg-sky-900/50' : ''}`}
            aria-pressed={editing}
            onClick={() => {
              setEditing((e) => !e)
              setAdding(null)
            }}
          >
            <Pencil className="h-4 w-4" /> {editing ? 'Done' : 'Edit'}
          </button>
        )}
        {p.onFullscreen && (
          <button type="button" className="btn-secondary flex items-center gap-1.5" onClick={p.onFullscreen}>
            <Maximize2 className="h-4 w-4" /> Fullscreen
          </button>
        )}
        {p.onShare && (
          <button type="button" className="btn-secondary flex items-center gap-1.5" onClick={p.onShare}>
            <Share2 className="h-4 w-4" /> Share
          </button>
        )}
      </div>
      {phone && canEdit && <p className="text-xs text-slate-500">View only on a phone · drag to move around, pinch to zoom</p>}
      {editing && (
        <EditBar
          hiddenDevices={map.hidden_devices}
          hiddenLinks={map.hidden_links}
          candidates={map.candidates}
          names={names}
          busy={actions.busy}
          onAddLink={() => {
            setSelected(null)
            setAdding({ changing: null, a: null, b: null, error: null })
          }}
          onShowDevice={(id) => run(actions.setDevice(id, { hidden: false }))}
          onShowLink={(key) => run(actions.setLinkHidden(key, false))}
          onAddDevice={(id) => run(actions.setDevice(id, { added: true }))}
          onReset={() => run(actions.reset())}
        />
      )}
      {err && <p className="text-sm text-red-400">{err}</p>}
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start">
        {/* lg:flex-1 only: in the phone column a flex-1 basis would squash the box to nothing. */}
        <div className="w-full min-w-0 overflow-hidden rounded-xl border border-white/10 lg:flex-1" style={{ height: p.height }}>
          {map.devices.length === 0 ? (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-slate-400">
              No routers or switches are monitored at this site yet.
            </div>
          ) : (
            <MapCanvas
              map={map}
              traffic={traffic}
              view={view}
              editing={editing}
              picking={!!adding}
              publicToken={p.publicToken}
              folds={folds}
              onFold={toggleFold}
              selected={selected}
              onSelect={(s) => !adding && setSelected(s)}
              onMove={(id, x, y) => run(actions.setPositions([{ device_id: id, x, y }]))}
              onPick={pick}
              fitKey={`${view}:${side ? 1 : 0}`}
            />
          )}
        </div>
        {side && <aside className="w-full shrink-0 rounded-xl border border-white/10 bg-slate-900 p-4 lg:w-[330px]">{side}</aside>}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-500">
        <span>
          <i className="mr-1.5 inline-block h-1 w-5 rounded bg-slate-500 align-middle" />
          idle
        </span>
        <span>
          <i className="mr-1.5 inline-block h-1 w-5 rounded bg-emerald-500 align-middle" />
          under half the busy threshold
        </span>
        <span>
          <i className="mr-1.5 inline-block h-1 w-5 rounded bg-amber-500 align-middle" />
          getting busy
        </span>
        <span>
          <i className="mr-1.5 inline-block h-1 w-5 rounded bg-red-500 align-middle" />
          at the busy threshold
        </span>
        <span>dashed amber: guessed from a MAC table · dashed grey: device down · width: link speed</span>
      </div>
    </div>
  )
}

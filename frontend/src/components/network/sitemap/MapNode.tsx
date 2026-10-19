import { useEffect } from 'react'
import { Network, Router } from 'lucide-react'
import { useMapDevicePorts } from '@/hooks/useSiteMap'
import MapDrawing, { drawnUnits } from '@/components/network/sitemap/MapDrawing'
import { TIER_SCALE, type Placed } from '@/utils/siteMap'
import type { MapDevice } from '@/types/siteMap'

/** How long a device has been down: "Down 4 min", "Down 3 h". */
// eslint-disable-next-line react-refresh/only-export-components
export function downFor(since: string | null, now: number): string {
  if (!since) return 'Down'
  const min = Math.max(0, Math.round((now - new Date(since).getTime()) / 60_000))
  return min < 120 ? `Down ${min} min` : `Down ${Math.round(min / 60)} h`
}

/** A card's size: the top rows' cards are a little wider. */
// eslint-disable-next-line react-refresh/only-export-components
export function cardWidth(p: Placed): number {
  return p.tier === 'router' || p.tier === 'core' ? 320 : 290
}

interface Props {
  device: MapDevice
  placed: Placed
  view: 'realistic' | 'simple'
  publicToken?: string
  folded: boolean
  editing: boolean
  selected: boolean
  now: number
  onFold: () => void
  onPort: (ifIndex: number) => void
  /** The node's size changed (its drawing arrived): links re-measure. */
  onLayout: () => void
}

const ROLE: Record<string, string> = { router: 'Router', switch: 'Switch' }

/** One router or switch on the map: its realistic front panel, or a card,
 *  with its name, model and state above it. */
export default function MapNode({ device, placed, view, publicToken, folded, editing, selected, now, onFold, onPort, onLayout }: Props) {
  const ports = useMapDevicePorts(device.id, publicToken, view === 'realistic')
  const drawn = view === 'realistic' && drawnUnits(ports).length > 0
  useEffect(() => {
    onLayout()
    // Only when the drawing appears or goes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [drawn])

  const down = device.status === 'down'
  const ring = down ? 'outline outline-2 outline-offset-2 outline-red-500' : device.status === 'error' ? 'outline outline-2 outline-offset-2 outline-amber-400' : ''
  const dim = device.status === 'paused' ? 'opacity-50' : ''
  const sel = selected ? 'ring-2 ring-sky-400/70 ring-offset-2 ring-offset-slate-950' : ''
  const Icon = device.type === 'router' ? Router : Network
  const fold =
    // Branches fold from a distribution or access switch; folding at the
    // router or core would fold the whole map.
    (placed.tier === 'dist' || placed.tier === 'access') && (placed.below > 0 || folded) ? (
      <button
        type="button"
        data-fold
        className="pointer-events-auto rounded-full border border-slate-600 bg-slate-800 px-2 text-[11px] text-slate-300 hover:border-slate-400"
        onClick={(e) => {
          e.stopPropagation()
          onFold()
        }}
        aria-expanded={!folded}
      >
        {folded ? '▸' : '▾'} {placed.below || ''} below
      </button>
    ) : null

  return (
    <div
      data-node={device.id}
      className={`absolute select-none ${editing ? 'cursor-grab rounded outline-dashed outline-1 outline-offset-[6px] outline-sky-400/50' : 'cursor-pointer'} ${dim}`}
      style={{ left: placed.x, top: placed.y }}
      title={device.status === 'error' && device.status_detail ? device.status_detail : undefined}
    >
      {drawn && (
      <div className="pointer-events-none absolute -top-[22px] left-0 flex items-center gap-2 whitespace-nowrap text-xs">
        <span className={`h-2 w-2 rounded-full ${down ? 'bg-red-500' : device.status === 'up' ? 'bg-emerald-500' : 'bg-slate-500'}`} />
        <b className="font-semibold text-white">{device.name}</b>
        <span className="text-slate-400">{device.model}</span>
        {down && <span className="font-semibold text-red-300">{downFor(device.down_since, now)}</span>}
        {fold}
      </div>
      )}
      <div data-box className={`rounded ${ring} ${sel}`}>
        {drawn && ports ? (
          <MapDrawing ports={ports} scale={TIER_SCALE[placed.tier]} isPublic={!!publicToken} onPort={onPort} />
        ) : (
          <div className="flex h-11 items-center gap-2.5 rounded-lg border border-white/10 bg-slate-800 px-2.5" style={{ width: cardWidth(placed) }}>
            <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${device.type === 'router' ? 'bg-sky-500/15 text-sky-300' : 'bg-emerald-500/15 text-emerald-300'}`}>
              <Icon className="h-4 w-4" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12.5px] font-semibold text-white">{device.name}</span>
              <span className="block truncate text-[11px] text-slate-400">
                {ROLE[device.type] ?? 'Device'}
                {device.model ? ` · ${device.model}` : ''}
                {down ? ` · ${downFor(device.down_since, now)}` : ''}
              </span>
            </span>
            {fold}
          </div>
        )}
      </div>
    </div>
  )
}

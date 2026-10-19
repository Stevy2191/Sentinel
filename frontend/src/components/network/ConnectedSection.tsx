import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { endpointStale, useConnected, type Endpoint } from '@/hooks/useEndpoints'
import { timeAgo } from '@/utils/network'
import ConnectedMap from '@/components/network/ConnectedMap'
import EndpointPanel from '@/components/network/EndpointPanel'

type SortKey = 'port' | 'name' | 'ip' | 'mac' | 'maker' | 'vlan' | 'how' | 'seen'

const COLUMNS: { key: SortKey; label: string }[] = [
  { key: 'port', label: 'Port' },
  { key: 'name', label: 'Name' },
  { key: 'ip', label: 'IP' },
  { key: 'mac', label: 'MAC' },
  { key: 'maker', label: 'Maker' },
  { key: 'vlan', label: 'VLAN' },
  { key: 'how', label: 'How' },
  { key: 'seen', label: 'Last seen' },
]

function howText(e: Endpoint): string {
  switch (e.how) {
    case 'wireless':
      return `via ${e.via || 'AP'}`
    case 'behind':
      return 'behind a switch'
    case 'router':
      return 'ARP only'
    default:
      return 'wired'
  }
}

function sortValue(e: Endpoint, k: SortKey): string | number {
  switch (k) {
    case 'port':
      return e.if_index ?? Number.MAX_SAFE_INTEGER
    case 'name':
      return (e.name || '￿').toLowerCase()
    case 'ip':
      return e.ips[0] ? e.ips[0].split('.').map((p) => p.padStart(3, '0')).join('.') : '￿'
    case 'mac':
      return e.mac
    case 'maker':
      return (e.maker || '￿').toLowerCase()
    case 'vlan':
      return e.vlan
    case 'how':
      return howText(e)
    case 'seen':
      return -new Date(e.last_seen).getTime()
  }
}

/** A switch's (or router's) Connected section: what is plugged into it,
 *  as a map or a list. ?endpoint=<mac> (from Find a device) highlights a
 *  device and opens its record. */
export default function ConnectedSection({
  deviceId,
  deviceName,
  canEdit,
  highlight,
}: {
  deviceId: string
  deviceName: string
  canEdit: boolean
  highlight: string | null
}) {
  const { view, loading, error, refetch } = useConnected(deviceId)
  const [mode, setMode] = useState<'map' | 'list'>('map')
  const [selected, setSelected] = useState<string | null>(highlight)
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'port', desc: false })
  const ref = useRef<HTMLElement>(null)

  useEffect(() => {
    if (!highlight) return
    setSelected(highlight)
    ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [highlight])

  const all = useMemo(() => {
    if (!view) return []
    return [...view.endpoints, ...view.aps.flatMap((g) => g.clients), ...view.behind.flatMap((g) => g.endpoints ?? [])]
  }, [view])

  const rows = useMemo(() => {
    const f = filter.trim().toLowerCase()
    const hit = (e: Endpoint) =>
      !f || [e.name, e.mac, e.maker, e.port_label, howText(e), ...e.ips].some((s) => s.toLowerCase().includes(f))
    const out = all.filter(hit)
    out.sort((a, b) => {
      const x = sortValue(a, sort.key)
      const y = sortValue(b, sort.key)
      const c = x < y ? -1 : x > y ? 1 : a.mac.localeCompare(b.mac)
      return sort.desc ? -c : c
    })
    return out
  }, [all, filter, sort])

  const counts = view
    ? [
        `${view.endpoints.length} wired`,
        view.aps.length ? `${view.aps.length} AP${view.aps.length === 1 ? '' : 's'} (${view.aps.reduce((n, g) => n + g.clients.length, 0)} wireless)` : '',
        view.behind.length ? `${view.behind.reduce((n, g) => n + g.count, 0)} behind unmanaged switches` : '',
      ].filter(Boolean)
    : []

  return (
    <section ref={ref} className="scroll-mt-4 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-baseline gap-3">
          <h2 className="text-lg font-light text-white">Connected</h2>
          <div className="inline-flex overflow-hidden rounded-md border border-white/10 text-xs" role="group" aria-label="Connected view">
            {(['map', 'list'] as const).map((m) => (
              <button
                key={m}
                type="button"
                aria-pressed={mode === m}
                onClick={() => setMode(m)}
                className={`px-2.5 py-1 capitalize ${mode === m ? 'bg-primary-600 text-white' : 'text-slate-400 hover:text-slate-200'}`}
              >
                {m}
              </button>
            ))}
          </div>
          {counts.length > 0 && <span className="text-xs text-slate-500">{counts.join(' · ')}</span>}
        </div>
        {mode === 'list' && (
          <input
            type="search"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter"
            aria-label="Filter connected devices"
            className="w-48 rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-sm text-slate-200 placeholder-slate-500 focus:border-primary-500 focus:outline-none"
          />
        )}
      </div>

      {loading && !view && <p className="text-sm text-slate-400">Loading…</p>}
      {error && !view && <p className="text-sm text-red-400">{error}</p>}
      {view && all.length === 0 && view.aps.length === 0 && (
        <p className="card p-4 text-sm text-slate-400">
          No devices seen here yet. Sentinel reads the MAC address and ARP tables every 5 minutes.
        </p>
      )}

      {view && (all.length > 0 || view.aps.length > 0) && (
        <div className={`grid gap-3 ${selected ? 'lg:grid-cols-[minmax(0,1fr)_22rem]' : ''}`}>
          {mode === 'map' ? (
            <ConnectedMap view={view} deviceName={deviceName} highlight={highlight} selected={selected} onSelect={setSelected} onShowList={() => setMode('list')} />
          ) : (
            <div className="card overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-white/5 text-xs text-slate-500">
                  <tr>
                    {COLUMNS.map((c) => (
                      <th key={c.key} className="px-3 py-2 font-normal">
                        <button
                          type="button"
                          className="inline-flex items-center gap-1 hover:text-slate-300"
                          onClick={() => setSort((s) => ({ key: c.key, desc: s.key === c.key ? !s.desc : false }))}
                        >
                          {c.label}
                          {sort.key === c.key && (sort.desc ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />)}
                        </button>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((e) => (
                    <tr
                      key={e.mac}
                      onClick={() => setSelected(e.mac)}
                      className={`cursor-pointer border-b border-white/5 hover:bg-white/5 ${endpointStale(e) ? 'opacity-50' : ''} ${
                        e.mac === highlight || e.mac === selected ? 'bg-primary-500/10' : ''
                      }`}
                    >
                      <td className="px-3 py-1.5 text-slate-300">{e.port_label || '—'}</td>
                      <td className="px-3 py-1.5 text-slate-100">{e.name || '—'}</td>
                      <td className="px-3 py-1.5 tabular-nums text-slate-300">{e.ips.join(', ') || '—'}</td>
                      <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{e.mac}</td>
                      <td className="max-w-[14rem] truncate px-3 py-1.5 text-slate-400">{e.maker || '—'}</td>
                      <td className="px-3 py-1.5 text-slate-400">{e.vlan || '—'}</td>
                      <td className="px-3 py-1.5 text-slate-400">{howText(e)}</td>
                      <td className="whitespace-nowrap px-3 py-1.5 text-slate-400">{timeAgo(e.last_seen)}</td>
                    </tr>
                  ))}
                  {rows.length === 0 && (
                    <tr>
                      <td colSpan={COLUMNS.length} className="px-3 py-4 text-center text-sm text-slate-500">
                        Nothing matches “{filter}”.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
          {selected && <EndpointPanel mac={selected} canEdit={canEdit} onClose={() => setSelected(null)} onNamed={() => void refetch()} />}
        </div>
      )}
    </section>
  )
}

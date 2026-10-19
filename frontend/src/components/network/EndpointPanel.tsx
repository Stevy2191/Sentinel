import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, X } from 'lucide-react'
import { endpointTitle, locationLine, setEndpointName, useEndpoint } from '@/hooks/useEndpoints'
import type { ApiError } from '@/services/api'
import { timeAgo } from '@/utils/network'
import { KIND_ICON, KIND_LABEL } from '@/components/network/endpointKind'

/** One device's full record: addresses, maker, where it is, its last 20
 *  sightings, and (with edit access) its name. */
export default function EndpointPanel({
  mac,
  canEdit,
  onClose,
  onNamed,
}: {
  mac: string
  canEdit: boolean
  onClose: () => void
  onNamed: () => void
}) {
  const { endpoint: e, sightings, loading, error, refetch } = useEndpoint(mac)
  const [name, setName] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const userName = e?.name_source === 'user' ? e.name : ''
  useEffect(() => setName(userName), [userName, mac])

  const save = async (next: string) => {
    setSaving(true)
    setSaveError(null)
    try {
      await setEndpointName(mac, next)
      await refetch()
      onNamed()
    } catch (err) {
      setSaveError((err as ApiError).message || 'Could not save the name')
    } finally {
      setSaving(false)
    }
  }

  if (!e) {
    return (
      <div className="card flex items-center justify-between gap-3 p-4 text-sm">
        {loading ? <span className="text-slate-400">Loading…</span> : <span className="text-red-400">{error}</span>}
        <button type="button" onClick={onClose} className="text-slate-400 hover:text-white" aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
    )
  }
  const Icon = KIND_ICON[e.kind]
  const rows: [string, React.ReactNode][] = [
    ['IP address', e.ips.length ? e.ips.join(', ') : '—'],
    ['MAC', <span className="font-mono">{e.mac}</span>],
    ['Maker', e.maker || 'Unknown'],
    ['Kind', `${KIND_LABEL[e.kind]} (${e.maker ? 'a guess from the maker' : 'a guess'})`],
    [
      'Where',
      <span>
        {locationLine(e)}
        {e.device_id && e.if_index != null && (
          <Link to={`/network/devices/${e.device_id}/ports/${e.if_index}`} className="ml-2 text-primary-400 hover:underline">
            Open port →
          </Link>
        )}
      </span>,
    ],
    ['VLAN', e.vlan ? String(e.vlan) : '—'],
    ['First seen', new Date(e.first_seen).toLocaleString()],
    ['Last seen', `${timeAgo(e.last_seen)} (${new Date(e.last_seen).toLocaleString()})`],
  ]
  return (
    <div className="card space-y-4 p-4" aria-label="Device record">
      <div className="flex items-start gap-3">
        <Icon className="mt-0.5 h-5 w-5 shrink-0 text-slate-300" aria-hidden />
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium text-white">{endpointTitle(e)}</p>
          <p className="text-xs text-slate-500">
            {e.name_source === 'user' ? 'Your name for it' : e.name_source === 'dns' ? 'Name from DNS' : e.name_source === 'lldp' ? 'Name it announces (LLDP)' : 'No name yet'}
          </p>
        </div>
        <button type="button" onClick={onClose} className="text-slate-400 hover:text-white" aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-1.5 text-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-slate-500">{k}</dt>
            <dd className="min-w-0 break-words text-slate-200">{v}</dd>
          </div>
        ))}
      </dl>
      {canEdit && (
        <form
          className="space-y-1"
          onSubmit={(ev) => {
            ev.preventDefault()
            void save(name)
          }}
        >
          <label htmlFor={`name-${mac}`} className="text-xs uppercase tracking-widest text-slate-500">
            Name
          </label>
          <div className="flex gap-2">
            <input
              id={`name-${mac}`}
              value={name}
              maxLength={64}
              onChange={(ev) => setName(ev.target.value)}
              placeholder={e.name_source === 'dns' || e.name_source === 'lldp' ? e.name : 'Front desk phone'}
              className="min-w-0 flex-1 rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-sm text-slate-200 focus:border-primary-500 focus:outline-none"
            />
            <button type="submit" disabled={saving || name.trim() === userName} className="btn-primary px-3 py-1 text-sm disabled:opacity-50">
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save'}
            </button>
            {userName && (
              <button type="button" disabled={saving} onClick={() => void save('')} className="text-sm text-slate-400 hover:text-white">
                Clear
              </button>
            )}
          </div>
          {saveError && <p className="text-xs text-red-400">{saveError}</p>}
        </form>
      )}
      {sightings.length > 0 && (
        <div>
          <p className="mb-1 text-xs uppercase tracking-widest text-slate-500">Seen on</p>
          <table className="w-full text-left text-xs">
            <thead className="text-slate-500">
              <tr>
                <th className="py-1 font-normal">Switch</th>
                <th className="py-1 font-normal">Port</th>
                <th className="py-1 font-normal">VLAN</th>
                <th className="py-1 font-normal">Last seen</th>
              </tr>
            </thead>
            <tbody className="text-slate-300">
              {sightings.map((s) => (
                <tr key={`${s.device_id}-${s.if_index}-${s.vlan}`} className="border-t border-white/5">
                  <td className="py-1">{s.device_name}</td>
                  <td className="py-1">{s.port_label}</td>
                  <td className="py-1">{s.vlan || '—'}</td>
                  <td className="py-1">{timeAgo(s.last_seen)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

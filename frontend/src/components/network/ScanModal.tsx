import { useEffect, useMemo, useState } from 'react'
import { Loader2, X } from 'lucide-react'
import { useSiteScan, type ScanResult } from '@/hooks/useSiteScan'
import { useSiteCredentialOptions } from '@/hooks/useSnmpCredentials'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

export default function ScanModal({ siteId, onClose, onAdded }: { siteId: string; onClose: () => void; onAdded: () => void }) {
  const { options } = useSiteCredentialOptions(siteId, true)
  const { job, start, busy, error, addDevices } = useSiteScan(siteId)
  const [cidr, setCidr] = useState('')
  const [chosen, setChosen] = useState<string[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [message, setMessage] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  // Every profile available to the site is tried by default.
  useEffect(() => setChosen(options.map((o) => o.id)), [options])

  const results = useMemo(() => job?.results ?? [], [job])
  const addable = useMemo(() => results.filter((r) => !r.already_added), [results])
  const key = (r: ScanResult) => `${r.host}:${r.port}`

  const add = async () => {
    setAdding(true)
    setMessage(null)
    try {
      const rows = addable.filter((r) => picked.has(key(r)))
      const res = await addDevices(rows)
      setMessage(`Added ${res.added.length}${res.failed.length ? `; ${res.failed.length} failed: ${res.failed.map((f) => `${f.host} (${f.error})`).join(', ')}` : ''}.`)
      if (res.failed.length === 0) onAdded()
    } catch (err) {
      setMessage((err as ApiError).message || 'Could not add the devices')
    } finally {
      setAdding(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card flex max-h-[90vh] w-full max-w-3xl flex-col gap-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <div>
            <h2 className="text-lg font-semibold">Scan subnet</h2>
            <p className="text-sm text-slate-400">Tries each chosen profile on every address, stopping at the first that answers.</p>
          </div>
          <button className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {!job && (
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void start(cidr.trim(), chosen) }}>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Subnet</span>
              <input className={inputCls} value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.20.0.0/24 (a /22 at most)" required autoFocus />
            </label>
            <fieldset className="space-y-1">
              <legend className="text-sm text-slate-300">Credential profiles to try</legend>
              {options.length === 0 && <p className="text-sm text-slate-500">No profiles are available to this site; an admin can add one under Credentials.</p>}
              {options.map((o) => (
                <label key={o.id} className="flex items-center gap-2 text-sm text-slate-300">
                  <input type="checkbox" checked={chosen.includes(o.id)}
                    onChange={(e) => setChosen((c) => (e.target.checked ? [...c, o.id] : c.filter((x) => x !== o.id)))} />
                  {o.name} <span className="text-slate-500">(v{o.version})</span>
                </label>
              ))}
            </fieldset>
            {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
            <div className="flex justify-end">
              <button className="btn-primary" disabled={busy || chosen.length === 0 || !cidr.trim()}>
                {busy ? 'Starting…' : 'Scan'}
              </button>
            </div>
          </form>
        )}

        {job && (
          <>
            <div className="space-y-1">
              <div className="flex items-center justify-between text-sm text-slate-300">
                <span className="flex items-center gap-2">
                  {job.running && <Loader2 className="h-4 w-4 animate-spin" />}
                  {job.cidr}: {job.done} of {job.total} addresses checked, {results.length} answered
                </span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                <div className="h-full rounded-full bg-emerald-500 transition-all" style={{ width: `${job.total ? (100 * job.done) / job.total : 0}%` }} />
              </div>
            </div>
            {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

            <div className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-white/10">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-slate-900">
                  <tr className="text-left text-xs text-slate-400">
                    <th className="w-8 px-3 py-2">
                      <input type="checkbox" aria-label="Select all"
                        checked={addable.length > 0 && addable.every((r) => picked.has(key(r)))}
                        onChange={(e) => setPicked(e.target.checked ? new Set(addable.map(key)) : new Set())} />
                    </th>
                    <th className="px-3 py-2 font-medium">Address</th>
                    <th className="px-3 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">Vendor</th>
                    <th className="px-3 py-2 font-medium">Profile</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {[...results].sort((a, b) => a.host.localeCompare(b.host, undefined, { numeric: true })).map((r) => (
                    <tr key={key(r)} className={r.already_added ? 'opacity-50' : ''}>
                      <td className="px-3 py-2">
                        <input type="checkbox" disabled={r.already_added} checked={picked.has(key(r))}
                          onChange={(e) => setPicked((p) => { const n = new Set(p); if (e.target.checked) n.add(key(r)); else n.delete(key(r)); return n })} />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-slate-300">{r.host}</td>
                      <td className="px-3 py-2 text-slate-200" title={r.descr}>{r.name || '—'}{r.already_added && <span className="ml-2 text-xs text-slate-500">already added</span>}</td>
                      <td className="px-3 py-2 text-slate-400">{r.vendor || '—'}</td>
                      <td className="px-3 py-2 text-slate-400">{r.credential_name}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {message && <p className="text-sm text-slate-300">{message}</p>}
            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>Close</button>
              <button className="btn-primary" disabled={adding || picked.size === 0} onClick={() => void add()}>
                {adding ? 'Adding…' : `Add selected (${picked.size})`}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Copy, Plus, Trash2 } from 'lucide-react'
import { copyProfile, createProfile, deleteProfile, useProfiles, type ProfileView } from '@/hooks/useProfiles'
import ProfileFields from '@/components/network/ProfileFields'
import type { ProfileFieldsValue } from '@/utils/metrics'
import type { ApiError } from '@/services/api'

const EMPTY_FIELDS: ProfileFieldsValue = { name: '', description: '', prefixes: '', pollMinutes: 1 }

export default function Profiles() {
  const { profiles, loading, error, refetch } = useProfiles()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const [adding, setAdding] = useState(false)
  const [fields, setFields] = useState<ProfileFieldsValue>(EMPTY_FIELDS)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [confirm, setConfirm] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [copyingId, setCopyingId] = useState<string | null>(null)

  const submitNew = async (e: React.FormEvent) => {
    e.preventDefault()
    setCreateError(null)
    setCreating(true)
    try {
      const p = await createProfile({
        name: fields.name,
        description: fields.description,
        match_prefixes: fields.prefixes
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
        poll_interval_minutes: fields.pollMinutes,
      })
      navigate(`/settings/network/profiles/${p.id}`)
    } catch (err) {
      setCreateError((err as ApiError).message || 'Could not create the profile')
    } finally {
      setCreating(false)
    }
  }

  const doDelete = async (p: ProfileView) => {
    setActionError(null)
    try {
      await deleteProfile(p.id)
      await refetch()
    } catch (err) {
      setActionError((err as ApiError).message)
    }
    setConfirm(null)
  }

  const doCopy = async (p: ProfileView) => {
    setActionError(null)
    setCopyingId(p.id)
    try {
      const cp = await copyProfile(p.id)
      navigate(`/settings/network/profiles/${cp.id}`)
    } catch (err) {
      setActionError((err as ApiError).message)
      setCopyingId(null)
    }
  }

  // "Make a metric from this", from the MIB browser's test-walk panel: pick
  // which profile gets the new metric, then hand off to its detail page.
  const addOid = searchParams.get('oid')
  const isNewFlow = searchParams.get('new') === '1'
  if (isNewFlow && addOid) {
    return (
      <div className="space-y-6">
        <h2 className="text-2xl font-light text-white">Add this as a metric to which profile?</h2>
        {loading ? (
          <p className="text-sm text-slate-400">Loading…</p>
        ) : profiles.length === 0 ? (
          <div className="card p-8 text-center text-sm text-slate-400">No profiles yet. Create one first.</div>
        ) : (
          <div className="card divide-y divide-white/10">
            {profiles.map((p) => (
              <button
                key={p.id}
                type="button"
                className="flex w-full items-center justify-between gap-3 p-4 text-left hover:bg-white/5"
                onClick={() => navigate(`/settings/network/profiles/${p.id}?addOid=${encodeURIComponent(addOid)}`)}
              >
                <span className="text-slate-200">{p.name}</span>
                {p.builtin && <span className="text-xs text-slate-500">Built-in</span>}
              </button>
            ))}
          </div>
        )}
        <Link to="/settings/network/profiles" className="text-sm text-slate-400 hover:text-slate-300">
          Cancel
        </Link>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-light text-white">Device profiles</h2>
          <p className="mt-2 text-sm text-slate-400">
            Sets of custom metrics polled from devices by sysObjectID, on top of Sentinel&apos;s own built-in checks.
          </p>
        </div>
        <button className="btn-primary flex items-center gap-2" onClick={() => setAdding((a) => !a)}>
          <Plus className="h-4 w-4" /> New profile
        </button>
      </div>

      {adding && (
        <form className="card max-w-2xl space-y-4 p-6" onSubmit={(e) => void submitNew(e)}>
          <ProfileFields value={fields} onChange={setFields} />
          {createError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{createError}</div>}
          <div className="flex justify-end gap-2">
            <button type="button" className="btn-secondary" onClick={() => setAdding(false)}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={creating}>
              Create
            </button>
          </div>
        </form>
      )}

      {(error || actionError) && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error ?? actionError}</div>
      )}

      {loading ? (
        <p className="text-sm text-slate-400">Loading…</p>
      ) : profiles.length === 0 ? (
        <div className="card p-8 text-center">
          <p className="text-slate-300">No profiles yet.</p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="px-4 py-3 font-medium">Name</th>
                  <th className="px-4 py-3 font-medium">Matches</th>
                  <th className="px-4 py-3 font-medium">Every</th>
                  <th className="px-4 py-3 font-medium text-right">Metrics</th>
                  <th className="px-4 py-3 font-medium text-right">Devices</th>
                  <th className="px-4 py-3 font-medium" />
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {profiles.map((p) => (
                  <tr key={p.id}>
                    <td className="px-4 py-3">
                      <Link to={`/settings/network/profiles/${p.id}`} className="font-medium text-slate-200 hover:underline">
                        {p.name}
                      </Link>
                      {p.builtin && (
                        <span className="ml-2 inline-flex items-center rounded-full border border-slate-500/30 bg-slate-500/15 px-2 py-0.5 text-xs font-medium text-slate-300">
                          Built-in
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-400">{p.match_prefixes.join(', ') || '—'}</td>
                    <td className="px-4 py-3 text-slate-400">{p.poll_interval_minutes} min</td>
                    <td className="px-4 py-3 text-right tabular-nums text-slate-300">{p.metrics}</td>
                    <td className="px-4 py-3 text-right tabular-nums text-slate-300">{p.devices}</td>
                    <td className="px-4 py-3 text-right">
                      {confirm === p.id ? (
                        <div className="flex items-center justify-end gap-2">
                          <span className="text-xs text-slate-400">Delete {p.name}?</span>
                          <button className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                            Cancel
                          </button>
                          <button className="btn bg-red-600 !py-1 text-white hover:bg-red-700" onClick={() => void doDelete(p)}>
                            Delete
                          </button>
                        </div>
                      ) : (
                        <div className="flex justify-end gap-2">
                          <button
                            className="btn-secondary !px-2 !py-1 disabled:opacity-40"
                            title={`Copy ${p.name} (the copy starts with no match prefixes)`}
                            disabled={copyingId === p.id}
                            onClick={() => void doCopy(p)}
                          >
                            <Copy className="h-4 w-4" />
                          </button>
                          <button
                            className="btn-secondary !px-2 !py-1 text-red-400 disabled:opacity-40"
                            title={p.builtin ? 'Copy it to make your own' : `Delete ${p.name}`}
                            disabled={p.builtin}
                            onClick={() => setConfirm(p.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

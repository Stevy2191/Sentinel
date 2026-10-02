import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import {
  deleteMetric,
  metricDataDevices,
  updateProfile,
  useProfile,
  type ProfileInput,
  type ProfileMetric,
} from '@/hooks/useProfiles'
import { mibObject } from '@/hooks/useMibs'
import { metricFromMibObject, ruleText, type ProfileFieldsValue } from '@/utils/metrics'
import MetricEditor from '@/components/network/MetricEditor'
import ProfileFields from '@/components/network/ProfileFields'
import type { ApiError } from '@/services/api'

const SOURCE_LABEL: Record<ProfileMetric['source'], string> = {
  scalar: 'Scalar',
  column: 'Table column',
  used_free_pct: 'Used/free %',
}
const KIND_LABEL: Record<ProfileMetric['kind'], string> = {
  gauge: 'Gauge',
  counter: 'Counter',
  status: 'Status',
}

export default function ProfileDetail() {
  const { id } = useParams<{ id: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { profile, loading, notFound, refetch } = useProfile(id)

  const [fields, setFields] = useState<ProfileFieldsValue>({ name: '', description: '', prefixes: '', pollMinutes: 1 })
  const [savingHeader, setSavingHeader] = useState(false)
  const [headerSaved, setHeaderSaved] = useState(false)
  const [headerError, setHeaderError] = useState<string | null>(null)

  const [editing, setEditing] = useState<{ metric: ProfileMetric | null; seed?: Partial<ProfileMetric> } | null>(null)
  const [confirm, setConfirm] = useState<{ metric: ProfileMetric; devices: number } | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [seedError, setSeedError] = useState<string | null>(null)

  useEffect(() => {
    if (profile) {
      setFields({
        name: profile.name,
        description: profile.description,
        prefixes: profile.match_prefixes.join('\n'),
        pollMinutes: profile.poll_interval_minutes,
      })
      setHeaderSaved(false)
    }
  }, [profile])

  // A "make a metric from this" link from the MIB browser (via the Profiles
  // picker): open the editor pre-filled from the object, then drop the param
  // so reloading the page doesn't reopen it. Runs once on mount only - it
  // must not re-fire when setSearchParams below changes searchParams itself.
  useEffect(() => {
    const addOid = searchParams.get('addOid')
    if (!addOid) return
    let cancelled = false
    mibObject(addOid)
      .then((detail) => {
        if (cancelled) return
        setEditing({ metric: null, seed: metricFromMibObject(detail) })
      })
      .catch((err) => {
        if (cancelled) return
        setSeedError((err as ApiError).message || 'Could not load that MIB object')
      })
      .finally(() => {
        if (cancelled) return
        const next = new URLSearchParams(searchParams)
        next.delete('addOid')
        setSearchParams(next, { replace: true })
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (notFound || !profile) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Profile not found.</p>
        <Link to="/network/profiles" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to profiles
        </Link>
      </div>
    )
  }

  const submitHeader = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!id) return
    setHeaderError(null)
    setHeaderSaved(false)
    setSavingHeader(true)
    const input: ProfileInput = {
      name: fields.name,
      description: fields.description,
      match_prefixes: fields.prefixes
        .split('\n')
        .map((p) => p.trim())
        .filter(Boolean),
      poll_interval_minutes: fields.pollMinutes,
    }
    try {
      await updateProfile(id, input)
      await refetch()
      setHeaderSaved(true)
    } catch (err) {
      setHeaderError((err as ApiError).message || 'Could not save the profile')
    } finally {
      setSavingHeader(false)
    }
  }

  const startDelete = async (m: ProfileMetric) => {
    setActionError(null)
    try {
      const n = await metricDataDevices(m.id)
      setConfirm({ metric: m, devices: n })
    } catch (err) {
      setActionError((err as ApiError).message || 'Could not check this metric')
    }
  }

  const confirmDelete = async () => {
    if (!confirm) return
    setActionError(null)
    try {
      await deleteMetric(confirm.metric.id)
      await refetch()
    } catch (err) {
      setActionError((err as ApiError).message || 'Could not delete the metric')
    }
    setConfirm(null)
  }

  return (
    <div className="space-y-8">
      <div>
        <Link to="/network/profiles" className="text-sm text-slate-400 hover:text-slate-300">
          ← Profiles
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="text-4xl font-light text-white">{profile.name}</h1>
          {profile.builtin && (
            <span className="inline-flex items-center rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs font-medium text-slate-300">
              Built-in
            </span>
          )}
        </div>
      </div>

      {profile.match_prefixes.length === 0 && (
        <div className="max-w-2xl rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-300">
          Not applied to any device automatically — add match prefixes or attach it to devices in Edit details.
        </div>
      )}

      <form className="card max-w-2xl space-y-4 p-6" onSubmit={(e) => void submitHeader(e)}>
        <ProfileFields value={fields} onChange={setFields} />
        {headerError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{headerError}</div>}
        <div className="flex items-center gap-3">
          <button type="submit" className="btn-primary" disabled={savingHeader}>
            Save
          </button>
          {headerSaved && <span className="text-sm text-emerald-400">Saved</span>}
        </div>
      </form>

      {seedError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{seedError}</div>}
      {actionError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{actionError}</div>}

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Metrics</h2>
          <button className="btn-primary flex items-center gap-2" onClick={() => setEditing({ metric: null })}>
            <Plus className="h-4 w-4" /> Add metric
          </button>
        </div>
        {profile.metrics.length === 0 ? (
          <div className="card p-8 text-center text-sm text-slate-400">No metrics yet.</div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                    <th className="px-4 py-3 font-medium">Name</th>
                    <th className="px-4 py-3 font-medium">Key</th>
                    <th className="px-4 py-3 font-medium">Source</th>
                    <th className="px-4 py-3 font-medium">Kind</th>
                    <th className="px-4 py-3 font-medium">Units</th>
                    <th className="px-4 py-3 font-medium">Rule</th>
                    <th className="px-4 py-3 font-medium" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {profile.metrics.map((m) => (
                    <tr key={m.id}>
                      <td className="px-4 py-3 font-medium text-slate-200">{m.name}</td>
                      <td className="px-4 py-3 font-mono text-xs text-slate-400">{m.key}</td>
                      <td className="px-4 py-3 text-slate-300">{SOURCE_LABEL[m.source]}</td>
                      <td className="px-4 py-3 text-slate-300">{KIND_LABEL[m.kind]}</td>
                      <td className="px-4 py-3 text-slate-400">{m.units || '—'}</td>
                      <td className="px-4 py-3 text-slate-400">{ruleText(m)}</td>
                      <td className="px-4 py-3 text-right">
                        {confirm?.metric.id === m.id ? (
                          <div className="flex items-center justify-end gap-2">
                            <span className="text-xs text-slate-400">
                              Delete {m.name}? Its history on {confirm.devices} device{confirm.devices === 1 ? '' : 's'} is deleted too.
                            </span>
                            <button type="button" className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                              Cancel
                            </button>
                            <button type="button" className="btn bg-red-600 !py-1 text-white hover:bg-red-700" onClick={() => void confirmDelete()}>
                              Delete
                            </button>
                          </div>
                        ) : (
                          <div className="flex justify-end gap-2">
                            <button
                              type="button"
                              className="btn-secondary !px-2 !py-1"
                              title={`Edit ${m.name}`}
                              onClick={() => setEditing({ metric: m })}
                            >
                              <Pencil className="h-4 w-4" />
                            </button>
                            <button
                              type="button"
                              className="btn-secondary !px-2 !py-1 text-red-400"
                              title={`Delete ${m.name}`}
                              onClick={() => void startDelete(m)}
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
      </section>

      {editing && (
        <MetricEditor
          key={editing.metric?.id ?? 'new'}
          profileId={profile.id}
          metric={editing.metric}
          seed={editing.seed}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            void refetch()
          }}
        />
      )}
    </div>
  )
}

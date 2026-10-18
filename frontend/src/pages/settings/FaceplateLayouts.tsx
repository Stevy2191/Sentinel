import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Pencil, Trash2 } from 'lucide-react'
import { deleteFaceplateLayout, describeGroups, useFaceplateLayouts, type FaceplateLayoutRow } from '@/hooks/useFaceplateLayouts'
import type { ApiError } from '@/services/api'

/** Settings → Faceplate layouts: every saved and built-in model layout,
 *  how many devices it draws, and Edit (on one of them) or Delete. */
export default function FaceplateLayouts() {
  const { data, loading, error, refetch } = useFaceplateLayouts()
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const rows = data ?? []
  const anySaved = rows.some((r) => r.source === 'saved')

  const doDelete = async (row: FaceplateLayoutRow) => {
    if (!row.id) return
    setActionError(null)
    try {
      await deleteFaceplateLayout(row.id)
      setConfirm(null)
      await refetch()
    } catch (err) {
      setActionError((err as ApiError).message || 'Delete failed')
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-2xl font-light text-white">Faceplate layouts</h2>
        <p className="mt-2 text-sm text-slate-400">
          How each switch model&apos;s front panel is drawn. A saved layout replaces the built-in one and the automatic drawing for every
          device of that model; a device&apos;s own faceplate settings in Edit details still override it.
        </p>
      </div>

      {error && <p className="text-sm text-red-400">{error}</p>}
      {actionError && <p className="text-sm text-red-400">{actionError}</p>}

      {loading && !data ? (
        <p className="text-sm text-slate-400">Loading…</p>
      ) : (
        <>
          {!anySaved && (
            <div className="card p-8 text-center">
              <p className="text-slate-300">No saved layouts. Open a device&apos;s faceplate and choose Fix layout to make one.</p>
            </div>
          )}
          {rows.length > 0 && (
            <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                      <th className="px-4 py-3 font-medium">Model</th>
                      <th className="px-4 py-3 font-medium">Groups</th>
                      <th className="px-4 py-3 font-medium text-right">Devices</th>
                      <th className="px-4 py-3 font-medium">Updated</th>
                      <th className="px-4 py-3 font-medium">Source</th>
                      <th className="px-4 py-3 font-medium" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {rows.map((r) => {
                      const key = r.id ?? `builtin-${r.model}`
                      return (
                        <tr key={key}>
                          <td className="px-4 py-3 font-medium text-slate-200">{r.model}</td>
                          <td className="px-4 py-3 font-mono text-xs text-slate-400">{describeGroups(r.groups)}</td>
                          <td className="px-4 py-3 text-right tabular-nums text-slate-300">{r.devices}</td>
                          <td className="px-4 py-3 text-slate-400">
                            {r.updated_at ? `${new Date(r.updated_at).toLocaleDateString()}${r.updated_by ? ` by ${r.updated_by}` : ''}` : '—'}
                          </td>
                          <td className="px-4 py-3">
                            <span className="inline-flex items-center rounded-full border border-slate-500/30 bg-slate-500/15 px-2 py-0.5 text-xs font-medium text-slate-300">
                              {r.source === 'saved' ? 'Saved' : 'Built in'}
                            </span>
                          </td>
                          <td className="px-4 py-3 text-right">
                            {confirm === key ? (
                              <div className="flex items-center justify-end gap-2">
                                <span className="text-xs text-slate-400">Remove the saved layout for {r.model}?</span>
                                <button className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                                  Cancel
                                </button>
                                <button className="btn bg-red-600 !py-1 text-white hover:bg-red-700" onClick={() => void doDelete(r)}>
                                  Delete
                                </button>
                              </div>
                            ) : (
                              <div className="flex justify-end gap-2">
                                <button
                                  className="btn-secondary !px-2 !py-1 disabled:opacity-40"
                                  title={r.preview_device_id ? `Edit on a ${r.model} device` : 'No device of this model to preview on'}
                                  aria-label={`Edit the layout for ${r.model}`}
                                  disabled={!r.preview_device_id}
                                  onClick={() => navigate(`/network/devices/${r.preview_device_id}?fixLayout=1`)}
                                >
                                  <Pencil className="h-4 w-4" />
                                </button>
                                {r.source === 'saved' && (
                                  <button
                                    className="btn-secondary !px-2 !py-1 text-red-400"
                                    title={`Delete the saved layout for ${r.model}`}
                                    aria-label={`Delete the saved layout for ${r.model}`}
                                    onClick={() => setConfirm(key)}
                                  >
                                    <Trash2 className="h-4 w-4" />
                                  </button>
                                )}
                              </div>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

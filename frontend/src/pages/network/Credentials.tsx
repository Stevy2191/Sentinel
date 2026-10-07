import { useState } from 'react'
import { KeyRound, Pencil, Plus, Trash2 } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSnmpCredentials, type CredentialView } from '@/hooks/useSnmpCredentials'
import CredentialFormModal from '@/components/network/CredentialFormModal'
import type { ApiError } from '@/services/api'

function describe(c: CredentialView): string {
  if (c.version !== '3') return `v${c.version} community`
  if (c.auth_protocol === 'none') return `v3 ${c.username}, no auth`
  return `v3 ${c.username}, ${c.auth_protocol}${c.priv_protocol !== 'none' ? ` + ${c.priv_protocol}` : ''}`
}

export default function Credentials() {
  const { currentUser } = useAuthContext()
  const { credentials, loading, error, create, update, remove } = useSnmpCredentials()
  const [editing, setEditing] = useState<CredentialView | 'new' | null>(null)
  const [confirm, setConfirm] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  if (!currentUser?.is_admin) {
    return <p className="text-sm text-slate-400">Only administrators manage credential profiles.</p>
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-light text-white">SNMP credentials</h2>
          <p className="mt-2 text-sm text-slate-400">
            SNMP credential profiles. Secrets are stored encrypted and never shown again after saving.
          </p>
        </div>
        <button className="btn-primary flex items-center gap-2" onClick={() => setEditing('new')}>
          <Plus className="h-4 w-4" /> Add profile
        </button>
      </div>

      {(error || actionError) && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error ?? actionError}</div>
      )}

      {loading ? (
        <p className="text-sm text-slate-400">Loading…</p>
      ) : credentials.length === 0 ? (
        <div className="card p-8 text-center">
          <KeyRound className="mx-auto h-8 w-8 text-slate-500" />
          <p className="mt-2 text-slate-300">No credential profiles yet.</p>
          <p className="mt-1 text-sm text-slate-500">Add one before adding devices; most switches use a v2c community.</p>
        </div>
      ) : (
        <div className="card divide-y divide-white/10">
          {credentials.map((c) => (
            <div key={c.id} className="flex flex-wrap items-center gap-4 p-4">
              <div className="min-w-0 flex-1">
                <div className="font-medium text-white">{c.name}</div>
                <div className="text-xs text-slate-400">
                  {describe(c)} · {c.site_id ? `only ${c.site_name}` : 'every site'} · used by {c.used_by}{' '}
                  device{c.used_by === 1 ? '' : 's'}
                </div>
              </div>
              {confirm === c.id ? (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-slate-400">Delete {c.name}?</span>
                  <button className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                    Cancel
                  </button>
                  <button
                    className="btn bg-red-600 !py-1 text-white hover:bg-red-700"
                    onClick={async () => {
                      setActionError(null)
                      try {
                        await remove(c.id)
                      } catch (err) {
                        setActionError((err as ApiError).message)
                      }
                      setConfirm(null)
                    }}
                  >
                    Delete
                  </button>
                </div>
              ) : (
                <div className="flex gap-2">
                  <button className="btn-secondary !px-2 !py-1" title={`Edit ${c.name}`} onClick={() => setEditing(c)}>
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    className="btn-secondary !px-2 !py-1 text-red-400 disabled:opacity-40"
                    title={c.used_by > 0 ? 'In use by devices' : `Delete ${c.name}`}
                    disabled={c.used_by > 0}
                    onClick={() => setConfirm(c.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {editing && (
        <CredentialFormModal
          initial={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSave={(input) => (editing === 'new' ? create(input) : update(editing.id, input))}
        />
      )}
    </div>
  )
}

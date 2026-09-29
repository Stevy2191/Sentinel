import { useState } from 'react'
import { X } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import {
  AUTH_PROTOCOLS,
  PRIV_PROTOCOLS,
  type CredentialInput,
  type CredentialView,
  type SnmpVersion,
} from '@/hooks/useSnmpCredentials'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  initial?: CredentialView
  onClose: () => void
  onSave: (input: CredentialInput) => Promise<unknown>
}

/** Secret fields are write-only: they start blank, and blank keeps what is stored. */
function SecretField({ label, isSet, value, onChange }: { label: string; isSet: boolean; value: string; onChange: (v: string) => void }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-slate-300">
        {label} {isSet && <span className="text-xs text-emerald-400">(set — leave blank to keep)</span>}
      </span>
      <input
        type="password"
        autoComplete="new-password"
        className={inputCls}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={isSet ? '••••••••' : ''}
      />
    </label>
  )
}

export default function CredentialFormModal({ initial, onClose, onSave }: Props) {
  const { sites } = useSites()
  const [name, setName] = useState(initial?.name ?? '')
  const [siteId, setSiteId] = useState<string>(initial?.site_id ?? '')
  const [version, setVersion] = useState<SnmpVersion>(initial?.version ?? '2c')
  const [community, setCommunity] = useState('')
  const [username, setUsername] = useState(initial?.username ?? '')
  const [authProto, setAuthProto] = useState(initial?.auth_protocol ?? 'SHA')
  const [authPass, setAuthPass] = useState('')
  const [privProto, setPrivProto] = useState(initial?.priv_protocol ?? 'AES')
  const [privPass, setPrivPass] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSaving(true)
    const input: CredentialInput = { name, site_id: siteId || null, version }
    if (version === '3') {
      Object.assign(input, {
        username,
        auth_protocol: authProto,
        priv_protocol: authProto === 'none' ? 'none' : privProto,
        ...(authPass ? { auth_password: authPass } : {}),
        ...(privPass ? { priv_password: privPass } : {}),
      })
    } else if (community) {
      input.community = community
    }
    try {
      await onSave(input)
      onClose()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the profile')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-md space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit credential profile' : 'Add credential profile'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} required maxLength={255} autoFocus />
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Available to</span>
          <select className={inputCls} value={siteId} onChange={(e) => setSiteId(e.target.value)}>
            <option value="">Every site</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                Only {s.name}
              </option>
            ))}
          </select>
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">SNMP version</span>
          <select className={inputCls} value={version} onChange={(e) => setVersion(e.target.value as SnmpVersion)}>
            <option value="2c">v2c</option>
            <option value="3">v3</option>
            <option value="1">v1 (older devices)</option>
          </select>
        </label>

        {version !== '3' ? (
          <SecretField label="Community" isSet={!!initial?.has_community && initial.version !== '3'} value={community} onChange={setCommunity} />
        ) : (
          <>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Username</span>
              <input className={inputCls} value={username} onChange={(e) => setUsername(e.target.value)} required />
            </label>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Authentication</span>
              <select className={inputCls} value={authProto} onChange={(e) => setAuthProto(e.target.value)}>
                {AUTH_PROTOCOLS.map((p) => (
                  <option key={p} value={p}>
                    {p === 'none' ? 'None' : p}
                  </option>
                ))}
              </select>
            </label>
            {authProto !== 'none' && (
              <>
                <SecretField label="Auth password (8+ characters)" isSet={!!initial?.has_auth_password} value={authPass} onChange={setAuthPass} />
                <label className="block space-y-1">
                  <span className="text-sm text-slate-300">Privacy</span>
                  <select className={inputCls} value={privProto} onChange={(e) => setPrivProto(e.target.value)}>
                    {PRIV_PROTOCOLS.map((p) => (
                      <option key={p} value={p}>
                        {p === 'none' ? 'None' : p}
                      </option>
                    ))}
                  </select>
                </label>
                {privProto !== 'none' && (
                  <SecretField label="Privacy password (8+ characters)" isSet={!!initial?.has_priv_password} value={privPass} onChange={setPrivPass} />
                )}
              </>
            )}
          </>
        )}

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={saving || !name.trim()}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  )
}

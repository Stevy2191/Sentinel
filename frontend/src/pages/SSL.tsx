import { useMemo, useState } from 'react'
import { Plus, RefreshCw, Search, X, Loader2 } from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import CreateSSLModal from '@/components/CreateSSLModal'
import SSLCertificateTable from '@/components/SSLCertificateTable'
import {
  useSSLCertificates,
  useSSLCertificateActions,
  type SSLCertificate,
} from '@/hooks/useSSLCertificates'
import type { ApiError } from '@/services/api'

const MIN_DAYS = 1
const MAX_DAYS = 365

/** Stat card. Not ShimmerStatCard: these three carry their own literal tones. */
function StatCard({
  id,
  title,
  value,
  subtitle,
  tone,
  shimmer,
}: {
  id: string
  title: string
  value: number
  subtitle: string
  tone: { bg: string; border: string; text: string; glow: string; hover: string }
  shimmer: ReturnType<typeof useCardShimmer>
}) {
  return (
    <div
      className={`group relative overflow-hidden rounded-lg border ${tone.border} ${tone.hover} bg-gradient-to-br ${tone.bg} to-slate-800/40 p-6 backdrop-blur-sm transition-all`}
      onMouseMove={(e) => shimmer.handleCardMouseMove(e, id)}
      onMouseEnter={() => shimmer.handleCardMouseEnter(id)}
      onMouseLeave={() => shimmer.handleCardMouseLeave(id)}
    >
      <div
        className={`pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full ${tone.glow} blur-2xl transition-all`}
      />
      {shimmer.isShown(id) && (
        <div
          className="pointer-events-none absolute inset-0 rounded-lg transition-all duration-75"
          style={shimmer.getShimmerStyle(id)}
        />
      )}
      <div className="relative z-10">
        <div className={`mb-4 text-xs font-semibold uppercase tracking-widest ${tone.text}`}>
          {title}
        </div>
        <div className="flex items-end justify-between gap-3">
          <div className="text-3xl font-light text-white">{value}</div>
          <div className={`text-xs font-medium ${tone.text}/70`}>{subtitle}</div>
        </div>
      </div>
    </div>
  )
}

const TONES = {
  valid: {
    bg: 'from-emerald-600/15',
    border: 'border-emerald-500/30',
    hover: 'hover:border-emerald-500/50',
    text: 'text-emerald-400',
    glow: 'bg-emerald-500/10 group-hover:bg-emerald-500/20',
  },
  expiring: {
    bg: 'from-amber-600/15',
    border: 'border-amber-500/30',
    hover: 'hover:border-amber-500/50',
    text: 'text-amber-400',
    glow: 'bg-amber-500/10 group-hover:bg-amber-500/20',
  },
  expired: {
    bg: 'from-red-600/15',
    border: 'border-red-500/30',
    hover: 'hover:border-red-500/50',
    text: 'text-red-400',
    glow: 'bg-red-500/10 group-hover:bg-red-500/20',
  },
}

/** Edit dialog: only the two fields the operator owns. */
function EditModal({
  cert,
  onClose,
  onSaved,
  push,
}: {
  cert: SSLCertificate
  onClose: () => void
  onSaved: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const { update, busy } = useSSLCertificateActions()
  const [days, setDays] = useState(cert.expiry_notification_days)
  const [enabled, setEnabled] = useState(cert.enabled)
  const invalid = days < MIN_DAYS || days > MAX_DAYS

  const save = async () => {
    if (invalid) return
    try {
      await update(cert.id, { expiry_notification_days: days, enabled })
      push(`${cert.domain} updated`, 'success')
      onSaved()
      onClose()
    } catch (err) {
      push((err as ApiError).message || 'Could not update the domain', 'error')
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6"
      >
        <div className="mb-4 flex items-start justify-between">
          <h3 className="text-lg font-semibold text-white">Edit certificate</h3>
          <button
            onClick={onClose}
            className="rounded p-1 text-slate-400 transition hover:text-white"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Shown as a labelled read-only field rather than a heading: as a
            title it reads as decoration, and it needs to be obvious both which
            certificate is being edited and that the domain is not one of the
            things you can change here. */}
        <div className="mb-5">
          <span className="mb-1 block text-sm font-medium text-white">Domain</span>
          <p
            className="truncate rounded-lg border border-white/10 bg-slate-800/40 px-3 py-2 text-sm text-slate-300"
            title={cert.domain}
          >
            {cert.domain}
          </p>
          <p className="mt-1 text-xs text-slate-500">
            Checked once a day. To watch a different domain, add it as a new entry.
          </p>
        </div>

        <label htmlFor="edit-days" className="mb-1 block text-sm font-medium text-white">
          Alert when the certificate expires in
        </label>
        <div className="flex items-center gap-2">
          <input
            id="edit-days"
            type="number"
            min={MIN_DAYS}
            max={MAX_DAYS}
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className={`w-28 rounded-lg border bg-slate-800/50 px-4 py-2 text-white focus:outline-none ${
              invalid ? 'border-red-500/60' : 'border-white/10 focus:border-white/30'
            }`}
          />
          <span className="text-sm text-slate-400">days</span>
        </div>
        <p className={`mt-1 text-xs ${invalid ? 'text-red-400' : 'text-slate-500'}`}>
          {invalid ? `Must be between ${MIN_DAYS} and ${MAX_DAYS}` : 'Changing this re-evaluates the status straight away.'}
        </p>

        <label className="mt-5 flex cursor-pointer items-center justify-between gap-4 rounded-lg border border-white/10 bg-slate-800/40 p-4">
          <span>
            <span className="block text-sm font-medium text-white">Enabled</span>
            <span className="block text-xs text-slate-500">
              A paused domain is not checked and never alerts.
            </span>
          </span>
          <button
            type="button"
            role="switch"
            aria-checked={enabled}
            aria-label="Enabled"
            onClick={() => setEnabled((v) => !v)}
            className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
              enabled ? 'bg-emerald-600' : 'bg-slate-600'
            }`}
          >
            <span
              className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${
                enabled ? 'translate-x-5' : 'translate-x-0'
              }`}
            />
          </button>
        </label>

        <div className="mt-6 flex justify-end gap-2">
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn-primary" onClick={() => void save()} disabled={busy || invalid}>
            {busy ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

export default function SSL() {
  const { toasts, push } = useToasts()
  const { certificates, loading, error, refetch } = useSSLCertificates()
  const { remove, checkNow, checkAll, busy } = useSSLCertificateActions()
  const shimmer = useCardShimmer(['valid', 'expiring', 'expired'])

  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<SSLCertificate | null>(null)
  const [confirm, setConfirm] = useState<SSLCertificate | null>(null)
  const [checkingId, setCheckingId] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  const counts = useMemo(() => {
    let valid = 0
    let expiring = 0
    let expired = 0
    for (const c of certificates) {
      if (c.status === 'valid') valid++
      else if (c.status === 'expiring_soon') expiring++
      else if (c.status === 'expired') expired++
    }
    return { valid, expiring, expired, unknown: certificates.length - valid - expiring - expired }
  }, [certificates])

  const handleCheckNow = async (cert: SSLCertificate) => {
    setCheckingId(cert.id)
    try {
      const res = await checkNow(cert.id)
      if (res.check_error) push(`${cert.domain}: ${res.check_error}`, 'error')
      else push(`${cert.domain} re-checked`, 'success')
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Check failed', 'error')
    } finally {
      setCheckingId(null)
    }
  }

  const handleRefreshAll = async () => {
    try {
      const res = await checkAll()
      push(res.message, res.failed > 0 ? 'info' : 'success')
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Could not re-check certificates', 'error')
    }
  }

  const handleDelete = async (cert: SSLCertificate) => {
    try {
      await remove(cert.id)
      push(`${cert.domain} removed`, 'success')
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Could not remove the domain', 'error')
    } finally {
      setConfirm(null)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">SSL &amp; Domain Management</h1>
          <p className="mt-2 text-sm text-slate-400">
            Monitor and manage SSL certificates for your domains
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button
            className="btn-secondary"
            onClick={() => void handleRefreshAll()}
            disabled={busy || certificates.length === 0}
          >
            <RefreshCw className={`h-4 w-4 ${busy ? 'animate-spin' : ''}`} /> Refresh All
          </button>
          <button className="btn-primary" onClick={() => setAddOpen(true)}>
            <Plus className="h-4 w-4" /> Add Domain
          </button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          id="valid"
          title="Valid Certificates"
          value={counts.valid}
          subtitle="healthy"
          tone={TONES.valid}
          shimmer={shimmer}
        />
        <StatCard
          id="expiring"
          title="Expiring Soon"
          value={counts.expiring}
          subtitle="renew these"
          tone={TONES.expiring}
          shimmer={shimmer}
        />
        <StatCard
          id="expired"
          title="Expired"
          value={counts.expired}
          subtitle="acting now"
          tone={TONES.expired}
          shimmer={shimmer}
        />
      </div>

      {error && (
        <div className="flex items-center justify-between rounded-lg border border-red-500/30 bg-red-500/10 p-3">
          <span className="text-sm text-red-400">{error}</span>
          <button className="btn-secondary !py-1" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {certificates.length > 0 && (
        <div className="relative max-w-sm">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search domains"
            aria-label="Search domains"
            className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
          />
        </div>
      )}

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-6 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading certificates…
        </div>
      ) : certificates.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-12 text-center backdrop-blur-sm">
          <p className="text-sm text-slate-300">No domains are being watched yet.</p>
          <p className="mt-1 text-xs text-slate-500">
            Add one and Sentinel will read its certificate and warn you before it expires.
          </p>
          <button className="btn-primary mx-auto mt-4" onClick={() => setAddOpen(true)}>
            <Plus className="h-4 w-4" /> Add Domain
          </button>
        </div>
      ) : (
        <SSLCertificateTable
          certificates={certificates}
          search={search}
          onEdit={setEditing}
          onDelete={setConfirm}
          onCheckNow={(c) => void handleCheckNow(c)}
          checkingId={checkingId}
        />
      )}

      <CreateSSLModal
        isOpen={addOpen}
        onClose={() => setAddOpen(false)}
        onCreated={() => void refetch()}
        push={push}
      />

      {editing && (
        <EditModal
          cert={editing}
          onClose={() => setEditing(null)}
          onSaved={() => void refetch()}
          push={push}
        />
      )}

      {confirm && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4"
          onClick={() => setConfirm(null)}
        >
          <div className="card w-full max-w-sm p-6" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold">Stop watching {confirm.domain}?</h3>
            <p className="mt-2 text-sm text-slate-400">
              You will no longer be warned before this certificate expires.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirm(null)}>
                Cancel
              </button>
              <button className="btn-danger" onClick={() => void handleDelete(confirm)}>
                Remove
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

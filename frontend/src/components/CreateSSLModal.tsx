import { useEffect, useRef, useState } from 'react'
import { X, Loader2, ShieldCheck, ShieldAlert, ShieldX } from 'lucide-react'
import {
  useSSLCertificateActions,
  type SSLCertificate,
  type SSLStatus,
} from '@/hooks/useSSLCertificates'
import type { ApiError } from '@/services/api'

const MIN_DAYS = 1
const MAX_DAYS = 365
const DEFAULT_DAYS = 7

const field =
  'w-full rounded-lg border border-white/10 bg-slate-800/50 px-4 py-2 text-white placeholder-slate-500 transition focus:border-white/30 focus:outline-none'

/** Whole literal class strings: Tailwind cannot build these from a key. */
export const STATUS_STYLE: Record<SSLStatus, { label: string; cls: string }> = {
  valid: { label: 'Valid', cls: 'border-emerald-500/30 bg-emerald-500/20 text-emerald-400' },
  expiring_soon: { label: 'Expiring Soon', cls: 'border-amber-500/30 bg-amber-500/20 text-amber-400' },
  expired: { label: 'Expired', cls: 'border-red-500/30 bg-red-500/20 text-red-400' },
  unknown: { label: 'Unknown', cls: 'border-slate-500/30 bg-slate-500/20 text-slate-400' },
}

const STATUS_ICON: Record<SSLStatus, typeof ShieldCheck> = {
  valid: ShieldCheck,
  expiring_soon: ShieldAlert,
  expired: ShieldX,
  unknown: ShieldAlert,
}

/** Days-left colouring: comfortable, worth planning for, act now. */
export function daysLeftClass(days: number | null): string {
  if (days == null) return 'text-slate-500'
  if (days < 7) return 'text-red-400'
  if (days <= 30) return 'text-amber-400'
  return 'text-emerald-400'
}

/** A client-side sanity check only; the server validates properly. */
function looksLikeDomain(raw: string): boolean {
  const d = raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, '')
    .split(/[/?#]/)[0]
  return /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:\d{1,5})?$/.test(d)
}

interface Props {
  isOpen: boolean
  onClose: () => void
  onCreated: (cert: SSLCertificate) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/**
 * CreateSSLModal adds a domain to watch.
 *
 * There is no separate "validate" step: the create call reads the certificate
 * server-side before it answers, so the result panel shows what was actually
 * found rather than a guess, and a domain whose certificate cannot be read is
 * still added with the reason recorded.
 */
export default function CreateSSLModal({ isOpen, onClose, onCreated, push }: Props) {
  const { create, busy } = useSSLCertificateActions()
  const dialogRef = useRef<HTMLDivElement>(null)
  const firstFieldRef = useRef<HTMLInputElement>(null)

  const [domain, setDomain] = useState('')
  const [notifyDays, setNotifyDays] = useState(DEFAULT_DAYS)
  const [touched, setTouched] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<SSLCertificate | null>(null)

  useEffect(() => {
    if (!isOpen) return
    setDomain('')
    setNotifyDays(DEFAULT_DAYS)
    setTouched(false)
    setError(null)
    setResult(null)
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen])

  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [isOpen, busy, onClose])

  const domainError =
    touched && !domain.trim()
      ? 'A domain is required'
      : touched && !looksLikeDomain(domain)
        ? 'Enter a domain like example.com'
        : undefined
  const daysError =
    notifyDays < MIN_DAYS || notifyDays > MAX_DAYS
      ? `Must be between ${MIN_DAYS} and ${MAX_DAYS}`
      : undefined
  const canSubmit = !busy && domain.trim() !== '' && !domainError && !daysError

  const submit = async () => {
    setTouched(true)
    if (!domain.trim() || !looksLikeDomain(domain) || daysError) return
    setError(null)
    try {
      const cert = await create({
        domain: domain.trim(),
        expiry_notification_days: notifyDays,
        enabled: true,
      })
      setResult(cert)
      onCreated(cert)
      if (cert.status === 'unknown') {
        push(`Added ${cert.domain}, but its certificate could not be read`, 'info')
      } else {
        push(`Now watching ${cert.domain}`, 'success')
      }
    } catch (err) {
      setError((err as ApiError).message || 'Could not add the domain')
    }
  }

  if (!isOpen) return null

  const Icon = result ? STATUS_ICON[result.status] : ShieldCheck

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !busy) onClose()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="add-domain-title"
        className="flex max-h-[90vh] w-full max-w-xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        <div className="flex shrink-0 items-center justify-between border-b border-white/10 p-6">
          <div>
            <h2 id="add-domain-title" className="text-2xl font-light text-white">
              Add Domain
            </h2>
            <p className="mt-1 text-sm text-slate-400">Watch a TLS certificate for expiry</p>
          </div>
          <button
            onClick={onClose}
            disabled={busy}
            className="rounded p-1 text-slate-400 transition hover:text-white disabled:opacity-50"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Basic Information
            </h3>
            <label htmlFor="ssl-domain" className="mb-1 block text-sm font-medium text-white">
              Domain
            </label>
            <input
              id="ssl-domain"
              ref={firstFieldRef}
              type="text"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              onBlur={() => setTouched(true)}
              placeholder="example.com"
              aria-invalid={!!domainError}
              className={`${field} ${domainError ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${domainError ? 'text-red-400' : 'text-slate-500'}`}>
              {domainError ?? 'The domain to monitor the SSL certificate for'}
            </p>
          </section>

          <div className="border-t border-white/10" />

          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Notification Settings
            </h3>
            <label htmlFor="ssl-days" className="mb-1 block text-sm font-medium text-white">
              Alert when the certificate expires in
            </label>
            <div className="flex items-center gap-2">
              <input
                id="ssl-days"
                type="number"
                min={MIN_DAYS}
                max={MAX_DAYS}
                value={notifyDays}
                onChange={(e) => setNotifyDays(Number(e.target.value))}
                aria-invalid={!!daysError}
                className={`w-28 ${field} ${daysError ? 'border-red-500/60' : ''}`}
              />
              <span className="text-sm text-slate-400">days</span>
            </div>
            <p className={`mt-1 text-xs ${daysError ? 'text-red-400' : 'text-slate-500'}`}>
              {daysError ?? 'For example, 7 sends the alert a week before it expires.'}
            </p>
          </section>

          {error && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
              {error}
            </div>
          )}

          {result && (
            <>
              <div className="border-t border-white/10" />
              <section>
                <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
                  Certificate
                </h3>
                {result.status === 'unknown' ? (
                  <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-300">
                    <p className="font-medium">The certificate could not be read.</p>
                    <p className="mt-1 text-xs">{result.last_error ?? 'No further detail.'}</p>
                    <p className="mt-2 text-xs text-slate-400">
                      The domain is still being watched — the next check will try again.
                    </p>
                  </div>
                ) : (
                  <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
                    <div className="mb-3 flex items-center gap-2">
                      <Icon className="h-5 w-5 text-slate-300" aria-hidden />
                      <span
                        className={`rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[result.status].cls}`}
                      >
                        {STATUS_STYLE[result.status].label}
                      </span>
                    </div>
                    <dl className="space-y-1.5 text-sm">
                      <div className="flex justify-between gap-4">
                        <dt className="text-slate-500">Issuer</dt>
                        <dd className="truncate text-right text-slate-200">{result.issuer ?? '—'}</dd>
                      </div>
                      <div className="flex justify-between gap-4">
                        <dt className="text-slate-500">Expires</dt>
                        <dd className="text-slate-200">
                          {result.expiry_date
                            ? new Date(result.expiry_date).toLocaleDateString()
                            : '—'}
                        </dd>
                      </div>
                      <div className="flex justify-between gap-4">
                        <dt className="text-slate-500">Days until expiry</dt>
                        <dd className={`font-medium ${daysLeftClass(result.days_until_expiry)}`}>
                          {result.days_until_expiry ?? '—'}
                        </dd>
                      </div>
                    </dl>
                  </div>
                )}
              </section>
            </>
          )}
        </div>

        <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 border-t border-white/10 p-6">
          <button
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-white/20 px-6 py-2 text-sm font-medium text-white transition hover:bg-white/5 disabled:opacity-50"
          >
            {result ? 'Close' : 'Cancel'}
          </button>
          {!result && (
            <button
              onClick={() => void submit()}
              disabled={!canSubmit}
              className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-600 to-emerald-500 px-6 py-2 text-sm font-medium text-white transition hover:from-emerald-500 hover:to-emerald-400 disabled:opacity-60"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              {busy ? 'Checking…' : 'Add Domain'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

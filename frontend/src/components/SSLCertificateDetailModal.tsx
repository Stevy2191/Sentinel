import { useEffect, useRef } from 'react'
import { X } from 'lucide-react'
import { STATUS_STYLE } from '@/components/CreateSSLModal'
import { useAvailableChannels } from '@/hooks/useNotificationConfig'
import type { SSLCertificate } from '@/hooks/useSSLCertificates'

interface Props {
  certificate: SSLCertificate
  isOpen: boolean
  onClose: () => void
}

/** "8/22/2026, 8:44:04 AM", or an em dash when there is nothing to show. */
function dateTime(value: string | null | undefined): string {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

function days(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—'
  return `${n} ${Math.abs(n) === 1 ? 'day' : 'days'}`
}

/** A label above its value, which is the shape every field in here takes. */
function Item({
  label,
  children,
  wide = false,
}: {
  label: string
  children: React.ReactNode
  wide?: boolean
}) {
  return (
    <div className={wide ? 'sm:col-span-2' : undefined}>
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className="mt-0.5 break-words text-sm text-white">{children}</dd>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-slate-300">
        {title}
      </h3>
      <dl className="grid grid-cols-1 gap-x-6 gap-y-3 rounded-lg border border-white/10 bg-slate-800/40 p-4 sm:grid-cols-2">
        {children}
      </dl>
    </section>
  )
}

/**
 * Read-only detail view for one watched domain.
 *
 * Everything shown was captured at the last successful check rather than read
 * live, so opening it costs nothing and it still says something useful about a
 * host that has since gone unreachable.
 */
export default function SSLCertificateDetailModal({ certificate: c, isOpen, onClose }: Props) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const { available } = useAvailableChannels(isOpen)

  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const t = window.setTimeout(() => closeRef.current?.focus(), 50)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
      window.clearTimeout(t)
    }
  }, [isOpen, onClose])

  if (!isOpen) return null

  // null means every channel, which is a different thing from none.
  const channelNames =
    c.notify_channels === null
      ? 'All configured channels'
      : c.notify_channels.length === 0
        ? 'None — this domain does not alert'
        : c.notify_channels
            .map((id) => available.find((a) => a.id === id)?.name ?? id)
            .join(', ')

  const sans = c.subject_alternative_names ?? []

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={`Certificate details for ${c.domain}`}
        className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        {/* Header */}
        <div className="flex shrink-0 items-start justify-between gap-4 border-b border-white/10 p-6">
          <div className="min-w-0">
            <h2 className="truncate text-xl font-light text-white" title={c.domain}>
              {c.domain}
            </h2>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span
                className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[c.status].cls}`}
              >
                {STATUS_STYLE[c.status].label}
              </span>
              {!c.enabled && <span className="text-xs text-slate-500">paused</span>}
            </div>
          </div>
          <button
            ref={closeRef}
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 text-slate-400 transition hover:text-white"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          {c.last_error && (
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-300">
              <p className="font-medium">The last certificate check failed.</p>
              <p className="mt-1 text-xs">{c.last_error}</p>
              <p className="mt-2 text-xs text-slate-400">
                What follows is from the last successful check and may be out of date.
              </p>
            </div>
          )}

          <Section title="Certificate Status">
            <Item label="Status">{STATUS_STYLE[c.status].label}</Item>
            <Item label="Days Left">{days(c.days_until_expiry)}</Item>
            <Item label="Valid From">{dateTime(c.valid_from)}</Item>
            <Item label="Valid Until">{dateTime(c.expiry_date)}</Item>
            <Item label="Validity Period">{days(c.validity_period_days)}</Item>
            <Item label="Resolved IP">{c.resolved_ip ?? '—'}</Item>
          </Section>

          <Section title="Certificate Details">
            <Item label="Issued To">{c.subject_common_name ?? c.domain}</Item>
            <Item label="Issuer">{c.issuer ?? '—'}</Item>
            <Item label="Issuer Common Name">{c.issuer_common_name ?? '—'}</Item>
            <Item label="Algorithm">
              {c.signature_algorithm
                ? `${c.signature_algorithm}${c.public_key_algorithm ? ` (${c.public_key_algorithm})` : ''}`
                : '—'}
            </Item>
            <Item label="Serial Number" wide>
              <span className="font-mono text-xs">{c.serial_number ?? '—'}</span>
            </Item>
            <Item label="Subject Alternative Names" wide>
              {sans.length === 0 ? (
                '—'
              ) : (
                <span className="flex flex-wrap gap-1.5">
                  {sans.map((n) => (
                    <span
                      key={n}
                      className="rounded border border-white/10 bg-slate-800/60 px-2 py-0.5 font-mono text-xs text-slate-300"
                    >
                      {n}
                    </span>
                  ))}
                </span>
              )}
            </Item>
          </Section>

          <Section title="Domain Registration">
            <Item label="Registered Domain">{c.registrable_domain ?? '—'}</Item>
            <Item label="Registrar">{c.registrar ?? '—'}</Item>
            <Item label="Expires">{dateTime(c.domain_expiry_date)}</Item>
            <Item label="Days Left">{days(c.domain_days_until_expiry)}</Item>
            {c.registration_error && (
              <Item label="Last Lookup" wide>
                <span className="text-amber-300">{c.registration_error}</span>
              </Item>
            )}
          </Section>

          <Section title="Monitoring Configuration">
            {/* One threshold governs both clocks, so it is named once rather
                than split into two rows that would imply two settings. */}
            <Item label="Alert Threshold">{days(c.expiry_notification_days)} before expiry</Item>
            <Item label="Check Interval">
              {days(Math.round(c.check_interval / 86400)) || '—'}
            </Item>
            <Item label="Notification Channels" wide>
              {channelNames}
            </Item>
          </Section>

          <Section title="Technical Information">
            <Item label="Last Checked">{dateTime(c.last_checked)}</Item>
            <Item label="Next Check">{dateTime(c.next_check)}</Item>
            <Item label="Registration Checked">{dateTime(c.registration_checked_at)}</Item>
            <Item label="Last Updated">{dateTime(c.updated_at)}</Item>
            <Item label="Created">{dateTime(c.created_at)}</Item>
            <Item label="Certificate ID">
              <span className="font-mono text-xs">{c.id}</span>
            </Item>
          </Section>
        </div>

        {/* Footer */}
        <div className="flex shrink-0 justify-end border-t border-white/10 p-6">
          <button className="btn-secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

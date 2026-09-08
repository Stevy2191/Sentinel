import { useMemo, useState } from 'react'
import { ArrowUpDown, Pencil, Trash2, RefreshCw } from 'lucide-react'
import { STATUS_STYLE, daysLeftClass } from '@/components/CreateSSLModal'
import ErrorTooltip, { trimDomainPrefix } from '@/components/ErrorTooltip'
import type { SSLCertificate } from '@/hooks/useSSLCertificates'

const PER_PAGE = 10

type SortKey = 'domain' | 'days' | 'expiry' | 'domain_days'

interface Props {
  certificates: SSLCertificate[]
  search: string
  onEdit: (cert: SSLCertificate) => void
  onDelete: (cert: SSLCertificate) => void
  onCheckNow: (cert: SSLCertificate) => void
  checkingId: string | null
}

/**
 * SSLCertificateTable lists watched domains, sorted and paginated.
 *
 * The default sort is by days remaining, ascending: the row that needs
 * attention soonest is the one worth putting at the top, and a certificate with
 * an unreadable expiry sorts last rather than pretending to be urgent.
 */
export default function SSLCertificateTable({
  certificates,
  search,
  onEdit,
  onDelete,
  onCheckNow,
  checkingId,
}: Props) {
  const [sort, setSort] = useState<SortKey>('days')
  const [asc, setAsc] = useState(true)
  const [page, setPage] = useState(1)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    const rows = q
      ? certificates.filter(
          (c) =>
            c.domain.toLowerCase().includes(q) ||
            (c.issuer ?? '').toLowerCase().includes(q) ||
            (c.registrar ?? '').toLowerCase().includes(q)
        )
      : certificates

    const dir = asc ? 1 : -1
    return [...rows].sort((a, b) => {
      switch (sort) {
        case 'domain':
          return dir * a.domain.localeCompare(b.domain)
        case 'expiry': {
          // Unknown expiry sorts to the end either way: it is not "very soon".
          const av = a.expiry_date ? Date.parse(a.expiry_date) : Number.POSITIVE_INFINITY
          const bv = b.expiry_date ? Date.parse(b.expiry_date) : Number.POSITIVE_INFINITY
          return dir * (av - bv)
        }
        case 'domain_days': {
          const av = a.domain_days_until_expiry ?? Number.POSITIVE_INFINITY
          const bv = b.domain_days_until_expiry ?? Number.POSITIVE_INFINITY
          return dir * (av - bv)
        }
        default: {
          const av = a.days_until_expiry ?? Number.POSITIVE_INFINITY
          const bv = b.days_until_expiry ?? Number.POSITIVE_INFINITY
          return dir * (av - bv)
        }
      }
    })
  }, [certificates, search, sort, asc])

  const pages = Math.max(1, Math.ceil(filtered.length / PER_PAGE))
  // Clamped rather than stored, so deleting the last row of the last page does
  // not strand the view on a page that no longer exists.
  const current = Math.min(page, pages)
  const rows = filtered.slice((current - 1) * PER_PAGE, current * PER_PAGE)

  const header = (key: SortKey, label: string) => (
    <button
      className="inline-flex items-center gap-1 text-xs font-medium text-slate-400 transition hover:text-white"
      onClick={() => {
        if (sort === key) setAsc((v) => !v)
        else {
          setSort(key)
          setAsc(true)
        }
        setPage(1)
      }}
    >
      {label} <ArrowUpDown className="h-3 w-3" />
    </button>
  )

  if (filtered.length === 0) {
    return (
      <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400 backdrop-blur-sm">
        {search.trim() ? 'No domains match that search.' : 'No domains are being watched yet.'}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              {/* Two header rows because the table carries two independent
                  clocks. Without the grouping, "Expires" appears twice with no
                  indication that one is renewed by reissuing a certificate and
                  the other by paying the registrar. The spanning labels carry
                  that on their own; there are no vertical rules, so the table
                  is divided horizontally only. */}
              <tr className="border-b border-white/5 bg-slate-800/30">
                <th className="px-4 pb-1 pt-3" />
                <th
                  colSpan={4}
                  className="px-4 pb-1 pt-3 text-left text-[11px] font-semibold uppercase tracking-widest text-slate-500"
                >
                  TLS Certificate
                </th>
                <th
                  colSpan={2}
                  className="px-4 pb-1 pt-3 text-left text-[11px] font-semibold uppercase tracking-widest text-slate-500"
                >
                  Domain Registration
                </th>
                <th className="px-4 pb-1 pt-3" />
              </tr>
              <tr className="border-b border-white/10 bg-slate-800/20">
                <th className="px-4 pb-3 text-left">{header('domain', 'Domain')}</th>
                <th className="px-4 pb-3 text-left text-xs font-medium text-slate-400">
                  Status
                </th>
                <th className="px-4 pb-3 text-left text-xs font-medium text-slate-400">Issuer</th>
                <th className="px-4 pb-3 text-left">{header('expiry', 'Valid Until')}</th>
                <th className="px-4 pb-3 text-left">{header('days', 'Days Left')}</th>
                <th className="px-4 pb-3 text-left text-xs font-medium text-slate-400">
                  Registrar
                </th>
                <th className="px-4 pb-3 text-left">{header('domain_days', 'Expires')}</th>
                <th className="px-4 pb-3 text-right text-xs font-medium text-slate-400">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {rows.map((c, i) => (
                <tr
                  key={c.id}
                  className={`transition hover:bg-white/5 ${i % 2 ? 'bg-white/[0.02]' : ''} ${
                    c.enabled ? '' : 'opacity-60'
                  }`}
                >
                  <td className="px-4 py-3">
                    <div className="font-medium text-slate-200">{c.domain}</div>
                    {!c.enabled && <div className="text-xs text-slate-500">paused</div>}
                  </td>
                  <td className="px-4 py-3">
                    {/* A status of "unknown" only ever means the read failed,
                        so the reason is the useful thing to show. The badge
                        would say "Unknown" and leave the reason to a column
                        wide enough to wreck the table. */}
                    {c.status === 'unknown' && c.last_error ? (
                      <ErrorTooltip
                        message={trimDomainPrefix(c.last_error, c.domain)}
                        label={`Certificate check failed for ${c.domain}`}
                      />
                    ) : (
                      <span
                        className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[c.status].cls}`}
                      >
                        {STATUS_STYLE[c.status].label}
                      </span>
                    )}
                  </td>
                  <td className="max-w-[220px] truncate px-4 py-3 text-slate-400" title={c.issuer ?? ''}>
                    {c.issuer ?? '—'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-slate-300">
                    {c.expiry_date ? new Date(c.expiry_date).toLocaleDateString() : '—'}
                  </td>
                  <td
                    className={`px-4 py-3 font-medium tabular-nums ${daysLeftClass(c.days_until_expiry)}`}
                  >
                    {c.days_until_expiry ?? '—'}
                  </td>
                  <td
                    className="max-w-[180px] px-4 py-3 text-slate-400"
                    title={c.registrar ?? ''}
                  >
                    {/* A failed lookup does not invalidate what was read
                        before, so a known registrar stays visible and the
                        failure is a small staleness marker beside it. The bare
                        icon is only for a domain nothing has ever been read
                        for, where there is nothing else to show. */}
                    {c.registrar ? (
                      <div className="flex items-center gap-1.5">
                        <span className="truncate">{c.registrar}</span>
                        {c.registration_error && (
                          <ErrorTooltip
                            message={`Last lookup failed, so this may be out of date: ${trimDomainPrefix(c.registration_error, c.domain)}`}
                            label={`Registration lookup for ${c.domain} is out of date`}
                          />
                        )}
                      </div>
                    ) : c.registration_error ? (
                      <ErrorTooltip
                        message={trimDomainPrefix(c.registration_error, c.domain)}
                        label={`Registration lookup failed for ${c.domain}`}
                      />
                    ) : (
                      <div className="truncate">—</div>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    {c.domain_expiry_date ? (
                      <>
                        <div className="text-slate-300">
                          {new Date(c.domain_expiry_date).toLocaleDateString()}
                        </div>
                        <div
                          className={`text-xs font-medium tabular-nums ${daysLeftClass(c.domain_days_until_expiry)}`}
                        >
                          {c.domain_days_until_expiry != null
                            ? `${c.domain_days_until_expiry} days left`
                            : '—'}
                        </div>
                      </>
                    ) : (
                      <span className="text-slate-500">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white disabled:opacity-50"
                        aria-label={`Check ${c.domain} now`}
                        title="Check now"
                        disabled={checkingId === c.id}
                        onClick={() => onCheckNow(c)}
                      >
                        <RefreshCw className={`h-4 w-4 ${checkingId === c.id ? 'animate-spin' : ''}`} />
                      </button>
                      <button
                        className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
                        aria-label={`Edit ${c.domain}`}
                        onClick={() => onEdit(c)}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        className="rounded p-1.5 text-red-400 transition hover:bg-red-500/10 hover:text-red-300"
                        aria-label={`Delete ${c.domain}`}
                        onClick={() => onDelete(c)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {pages > 1 && (
        <div className="flex items-center justify-between text-sm">
          <span className="text-slate-500">
            Page {current} of {pages} &middot; {filtered.length} domain
            {filtered.length === 1 ? '' : 's'}
          </span>
          <div className="flex gap-2">
            <button
              className="btn-secondary !py-1"
              disabled={current <= 1}
              onClick={() => setPage(current - 1)}
            >
              Previous
            </button>
            <button
              className="btn-secondary !py-1"
              disabled={current >= pages}
              onClick={() => setPage(current + 1)}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Activity, Network, Server } from 'lucide-react'
import type { ListRow } from '@/utils/monitoringList'

/** ago is a row's Last seen: "just now", "40s ago", "6m ago", "never". */
// eslint-disable-next-line react-refresh/only-export-components
export function ago(iso: string | null): string {
  if (!iso) return 'never'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${Math.floor(s)}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

const ICON: Record<ListRow['kind'], { Icon: typeof Activity; cls: string }> = {
  uptime: { Icon: Activity, cls: 'bg-sky-500/15 text-sky-300' },
  servers: { Icon: Server, cls: 'bg-violet-500/15 text-violet-300' },
  devices: { Icon: Network, cls: 'bg-emerald-500/15 text-emerald-300' },
}

interface Props {
  kind: ListRow['kind']
  name: ReactNode
  line: string
  kindText: string
  site: string | null
  status: ReactNode
  health: ReactNode
  /** Health as plain text, for the phone card. */
  healthText: string
  uptime: ReactNode
  seen: string
  actions: ReactNode
  /** The row's own page. */
  to: string
}

/** One Monitoring list row: the eight columns from md up, and below that a
 *  two-line card in a single cell (name and status, then kind, site, health
 *  and last seen). A click anywhere on the row opens its page; the name is a
 *  real link, so Tab reaches it, Enter opens it, and it opens in a new tab. */
export default function ListRowLayout(p: Props) {
  const navigate = useNavigate()
  const { Icon, cls } = ICON[p.kind]
  const icon = (
    <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${cls}`} aria-hidden>
      <Icon className="h-4 w-4" />
    </span>
  )
  const stop = (e: React.MouseEvent) => e.stopPropagation()
  // The link navigates by itself; the row's click must not do it again.
  const name = (
    <Link
      to={p.to}
      onClick={stop}
      className="-mx-1 inline-flex min-w-0 max-w-full items-center gap-2 rounded px-1 font-medium text-slate-200 outline-none hover:text-white focus-visible:ring-2 focus-visible:ring-primary-400"
    >
      {p.name}
    </Link>
  )
  return (
    <tr className="cursor-pointer transition focus-within:bg-white/5 hover:bg-white/5" onClick={() => navigate(p.to)} data-row-kind={p.kind}>
      <td className="hidden px-4 py-3 md:table-cell">
        <div className="flex items-center gap-3">
          {icon}
          <div className="min-w-0">
            {name}
            <div className="max-w-[22rem] truncate text-xs text-slate-500">{p.line}</div>
          </div>
        </div>
      </td>
      <td className="hidden whitespace-nowrap px-4 py-3 text-slate-400 md:table-cell">{p.kindText}</td>
      <td className="hidden px-4 py-3 text-slate-400 md:table-cell">{p.site ?? '—'}</td>
      <td className="hidden px-4 py-3 md:table-cell">{p.status}</td>
      <td className="hidden px-4 py-3 md:table-cell">{p.health}</td>
      <td className="hidden px-4 py-3 md:table-cell">{p.uptime}</td>
      <td className="hidden whitespace-nowrap px-4 py-3 text-xs text-slate-500 md:table-cell">{p.seen}</td>
      <td className="hidden px-4 py-3 text-slate-400 md:table-cell" onClick={stop}>
        {p.actions}
      </td>
      <td className="px-3 py-3 md:hidden" data-phone-card>
        <div className="flex items-center gap-2.5">
          {icon}
          <div className="min-w-0 flex-1">
            {name}
            <div className="truncate text-xs text-slate-500">
              {[p.kindText, p.site ?? 'No site', p.healthText, p.seen].filter(Boolean).join(' · ')}
            </div>
          </div>
          {p.status}
          <div onClick={stop}>{p.actions}</div>
        </div>
      </td>
    </tr>
  )
}

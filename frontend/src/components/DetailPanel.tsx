import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Pause, Pencil, Trash2, Loader2, AlertTriangle } from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import api from '@/services/api'
import {
  usePauseMonitor,
  useResumeMonitor,
  useDeleteMonitor,
  useTestMonitor,
} from '@/hooks/useMonitors'
import { useMoveMonitorToGroup } from '@/hooks/useMonitorGroups'
import ActionMenu, { type ActionItem } from '@/components/ActionMenu'
import { formatDatetime } from '@/utils/formatters'
import type { HourPoint, HourStatus, UptimeHistory } from '@/hooks/useMonitorUptime'
import type { MonitorAccess } from '@/utils/monitorAccess'
import type { Monitor, MonitorGroup } from '@/types'

export function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-emerald-500'
  if (pct >= 80) return 'text-amber-500'
  return 'text-red-500'
}

const STATUS_COLOR: Record<HourStatus, string> = {
  up: '#10b981',
  down: '#ef4444',
  partial: '#f59e0b',
  nodata: '#cbd5e1',
}

// Sparkline renders 24 hourly bars colored by status. Bar height reflects the
// hour's uptime (with a floor so down/no-data hours stay visible). Native title
// tooltips show the hour + status. Shared by the card and the detail panel.
export function Sparkline({ data, className }: { data: HourPoint[]; className?: string }) {
  const height = (d: HourPoint) => (d.status === 'nodata' ? 15 : Math.max(12, d.uptime))
  return (
    <div className={`flex items-end gap-px ${className ?? ''}`}>
      {data.map((d, i) => (
        <div
          key={i}
          title={`${String(d.hour).padStart(2, '0')}:00 — ${d.status}${d.status === 'nodata' ? '' : ` (${d.uptime}%)`}`}
          className="flex-1 rounded-sm"
          style={{ height: `${height(d)}%`, minWidth: 2, backgroundColor: STATUS_COLOR[d.status] }}
        />
      ))}
    </div>
  )
}

interface IncidentRow {
  id: string
  start_time: string
  end_time: string | null
  duration_seconds: number
}

// severity is derived from downtime length (or ongoing).
function severityOf(inc: IncidentRow): { label: string; cls: string } {
  if (inc.end_time === null || inc.duration_seconds >= 3600)
    return { label: 'high', cls: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300' }
  if (inc.duration_seconds >= 900)
    return { label: 'medium', cls: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300' }
  return { label: 'low', cls: 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400' }
}

function UptimeBox({ label, pct }: { label: string; pct: number | undefined }) {
  return (
    <div className="rounded-md bg-neutral-50 p-3 text-center dark:bg-neutral-800/50">
      <div className={`text-2xl font-bold ${pct != null ? uptimeColor(pct) : 'text-neutral-400'}`}>
        {pct != null ? `${pct.toFixed(2)}%` : '—'}
      </div>
      <div className="text-xs text-neutral-500 dark:text-neutral-400">{label}</div>
    </div>
  )
}

interface Props {
  monitor: Monitor
  uptime: UptimeHistory | null
  uptimeLoading: boolean
  groups: MonitorGroup[]
  access: MonitorAccess
  ownerUsername?: string
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function DetailPanel({ monitor, uptime, uptimeLoading, groups, access, ownerUsername, onChanged, push }: Props) {
  const navigate = useNavigate()
  const { pause, loading: pausing } = usePauseMonitor(monitor.id)
  const { resume, loading: resuming } = useResumeMonitor(monitor.id)
  const { delete: del, loading: deleting } = useDeleteMonitor(monitor.id)
  const { test, loading: testing } = useTestMonitor(monitor.id)
  const { move } = useMoveMonitorToGroup()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const busy = pausing || resuming || deleting || testing

  // Incidents load lazily — the panel only mounts when the card is expanded.
  const [incidents, setIncidents] = useState<IncidentRow[] | null>(null)
  useEffect(() => {
    let active = true
    const now = new Date()
    const start = new Date(now.getTime() - 30 * 864e5)
    api
      .get<{ data: { incidents: IncidentRow[] } }>(`/monitors/${monitor.id}/incidents`, {
        params: { start_time: start.toISOString(), end_time: now.toISOString() },
      })
      .then((r) => active && setIncidents(r.data.data.incidents ?? []))
      .catch(() => active && setIncidents([]))
    return () => {
      active = false
    }
  }, [monitor.id])

  const act = async (fn: () => Promise<unknown>, okMsg: string) => {
    try {
      await fn()
      push(okMsg, 'success')
      onChanged()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Action failed', 'error')
    }
  }

  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const inMaintenance = monitor.is_in_maintenance ?? false

  // Action set gated by permission: readonly users get Test only; editable/admin
  // add Pause/Resume + Edit; only owner/admin get Delete (see monitorAccess).
  const actions: ActionItem[] = [
    { key: 'test', label: 'Test', icon: Play, disabled: busy, onClick: () => void act(() => test(), 'Test complete') },
  ]
  if (access.canEdit) {
    actions.push(
      monitor.enabled
        ? { key: 'pause', label: 'Pause', icon: Pause, disabled: busy, onClick: () => void act(() => pause(), 'Monitor paused') }
        : { key: 'resume', label: 'Resume', icon: Play, disabled: busy, onClick: () => void act(() => resume(), 'Monitor resumed') },
      { key: 'edit', label: 'Edit', icon: Pencil, onClick: () => navigate(`/monitors/${monitor.id}/edit`) }
    )
  }
  if (access.canDelete) {
    actions.push({ key: 'delete', label: 'Delete', icon: Trash2, danger: true, disabled: busy, onClick: () => setConfirmDelete(true) })
  }

  return (
    <div className="space-y-4 border-t border-neutral-200 p-4 dark:border-neutral-800">
      {/* Actions row */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-lg font-semibold">{monitor.name}</span>
            <span
              className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                inMaintenance
                  ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
                  : online
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : offline
                      ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                      : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
              }`}
            >
              {inMaintenance ? 'Maintenance' : online ? 'Online' : offline ? 'Offline' : 'Unknown'}
            </span>
          </div>
          <div className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">
            {access.isOwner
              ? 'Your monitor'
              : access.permission === 'admin'
                ? `Owned by ${ownerUsername ?? 'another user'} · admin access`
                : `Shared with you by ${ownerUsername ?? 'another user'} · ${access.permission === 'editable' ? 'can edit' : 'read-only'}`}
          </div>
        </div>
        {/* Desktop: inline buttons. Mobile: dropdown menu. */}
        <div className="hidden flex-wrap gap-1.5 sm:flex">
          {actions.map((a) => (
            <button
              key={a.key}
              disabled={a.disabled}
              onClick={a.onClick}
              className={`btn-secondary !py-1 ${
                a.danger ? '!border-error-300 !text-error-600 hover:!bg-error-50 dark:hover:!bg-error-900/20' : ''
              }`}
            >
              <a.icon className="h-4 w-4" /> {a.label}
            </button>
          ))}
        </div>
        <div className="sm:hidden">
          <ActionMenu items={actions} />
        </div>
      </div>

      {!access.canEdit && (
        <div className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 text-xs text-neutral-500 dark:border-neutral-800 dark:bg-neutral-800/50 dark:text-neutral-400">
          Read-only access — you can view and test this monitor but not edit it.
        </div>
      )}

      {confirmDelete && (
        <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-900/30">
          <p className="mb-2 text-amber-800 dark:text-amber-200">Delete “{monitor.name}” and all its history?</p>
          <div className="flex justify-end gap-2">
            <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
              Cancel
            </button>
            <button
              className="btn bg-error-600 !py-1 text-white hover:bg-error-700"
              disabled={deleting}
              onClick={() =>
                void act(async () => {
                  await del()
                  setConfirmDelete(false)
                }, 'Monitor deleted')
              }
            >
              {deleting ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </div>
      )}

      {/* Uptime windows */}
      <div className="grid grid-cols-3 gap-2">
        <UptimeBox label="24-hour" pct={uptime?.uptime_24h} />
        <UptimeBox label="7-day" pct={uptime?.uptime_7d} />
        <UptimeBox label="30-day" pct={uptime?.uptime_30d} />
      </div>

      {/* Detailed 24h sparkline */}
      <div>
        <div className="mb-1 flex items-center justify-between text-xs font-medium text-neutral-500 dark:text-neutral-400">
          <span>Uptime (last 24h)</span>
          <span className="flex items-center gap-3">
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.up }} /> up</span>
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.partial }} /> partial</span>
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.down }} /> down</span>
          </span>
        </div>
        {uptime ? (
          <Sparkline data={uptime.hourly_data} className="h-10" />
        ) : (
          <div className="flex h-10 items-center text-xs text-neutral-400">{uptimeLoading ? 'Loading…' : 'No data'}</div>
        )}
      </div>

      {/* Response time chart */}
      <div>
        <div className="mb-1 text-xs font-medium text-neutral-500 dark:text-neutral-400">Response time (24h)</div>
        <div className="h-52">
          {uptimeLoading ? (
            <div className="flex h-full items-center justify-center text-neutral-400">
              <Loader2 className="h-5 w-5 animate-spin" />
            </div>
          ) : uptime && uptime.response_time_data.some((p) => p.responseTime > 0) ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={uptime.response_time_data} margin={{ top: 5, right: 10, bottom: 0, left: -8 }}>
                <defs>
                  <linearGradient id={`rt-${monitor.id}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.25} />
                    <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#64748b" strokeOpacity={0.15} />
                <XAxis dataKey="time" tick={{ fontSize: 10 }} interval={5} minTickGap={16} />
                <YAxis tick={{ fontSize: 10 }} width={40} unit="" />
                <Tooltip formatter={(v: number) => [`${v} ms`, 'response']} />
                <Area type="monotone" dataKey="responseTime" stroke="#10b981" strokeWidth={2} fill={`url(#rt-${monitor.id})`} />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-neutral-400">
              No response data in the last 24h.
            </div>
          )}
        </div>
      </div>

      {/* Tags + move-to-group */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-neutral-500 dark:text-neutral-400">Tags:</span>
          {monitor.tags && monitor.tags.length > 0 ? (
            monitor.tags.map((t) => (
              <span key={t} className="rounded-full bg-primary-100 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                {t}
              </span>
            ))
          ) : (
            <span className="text-xs text-neutral-400">none</span>
          )}
        </div>
        {access.canEdit && (
          <label className="flex items-center gap-2 text-xs">
            <span className="text-neutral-500 dark:text-neutral-400">Group:</span>
            <select
              className="rounded-md border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-800"
              value={monitor.group_id ?? ''}
              onChange={(e) => void act(() => move(monitor.id, e.target.value || null), 'Monitor group updated')}
            >
              <option value="">Ungrouped</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>

      {/* Incident timeline */}
      <div>
        <div className="mb-1 text-xs font-medium text-neutral-500 dark:text-neutral-400">Recent incidents</div>
        {incidents === null ? (
          <p className="text-xs text-neutral-400">Loading…</p>
        ) : incidents.length === 0 ? (
          <p className="text-xs text-neutral-400">No incidents in the last 30 days.</p>
        ) : (
          <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
            {incidents.slice(0, 5).map((inc) => {
              const sev = severityOf(inc)
              const ongoing = inc.end_time === null
              return (
                <div key={inc.id} className="flex items-center justify-between gap-2 py-1.5 text-xs">
                  <span className="flex items-center gap-1.5">
                    <AlertTriangle className="h-3 w-3 text-red-400" />
                    {formatDatetime(inc.start_time)}
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="text-neutral-500 dark:text-neutral-400">
                      {ongoing ? 'ongoing' : `${Math.max(1, Math.round(inc.duration_seconds / 60))} min`}
                    </span>
                    <span className={`rounded px-1.5 py-0.5 font-medium ${sev.cls}`}>{sev.label}</span>
                    <span className={`rounded px-1.5 py-0.5 ${ongoing ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300' : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'}`}>
                      {ongoing ? 'ongoing' : 'closed'}
                    </span>
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

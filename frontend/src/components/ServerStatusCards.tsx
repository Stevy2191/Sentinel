import type { ReactNode } from 'react'
import { CheckCircle2, Clock, Cpu, MemoryStick, HardDrive, XCircle, CircleDashed } from 'lucide-react'
import type { Agent, AgentMetric } from '@/hooks/useAgents'

/** Whole literal class strings per tone — Tailwind only emits what it can see. */
const TONES = {
  blue: { border: 'border-blue-500/30', bg: 'from-blue-600/10', icon: 'text-blue-400', bar: 'bg-blue-500' },
  green: { border: 'border-emerald-500/30', bg: 'from-emerald-600/10', icon: 'text-emerald-400', bar: 'bg-emerald-500' },
  slate: { border: 'border-slate-500/30', bg: 'from-slate-600/10', icon: 'text-slate-300', bar: 'bg-slate-400' },
  amber: { border: 'border-amber-500/30', bg: 'from-amber-600/10', icon: 'text-amber-400', bar: 'bg-amber-500' },
  red: { border: 'border-red-500/30', bg: 'from-red-600/10', icon: 'text-red-400', bar: 'bg-red-500' },
} as const

type Tone = keyof typeof TONES

/**
 * Picks the tone from the number rather than fixing it per card.
 *
 * A disk card that stays amber whether it is 12% or 96% full is decoration. It
 * should be quiet until the figure deserves attention.
 */
function usageTone(pct: number | null | undefined, quiet: Tone): Tone {
  if (pct == null) return 'slate'
  if (pct >= 90) return 'red'
  if (pct >= 75) return 'amber'
  return quiet
}

function StatusCard({
  icon,
  title,
  value,
  tone,
  children,
  progress,
}: {
  icon: ReactNode
  title: string
  value: string
  tone: Tone
  children?: ReactNode
  progress?: number | null
}) {
  const t = TONES[tone]
  return (
    <div
      className={`rounded-lg border bg-gradient-to-br to-transparent p-4 backdrop-blur-sm ${t.border} ${t.bg}`}
    >
      <div className="flex items-center gap-2">
        <span className={t.icon} aria-hidden>
          {icon}
        </span>
        <span className="text-xs font-medium uppercase tracking-wider text-slate-400">{title}</span>
      </div>
      <p className="mt-3 text-2xl font-light tabular-nums text-white">{value}</p>
      {progress != null && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-white/10">
          <div
            className={`h-full rounded-full ${t.bar}`}
            style={{ width: `${Math.max(0, Math.min(100, progress))}%` }}
          />
        </div>
      )}
      {children && <div className="mt-2 space-y-0.5 text-xs text-slate-500">{children}</div>}
    </div>
  )
}

function pct(v: number | null | undefined): string {
  return v == null ? '—' : `${v.toFixed(1)}%`
}

function gb(mb: number | null | undefined): string {
  if (mb == null) return '—'
  return `${(mb / 1024).toFixed(1)} GB`
}

/** "4d 4h 8m", the way an operator reads uptime. */
export function formatUptime(seconds: number | null | undefined): string {
  if (!seconds) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

const STATUS_TEXT: Record<string, { label: string; tone: Tone; connection: string }> = {
  active: { label: 'Up', tone: 'blue', connection: 'connected' },
  offline: { label: 'Down', tone: 'red', connection: 'not reporting' },
  pending: { label: 'Waiting', tone: 'slate', connection: 'never connected' },
}

export default function ServerStatusCards({
  agent,
  metric,
}: {
  agent: Agent
  metric: AgentMetric | null
}) {
  const status = STATUS_TEXT[agent.status] ?? STATUS_TEXT.pending
  const statusIcon =
    agent.status === 'active' ? (
      <CheckCircle2 className="h-4 w-4" />
    ) : agent.status === 'offline' ? (
      <XCircle className="h-4 w-4" />
    ) : (
      <CircleDashed className="h-4 w-4" />
    )

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
      <StatusCard icon={statusIcon} title="Status" value={status.label} tone={status.tone}>
        <p>Agent: {agent.status === 'active' ? 'running' : 'not reporting'}</p>
        <p>Connection: {status.connection}</p>
      </StatusCard>

      <StatusCard
        icon={<Clock className="h-4 w-4" />}
        title="Uptime"
        value={formatUptime(metric?.uptime_seconds)}
        tone="green"
      >
        <p>
          Last check:{' '}
          {agent.last_heartbeat ? new Date(agent.last_heartbeat).toLocaleString() : 'never'}
        </p>
      </StatusCard>

      <StatusCard
        icon={<Cpu className="h-4 w-4" />}
        title="CPU"
        value={pct(metric?.cpu_percent)}
        tone={usageTone(metric?.cpu_percent, 'slate')}
        progress={metric?.cpu_percent ?? null}
      >
        <p>
          Used: {pct(metric?.cpu_percent)}
          {agent.cpu_cores ? ` · ${agent.cpu_cores} core${agent.cpu_cores === 1 ? '' : 's'}` : ''}
        </p>
      </StatusCard>

      <StatusCard
        icon={<MemoryStick className="h-4 w-4" />}
        title="Memory"
        value={pct(metric?.memory_percent)}
        tone={usageTone(metric?.memory_percent, 'green')}
        progress={metric?.memory_percent ?? null}
      >
        <p>
          Used: {gb(metric?.memory_used_mb)} · Total: {gb(metric?.memory_total_mb)}
        </p>
      </StatusCard>

      <StatusCard
        icon={<HardDrive className="h-4 w-4" />}
        title="Disk"
        value={pct(metric?.disk_percent)}
        tone={usageTone(metric?.disk_percent, 'amber')}
        progress={metric?.disk_percent ?? null}
      >
        <p>
          Used: {metric?.disk_used_gb?.toFixed(1) ?? '—'} GB · Total:{' '}
          {metric?.disk_total_gb?.toFixed(1) ?? '—'} GB
        </p>
      </StatusCard>
    </div>
  )
}

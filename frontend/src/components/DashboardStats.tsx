interface Props {
  total: number
  online: number
  offline: number
}

function StatCard({ label, value, color }: { label: string; value: string | number; color: string }) {
  return (
    <div className="rd-card p-6" style={{ borderRadius: '12px', ['--rd-accent' as string]: color }}>
      <p className="mb-3 text-xs font-black tracking-wider" style={{ color }}>
        {label}
      </p>
      <p className="text-3xl font-black" style={{ color: 'var(--rd-text)' }}>
        {value}
      </p>
    </div>
  )
}

/** Three accent stat cards: Total Monitors, Online, Offline. */
export default function DashboardStats({ total, online, offline }: Props) {
  return (
    <div className="grid grid-cols-3 gap-4">
      <StatCard label="TOTAL MONITORS" value={total} color="var(--color-accent-primary)" />
      <StatCard label="ONLINE" value={online} color="var(--color-accent-online)" />
      <StatCard label="OFFLINE" value={offline} color="var(--color-accent-offline)" />
    </div>
  )
}

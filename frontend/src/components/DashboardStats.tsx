interface Props {
  total: number
  online: number
  offline: number
}

function Readout({
  label,
  value,
  color,
  unit,
}: {
  label: string
  value: number
  color: string
  unit: string
}) {
  return (
    <div className="rd-card px-5 py-4" style={{ ['--rd-accent' as string]: color }}>
      <p className="vs-eyebrow">{label}</p>
      <div className="mt-2 flex items-baseline gap-1.5">
        <span className="vs-readout vs-glow text-4xl font-medium leading-none" style={{ color }}>
          {value}
        </span>
        <span className="vs-eyebrow" style={{ letterSpacing: '0.1em' }}>
          {unit}
        </span>
      </div>
    </div>
  )
}

/** Three station readouts: services watched, alive, critical. */
export default function DashboardStats({ total, online, offline }: Props) {
  return (
    <div className="grid grid-cols-3 gap-3">
      <Readout label="Watched" value={total} unit="svc" color="var(--vs-cyan)" />
      <Readout label="Alive" value={online} unit="up" color="var(--vs-ecg)" />
      <Readout label="Critical" value={offline} unit="down" color="var(--vs-flat)" />
    </div>
  )
}

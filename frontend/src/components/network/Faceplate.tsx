import { useMemo } from 'react'
import type { FaceBlock, FacePort, Faceplate as Layout, PortView } from '@/hooks/usePorts'
import { busiestUtil, PORT_STATE, portLiveDetail, portState, portTitle, type PortState } from '@/utils/network'

// RJ45 outline with the latch notch facing away from the other row, as on
// the real switch: top-row notch at the bottom, bottom-row notch at the top.
const RJ45_TOP = 'polygon(0 0,100% 0,100% 70%,72% 70%,72% 100%,28% 100%,28% 70%,0 70%)'
const RJ45_BOTTOM = 'polygon(28% 0,72% 0,72% 30%,100% 30%,100% 100%,0 100%,0 30%,28% 30%)'

interface Props {
  layout: Layout
  ports: PortView[]
  title: string
  subtitle?: string
  selected: number | null
  onSelect: (ifIndex: number) => void
}

/** The virtual switch: every physical port coloured by state and filled from
 *  the bottom by how busy it is. */
export default function Faceplate({ layout, ports, title, subtitle, selected, onSelect }: Props) {
  const byIndex = useMemo(() => new Map(ports.map((p) => [p.if_index, p])), [ports])
  return (
    <div className="overflow-x-auto pb-1">
      <div className="inline-flex items-center gap-5 rounded-lg border border-slate-700/70 bg-gradient-to-b from-slate-900 to-slate-950 px-4 py-3 shadow-inner">
        <div className="w-28 shrink-0">
          <p className="flex items-center gap-1.5 truncate text-xs font-semibold text-slate-200">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-400" aria-hidden />
            {title}
          </p>
          {subtitle && <p className="truncate text-[11px] text-slate-500">{subtitle}</p>}
        </div>
        {layout.blocks.map((b, i) => (
          <Block key={i} block={b} rows={layout.rows} byIndex={byIndex} selected={selected} onSelect={onSelect} />
        ))}
      </div>
    </div>
  )
}

function Block({
  block,
  rows,
  byIndex,
  selected,
  onSelect,
}: {
  block: FaceBlock
  rows: number
  byIndex: Map<number, PortView>
  selected: number | null
  onSelect: (ifIndex: number) => void
}) {
  const cols = Math.max(block.top.length, block.bottom.length)
  const cell = (fp: FacePort | undefined, flip: boolean) =>
    fp ? (
      <PortCell fp={fp} port={byIndex.get(fp.if_index)} sfp={block.sfp} flip={flip} selected={selected === fp.if_index} onSelect={onSelect} />
    ) : (
      <span className={`h-5 ${block.sfp ? 'w-8' : 'w-6'}`} />
    )
  return (
    <div className="flex gap-1">
      {Array.from({ length: cols }, (_, c) => (
        <div key={c} className="flex flex-col items-center gap-1">
          <Num fp={block.top[c]} />
          {cell(block.top[c], false)}
          {rows === 2 && cell(block.bottom[c], true)}
          {rows === 2 && <Num fp={block.bottom[c]} />}
        </div>
      ))}
    </div>
  )
}

function Num({ fp }: { fp?: FacePort }) {
  return <span className="h-3 text-[9.5px] leading-3 tabular-nums text-slate-500">{fp?.number ?? ''}</span>
}

function PortCell({
  fp,
  port,
  sfp,
  flip,
  selected,
  onSelect,
}: {
  fp: FacePort
  port: PortView | undefined
  sfp: boolean
  flip: boolean
  selected: boolean
  onSelect: (ifIndex: number) => void
}) {
  const state: PortState = port ? portState(port) : 'down'
  const util = port ? busiestUtil(port) : null
  const detail = port ? portLiveDetail(port) : ''
  const label = port ? `${portTitle(port)}: ${PORT_STATE[state].label}${detail ? ` · ${detail}` : ''}` : `Port ${fp.number}`
  return (
    <button
      type="button"
      onClick={() => onSelect(fp.if_index)}
      title={label}
      aria-label={label}
      aria-pressed={selected}
      className={`relative h-5 ${sfp ? 'w-8' : 'w-6'} overflow-hidden rounded-sm transition hover:brightness-125 focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 ${PORT_STATE[state].fill} ${selected ? 'ring-2 ring-sky-400' : ''}`}
      style={sfp || selected ? undefined : { clipPath: flip ? RJ45_BOTTOM : RJ45_TOP }}
    >
      {util != null && util > 0 && (
        <span className="absolute inset-x-0 bottom-0 bg-white/55" style={{ height: `${Math.min(100, Math.max(8, util))}%` }} />
      )}
    </button>
  )
}

export function FaceplateLegend() {
  const order: PortState[] = ['up', 'warning', 'critical', 'down', 'disabled']
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-400">
      {order.map((s) => (
        <span key={s} className="flex items-center gap-1.5">
          <span className={`h-2.5 w-3.5 rounded-sm ${PORT_STATE[s].fill}`} aria-hidden />
          {PORT_STATE[s].label}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <span className="relative h-2.5 w-3.5 overflow-hidden rounded-sm bg-emerald-500" aria-hidden>
          <span className="absolute inset-x-0 bottom-0 h-1/2 bg-white/55" />
        </span>
        Fill shows how busy
      </span>
    </div>
  )
}

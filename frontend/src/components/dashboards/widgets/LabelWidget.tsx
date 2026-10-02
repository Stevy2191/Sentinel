import type { LabelData } from '@/types/dashboards'

const SIZE = { s: 'text-lg', m: 'text-2xl', l: 'text-4xl' } as const

export default function LabelWidget({ data }: { data: LabelData }) {
  return (
    <div className="flex h-full items-center">
      <p className={`whitespace-pre-line font-light text-white ${SIZE[data.size] ?? SIZE.m}`}>{data.text}</p>
    </div>
  )
}

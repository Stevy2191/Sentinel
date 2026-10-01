import type { ReactNode } from 'react'
import { Search } from 'lucide-react'
import { DEVICE_TYPE_LABEL, type DeviceStatus, type DeviceType } from '@/hooks/useDevices'
import type { DeviceFilters } from '@/utils/devices'

interface Props {
  filters: DeviceFilters
  onChange: (f: DeviceFilters) => void
  /** Extra controls after the search box, e.g. the Devices page's site picker. */
  children?: ReactNode
}

export default function DeviceFilterBar({ filters, onChange, children }: Props) {
  const set = <K extends keyof DeviceFilters>(k: K, v: DeviceFilters[K]) => onChange({ ...filters, [k]: v })
  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <div className="relative min-w-[220px] flex-1">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
        <input
          value={filters.search}
          onChange={(e) => set('search', e.target.value)}
          placeholder="Search name, address, vendor, model…"
          aria-label="Search devices"
          className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
        />
      </div>
      {children}
      <select className="rd-select" value={filters.status} onChange={(e) => set('status', e.target.value as DeviceStatus | '')} aria-label="Filter by status">
        <option value="">All statuses</option>
        <option value="down">Down</option>
        <option value="up">Up</option>
        <option value="error">Error</option>
        <option value="pending">Pending</option>
        <option value="paused">Paused</option>
      </select>
      <select className="rd-select" value={filters.type} onChange={(e) => set('type', e.target.value as DeviceType | '')} aria-label="Filter by type">
        <option value="">All types</option>
        {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
          <option key={t} value={t}>
            {DEVICE_TYPE_LABEL[t]}
          </option>
        ))}
      </select>
    </div>
  )
}

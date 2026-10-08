import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import DeviceTable from '@/components/network/DeviceTable'
import { DEVICE_TYPE_LABEL, type Device, type DeviceType } from '@/hooks/useDevices'
import type { MonitoringFilters } from '@/utils/monitoringView'

interface Props {
  /** Rows after every filter. */
  devices: Device[]
  /** How many devices there are at all. */
  total: number
  isAdmin: boolean
  filters: MonitoringFilters
  onFilters: (next: MonitoringFilters) => void
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onAdd: () => void
}

export default function DevicesSection(p: Props) {
  const down = p.devices.filter((d) => d.status === 'down').length
  return (
    <SectionShell
      title="Devices"
      count={p.devices.length}
      down={down}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      controls={
        <select
          className="rd-select"
          aria-label="Filter by device type"
          value={p.filters.deviceType ?? ''}
          onChange={(e) => p.onFilters({ ...p.filters, deviceType: (e.target.value || null) as DeviceType | null })}
        >
          <option value="">All device types</option>
          {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
            <option key={t} value={t}>
              {DEVICE_TYPE_LABEL[t]}
            </option>
          ))}
        </select>
      }
    >
      {p.total === 0 ? (
        !p.error && (
          <EmptyLine
            text={p.isAdmin ? "No devices yet. Add one, or scan a subnet from a site's page." : 'No devices yet.'}
            action={p.isAdmin ? 'Add a device' : undefined}
            onAction={p.onAdd}
          />
        )
      ) : p.devices.length === 0 ? (
        <EmptyLine text="Nothing in this section matches these filters." />
      ) : (
        <DeviceTable devices={p.devices} showSite />
      )}
    </SectionShell>
  )
}

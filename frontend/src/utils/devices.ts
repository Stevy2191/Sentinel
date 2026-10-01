import type { Device, DeviceStatus, DeviceType } from '@/hooks/useDevices'

export function deviceType(d: Device): DeviceType {
  return d.effective_type ?? d.device_type_detected ?? 'other'
}

export function vendorModel(d: Device): string {
  return [d.effective_vendor || d.vendor, d.effective_model || d.model].filter(Boolean).join(' · ')
}

export interface DeviceFilters {
  search: string
  status: DeviceStatus | ''
  type: DeviceType | ''
}

export const NO_DEVICE_FILTERS: DeviceFilters = { search: '', status: '', type: '' }

/** The devices matching the filters: search covers name, address, vendor,
 *  model and site. */
export function filterDevices(devices: Device[], f: DeviceFilters): Device[] {
  const q = f.search.trim().toLowerCase()
  return devices.filter(
    (d) =>
      (!f.status || d.status === f.status) &&
      (!f.type || deviceType(d) === f.type) &&
      (!q || [d.name, d.host, vendorModel(d), d.site_name].some((v) => v?.toLowerCase().includes(q)))
  )
}

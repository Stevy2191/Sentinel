import { useState } from 'react'
import { X } from 'lucide-react'
import { DEVICE_TYPE_LABEL, type Device, type DeviceType } from '@/hooks/useDevices'
import { usePortActions, type FaceplatePortStyle } from '@/hooks/usePorts'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  device: Device
  onClose: () => void
  onSaved: () => void
  /** The instance UPS thresholds, shown as the fields' placeholders. */
  upsDefaults?: { low: number; high: number }
}

/** The user's corrections to what SNMP reports. Inventory never overwrites
 *  them; clearing a field shows the SNMP value again. */
export default function EditDetailsModal({ device, onClose, onSaved, upsDefaults }: Props) {
  const { updateDetails, busy } = usePortActions()
  const [vendor, setVendor] = useState(device.vendor_override ?? '')
  const [model, setModel] = useState(device.model_override ?? '')
  const [location, setLocation] = useState(device.location_override ?? '')
  const [type, setType] = useState<DeviceType | ''>(device.device_type ?? '')
  const [rows, setRows] = useState(device.faceplate_rows ? String(device.faceplate_rows) : '')
  const [sfp, setSfp] = useState((device.faceplate_sfp_ports ?? []).join(', '))
  const [lowBattery, setLowBattery] = useState(device.ups_low_battery_pct != null ? String(device.ups_low_battery_pct) : '')
  const [highLoad, setHighLoad] = useState(device.ups_high_load_pct != null ? String(device.ups_high_load_pct) : '')
  const [portStyle, setPortStyle] = useState<FaceplatePortStyle | ''>(device.faceplate_port_style ?? '')
  const [error, setError] = useState<string | null>(null)

  const isUPS = (type || device.device_type_detected) === 'ups'

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const ports = sfp.split(/[\s,]+/).filter(Boolean).map(Number)
    if (ports.some((n) => !Number.isInteger(n) || n < 1)) {
      setError('SFP ports are port numbers, e.g. 49, 50, 51, 52')
      return
    }
    try {
      await updateDetails(device.id, {
        vendor_override: vendor,
        model_override: model,
        location_override: location,
        device_type: type || null,
        faceplate_rows: rows ? Number(rows) : null,
        faceplate_sfp_ports: ports.length ? ports : null,
        faceplate_port_style: portStyle || null,
        // Only a UPS sends its thresholds, so saving a switch never touches them.
        ...(isUPS ? { ups_low_battery_pct: lowBattery ? Number(lowBattery) : null, ups_high_load_pct: highLoad ? Number(highLoad) : null } : {}),
      })
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the details')
    }
  }

  const field = (label: string, value: string, set: (v: string) => void, reported: string) => (
    <label className="block space-y-1">
      <span className="text-sm text-slate-300">{label}</span>
      <input className={inputCls} value={value} onChange={(e) => set(e.target.value)} placeholder={reported || 'Not reported'} maxLength={255} />
      <span className="flex items-center justify-between gap-2 text-xs text-slate-500">
        <span className="truncate">SNMP says: {reported || 'nothing'}</span>
        {value && (
          <button type="button" className="shrink-0 text-primary-400 hover:underline" onClick={() => set('')}>
            Use SNMP value
          </button>
        )}
      </span>
    </label>
  )

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-md space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Edit device details</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">Your values are shown instead of what the device reports, and are never overwritten.</p>

        {field('Vendor', vendor, setVendor, device.vendor)}
        {field('Model', model, setModel, device.model)}
        {field('Location', location, setLocation, device.sys_location)}

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Device type</span>
          <select className={inputCls} value={type} onChange={(e) => setType(e.target.value as DeviceType | '')}>
            <option value="">Automatic ({DEVICE_TYPE_LABEL[device.device_type_detected] ?? 'Other'})</option>
            {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
              <option key={t} value={t}>
                {DEVICE_TYPE_LABEL[t]}
              </option>
            ))}
          </select>
          {type && type !== device.device_type_detected && (
            <span className="block text-xs text-amber-400">
              Sentinel detects this device as {DEVICE_TYPE_LABEL[device.device_type_detected] ?? 'Other'}. Your choice overrides
              that; pick Automatic to follow detection.
            </span>
          )}
          <span className="text-xs text-slate-500">Switches and routers are drawn as a faceplate.</span>
        </label>

        <fieldset className="space-y-3 rounded-lg border border-white/10 p-3">
          <legend className="px-1 text-sm text-slate-300">Faceplate</legend>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">Rows</span>
            <select className={inputCls} value={rows} onChange={(e) => setRows(e.target.value)}>
              <option value="">Automatic</option>
              <option value="1">One row</option>
              <option value="2">Two rows</option>
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">Port style</span>
            <select className={inputCls} value={portStyle} onChange={(e) => setPortStyle(e.target.value as FaceplatePortStyle | '')}>
              <option value="">Automatic</option>
              <option value="sfp">All SFP</option>
              <option value="rj45">All RJ45</option>
            </select>
            <span className="text-xs text-slate-500">How the main ports are drawn. Module ports are always SFP.</span>
          </label>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">SFP ports</span>
            <input className={inputCls} value={sfp} onChange={(e) => setSfp(e.target.value)} placeholder="Detected automatically" />
            <span className="text-xs text-slate-500">Port numbers to draw as SFP, e.g. 49, 50, 51, 52.</span>
          </label>
        </fieldset>

        {isUPS && (
  <fieldset className="space-y-3 rounded-lg border border-white/10 p-3">
    <legend className="px-1 text-sm text-slate-300">UPS alerts</legend>
    <label className="block space-y-1">
      <span className="text-xs text-slate-400">Low battery below (%)</span>
      <input className={inputCls} type="number" min={5} max={95} value={lowBattery} onChange={(e) => setLowBattery(e.target.value)}
        placeholder={upsDefaults ? `Use the default (${upsDefaults.low} %)` : 'Use the default'} />
    </label>
    <label className="block space-y-1">
      <span className="text-xs text-slate-400">High load at (%)</span>
      <input className={inputCls} type="number" min={10} max={100} value={highLoad} onChange={(e) => setHighLoad(e.target.value)}
        placeholder={upsDefaults ? `Use the default (${upsDefaults.high} %)` : 'Use the default'} />
    </label>
    <span className="block text-xs text-slate-500">Leave blank to use the default from Network settings.</span>
  </fieldset>
)}

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy}>
            Save details
          </button>
        </div>
      </form>
    </div>
  )
}

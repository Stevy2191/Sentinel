import { str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'

/** One device: the port grid and device health widgets. */
export default function DeviceSettings({ config, siteId, onChange }: SettingsProps) {
  const device = str(config, 'device_id')
  return (
    <Field label="Device">
      <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => onChange({ ...config, device_id: ids[0] })} />
    </Field>
  )
}

import { str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'

/** One site: the site power widget. */
export default function SiteSettings({ config, onChange }: SettingsProps) {
  return (
    <Field label="Site">
      <SitePicker value={str(config, 'site_id') || null} onChange={(id) => onChange({ ...config, site_id: id })} />
    </Field>
  )
}

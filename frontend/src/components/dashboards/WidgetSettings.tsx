import type { WidgetType } from '@/types/dashboards'
import LabelSettings from '@/components/dashboards/settings/LabelSettings'
import TimeseriesSettings from '@/components/dashboards/settings/TimeseriesSettings'
import StatSettings from '@/components/dashboards/settings/StatSettings'

interface Props {
  type: WidgetType
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}

/** The settings form for a widget's type. */
export default function WidgetSettings({ type, config, siteId, onChange }: Props) {
  const props = { config, siteId, onChange }
  switch (type) {
    case 'label':
      return <LabelSettings {...props} />
    case 'timeseries':
      return <TimeseriesSettings {...props} />
    case 'stat':
      return <StatSettings {...props} />
    default:
      return <p className="text-sm text-slate-500">This widget has no settings form yet.</p>
  }
}

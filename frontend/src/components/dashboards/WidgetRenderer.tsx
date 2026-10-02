import type { DeviceHealthData, LabelData, PortGridData, SitePowerData, StatData, TimeseriesData, WidgetType } from '@/types/dashboards'
import PortGridWidget from '@/components/dashboards/widgets/PortGridWidget'
import DeviceHealthWidget from '@/components/dashboards/widgets/DeviceHealthWidget'
import SitePowerWidget from '@/components/dashboards/widgets/SitePowerWidget'
import LabelWidget from '@/components/dashboards/widgets/LabelWidget'
import TimeseriesWidget from '@/components/dashboards/widgets/TimeseriesWidget'
import StatWidget from '@/components/dashboards/widgets/StatWidget'

interface Props {
  type: WidgetType
  data: unknown
  /** Logged-in views link into Sentinel; public views never do. */
  linkable: boolean
}

/** Draws a widget's data by its type. Each type's renderer only ever sees
 *  data in the ok state; WidgetFrame handles the rest. */
export default function WidgetRenderer({ type, data, linkable }: Props) {
  switch (type) {
    case 'label':
      return <LabelWidget data={data as LabelData} />
    case 'timeseries':
      return <TimeseriesWidget data={data as TimeseriesData} />
    case 'stat':
      return <StatWidget data={data as StatData} />
    case 'port_grid':
      return <PortGridWidget data={data as PortGridData} linkable={linkable} />
    case 'device_health':
      return <DeviceHealthWidget data={data as DeviceHealthData} linkable={linkable} />
    case 'site_power':
      return <SitePowerWidget data={data as SitePowerData} linkable={linkable} />
    default:
      return <p className="text-sm text-slate-500">This version of Sentinel cannot show this widget.</p>
  }
}

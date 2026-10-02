import type { LabelData, StatData, TimeseriesData, WidgetType } from '@/types/dashboards'
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
export default function WidgetRenderer({ type, data }: Props) {
  switch (type) {
    case 'label':
      return <LabelWidget data={data as LabelData} />
    case 'timeseries':
      return <TimeseriesWidget data={data as TimeseriesData} />
    case 'stat':
      return <StatWidget data={data as StatData} />
    default:
      return <p className="text-sm text-slate-500">This version of Sentinel cannot show this widget.</p>
  }
}

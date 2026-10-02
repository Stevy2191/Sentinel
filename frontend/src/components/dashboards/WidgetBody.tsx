import { useWidgetData, type WidgetSource } from '@/hooks/useWidgetData'
import type { WidgetType } from '@/types/dashboards'
import { widgetInfo } from '@/utils/dashboards'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetRenderer from '@/components/dashboards/WidgetRenderer'

interface Props {
  widget: { id: string; type: WidgetType; title: string }
  source: WidgetSource
  linkable: boolean
  display?: boolean
}

/** One widget on a dashboard: loads its data and draws it in its frame. */
export default function WidgetBody({ widget, source, linkable, display }: Props) {
  const { response, error, stale } = useWidgetData<unknown>(source)
  return (
    <WidgetFrame
      title={widget.title}
      typeLabel={widget.type ? widgetInfo(widget.type).label : 'Widget'}
      state={response?.state}
      hidden={response?.hidden}
      stale={stale}
      loading={!response && !error}
      error={response ? null : error}
      display={display}
    >
      {response?.state === 'ok' && <WidgetRenderer type={widget.type} data={response.data} linkable={linkable} />}
    </WidgetFrame>
  )
}

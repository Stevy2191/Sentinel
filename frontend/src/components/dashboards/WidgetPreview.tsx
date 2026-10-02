import { useEffect, useState } from 'react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { WidgetResponse, WidgetType } from '@/types/dashboards'
import { widgetInfo } from '@/utils/dashboards'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetRenderer from '@/components/dashboards/WidgetRenderer'

interface Props {
  dashboardId: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
}

/** A live preview of an unsaved widget, 600 ms after the last change. The
 *  server resolves it as the editor, so it shows only what the editor may see. */
export default function WidgetPreview({ dashboardId, type, title, config }: Props) {
  const { preview } = useDashboardActions()
  const [result, setResult] = useState<{ response?: WidgetResponse; error?: string } | null>(null)
  const key = JSON.stringify({ type, config })

  useEffect(() => {
    let cancelled = false
    const body = JSON.parse(key) as { type: WidgetType; config: Record<string, unknown> }
    const timer = window.setTimeout(() => {
      preview(dashboardId, body)
        .then((response) => {
          if (!cancelled) setResult({ response })
        })
        .catch((err: ApiError) => {
          if (!cancelled) setResult({ error: err.message || 'Preview failed' })
        })
    }, 600)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [preview, dashboardId, key])

  return (
    <div className="h-64">
      <WidgetFrame
        title={title}
        typeLabel={widgetInfo(type).label}
        state={result?.response?.state}
        hidden={result?.response?.hidden}
        loading={!result}
        error={result?.error}
      >
        {result?.response?.state === 'ok' && <WidgetRenderer type={type} data={result.response.data} linkable={false} />}
      </WidgetFrame>
    </div>
  )
}

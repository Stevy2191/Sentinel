import { useMemo, type ReactNode, type RefObject } from 'react'
import { GridLayout, useContainerWidth, type Layout } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import type { WidgetType } from '@/types/dashboards'
import { GRID_COLS, GRID_MARGIN, ROW_HEIGHT, stackOrder, widgetInfo } from '@/utils/dashboards'

interface GridItem {
  id: string
  type: WidgetType
  x: number
  y: number
  w: number
  h: number
}

interface Props<T extends GridItem> {
  items: T[]
  editing: boolean
  render: (item: T) => ReactNode
  onLayoutChange?: (layout: Layout) => void
  rowHeight?: number
}

/** The 12-column widget grid. Dragging and resizing only while editing; at
 *  phone width the widgets stack in their saved order instead. */
export default function DashboardGrid<T extends GridItem>({ items, editing, render, onLayoutChange, rowHeight = ROW_HEIGHT }: Props<T>) {
  const { width, mounted, containerRef: libRef } = useContainerWidth()
  // The library types its ref for React 19 (current may be null); React 18's div ref wants a non-null one.
  const containerRef = libRef as RefObject<HTMLDivElement>
  const layout = useMemo<Layout>(
    () =>
      items.map((it) => {
        const info = widgetInfo(it.type)
        return { i: it.id, x: it.x, y: it.y, w: it.w, h: it.h, minW: info.minW, minH: info.minH }
      }),
    [items],
  )

  if (mounted && width < 640) {
    return (
      <div ref={containerRef} className="space-y-3">
        {stackOrder(items).map((it) => (
          <div key={it.id} style={{ height: it.h * rowHeight }}>
            {render(it)}
          </div>
        ))}
      </div>
    )
  }
  return (
    <div ref={containerRef}>
      {mounted && (
        <GridLayout
          width={width}
          layout={layout}
          gridConfig={{ cols: GRID_COLS, rowHeight, margin: GRID_MARGIN }}
          dragConfig={{ enabled: editing, handle: '.widget-drag-handle', cancel: '.widget-no-drag' }}
          resizeConfig={{ enabled: editing }}
          onLayoutChange={onLayoutChange}
        >
          {items.map((it) => (
            <div key={it.id}>{render(it)}</div>
          ))}
        </GridLayout>
      )}
    </div>
  )
}

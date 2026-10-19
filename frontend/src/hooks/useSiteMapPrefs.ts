import { useCallback, useState } from 'react'

const VIEW_KEY = 'sentinel:map-view'
const FOLDS_KEY = 'sentinel:map-folds'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Private browsing or a full quota: it applies now, it just isn't kept.
  }
}

/** Realistic or Simple, remembered in this browser (realistic by default). */
export function useMapView(): ['realistic' | 'simple', (v: 'realistic' | 'simple') => void] {
  const [view, setView] = useState<'realistic' | 'simple'>(() => (read(VIEW_KEY) === 'simple' ? 'simple' : 'realistic'))
  const set = useCallback((v: 'realistic' | 'simple') => {
    setView(v)
    write(VIEW_KEY, v)
  }, [])
  return [view, set]
}

function allFolds(): Record<string, string[]> {
  try {
    const v = JSON.parse(read(FOLDS_KEY) ?? '{}')
    return v && typeof v === 'object' ? (v as Record<string, string[]>) : {}
  } catch {
    return {}
  }
}

/** The branches folded on one map (site id, or "public:<token>"),
 *  remembered in this browser. */
export function useMapFolds(mapKey: string): [Set<string>, (id: string) => void] {
  const [folds, setFolds] = useState<Set<string>>(() => new Set(allFolds()[mapKey] ?? []))
  const toggle = useCallback(
    (id: string) =>
      setFolds((cur) => {
        const next = new Set(cur)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        const all = allFolds()
        all[mapKey] = [...next]
        write(FOLDS_KEY, JSON.stringify(all))
        return next
      }),
    [mapKey]
  )
  return [folds, toggle]
}

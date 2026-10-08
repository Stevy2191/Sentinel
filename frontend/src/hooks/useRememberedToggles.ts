import { useCallback, useState } from 'react'

function load(key: string): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {}
  } catch {
    return {}
  }
}

/** useRememberedToggles is a set of on/off switches (collapsed sections or
 *  groups) kept in this browser under storageKey, so they survive leaving the
 *  page and coming back. */
export function useRememberedToggles(storageKey: string) {
  const [on, setOn] = useState<Record<string, boolean>>(() => load(storageKey))
  const toggle = useCallback(
    (key: string) =>
      setOn((cur) => {
        const next = { ...cur, [key]: !cur[key] }
        try {
          localStorage.setItem(storageKey, JSON.stringify(next))
        } catch {
          // Private browsing or a full quota: the toggle still works for this
          // visit, it just will not be remembered.
        }
        return next
      }),
    [storageKey]
  )
  return { on, toggle }
}

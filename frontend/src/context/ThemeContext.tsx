import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

export type ThemeMode = 'light' | 'dark' | 'auto'

interface ThemeContextValue {
  mode: ThemeMode
  isDark: boolean
  setMode: (mode: ThemeMode) => void
  toggle: () => void
}

const STORAGE_KEY = 'sentinel-theme'

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

// The redesign commits to a fixed dark theme, so the app is always dark
// regardless of the stored mode (light mode is retired for the new look).
function resolveIsDark(_mode: ThemeMode): boolean {
  return true
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const saved = localStorage.getItem(STORAGE_KEY) as ThemeMode | null
    return saved ?? 'auto'
  })
  const [isDark, setIsDark] = useState<boolean>(() => resolveIsDark(mode))

  const apply = useCallback((next: ThemeMode) => {
    const dark = resolveIsDark(next)
    setIsDark(dark)
    document.documentElement.classList.toggle('dark', dark)
  }, [])

  const setMode = useCallback(
    (next: ThemeMode) => {
      setModeState(next)
      localStorage.setItem(STORAGE_KEY, next)
      apply(next)
    },
    [apply]
  )

  const toggle = useCallback(() => {
    setMode(isDark ? 'light' : 'dark')
  }, [isDark, setMode])

  // Re-apply on mount and react to OS changes while in 'auto' mode.
  useEffect(() => {
    apply(mode)
    if (mode !== 'auto') return
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => apply('auto')
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [mode, apply])

  const value = useMemo(
    () => ({ mode, isDark, setMode, toggle }),
    [mode, isDark, setMode, toggle]
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return ctx
}

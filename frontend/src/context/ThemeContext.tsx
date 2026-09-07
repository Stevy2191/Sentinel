import { createContext, useContext, useEffect, type ReactNode } from 'react'

/**
 * The app is dark-only.
 *
 * The redesign's surfaces (slate-950 ground, slate-800/40 cards, white/10
 * borders) have no light equivalent, so a light setting could not be honoured
 * without a second palette. Rather than leave a control that silently does
 * nothing, the choice is gone: this provider just guarantees the `dark` class
 * is on <html> and exposes nothing to switch.
 *
 * public/theme-init.js already sets the class before first paint to avoid a
 * flash; this is the belt-and-braces for client-side navigation and for any
 * code that re-renders the root.
 */
interface ThemeContextValue {
  /** Always true. Kept so callers can read the resolved theme without branching. */
  isDark: true
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

const value: ThemeContextValue = { isDark: true }

export function ThemeProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    document.documentElement.classList.add('dark')
  }, [])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return ctx
}

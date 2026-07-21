import { useCallback, useState } from 'react'
import api from '@/services/api'
import { useTheme, type ThemeMode } from '@/context/ThemeContext'
import { applyThemeColors } from '@/utils/themeUtils'
import { PREF, setString } from '@/utils/preferences'

interface SavedTheme {
  primary_color: string
  accent_color: string
  mode: string
}

/**
 * useThemeColors saves the user's theme to the backend (so it syncs across
 * devices), applies it locally immediately, and mirrors it into localStorage +
 * the mode context.
 */
export function useThemeColors() {
  const { setMode } = useTheme()
  const [saving, setSaving] = useState(false)

  const saveTheme = useCallback(
    async (primaryColor: string, accentColor: string, mode: ThemeMode): Promise<SavedTheme> => {
      setSaving(true)
      try {
        const { data } = await api.patch<{ data: SavedTheme }>('/settings/theme', {
          primary_color: primaryColor,
          accent_color: accentColor,
          mode,
        })
        applyThemeColors(primaryColor, accentColor)
        setString(PREF.primaryColor, primaryColor)
        setString(PREF.accentColor, accentColor)
        setMode(mode)
        return data.data
      } finally {
        setSaving(false)
      }
    },
    [setMode]
  )

  return { saveTheme, saving }
}

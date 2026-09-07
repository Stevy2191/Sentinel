import { useCallback, useState } from 'react'
import api from '@/services/api'
import { applyThemeColors } from '@/utils/themeUtils'
import { PREF, setString } from '@/utils/preferences'

interface SavedTheme {
  primary_color: string
  accent_color: string
  mode: string
}

/**
 * useThemeColors saves the user's theme to the backend (so it syncs across
 * devices), applies it locally immediately, and mirrors it into localStorage.
 */
export function useThemeColors() {
  const [saving, setSaving] = useState(false)

  const saveTheme = useCallback(
    async (primaryColor: string, accentColor: string): Promise<SavedTheme> => {
      setSaving(true)
      try {
        const { data } = await api.patch<{ data: SavedTheme }>('/settings/theme', {
          primary_color: primaryColor,
          accent_color: accentColor,
          // mode is deliberately omitted: the app is dark-only, and the API
          // leaves theme_mode untouched when it is absent, so an existing
          // stored value is preserved rather than blanked.
        })
        applyThemeColors(primaryColor, accentColor)
        setString(PREF.primaryColor, primaryColor)
        setString(PREF.accentColor, accentColor)
        return data.data
      } finally {
        setSaving(false)
      }
    },
    []
  )

  return { saveTheme, saving }
}

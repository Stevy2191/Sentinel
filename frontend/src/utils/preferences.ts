// Client-side user preferences persisted to localStorage under 'sentinel:*'.
// (Theme mode is handled separately by ThemeContext under 'sentinel-theme'.)

export type FontSize = 'compact' | 'normal' | 'large'
export type TimeFormat = '12h' | '24h'
export type DateFormatPref = 'MMM DD, YYYY' | 'DD/MM/YYYY' | 'YYYY-MM-DD'
export type ReportRange = '7d' | '30d' | '90d' | 'custom'

export const PREF = {
  fontSize: 'sentinel:fontSize',
  soundAlerts: 'sentinel:soundAlerts',
  desktopNotifications: 'sentinel:desktopNotifications',
  timeFormat: 'sentinel:timeFormat',
  timezone: 'sentinel:timezone',
  dateFormat: 'sentinel:dateFormat',
  reportRange: 'sentinel:reportRange',
  logo: 'sentinel:logo',
} as const

export const DEFAULTS = {
  fontSize: 'normal' as FontSize,
  soundAlerts: false,
  desktopNotifications: false,
  timeFormat: '24h' as TimeFormat,
  dateFormat: 'MMM DD, YYYY' as DateFormatPref,
  reportRange: '7d' as ReportRange,
}

export const FONT_SCALE: Record<FontSize, string> = {
  compact: '90%',
  normal: '100%',
  large: '110%',
}

export function getString(key: string, fallback: string): string {
  return localStorage.getItem(key) ?? fallback
}
export function getBool(key: string, fallback: boolean): boolean {
  const v = localStorage.getItem(key)
  return v === null ? fallback : v === 'true'
}
export function setString(key: string, value: string): void {
  localStorage.setItem(key, value)
}
export function setBool(key: string, value: boolean): void {
  localStorage.setItem(key, String(value))
}

/** Default timezone: the browser's resolved zone. */
export function defaultTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

/** Apply the visual preferences that affect the whole document. Call at startup
 *  and whenever these prefs change.
 *
 *  Font size is all that is left here. Brand colours used to be applied from
 *  this function, overriding the primary and accent CSS variables at runtime;
 *  they are now fixed by the design system in styles/theme.css, so the static
 *  values stand and nothing recolours the app after load. */
export function applyStoredPreferences(): void {
  const font = getString(PREF.fontSize, DEFAULTS.fontSize) as FontSize
  document.documentElement.style.fontSize = FONT_SCALE[font] ?? '100%'
}

/** Remove every sentinel:* preference key. */
export function resetAllPreferences(): void {
  Object.values(PREF).forEach((k) => localStorage.removeItem(k))
}

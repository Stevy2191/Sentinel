import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

export const DEFAULT_APP_NAME = 'Sentinel'
/** Mirrors models.DefaultMonitorCheckInterval on the backend. */
export const DEFAULT_CHECK_INTERVAL = 60

interface AppConfig {
  /** The instance's display name, as set in Settings → System. */
  appName: string
  /** Whether self-registration is open. */
  registrationEnabled: boolean
  /** True when no accounts exist yet, so the first admin can still be created. */
  setupRequired: boolean
  /** The interval, in seconds, a newly created monitor starts with. */
  defaultCheckInterval: number
  /** False until the first read completes. Callers that would otherwise flash
   *  a wrong answer — "registration disabled" before we know — wait on this. */
  loaded: boolean
  /** Re-reads the config — call after an admin changes it. */
  refresh: () => Promise<void>
}

const AppConfigContext = createContext<AppConfig>({
  appName: DEFAULT_APP_NAME,
  registrationEnabled: false,
  setupRequired: false,
  defaultCheckInterval: DEFAULT_CHECK_INTERVAL,
  loaded: false,
  refresh: async () => {},
})

/**
 * AppConfigProvider supplies the instance-wide values every screen needs before
 * anyone has signed in — the display name above all.
 *
 * It reads /auth/status, which is public, rather than /settings, which is
 * admin-only: the sign-in screen has to render the instance's name and decide
 * whether to offer a sign-up link while the visitor has no credentials at all.
 *
 * Uses fetch rather than the api client so it carries no auth expectations and
 * cannot trigger the client's 401 handling on a page where being signed out is
 * the normal case.
 */
export function AppConfigProvider({ children }: { children: ReactNode }) {
  const [appName, setAppName] = useState(DEFAULT_APP_NAME)
  const [registrationEnabled, setRegistrationEnabled] = useState(false)
  const [setupRequired, setSetupRequired] = useState(false)
  const [defaultCheckInterval, setDefaultCheckInterval] = useState(DEFAULT_CHECK_INTERVAL)
  const [loaded, setLoaded] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const res = await fetch('/api/v1/auth/status')
      const body = await res.json()
      const data = body?.data ?? {}
      // Fall back rather than render an empty wordmark if the field is missing,
      // e.g. against an older backend.
      setAppName(typeof data.app_name === 'string' && data.app_name.trim() ? data.app_name : DEFAULT_APP_NAME)
      setRegistrationEnabled(Boolean(data.registration_enabled))
      setSetupRequired(Boolean(data.setup_required))
      setDefaultCheckInterval(
        Number.isFinite(data.default_check_interval) && data.default_check_interval > 0
          ? data.default_check_interval
          : DEFAULT_CHECK_INTERVAL
      )
    } catch {
      // A failed probe leaves the defaults in place: the app still renders,
      // just under its stock name with sign-up closed.
      setAppName(DEFAULT_APP_NAME)
      setRegistrationEnabled(false)
      setSetupRequired(false)
      setDefaultCheckInterval(DEFAULT_CHECK_INTERVAL)
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // The browser tab is part of the branding, so it follows the same name.
  useEffect(() => {
    document.title = appName
  }, [appName])

  return (
    <AppConfigContext.Provider value={{ appName, registrationEnabled, setupRequired, defaultCheckInterval, loaded, refresh }}>
      {children}
    </AppConfigContext.Provider>
  )
}

export function useAppConfig(): AppConfig {
  return useContext(AppConfigContext)
}

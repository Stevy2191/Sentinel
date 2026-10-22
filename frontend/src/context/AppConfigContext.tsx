import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { setDisplayTimezone } from '@/utils/formatters'

export const DEFAULT_APP_NAME = 'Sentinel'
/** Mirrors models.DefaultMonitorCheckInterval on the backend. */
export const DEFAULT_CHECK_INTERVAL = 60
/** Mirrors models.DefaultSLATargetPercent on the backend. */
export const DEFAULT_SLA_TARGET = 99.9
/** A failed read is retried after this long, doubling up to RETRY_MAX_MS. */
const RETRY_FIRST_MS = 2000
const RETRY_MAX_MS = 15000

interface AppConfig {
  /** The instance's display name, as set in Settings → General. */
  appName: string
  /** Whether self-registration is open. */
  registrationEnabled: boolean
  /** True when no accounts exist yet, so the first admin can still be created. */
  setupRequired: boolean
  /** The interval, in seconds, a newly created monitor starts with. */
  defaultCheckInterval: number
  /** The uptime percentage a monitor is held to when it has no override. */
  defaultSLATarget: number
  /** The IANA zone reports are rendered in, and that the UI displays times in.
   *  Undefined until the config loads, when the browser's zone is used. */
  reportTimezone: string | undefined
  /** False until the first read succeeds. Callers that would otherwise show a
   *  wrong answer — "registration disabled" before we know — wait on this. */
  loaded: boolean
  /** Re-reads the config — call after an admin changes it. */
  refresh: () => Promise<void>
}

const AppConfigContext = createContext<AppConfig>({
  appName: DEFAULT_APP_NAME,
  registrationEnabled: false,
  setupRequired: false,
  reportTimezone: undefined,
  defaultCheckInterval: DEFAULT_CHECK_INTERVAL,
  defaultSLATarget: DEFAULT_SLA_TARGET,
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
 *
 * A failed read is retried until one succeeds, and `loaded` stays false until
 * then. The sign-in page is often the first thing opened while the backend is
 * still starting; treating that failure as an answer showed a fresh install's
 * first visitor "registration disabled" and never asked again.
 */
export function AppConfigProvider({ children }: { children: ReactNode }) {
  const [appName, setAppName] = useState(DEFAULT_APP_NAME)
  const [registrationEnabled, setRegistrationEnabled] = useState(false)
  const [setupRequired, setSetupRequired] = useState(false)
  const [defaultCheckInterval, setDefaultCheckInterval] = useState(DEFAULT_CHECK_INTERVAL)
  const [defaultSLATarget, setDefaultSLATarget] = useState(DEFAULT_SLA_TARGET)
  const [reportTimezone, setReportTimezone] = useState<string | undefined>(undefined)
  const [loaded, setLoaded] = useState(false)
  // Each refresh takes a new number; a response or retry from an older one
  // is dropped, so a slow failed read cannot overwrite a newer good one.
  const seq = useRef(0)
  const retryTimer = useRef<ReturnType<typeof setTimeout>>()

  // Resolves after the first attempt; retries continue in the background.
  const refresh = useCallback(async () => {
    clearTimeout(retryTimer.current)
    const mine = ++seq.current

    async function attempt(retryIn: number): Promise<void> {
      try {
        const res = await fetch('/api/v1/auth/status')
        // A 502 from nginx while the backend starts is HTML; not an answer.
        if (!res.ok) throw new Error(`auth status: HTTP ${res.status}`)
        const body = await res.json()
        if (mine !== seq.current) return
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
        setDefaultSLATarget(
          Number.isFinite(data.default_sla_target) && data.default_sla_target >= 0
            ? data.default_sla_target
            : DEFAULT_SLA_TARGET
        )
        // Applied to the formatters immediately, so every timestamp on screen
        // is in the same zone the server writes into reports.
        const tz = typeof data.report_timezone === 'string' ? data.report_timezone : undefined
        setReportTimezone(tz)
        setDisplayTimezone(tz)
        setLoaded(true)
      } catch {
        // Keep whatever is in place (the defaults, or the last good read) and
        // try again shortly. The app still renders meanwhile; the sign-in page
        // offers neither the sign-up link nor "registration disabled" until a
        // read succeeds.
        if (mine !== seq.current) return
        retryTimer.current = setTimeout(() => {
          void attempt(Math.min(retryIn * 2, RETRY_MAX_MS))
        }, retryIn)
      }
    }

    await attempt(RETRY_FIRST_MS)
  }, [])

  useEffect(() => {
    void refresh()
    return () => {
      seq.current += 1
      clearTimeout(retryTimer.current)
    }
  }, [refresh])

  // The browser tab is part of the branding, so it follows the same name.
  useEffect(() => {
    document.title = appName
  }, [appName])

  return (
    <AppConfigContext.Provider value={{ appName, registrationEnabled, setupRequired, defaultCheckInterval, defaultSLATarget, reportTimezone, loaded, refresh }}>
      {children}
    </AppConfigContext.Provider>
  )
}

export function useAppConfig(): AppConfig {
  return useContext(AppConfigContext)
}

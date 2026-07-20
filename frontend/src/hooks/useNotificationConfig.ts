import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'

// ChannelName is the set of channels the backend supports.
export type ChannelName = 'email' | 'slack' | 'discord' | 'telegram' | 'ntfy' | 'webhook'

// NotificationConfig mirrors the backend model. Secret fields are omitted from
// list responses (see HideSecrets) and only present when fetching a single
// config for editing.
export interface NotificationConfig {
  id?: string
  channel: ChannelName
  enabled: boolean
  // Email/SMTP
  smtp_host?: string | null
  smtp_port?: number | null
  smtp_user?: string | null
  smtp_password?: string | null
  smtp_from?: string | null
  // Slack/Discord/Webhook (generic URL)
  webhook_url?: string | null
  // Telegram
  telegram_bot_token?: string | null
  telegram_chat_id?: string | null
  // Ntfy
  ntfy_url?: string | null
  ntfy_topic?: string | null
  // Webhook custom headers
  custom_headers?: Record<string, string> | null
  // Test status
  last_test_at?: string | null
  last_test_success?: boolean | null
  last_test_error?: string | null
  created_at?: string
  updated_at?: string
}

// TestResult is the payload returned by the /test endpoint.
export interface TestResult {
  channel: string
  test_success: boolean
  test_error: string | null
  last_test_at: string
}

// Static per-channel presentation metadata (icon, label, description).
export const CHANNEL_META: Record<
  ChannelName,
  { label: string; emoji: string; description: string }
> = {
  email: { label: 'Email', emoji: '📧', description: 'Send alerts over SMTP.' },
  slack: { label: 'Slack', emoji: '💬', description: 'Post alerts to a Slack channel.' },
  discord: { label: 'Discord', emoji: '🎮', description: 'Post alerts to a Discord channel.' },
  telegram: { label: 'Telegram', emoji: '✈️', description: 'Send alerts via a Telegram bot.' },
  ntfy: { label: 'Ntfy', emoji: '🔔', description: 'Push alerts to an ntfy topic.' },
  webhook: { label: 'Webhook', emoji: '🪝', description: 'POST alert JSON to any URL.' },
}

export const CHANNEL_ORDER: ChannelName[] = ['email', 'slack', 'discord', 'telegram', 'ntfy', 'webhook']

const BASE = '/settings/notification-channels'

/** List all channel configs (secrets stripped by the backend). */
export function useNotificationConfigs() {
  const [configs, setConfigs] = useState<NotificationConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.get<{ data: NotificationConfig[] }>(BASE)
      setConfigs(res.data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load notification channels')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { configs, loading, error, refetch }
}

/** Fetch a single channel's config (including secrets) for editing. A channel
 *  with no stored config yields config=null (not an error). */
export function useNotificationConfig(channel: ChannelName | null) {
  const [config, setConfig] = useState<NotificationConfig | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!channel) {
      setConfig(null)
      return
    }
    let active = true
    setLoading(true)
    setError(null)
    api
      .get<{ data: NotificationConfig }>(`${BASE}/${channel}`)
      .then((res) => active && setConfig(res.data.data))
      .catch((err: ApiError) => {
        if (!active) return
        if (err.status === 404) {
          setConfig(null) // never configured — start from a blank form
        } else {
          setError(err.message || 'Failed to load channel configuration')
        }
      })
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [channel])

  return { config, loading, error }
}

/** Create or update a channel config. */
export function useSaveNotificationConfig() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const save = useCallback(
    async (channel: ChannelName, configData: Partial<NotificationConfig>): Promise<NotificationConfig | null> => {
      setLoading(true)
      setError(null)
      try {
        const res = await api.post<{ data: NotificationConfig }>(`${BASE}/${channel}`, configData)
        return res.data.data
      } catch (err) {
        setError((err as ApiError).message || 'Failed to save configuration')
        throw err
      } finally {
        setLoading(false)
      }
    },
    []
  )

  return { save, loading, error }
}

/** Send a test message through a channel's stored config. */
export function useTestNotificationConfig() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<TestResult | null>(null)

  const test = useCallback(async (channel: ChannelName): Promise<TestResult | null> => {
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await api.post<{ data: TestResult }>(`${BASE}/${channel}/test`)
      setResult(res.data.data)
      return res.data.data
    } catch (err) {
      setError((err as ApiError).message || 'Test request failed')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { test, loading, result, error }
}

/** Disable and clear a channel config. */
export function useDeleteNotificationConfig() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const deleteConfig = useCallback(async (channel: ChannelName): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      await api.delete(`${BASE}/${channel}`)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to disable channel')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { delete: deleteConfig, loading, error }
}

import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'

// ChannelName is the set of channels the backend supports.
export type ChannelName = 'email' | 'slack' | 'discord' | 'telegram' | 'ntfy' | 'webhook'

// SMTPSecurity is the connection security mode for the email channel; it mirrors
// models.SMTPSecurity* in the backend.
export type SMTPSecurity = 'none' | 'starttls' | 'ssltls'

// NotificationConfig mirrors the backend model. Secret fields are omitted from
// list responses (see HideSecrets) and only present when fetching a single
// config for editing.
export interface NotificationConfig {
  id: string
  /** Operator-facing label. Several channels may share a type, so this is what
   *  identifies one to a person; `id` is what identifies it to the API. */
  name: string
  channel: ChannelName
  enabled: boolean
  /** Non-secret one-line summary of where this delivers, computed server-side
   *  because the identifying fields are often the secret ones. */
  details?: string
  // Email/SMTP
  smtp_host?: string | null
  smtp_port?: number | null
  smtp_user?: string | null
  smtp_password?: string | null
  smtp_from?: string | null
  /** Connection security. Null means starttls (the backend default). */
  smtp_security?: SMTPSecurity | null
  /** Skip TLS certificate verification for self-signed internal mail servers. */
  smtp_skip_tls_verify?: boolean | null
  // Slack/Discord/Webhook (generic URL)
  webhook_url?: string | null
  // Telegram
  telegram_bot_token?: string | null
  telegram_chat_id?: string | null
  // Ntfy
  ntfy_url?: string | null
  ntfy_topic?: string | null
  ntfy_auth_token?: string | null
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

/** One channel as any authenticated user may see it: name and on/off, no config. */
export interface AvailableChannel {
  id: string
  name: string
  channel: ChannelName
  enabled: boolean
}

/**
 * useAvailableChannels lists the channels that can deliver an alert.
 *
 * Reads /notification-channels, not the admin listing: any user may create a
 * monitor and therefore needs this, while the admin endpoint returns SMTP hosts
 * and webhook URLs that a non-admin has no business seeing — and would 403 on
 * anyway, leaving the picker silently empty.
 *
 * `enabled` gates the request so a closed modal does not fetch.
 */
export function useAvailableChannels(enabled = true) {
  const [channels, setChannels] = useState<AvailableChannel[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!enabled) return
    let active = true
    setLoading(true)
    setError(null)
    api
      .get<{ data: AvailableChannel[] }>('/notification-channels')
      .then((r) => active && setChannels(r.data.data ?? []))
      .catch((e) => {
        if (!active) return
        setChannels([])
        setError((e as ApiError).message || 'Could not load notification channels')
      })
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [enabled])

  // Only a configured, switched-on channel can actually deliver.
  const available = channels.filter((c) => c.enabled)
  return { available, loading, error }
}

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
export function useNotificationConfig(id: string | null) {
  const [config, setConfig] = useState<NotificationConfig | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) {
      setConfig(null)
      return
    }
    let active = true
    setLoading(true)
    setError(null)
    api
      .get<{ data: NotificationConfig }>(`${BASE}/${id}`)
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
  }, [id])

  return { config, loading, error }
}

/**
 * Create a new channel, or update an existing one when an id is given.
 *
 * These are separate verbs on the API now that an install may hold several
 * channels of one type: POST always adds, PUT always edits the named row. The
 * old single endpoint upserted by type, which cannot express either.
 */
export function useSaveNotificationConfig() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const save = useCallback(
    async (
      id: string | null,
      configData: Partial<NotificationConfig>
    ): Promise<NotificationConfig | null> => {
      setLoading(true)
      setError(null)
      try {
        const res = id
          ? await api.put<{ data: NotificationConfig }>(`${BASE}/${id}`, configData)
          : await api.post<{ data: NotificationConfig }>(BASE, configData)
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

  const test = useCallback(async (id: string): Promise<TestResult | null> => {
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await api.post<{ data: TestResult }>(`${BASE}/${id}/test`)
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

/**
 * Switch a channel on or off.
 *
 * Its own call rather than a full save: the list this is driven from has its
 * secrets stripped, so sending that row back as an update would fail validation
 * or blank the stored credential. This moves one column.
 */
export function useSetChannelEnabled() {
  const [loading, setLoading] = useState(false)

  const setEnabled = useCallback(async (id: string, enabled: boolean): Promise<void> => {
    setLoading(true)
    try {
      await api.patch(`${BASE}/${id}/enabled`, { enabled })
    } finally {
      setLoading(false)
    }
  }, [])

  return { setEnabled, loading }
}

/** Delete a channel. */
export function useDeleteNotificationConfig() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const deleteConfig = useCallback(async (id: string): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      await api.delete(`${BASE}/${id}`)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to delete channel')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { delete: deleteConfig, loading, error }
}

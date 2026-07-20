import { useEffect, useMemo, useState } from 'react'
import { X, Loader2, Eye, EyeOff, ExternalLink, Trash2 } from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import NotificationConfigCard from '@/components/NotificationConfigCard'
import {
  CHANNEL_META,
  CHANNEL_ORDER,
  useNotificationConfig,
  useNotificationConfigs,
  useSaveNotificationConfig,
  useTestNotificationConfig,
  useDeleteNotificationConfig,
  type ChannelName,
  type NotificationConfig,
} from '@/hooks/useNotificationConfig'

// ---------- validation helpers ----------
const emailRe = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function isHttpUrl(v: string): boolean {
  try {
    const u = new URL(v)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

function parseHeaders(v: string): Record<string, string> | null {
  try {
    const obj = JSON.parse(v)
    if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return null
    for (const val of Object.values(obj)) {
      if (typeof val !== 'string') return null
    }
    return obj as Record<string, string>
  } catch {
    return null
  }
}

// ---------- form state ----------
interface FormState {
  enabled: boolean
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_password: string
  smtp_from: string
  webhook_url: string
  telegram_bot_token: string
  telegram_chat_id: string
  ntfy_url: string
  ntfy_topic: string
  ntfy_auth_token: string
  custom_headers: string
}

const emptyForm: FormState = {
  enabled: true,
  smtp_host: '',
  smtp_port: '587',
  smtp_user: '',
  smtp_password: '',
  smtp_from: '',
  webhook_url: '',
  telegram_bot_token: '',
  telegram_chat_id: '',
  ntfy_url: '',
  ntfy_topic: '',
  ntfy_auth_token: '',
  custom_headers: '',
}

function formFromConfig(cfg: NotificationConfig | null): FormState {
  if (!cfg) return { ...emptyForm }
  return {
    enabled: cfg.enabled ?? true,
    smtp_host: cfg.smtp_host ?? '',
    smtp_port: cfg.smtp_port != null ? String(cfg.smtp_port) : '587',
    smtp_user: cfg.smtp_user ?? '',
    smtp_password: cfg.smtp_password ?? '',
    smtp_from: cfg.smtp_from ?? '',
    webhook_url: cfg.webhook_url ?? '',
    telegram_bot_token: cfg.telegram_bot_token ?? '',
    telegram_chat_id: cfg.telegram_chat_id ?? '',
    ntfy_url: cfg.ntfy_url ?? '',
    ntfy_topic: cfg.ntfy_topic ?? '',
    ntfy_auth_token: cfg.ntfy_auth_token ?? '',
    custom_headers: cfg.custom_headers ? JSON.stringify(cfg.custom_headers, null, 2) : '',
  }
}

function validate(channel: ChannelName, f: FormState): Record<string, string> {
  const e: Record<string, string> = {}
  switch (channel) {
    case 'email': {
      if (!f.smtp_host.trim()) e.smtp_host = 'SMTP host is required'
      const port = Number(f.smtp_port)
      if (!f.smtp_port.trim() || !Number.isInteger(port) || port < 1 || port > 65535)
        e.smtp_port = 'Port must be a number 1–65535'
      if (!f.smtp_user.trim()) e.smtp_user = 'SMTP user is required'
      else if (!emailRe.test(f.smtp_user.trim())) e.smtp_user = 'Must be a valid email address'
      if (!f.smtp_password) e.smtp_password = 'Password is required'
      if (!f.smtp_from.trim()) e.smtp_from = 'From address is required'
      else if (!emailRe.test(f.smtp_from.trim())) e.smtp_from = 'Must be a valid email address'
      break
    }
    case 'slack':
    case 'discord':
    case 'webhook': {
      if (!f.webhook_url.trim()) e.webhook_url = 'Webhook URL is required'
      else if (!isHttpUrl(f.webhook_url.trim())) e.webhook_url = 'Must be a valid http(s) URL'
      if (channel === 'webhook' && f.custom_headers.trim() && parseHeaders(f.custom_headers) === null)
        e.custom_headers = 'Must be a JSON object of string values'
      break
    }
    case 'telegram': {
      if (!f.telegram_bot_token) e.telegram_bot_token = 'Bot token is required'
      if (!f.telegram_chat_id.trim()) e.telegram_chat_id = 'Chat ID is required'
      break
    }
    case 'ntfy': {
      if (!f.ntfy_topic.trim()) e.ntfy_topic = 'Topic is required'
      if (f.ntfy_url.trim() && !isHttpUrl(f.ntfy_url.trim())) e.ntfy_url = 'Must be a valid http(s) URL'
      break
    }
  }
  return e
}

function buildPayload(channel: ChannelName, f: FormState): Partial<NotificationConfig> {
  const p: Partial<NotificationConfig> = { channel, enabled: f.enabled }
  switch (channel) {
    case 'email':
      p.smtp_host = f.smtp_host.trim()
      p.smtp_port = Number(f.smtp_port)
      p.smtp_user = f.smtp_user.trim()
      p.smtp_password = f.smtp_password
      p.smtp_from = f.smtp_from.trim()
      break
    case 'slack':
    case 'discord':
      p.webhook_url = f.webhook_url.trim()
      break
    case 'webhook':
      p.webhook_url = f.webhook_url.trim()
      p.custom_headers = f.custom_headers.trim() ? parseHeaders(f.custom_headers) : null
      break
    case 'telegram':
      p.telegram_bot_token = f.telegram_bot_token
      p.telegram_chat_id = f.telegram_chat_id.trim()
      break
    case 'ntfy':
      p.ntfy_topic = f.ntfy_topic.trim()
      p.ntfy_url = f.ntfy_url.trim() || null
      // Send null (not "") when blank — the backend rejects an empty token.
      p.ntfy_auth_token = f.ntfy_auth_token.trim() || null
      break
  }
  return p
}

// ---------- field primitives ----------
const inputCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'

function Label({ children, required }: { children: React.ReactNode; required?: boolean }) {
  return (
    <span className="mb-1 block text-sm font-medium">
      {children}
      {required && <span className="ml-0.5 text-red-500">*</span>}
    </span>
  )
}

function FieldError({ msg }: { msg?: string }) {
  if (!msg) return null
  return <p className="mt-1 text-xs text-red-500">{msg}</p>
}

function Helper({ children }: { children: React.ReactNode }) {
  return <p className="mt-1 text-xs text-neutral-400">{children}</p>
}

function ExtLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 text-xs text-primary-600 hover:underline"
    >
      {children} <ExternalLink className="h-3 w-3" />
    </a>
  )
}

function SecretInput({
  value,
  onChange,
  onBlur,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  onBlur: () => void
  placeholder?: string
}) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <input
        type={show ? 'text' : 'password'}
        className={`${inputCls} pr-10`}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        autoComplete="off"
      />
      <button
        type="button"
        onClick={() => setShow((s) => !s)}
        className="absolute right-2 top-1/2 -translate-y-1/2 text-neutral-400 hover:text-neutral-600"
        tabIndex={-1}
        aria-label={show ? 'Hide' : 'Show'}
      >
        {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  )
}

// ---------- config modal ----------
function ConfigModal({
  channel,
  onClose,
  onChanged,
  push,
}: {
  channel: ChannelName
  onClose: () => void
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const meta = CHANNEL_META[channel]
  const { config, loading: loadingConfig } = useNotificationConfig(channel)
  const { save, loading: saving } = useSaveNotificationConfig()
  const { test, loading: testing } = useTestNotificationConfig()
  const { delete: deleteConfig, loading: deleting } = useDeleteNotificationConfig()

  const [form, setForm] = useState<FormState>({ ...emptyForm })
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [submitAttempted, setSubmitAttempted] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  // Populate the form once the existing config loads.
  useEffect(() => {
    setForm(formFromConfig(config))
    setTouched({})
    setSubmitAttempted(false)
  }, [config])

  const errors = useMemo(() => validate(channel, form), [channel, form])
  const hasErrors = Object.keys(errors).length > 0
  const busy = saving || testing || deleting

  const set = (key: keyof FormState, value: string | boolean) =>
    setForm((f) => ({ ...f, [key]: value }))
  const markTouched = (key: string) => setTouched((t) => ({ ...t, [key]: true }))
  const errFor = (key: string) => (touched[key] || submitAttempted ? errors[key] : undefined)

  // Persist the current form. Returns true on success.
  const persist = async (): Promise<boolean> => {
    setSubmitAttempted(true)
    if (hasErrors) return false
    try {
      await save(channel, buildPayload(channel, form))
      return true
    } catch (err) {
      push(`✗ Failed to save: ${(err as { message?: string }).message ?? 'error'}`, 'error')
      return false
    }
  }

  const handleSave = async () => {
    if (await persist()) {
      push(`✓ ${meta.label} configured successfully`, 'success')
      onChanged()
      onClose()
    }
  }

  // Test saves the current form first (the backend tests the stored config), then
  // sends a test message through it.
  const handleTest = async () => {
    if (!(await persist())) return
    try {
      const result = await test(channel)
      if (result?.test_success) {
        push(`✓ Test sent! Message delivered via ${meta.label}`, 'success')
      } else {
        push(`✗ Test failed: ${result?.test_error ?? 'unknown error'}`, 'error')
      }
      onChanged()
    } catch (err) {
      push(`✗ Test failed: ${(err as { message?: string }).message ?? 'error'}`, 'error')
    }
  }

  const handleDelete = async () => {
    try {
      await deleteConfig(channel)
      push(`✓ ${meta.label} disabled`, 'success')
      onChanged()
      onClose()
    } catch (err) {
      push(`✗ Failed to disable: ${(err as { message?: string }).message ?? 'error'}`, 'error')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="card max-h-[90vh] w-full max-w-lg overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-start justify-between">
          <h3 className="flex items-center gap-2 text-lg font-semibold">
            <span aria-hidden>{meta.emoji}</span> Configure {meta.label}
          </h3>
          <button className="text-neutral-400 hover:text-neutral-600" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {loadingConfig ? (
          <div className="flex items-center justify-center py-10 text-neutral-400">
            <Loader2 className="h-6 w-6 animate-spin" />
          </div>
        ) : (
          <div className="space-y-4">
            {/* ---- EMAIL ---- */}
            {channel === 'email' && (
              <>
                <div>
                  <Label required>SMTP Host</Label>
                  <input
                    className={inputCls}
                    value={form.smtp_host}
                    onChange={(e) => set('smtp_host', e.target.value)}
                    onBlur={() => markTouched('smtp_host')}
                    placeholder="smtp.gmail.com"
                  />
                  <FieldError msg={errFor('smtp_host')} />
                </div>
                <div>
                  <Label required>SMTP Port</Label>
                  <input
                    type="number"
                    className={inputCls}
                    value={form.smtp_port}
                    onChange={(e) => set('smtp_port', e.target.value)}
                    onBlur={() => markTouched('smtp_port')}
                    placeholder="587"
                  />
                  <FieldError msg={errFor('smtp_port')} />
                </div>
                <div>
                  <Label required>SMTP User</Label>
                  <input
                    type="email"
                    className={inputCls}
                    value={form.smtp_user}
                    onChange={(e) => set('smtp_user', e.target.value)}
                    onBlur={() => markTouched('smtp_user')}
                    placeholder="you@example.com"
                  />
                  <FieldError msg={errFor('smtp_user')} />
                </div>
                <div>
                  <Label required>SMTP Password</Label>
                  <SecretInput
                    value={form.smtp_password}
                    onChange={(v) => set('smtp_password', v)}
                    onBlur={() => markTouched('smtp_password')}
                    placeholder="App password"
                  />
                  <FieldError msg={errFor('smtp_password')} />
                </div>
                <div>
                  <Label required>From Address</Label>
                  <input
                    type="email"
                    className={inputCls}
                    value={form.smtp_from}
                    onChange={(e) => set('smtp_from', e.target.value)}
                    onBlur={() => markTouched('smtp_from')}
                    placeholder="alerts@example.com"
                  />
                  <FieldError msg={errFor('smtp_from')} />
                  <Helper>Gmail: smtp.gmail.com:587 · Outlook: smtp-mail.outlook.com:587</Helper>
                </div>
              </>
            )}

            {/* ---- SLACK / DISCORD ---- */}
            {(channel === 'slack' || channel === 'discord') && (
              <div>
                <Label required>Webhook URL</Label>
                <SecretInput
                  value={form.webhook_url}
                  onChange={(v) => set('webhook_url', v)}
                  onBlur={() => markTouched('webhook_url')}
                  placeholder="https://hooks…"
                />
                <FieldError msg={errFor('webhook_url')} />
                {channel === 'slack' ? (
                  <>
                    <Helper>Get from Slack → Apps → Incoming Webhooks.</Helper>
                    <ExtLink href="https://api.slack.com/messaging/webhooks">Create Slack Webhook</ExtLink>
                  </>
                ) : (
                  <>
                    <Helper>Get from Discord → Server Settings → Integrations → Webhooks.</Helper>
                    <ExtLink href="https://support.discord.com/hc/en-us/articles/228383668">
                      Create Discord Webhook
                    </ExtLink>
                  </>
                )}
              </div>
            )}

            {/* ---- TELEGRAM ---- */}
            {channel === 'telegram' && (
              <>
                <div>
                  <Label required>Bot Token</Label>
                  <SecretInput
                    value={form.telegram_bot_token}
                    onChange={(v) => set('telegram_bot_token', v)}
                    onBlur={() => markTouched('telegram_bot_token')}
                    placeholder="123456:ABC-DEF…"
                  />
                  <FieldError msg={errFor('telegram_bot_token')} />
                </div>
                <div>
                  <Label required>Chat ID</Label>
                  <input
                    className={inputCls}
                    value={form.telegram_chat_id}
                    onChange={(e) => set('telegram_chat_id', e.target.value)}
                    onBlur={() => markTouched('telegram_chat_id')}
                    placeholder="123456789"
                  />
                  <FieldError msg={errFor('telegram_chat_id')} />
                  <Helper>Create a bot with @BotFather; get your chat ID from @userinfobot.</Helper>
                  <ExtLink href="https://core.telegram.org/bots#how-do-i-create-a-bot">
                    Create Telegram Bot
                  </ExtLink>
                </div>
              </>
            )}

            {/* ---- NTFY ---- */}
            {channel === 'ntfy' && (
              <>
                <div>
                  <Label required>Topic</Label>
                  <input
                    className={inputCls}
                    value={form.ntfy_topic}
                    onChange={(e) => set('ntfy_topic', e.target.value)}
                    onBlur={() => markTouched('ntfy_topic')}
                    placeholder="my-secret-topic"
                  />
                  <FieldError msg={errFor('ntfy_topic')} />
                </div>
                <div>
                  <Label>Server URL</Label>
                  <input
                    className={inputCls}
                    value={form.ntfy_url}
                    onChange={(e) => set('ntfy_url', e.target.value)}
                    onBlur={() => markTouched('ntfy_url')}
                    placeholder="https://ntfy.sh"
                  />
                  <FieldError msg={errFor('ntfy_url')} />
                  <Helper>Pick a hard-to-guess topic name — anyone who knows it can read your alerts.</Helper>
                  <ExtLink href="https://docs.ntfy.sh/">Learn about Ntfy</ExtLink>
                </div>
                <div>
                  <Label>Auth Token</Label>
                  <SecretInput
                    value={form.ntfy_auth_token}
                    onChange={(v) => set('ntfy_auth_token', v)}
                    onBlur={() => markTouched('ntfy_auth_token')}
                    placeholder="tk_… (optional)"
                  />
                  <Helper>
                    Optional. Required for protected topics or self-hosted servers with auth. Sent as a
                    Bearer token.
                  </Helper>
                  <ExtLink href="https://docs.ntfy.sh/config/#access-tokens">About access tokens</ExtLink>
                </div>
              </>
            )}

            {/* ---- WEBHOOK ---- */}
            {channel === 'webhook' && (
              <>
                <div>
                  <Label required>URL</Label>
                  <SecretInput
                    value={form.webhook_url}
                    onChange={(v) => set('webhook_url', v)}
                    onBlur={() => markTouched('webhook_url')}
                    placeholder="https://example.com/hook"
                  />
                  <FieldError msg={errFor('webhook_url')} />
                  <Helper>Sentinel sends a POST with the incident data as JSON to this URL.</Helper>
                </div>
                <div>
                  <Label>Custom Headers (JSON)</Label>
                  <textarea
                    className={`${inputCls} font-mono`}
                    rows={4}
                    value={form.custom_headers}
                    onChange={(e) => set('custom_headers', e.target.value)}
                    onBlur={() => markTouched('custom_headers')}
                    placeholder={'{"Authorization": "Bearer token"}'}
                  />
                  <FieldError msg={errFor('custom_headers')} />
                </div>
              </>
            )}

            {/* Enabled toggle */}
            <label className="flex cursor-pointer items-center gap-2 pt-1">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-neutral-300 text-primary-600 focus:ring-primary-500"
                checked={form.enabled}
                onChange={(e) => set('enabled', e.target.checked)}
              />
              <span className="text-sm">Enabled (send alerts through this channel)</span>
            </label>

            {/* Actions */}
            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-neutral-200 pt-4 dark:border-neutral-800">
              <button
                className="btn border border-error-300 text-error-600 hover:bg-error-50 dark:hover:bg-error-900/20"
                disabled={busy}
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 className="h-4 w-4" /> {deleting ? 'Disabling…' : 'Delete'}
              </button>
              <div className="flex gap-2">
                <button className="btn-secondary" disabled={busy} onClick={onClose}>
                  Cancel
                </button>
                <button className="btn-secondary" disabled={busy || hasErrors} onClick={() => void handleTest()}>
                  {testing ? 'Testing…' : 'Test'}
                </button>
                <button className="btn-primary" disabled={busy || hasErrors} onClick={() => void handleSave()}>
                  {saving ? 'Saving…' : 'Save'}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Delete confirmation */}
      {confirmDelete && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4"
          onClick={(e) => {
            e.stopPropagation()
            setConfirmDelete(false)
          }}
        >
          <div className="card w-full max-w-sm p-6" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold">Disable {meta.label}?</h3>
            <p className="mt-2 text-sm text-neutral-500 dark:text-neutral-400">
              This disables the channel and clears its stored settings. You can reconfigure it later.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button
                className="btn bg-error-600 text-white hover:bg-error-700"
                disabled={deleting}
                onClick={() => {
                  setConfirmDelete(false)
                  void handleDelete()
                }}
              >
                {deleting ? 'Disabling…' : 'Disable'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------- page ----------
export default function NotificationSettings() {
  const { toasts, push } = useToasts()
  const { configs, loading, error, refetch } = useNotificationConfigs()
  const { test } = useTestNotificationConfig()

  const [modalChannel, setModalChannel] = useState<ChannelName | null>(null)
  const [testingChannel, setTestingChannel] = useState<ChannelName | null>(null)

  const byChannel = useMemo(() => {
    const m = new Map<ChannelName, NotificationConfig>()
    for (const c of configs) m.set(c.channel, c)
    return m
  }, [configs])

  const anyConfigured = configs.some((c) => c.enabled)

  // Quick test straight from a card (tests the stored config).
  const handleCardTest = async (channel: ChannelName) => {
    setTestingChannel(channel)
    try {
      const result = await test(channel)
      if (result?.test_success) push(`✓ Test sent! Message delivered via ${CHANNEL_META[channel].label}`, 'success')
      else push(`✗ Test failed: ${result?.test_error ?? 'unknown error'}`, 'error')
      await refetch()
    } catch (err) {
      push(`✗ Test failed: ${(err as { message?: string }).message ?? 'error'}`, 'error')
    } finally {
      setTestingChannel(null)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-bold">Notification Channels</h2>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Configure how Sentinel sends you alerts.
        </p>
      </div>

      {error && (
        <div className="card flex items-center justify-between p-4 text-sm">
          <span className="text-red-500">{error}</span>
          <button className="btn-secondary !py-1" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {!error && !loading && !anyConfigured && (
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          No notification channels are configured yet. Click <span className="font-medium">Configure</span> on
          any channel below to get started.
        </p>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {CHANNEL_ORDER.map((channel) => (
          <NotificationConfigCard
            key={channel}
            channel={channel}
            config={byChannel.get(channel)}
            testing={testingChannel === channel}
            onConfigure={() => setModalChannel(channel)}
            onTest={() => void handleCardTest(channel)}
          />
        ))}
      </div>

      {modalChannel && (
        <ConfigModal
          channel={modalChannel}
          onClose={() => setModalChannel(null)}
          onChanged={() => void refetch()}
          push={push}
        />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

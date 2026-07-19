import { Mail, MessageSquare, Send, Bell, Radio, Webhook } from 'lucide-react'

const channels = [
  { id: 'email', label: 'Email', icon: Mail, hint: 'SMTP server, from address, recipients' },
  { id: 'ntfy', label: 'ntfy', icon: Radio, hint: 'Server URL and topic' },
  { id: 'slack', label: 'Slack', icon: MessageSquare, hint: 'Incoming webhook URL' },
  { id: 'discord', label: 'Discord', icon: MessageSquare, hint: 'Webhook URL' },
  { id: 'telegram', label: 'Telegram', icon: Send, hint: 'Bot token and chat ID' },
  { id: 'webhook', label: 'Webhook', icon: Webhook, hint: 'Custom HTTP endpoint' },
]

export default function Notifications() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Notifications</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Configure alert channels and view recent alerts
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {channels.map(({ id, label, icon: Icon, hint }) => (
          <div key={id} className="card p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="rounded-lg bg-primary-100 p-2 text-primary-600 dark:bg-primary-900/40 dark:text-primary-400">
                  <Icon className="h-5 w-5" />
                </div>
                <div>
                  <div className="font-semibold">{label}</div>
                  <div className="text-xs text-neutral-500">{hint}</div>
                </div>
              </div>
              <button className="btn-secondary !py-1" disabled>
                Test
              </button>
            </div>
          </div>
        ))}
      </div>

      <div className="card p-5">
        <div className="mb-3 flex items-center gap-2 font-semibold">
          <Bell className="h-4 w-4" /> Recent Alerts
        </div>
        <div className="py-8 text-center text-sm text-neutral-500">
          No recent alerts.
        </div>
      </div>
    </div>
  )
}

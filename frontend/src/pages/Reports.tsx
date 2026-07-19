import { useState } from 'react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { useMonitors } from '@/hooks/useMonitors'

type ReportTab = 'uptime' | 'response' | 'incidents'

// Placeholder series until the reporting API is wired in.
const placeholderData = Array.from({ length: 14 }, (_, i) => ({
  day: `D${i + 1}`,
  value: 95 + Math.round(Math.sin(i) * 3 + 3),
}))

export default function Reports() {
  const { monitors } = useMonitors()
  const [tab, setTab] = useState<ReportTab>('uptime')
  const [monitorId, setMonitorId] = useState('')
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')

  const tabs: { id: ReportTab; label: string }[] = [
    { id: 'uptime', label: 'Uptime %' },
    { id: 'response', label: 'Response Time' },
    { id: 'incidents', label: 'Incidents' },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Reports</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Historical uptime and SLA analytics
        </p>
      </div>

      <div className="card flex flex-wrap items-end gap-4 p-4">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-500">Monitor</span>
          <select
            value={monitorId}
            onChange={(e) => setMonitorId(e.target.value)}
            className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
          >
            <option value="">All monitors</option>
            {monitors.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-500">Start</span>
          <input
            type="date"
            value={start}
            onChange={(e) => setStart(e.target.value)}
            className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-500">End</span>
          <input
            type="date"
            value={end}
            onChange={(e) => setEnd(e.target.value)}
            className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
          />
        </label>
      </div>

      <div className="flex gap-2">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`rounded-md px-4 py-2 text-sm font-medium ${
              tab === t.id
                ? 'bg-primary-600 text-white'
                : 'bg-white text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="card p-4">
        <div className="h-72">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={placeholderData}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgb(148 163 184 / 0.2)" />
              <XAxis dataKey="day" stroke="rgb(148 163 184)" fontSize={12} />
              <YAxis stroke="rgb(148 163 184)" fontSize={12} domain={[90, 100]} />
              <Tooltip />
              <Line
                type="monotone"
                dataKey="value"
                stroke="#10b981"
                strokeWidth={2}
                dot={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <p className="mt-2 text-center text-xs text-neutral-400">
          Placeholder data — wire to the reporting API.
        </p>
      </div>
    </div>
  )
}

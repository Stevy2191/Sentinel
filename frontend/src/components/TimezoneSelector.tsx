import { useMemo, useState } from 'react'
import { Search } from 'lucide-react'

interface Props {
  value: string
  onChange: (tz: string) => void
}

const FALLBACK_ZONES = [
  'UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Asia/Tokyo',
  'Asia/Shanghai',
  'Asia/Kolkata',
  'Australia/Sydney',
]

function allZones(): string[] {
  // Intl.supportedValuesOf is available in modern runtimes; fall back to a curated list.
  const supported = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] })
    .supportedValuesOf
  if (typeof supported === 'function') {
    try {
      return supported('timeZone')
    } catch {
      /* fall through */
    }
  }
  return FALLBACK_ZONES
}

export default function TimezoneSelector({ value, onChange }: Props) {
  const [query, setQuery] = useState('')
  const zones = useMemo(allZones, [])
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = q ? zones.filter((z) => z.toLowerCase().includes(q)) : zones
    return list.slice(0, 200)
  }, [zones, query])

  return (
    <div>
      <div className="relative mb-2">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-neutral-400" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search timezones…"
          className="w-full rounded-md border border-neutral-300 bg-white py-2 pl-9 pr-3 text-sm dark:border-neutral-700 dark:bg-neutral-800"
        />
      </div>
      <select
        size={6}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-neutral-300 bg-white p-1 text-sm dark:border-neutral-700 dark:bg-neutral-800"
      >
        {filtered.map((z) => (
          <option key={z} value={z}>
            {z}
          </option>
        ))}
      </select>
      <div className="mt-1 text-xs text-neutral-500">Current: {value}</div>
    </div>
  )
}

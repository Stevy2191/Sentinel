import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { useDevices } from '@/hooks/useDevices'
import { groupBySite } from '@/utils/devices'
import { testWalk, type TestWalkResult } from '@/hooks/useMibs'
import type { ApiError } from '@/services/api'

interface Props {
  oid: string
  kind?: string
}

/** Walks oid against a chosen device and shows the result: one column per
 *  walked column, one row per index. Lets an admin jump from a successful
 *  walk into building a custom metric from it. */
export default function TestWalkPanel({ oid, kind }: Props) {
  const { currentUser } = useAuthContext()
  const navigate = useNavigate()
  const { devices } = useDevices()
  const grouped = groupBySite(devices)

  const [deviceId, setDeviceId] = useState('')
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<TestWalkResult | null>(null)

  // A new object was selected: the previous walk no longer applies to it.
  useEffect(() => {
    setResult(null)
    setError(null)
  }, [oid])

  const run = async () => {
    if (!deviceId) return
    setRunning(true)
    setError(null)
    setResult(null)
    try {
      const res = await testWalk(deviceId, oid)
      if (res.ok) {
        setResult(res.result ?? null)
      } else {
        setError(res.error || 'The walk failed')
      }
    } catch (err) {
      setError((err as ApiError).message || 'The walk failed')
    } finally {
      setRunning(false)
    }
  }

  return (
    <section className="card space-y-4 p-6">
      <h2 className="text-lg font-light text-white">Test walk</h2>
      <div className="flex flex-wrap items-center gap-2">
        <select className="rd-select" value={deviceId} onChange={(e) => setDeviceId(e.target.value)} aria-label="Device to test on">
          <option value="">Choose a device…</option>
          {grouped.map((g) => (
            <optgroup key={g.site} label={g.site}>
              {g.devices.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name} ({d.host})
                </option>
              ))}
            </optgroup>
          ))}
        </select>
        <button type="button" className="btn-secondary" disabled={!deviceId || running} onClick={() => void run()}>
          {running ? 'Testing…' : 'Test on device'}
        </button>
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {result && (
        <div className="space-y-2">
          <div className="overflow-x-auto rounded-lg border border-white/10">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="px-3 py-2 font-medium">Index</th>
                  {result.columns.map((c) => (
                    <th key={c.oid} className="whitespace-nowrap px-3 py-2 font-medium">
                      {c.name || c.oid}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {result.rows.map((row, i) => (
                  <tr key={row.index || i}>
                    <td className="px-3 py-2 font-mono text-xs text-slate-400">{row.index || '—'}</td>
                    {result.columns.map((c) => {
                      const cell = row.values[c.oid]
                      return (
                        <td key={c.oid} className="whitespace-nowrap px-3 py-2 text-slate-200">
                          {cell ? (
                            <>
                              {cell.raw}
                              {cell.meaning && <span className="ml-1 text-xs text-slate-500">· {cell.meaning}</span>}
                            </>
                          ) : (
                            '—'
                          )}
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {result.truncated && <p className="text-xs text-slate-500">First 500 rows of more</p>}
        </div>
      )}

      {result && currentUser?.is_admin && (
        <button
          type="button"
          className="btn-secondary"
          onClick={() => navigate(`/network/profiles?new=1&oid=${encodeURIComponent(oid)}&kind=${encodeURIComponent(kind ?? '')}`)}
        >
          Make a metric from this
        </button>
      )}
    </section>
  )
}

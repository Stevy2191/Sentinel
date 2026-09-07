import { useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, Download, FileUp, Loader2, Upload, X } from 'lucide-react'
import api from '@/services/api'
import { useToasts, Toaster } from '@/components/Toast'
import { parseCsvRecords } from '@/utils/csv'
import type { ApiResponse, MonitorInput, MonitorType } from '@/types'

const COLUMNS = ['name', 'type', 'url', 'interval_seconds', 'timeout_seconds', 'retries', 'method', 'tags']

const TEMPLATE = `name,type,url,interval_seconds,timeout_seconds,retries,method,tags
API Gateway,http,https://api.example.com/health,60,10,3,GET,"production,critical"
Postgres Primary,tcp,db.example.com:5432,60,10,3,,database
CDN Edge,ping,cdn.example.com,300,10,0,,edge
`

const VALID_TYPES: MonitorType[] = ['http', 'tcp', 'ping', 'dns', 'webhook']

/** One row's verdict as returned by the backend, aligned by index. */
interface RowResult {
  index: number
  name: string
  valid: boolean
  created: boolean
  id?: string
  error?: string
}

interface BulkResponse {
  dry_run: boolean
  total: number
  valid: number
  invalid: number
  created: number
  results: RowResult[]
}

/** A parsed CSV line paired with the monitor payload it produces. */
interface ParsedRow {
  line: number // 1-based row number in the file, for error messages
  input: MonitorInput
  raw: Record<string, string>
}

function toNumber(value: string, fallback: number): number {
  const n = Number(value)
  return value.trim() !== '' && Number.isFinite(n) ? n : fallback
}

/**
 * Map CSV records onto monitor payloads. Only name/type/url carry meaning here;
 * the rest fall back to the same defaults the single-monitor form uses, so an
 * import only has to include the columns someone actually cares about.
 *
 * Deliberately no validation: the backend is the single source of truth for
 * what a valid monitor is, and it reports per-row reasons. Duplicating those
 * rules here would let the preview drift from the import.
 */
function toInputs(records: Record<string, string>[]): ParsedRow[] {
  return records.map((raw, i) => {
    const type = (raw.type || 'http').toLowerCase() as MonitorType
    const input: MonitorInput = {
      name: raw.name ?? '',
      type: VALID_TYPES.includes(type) ? type : (raw.type as MonitorType),
      url: raw.url ?? '',
      interval_seconds: toNumber(raw.interval_seconds, 60),
      timeout_seconds: toNumber(raw.timeout_seconds, 10),
      retries: toNumber(raw.retries, 3),
    }
    if (raw.method?.trim()) input.method = raw.method.trim().toUpperCase()
    const tags = (raw.tags ?? '').split(',').map((t) => t.trim()).filter(Boolean)
    if (tags.length) input.tags = tags
    return { line: i + 2, input, raw } // +2: 1-based, and row 1 is the header
  })
}

function downloadTemplate() {
  const blob = new Blob([TEMPLATE], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'sentinel-monitors-template.csv'
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * BulkUpload — import many monitors from a spreadsheet export. The flow is
 * paste-or-drop, preview, confirm: the preview is produced by the same backend
 * endpoint that performs the import (with dry_run set), so what you approve is
 * exactly what gets applied.
 */
export default function BulkUpload() {
  const navigate = useNavigate()
  const { toasts, push } = useToasts()
  const fileRef = useRef<HTMLInputElement>(null)

  const [text, setText] = useState('')
  const [parseError, setParseError] = useState<string | null>(null)
  const [preview, setPreview] = useState<BulkResponse | null>(null)
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [done, setDone] = useState<BulkResponse | null>(null)

  const parsed = useMemo(() => {
    if (!text.trim()) return null
    const { records, error } = parseCsvRecords(text)
    if (error) return { rows: [] as ParsedRow[], error }
    return { rows: toInputs(records), error: undefined as string | undefined }
  }, [text])

  const readFile = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => {
      setText(String(reader.result ?? ''))
      setPreview(null)
      setDone(null)
    }
    reader.onerror = () => setParseError('Could not read that file.')
    reader.readAsText(file)
  }

  const validate = async () => {
    if (!parsed || parsed.rows.length === 0) return
    setBusy(true)
    setParseError(null)
    try {
      const { data } = await api.post<ApiResponse<BulkResponse>>('/monitors/bulk', {
        monitors: parsed.rows.map((r) => r.input),
        dry_run: true,
      })
      setPreview(data.data)
    } catch (err) {
      setParseError((err as { message?: string }).message ?? 'Could not validate the file')
    } finally {
      setBusy(false)
    }
  }

  const runImport = async () => {
    if (!parsed || parsed.rows.length === 0) return
    setBusy(true)
    try {
      const { data } = await api.post<ApiResponse<BulkResponse>>('/monitors/bulk', {
        monitors: parsed.rows.map((r) => r.input),
        dry_run: false,
      })
      setDone(data.data)
      push(`Imported ${data.data.created} of ${data.data.total} monitors`, data.data.created > 0 ? 'success' : 'error')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Import failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  const reset = () => {
    setText('')
    setPreview(null)
    setDone(null)
    setParseError(null)
  }

  // ---- after a completed import ----
  if (done) {
    const failures = done.results.filter((r) => !r.created)
    return (
      <div className="mx-auto max-w-3xl space-y-5 pb-10">
        <h1 className="vs-title text-2xl">Import complete</h1>
        <div className="rd-card p-6" style={{ ['--rd-accent' as string]: done.created > 0 ? 'var(--vs-ecg)' : 'var(--vs-flat)' }}>
          <p className="vs-readout text-3xl" style={{ color: done.created > 0 ? 'var(--vs-ecg)' : 'var(--vs-flat)' }}>
            {done.created}
            <span className="text-base" style={{ color: 'var(--vs-text-dim)' }}> of {done.total} created</span>
          </p>
          {failures.length > 0 && (
            <div className="mt-4">
              <p className="vs-eyebrow mb-2">Not imported</p>
              <ul className="space-y-1 text-sm">
                {failures.map((r) => (
                  <li key={r.index} style={{ color: 'var(--vs-text-dim)' }}>
                    <span style={{ color: 'var(--vs-flat)' }}>row {r.index + 2}</span>{' '}
                    {r.name || '(unnamed)'} — {r.error}
                  </li>
                ))}
              </ul>
              <p className="mt-3 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                Fix these rows and import them separately; the successful ones are already live.
              </p>
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <button className="rd-btn rd-btn-primary" onClick={() => navigate('/dashboard')}>
            Go to dashboard
          </button>
          <button className="rd-btn rd-btn-secondary" onClick={reset}>
            Import more
          </button>
        </div>
        <Toaster toasts={toasts} />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-4xl space-y-5 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="vs-title text-2xl">Bulk upload</h1>
        <div className="flex gap-2">
          <button className="rd-btn rd-btn-secondary" onClick={downloadTemplate}>
            <Download className="h-4 w-4" /> Template
          </button>
          <button className="rd-btn rd-btn-secondary" onClick={() => navigate('/dashboard')}>
            <X className="h-4 w-4" /> Cancel
          </button>
        </div>
      </div>

      <div className="rd-card p-5" style={{ ['--rd-accent' as string]: 'var(--vs-cyan)' }}>
        <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
          Paste CSV below or drop a file. <strong style={{ color: 'var(--vs-text)' }}>name</strong>,{' '}
          <strong style={{ color: 'var(--vs-text)' }}>type</strong> and{' '}
          <strong style={{ color: 'var(--vs-text)' }}>url</strong> are required; everything else falls
          back to the usual defaults.
        </p>
        <p className="vs-readout mt-2 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
          {COLUMNS.join(', ')}
        </p>
        <p className="mt-2 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
          HTTP headers and request bodies aren’t supported here — add those monitors with the form.
        </p>
      </div>

      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          const file = e.dataTransfer.files?.[0]
          if (file) readFile(file)
        }}
        className="rounded-lg p-4 transition-colors"
        style={{
          border: `1px dashed ${dragging ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
          backgroundColor: dragging ? 'rgba(61,225,255,0.05)' : 'transparent',
        }}
      >
        <textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            setPreview(null)
          }}
          rows={8}
          spellCheck={false}
          placeholder={TEMPLATE}
          className="rd-input w-full resize-y p-3"
          aria-label="CSV content"
        />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button className="rd-btn rd-btn-secondary" onClick={() => fileRef.current?.click()}>
            <FileUp className="h-4 w-4" /> Choose file
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".csv,text/csv,text/plain"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) readFile(file)
              e.target.value = ''
            }}
          />
          <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
            {parsed?.rows.length ? `${parsed.rows.length} row${parsed.rows.length === 1 ? '' : 's'} parsed` : 'or drop a .csv here'}
          </span>
          <span className="flex-1" />
          <button
            className="rd-btn rd-btn-primary"
            disabled={!parsed || parsed.rows.length === 0 || busy}
            onClick={() => void validate()}
          >
            {busy && !preview ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
            Check rows
          </button>
        </div>
      </div>

      {(parsed?.error || parseError) && (
        <div className="rd-card p-4 text-sm" style={{ ['--rd-accent' as string]: 'var(--vs-flat)', color: 'var(--vs-flat)' }}>
          {parsed?.error ?? parseError}
        </div>
      )}

      {preview && parsed && (
        <div className="rd-card p-5" style={{ ['--rd-accent' as string]: preview.invalid > 0 ? 'var(--vs-amber)' : 'var(--vs-ecg)' }}>
          <p className="mb-3 text-sm" style={{ color: 'var(--vs-text)' }}>
            {preview.total} rows parsed —{' '}
            <span style={{ color: 'var(--vs-ecg)' }}>{preview.valid} ready</span>
            {preview.invalid > 0 && (
              <>
                {', '}
                <span style={{ color: 'var(--vs-flat)' }}>{preview.invalid} need attention</span>
              </>
            )}
          </p>

          <div className="max-h-80 overflow-y-auto">
            <table className="w-full text-left text-sm">
              <thead className="sticky top-0" style={{ backgroundColor: 'var(--vs-panel-2)' }}>
                <tr>
                  {['', 'Row', 'Name', 'Type', 'Target'].map((h) => (
                    <th key={h} className="vs-eyebrow px-2 py-1.5">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {preview.results.map((r) => {
                  const row = parsed.rows[r.index]
                  return (
                    <tr key={r.index} style={{ borderTop: '1px solid var(--vs-line)' }}>
                      <td className="px-2 py-1.5">
                        {r.valid ? (
                          <Check className="h-4 w-4" style={{ color: 'var(--vs-ecg)' }} />
                        ) : (
                          <X className="h-4 w-4" style={{ color: 'var(--vs-flat)' }} />
                        )}
                      </td>
                      <td className="vs-readout px-2 py-1.5 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        {row?.line ?? r.index + 2}
                      </td>
                      <td className="px-2 py-1.5" style={{ color: 'var(--vs-text)' }}>
                        {r.name || <span style={{ color: 'var(--vs-text-dim)' }}>(unnamed)</span>}
                        {!r.valid && (
                          <span className="block text-xs" style={{ color: 'var(--vs-flat)' }}>
                            {r.error}
                          </span>
                        )}
                      </td>
                      <td className="vs-readout px-2 py-1.5 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        {row?.input.type}
                      </td>
                      <td className="px-2 py-1.5 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        <span className="block max-w-xs truncate">{row?.input.url}</span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          <div className="mt-4 flex flex-wrap items-center gap-2">
            <button className="rd-btn rd-btn-primary" disabled={preview.valid === 0 || busy} onClick={() => void runImport()}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
              Import {preview.valid} valid row{preview.valid === 1 ? '' : 's'}
            </button>
            {preview.invalid > 0 && (
              <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                The {preview.invalid} flagged row{preview.invalid === 1 ? '' : 's'} will be skipped.
              </span>
            )}
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

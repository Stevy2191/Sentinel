import { useRef, useState } from 'react'
import { FileStack, Trash2, Upload } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { CISCO_MIB_FILES, deleteMib, uploadMibs, useMibModules, type MibModule, type MibUploadResult } from '@/hooks/useMibs'
import type { ApiError } from '@/services/api'

function SourceBadge({ source }: { source: MibModule['source'] }) {
  return source === 'builtin' ? (
    <span className="inline-flex items-center rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs font-medium text-slate-300">
      Built-in
    </span>
  ) : (
    <span className="inline-flex items-center rounded-full border border-sky-500/30 bg-sky-500/15 px-2.5 py-0.5 text-xs font-medium text-sky-300">
      Uploaded
    </span>
  )
}

function StatusChip({ module }: { module: MibModule }) {
  if (module.status === 'ready') {
    return (
      <span className="inline-flex items-center rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
        Ready
      </span>
    )
  }
  return (
    <span className="inline-flex items-center rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300">
      Waiting for: {module.missing.join(', ') || 'an import'}
    </span>
  )
}

function UploadResultBox({ result }: { result: MibUploadResult }) {
  const waiting = Object.entries(result.waiting)
  return (
    <div className="space-y-1.5 rounded-lg border border-white/10 bg-slate-900/40 p-3 text-sm">
      {result.saved.length > 0 && (
        <p className="text-emerald-400">Saved: {result.saved.join(', ')}</p>
      )}
      {result.skipped.length > 0 && (
        <p className="text-slate-400">Skipped (not MIB files): {result.skipped.join(', ')}</p>
      )}
      {waiting.map(([name, missing]) => (
        <p key={name} className="text-amber-300">
          {name} is waiting for {missing.join(', ')}
        </p>
      ))}
      {result.saved.length === 0 && result.skipped.length === 0 && waiting.length === 0 && (
        <p className="text-slate-500">Nothing to report.</p>
      )}
    </div>
  )
}

export default function MibLibrary() {
  const { currentUser } = useAuthContext()
  const { modules, loading, error, refetch } = useMibModules()
  const fileInput = useRef<HTMLInputElement>(null)
  const [files, setFiles] = useState<File[]>([])
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const [result, setResult] = useState<MibUploadResult | null>(null)
  const [confirm, setConfirm] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  if (!currentUser?.is_admin) {
    return <p className="text-sm text-slate-400">Only administrators manage the MIB library.</p>
  }

  const doUpload = async () => {
    if (files.length === 0) return
    setUploading(true)
    setUploadError(null)
    setResult(null)
    try {
      const res = await uploadMibs(files)
      setResult(res)
      setFiles([])
      if (fileInput.current) fileInput.current.value = ''
      await refetch()
    } catch (err) {
      setUploadError((err as ApiError).message || 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  const doDelete = async (m: MibModule) => {
    setActionError(null)
    try {
      await deleteMib(m.id)
      await refetch()
    } catch (err) {
      setActionError((err as ApiError).message)
    }
    setConfirm(null)
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">MIB library</h1>
        <p className="mt-2 text-sm text-slate-400">
          The MIB modules Sentinel knows. Upload vendor MIBs (or a zip of them) to add names, descriptions and tables to the browser and to custom metrics.
        </p>
      </div>

      <div className="card space-y-4 p-6">
        <h2 className="text-lg font-light text-white">Upload MIBs</h2>
        <div className="flex flex-wrap items-center gap-3">
          <input
            ref={fileInput}
            type="file"
            multiple
            accept=".mib,.my,.txt,.zip"
            onChange={(e) => setFiles(Array.from(e.target.files ?? []))}
            className="text-sm text-slate-300 file:mr-3 file:rounded-md file:border-0 file:bg-white/10 file:px-3 file:py-1.5 file:text-sm file:text-slate-200 hover:file:bg-white/20"
          />
          <button type="button" className="btn-primary flex items-center gap-2" disabled={files.length === 0 || uploading} onClick={() => void doUpload()}>
            <Upload className="h-4 w-4" /> {uploading ? 'Uploading…' : 'Upload'}
          </button>
        </div>
        {uploadError && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{uploadError}</div>
        )}
        {result && <UploadResultBox result={result} />}
      </div>

      <div className="card space-y-3 p-6">
        <h2 className="text-lg font-light text-white">Cisco MIBs</h2>
        <p className="text-sm text-slate-400">
          Sentinel can&apos;t ship Cisco&apos;s MIB files. The Cisco switch health profile works without them; upload these for names and descriptions in the browser:
        </p>
        <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-sm">
          {CISCO_MIB_FILES.map((f) => (
            <li key={f.name}>
              <a href={f.url} target="_blank" rel="noreferrer" className="text-sky-400 hover:underline">
                {f.name}
              </a>
            </li>
          ))}
        </ul>
      </div>

      {(error || actionError) && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error ?? actionError}</div>
      )}

      {loading ? (
        <p className="text-sm text-slate-400">Loading…</p>
      ) : modules.length === 0 ? (
        <div className="card p-8 text-center">
          <FileStack className="mx-auto h-8 w-8 text-slate-500" />
          <p className="mt-2 text-slate-300">No MIB modules yet.</p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="px-4 py-3 font-medium">Name</th>
                  <th className="px-4 py-3 font-medium">Source</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium text-right">Objects</th>
                  <th className="px-4 py-3 font-medium">File</th>
                  <th className="px-4 py-3 font-medium">Updated</th>
                  <th className="px-4 py-3 font-medium" />
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {modules.map((m) => (
                  <tr key={m.id}>
                    <td className="px-4 py-3 font-medium text-slate-200">{m.name}</td>
                    <td className="px-4 py-3">
                      <SourceBadge source={m.source} />
                    </td>
                    <td className="px-4 py-3">
                      <StatusChip module={m} />
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-slate-300">{m.objects}</td>
                    <td className="px-4 py-3 text-slate-400">{m.file_name}</td>
                    <td className="px-4 py-3 text-slate-400">{new Date(m.updated_at).toLocaleString()}</td>
                    <td className="px-4 py-3 text-right">
                      {m.source === 'upload' &&
                        (confirm === m.id ? (
                          <div className="flex items-center justify-end gap-2">
                            <span className="text-xs text-slate-400">Delete {m.name}?</span>
                            <button className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                              Cancel
                            </button>
                            <button className="btn bg-red-600 !py-1 text-white hover:bg-red-700" onClick={() => void doDelete(m)}>
                              Delete
                            </button>
                          </div>
                        ) : (
                          <button
                            className="btn-secondary !px-2 !py-1 text-red-400"
                            title={`Delete ${m.name}`}
                            onClick={() => setConfirm(m.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

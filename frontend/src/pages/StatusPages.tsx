import { useEffect, useState } from 'react'
import { Plus, Pencil, Trash2, ExternalLink } from 'lucide-react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, StatusPage } from '@/types'

export default function StatusPages() {
  const [pages, setPages] = useState<StatusPage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  useEffect(() => {
    api
      .get<ApiResponse<{ pages: StatusPage[] }>>('/status-pages')
      .then(({ data }) => setPages(data.data.pages ?? []))
      .catch((err) => setError(err as ApiError))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Status Pages</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Public, shareable uptime pages
          </p>
        </div>
        <button className="btn-primary" disabled>
          <Plus className="h-4 w-4" />
          New Status Page
        </button>
      </div>

      {error && (
        <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
          {error.message}
        </div>
      )}

      {loading ? (
        <div className="text-neutral-500">Loading…</div>
      ) : pages.length === 0 ? (
        <div className="card p-10 text-center text-neutral-500">No status pages yet.</div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {pages.map((p) => (
            <div key={p.id} className="card p-5">
              <div className="flex items-start justify-between">
                <div>
                  <div className="font-semibold">{p.name}</div>
                  <div className="text-sm text-neutral-500">/{p.slug}</div>
                </div>
                <span
                  className={`rounded-md px-2 py-1 text-xs font-medium ${
                    p.published
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300'
                      : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
                  }`}
                >
                  {p.published ? 'published' : 'draft'}
                </span>
              </div>
              <div className="mt-4 flex gap-2">
                <a
                  href={`/public/status/${p.slug}`}
                  target="_blank"
                  rel="noreferrer"
                  className="btn-secondary !py-1"
                >
                  <ExternalLink className="h-4 w-4" /> View
                </a>
                <button className="btn-secondary !px-2 !py-1" disabled>
                  <Pencil className="h-4 w-4" />
                </button>
                <button className="btn-secondary !px-2 !py-1 text-error-600" disabled>
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Plus, Pencil, Trash2, ExternalLink, ArrowLeft, Globe } from 'lucide-react'
import {
  useStatusPages,
  useStatusPage,
  useCreateStatusPage,
  useUpdateStatusPage,
  useDeleteStatusPage,
  useAddMonitorToPage,
  useRemoveMonitorFromPage,
  useUpdateMonitorPosition,
} from '@/hooks/useStatusPages'
import { useMonitors } from '@/hooks/useMonitors'
import { useToasts, Toaster } from '@/components/Toast'
import StatusPageForm, { statusPageToForm } from '@/components/StatusPageForm'
import MonitorList from '@/components/MonitorList'
import { formatDate } from '@/utils/formatters'
import type { StatusPageInput } from '@/types'

type Mode = 'list' | 'create' | 'detail' | 'edit'

export default function StatusPages({ mode = 'list' }: { mode?: Mode }) {
  if (mode === 'create' || mode === 'edit') return <StatusPageEditor mode={mode} />
  if (mode === 'detail') return <StatusPageDetailView />
  return <StatusPageList />
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------
function StatusPageList() {
  const navigate = useNavigate()
  const { pages, loading, error, refetch } = useStatusPages()
  const { delete: deletePage } = useDeleteStatusPage()
  const { toasts, push } = useToasts()
  const [confirmSlug, setConfirmSlug] = useState<string | null>(null)

  const doDelete = async () => {
    if (!confirmSlug) return
    try {
      await deletePage(confirmSlug)
      push('Status page deleted', 'success')
      await refetch()
    } catch {
      push('Delete failed', 'error')
    } finally {
      setConfirmSlug(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-black" style={{ color: 'var(--rd-text)' }}>STATUS PAGES</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Create and manage public status dashboards
          </p>
        </div>
        <button className="btn-primary" onClick={() => navigate('/status-pages/create')}>
          <Plus className="h-4 w-4" /> Create New Status Page
        </button>
      </div>

      {error && (
        <div className="card flex items-center justify-between border-error-300 p-4">
          <span className="text-error-700 dark:text-error-300">{error.message}</span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {loading ? (
        <div className="text-neutral-500">Loading…</div>
      ) : pages.length === 0 ? (
        <div className="card p-12 text-center text-neutral-500">
          No status pages yet. Create your first one.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {pages.map((p) => (
            <div key={p.id} className="card p-5 transition duration-150 hover:scale-[1.01] hover:shadow-card-hover">
              <div className="flex items-start justify-between">
                <div className="min-w-0">
                  <Link
                    to={`/public/status/${p.slug}`}
                    className="truncate text-lg font-bold text-primary-600 hover:underline"
                  >
                    /{p.slug}
                  </Link>
                  <div className="truncate font-medium">{p.name}</div>
                </div>
                <span
                  className={`shrink-0 rounded-md px-2 py-1 text-xs font-medium ${
                    p.published
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                      : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
                  }`}
                >
                  {p.published ? 'Published' : 'Draft'}
                </span>
              </div>

              {p.description && (
                <p className="mt-2 line-clamp-2 text-sm text-neutral-500 dark:text-neutral-400" title={p.description}>
                  {p.description}
                </p>
              )}

              <div className="mt-2 text-xs text-neutral-400">
                {p.published ? 'Published' : 'Draft'} • {p.monitor_count ?? 0}{' '}
                {(p.monitor_count ?? 0) === 1 ? 'monitor' : 'monitors'}
              </div>
              <div className="mt-1 text-xs text-neutral-400">Created {formatDate(p.created_at)}</div>

              <div className="mt-4 flex gap-2">
                <a
                  href={`/public/status/${p.slug}`}
                  target="_blank"
                  rel="noreferrer"
                  className="btn-secondary !py-1"
                >
                  <ExternalLink className="h-4 w-4" /> View
                </a>
                <button className="btn-secondary !py-1" onClick={() => navigate(`/status-pages/${p.slug}/detail`)}>
                  Manage
                </button>
                <button
                  className="btn-secondary !py-1"
                  onClick={() => navigate(`/status-pages/${p.slug}/edit`)}
                >
                  <Pencil className="h-4 w-4" />
                </button>
                <button
                  className="btn-secondary !py-1 text-error-600"
                  onClick={() => setConfirmSlug(p.slug)}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {confirmSlug && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Delete status page?</h3>
            <p className="mt-2 text-sm text-neutral-500 dark:text-neutral-400">
              This deletes /{confirmSlug} and its monitor associations. This cannot be undone.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmSlug(null)}>
                Cancel
              </button>
              <button className="btn bg-error-600 text-white hover:bg-error-700" onClick={() => void doDelete()}>
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Create / Edit
// ---------------------------------------------------------------------------
function StatusPageEditor({ mode }: { mode: 'create' | 'edit' }) {
  const navigate = useNavigate()
  const { slug } = useParams<{ slug: string }>()
  const { toasts, push } = useToasts()

  const { pages } = useStatusPages()
  const { page, loading } = useStatusPage(mode === 'edit' ? slug : undefined)
  const { create, loading: creating, error: createErr } = useCreateStatusPage()
  const { update, loading: updating, error: updateErr } = useUpdateStatusPage(slug)

  const handleCreate = async (input: StatusPageInput) => {
    const created = await create(input)
    push(`Status page ${created.name} created`, 'success')
    navigate(`/status-pages/${created.slug}/detail`)
  }
  const handleUpdate = async (input: StatusPageInput) => {
    const updated = await update(input)
    push(`Status page ${updated.name} updated`, 'success')
    navigate(`/status-pages/${slug}/detail`)
  }

  if (mode === 'edit' && (loading || !page)) {
    return <div className="text-neutral-500">Loading…</div>
  }

  return (
    <div className="max-w-2xl space-y-6">
      <button className="btn-secondary" onClick={() => navigate('/status-pages')}>
        <ArrowLeft className="h-4 w-4" /> Back
      </button>
      <h1 className="text-2xl font-bold">
        {mode === 'create' ? 'Create Status Page' : 'Edit Status Page'}
      </h1>
      {mode === 'create' ? (
        <StatusPageForm
          onSubmit={handleCreate}
          isLoading={creating}
          error={createErr}
          submitLabel="Create Page"
          existingSlugs={pages.map((p) => p.slug)}
          onCancel={() => navigate('/status-pages')}
        />
      ) : (
        <StatusPageForm
          initialValues={page ? statusPageToForm(page) : undefined}
          slugReadOnly
          onSubmit={handleUpdate}
          isLoading={updating}
          error={updateErr}
          submitLabel="Update Page"
          onCancel={() => navigate(`/status-pages/${slug}/detail`)}
        />
      )}
      <Toaster toasts={toasts} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Detail
// ---------------------------------------------------------------------------
function StatusPageDetailView() {
  const navigate = useNavigate()
  const { slug } = useParams<{ slug: string }>()
  const { toasts, push } = useToasts()

  const { page, monitors, loading, error, refetch } = useStatusPage(slug)
  const { monitors: allMonitors } = useMonitors({ limit: 500 })
  const { add } = useAddMonitorToPage()
  const { remove } = useRemoveMonitorFromPage()
  const { update: updatePosition } = useUpdateMonitorPosition(slug)

  const [showAdd, setShowAdd] = useState(false)
  const [selMonitor, setSelMonitor] = useState('')
  const [groupName, setGroupName] = useState('')

  const pageMonitorIds = useMemo(() => new Set(monitors.map((m) => m.id)), [monitors])
  const available = useMemo(
    () => allMonitors.filter((m) => !pageMonitorIds.has(m.id)),
    [allMonitors, pageMonitorIds]
  )

  const handleAdd = async () => {
    if (!slug || !selMonitor) return
    try {
      await add(
        { monitor_id: selMonitor, group_name: groupName || undefined, position: monitors.length + 1 },
        slug
      )
      push('Monitor added', 'success')
      setShowAdd(false)
      setSelMonitor('')
      setGroupName('')
      await refetch()
    } catch (err) {
      push((err as { message?: string }).message || 'Failed to add monitor', 'error')
    }
  }

  const handleRemove = async (monitorId: string) => {
    if (!slug) return
    try {
      await remove(slug, monitorId)
      push('Monitor removed', 'success')
      await refetch()
    } catch {
      push('Failed to remove monitor', 'error')
    }
  }

  const handleUpdatePosition = async (monitorId: string, position: number) => {
    try {
      await updatePosition(monitorId, position)
      push('Position updated', 'success')
      await refetch()
    } catch (err) {
      push((err as { message?: string }).message || 'Failed to update position', 'error')
    }
  }

  if (loading && !page) return <div className="text-neutral-500">Loading…</div>
  if (error || !page) {
    return (
      <div className="space-y-4">
        <div className="card p-6 text-neutral-500">{error?.message ?? 'Status page not found.'}</div>
        <button className="btn-secondary" onClick={() => navigate('/status-pages')}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
      </div>
    )
  }

  return (
    <div className="max-w-4xl space-y-6">
      <button className="btn-secondary" onClick={() => navigate('/status-pages')}>
        <ArrowLeft className="h-4 w-4" /> Back
      </button>

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          {page.logo_url && (
            <img src={page.logo_url} alt="" className="h-10 w-10 rounded object-contain" />
          )}
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold">{page.name}</h1>
              {page.theme_color && (
                <span
                  className="inline-block h-4 w-4 rounded-full border border-neutral-300 dark:border-neutral-700"
                  style={{ backgroundColor: page.theme_color }}
                  title={page.theme_color}
                />
              )}
            </div>
            <div className="text-sm text-neutral-500">/{page.slug}</div>
          </div>
          <span
            className={`rounded-md px-2 py-1 text-xs font-medium ${
              page.published
                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
            }`}
          >
            {page.published ? 'Published' : 'Draft'}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn-secondary" onClick={() => navigate(`/status-pages/${slug}/edit`)}>
            <Pencil className="h-4 w-4" /> Edit
          </button>
          <a
            href={`/public/status/${page.slug}`}
            target="_blank"
            rel="noreferrer"
            className="btn-secondary"
          >
            <Globe className="h-4 w-4" /> View Public Page
          </a>
        </div>
      </div>

      {page.description && (
        <p className="text-neutral-500 dark:text-neutral-400">{page.description}</p>
      )}

      <div className="card p-5">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-semibold">Monitors on this page</h2>
          <button className="btn-primary !py-1.5" onClick={() => setShowAdd(true)}>
            <Plus className="h-4 w-4" /> Add Monitor
          </button>
        </div>
        <MonitorList
          monitors={monitors}
          onRemove={(id) => void handleRemove(id)}
          onUpdatePosition={handleUpdatePosition}
        />
      </div>

      {showAdd && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-md p-6">
            <h3 className="text-lg font-semibold">Add Monitor to Page</h3>
            <div className="mt-4 space-y-4">
              <label className="block">
                <span className="mb-1 block text-sm font-medium">Monitor</span>
                <select
                  className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800"
                  value={selMonitor}
                  onChange={(e) => setSelMonitor(e.target.value)}
                >
                  <option value="">Select a monitor…</option>
                  {available.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </select>
                {available.length === 0 && (
                  <span className="mt-1 block text-xs text-neutral-500">
                    All monitors are already on this page.
                  </span>
                )}
              </label>
              <label className="block">
                <span className="mb-1 block text-sm font-medium">Group (optional)</span>
                <input
                  className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800"
                  value={groupName}
                  onChange={(e) => setGroupName(e.target.value)}
                  placeholder="APIs"
                />
              </label>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setShowAdd(false)}>
                Cancel
              </button>
              <button className="btn-primary" disabled={!selMonitor} onClick={() => void handleAdd()}>
                Add
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

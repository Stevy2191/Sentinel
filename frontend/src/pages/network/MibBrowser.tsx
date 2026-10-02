import { useEffect, useRef, useState } from 'react'
import { Check, ChevronDown, ChevronRight, Copy, Search } from 'lucide-react'
import { mibChildren, mibObject, mibSearch, type MibObject, type MibObjectDetail } from '@/hooks/useMibs'
import { formatEnum, isValidOid } from '@/utils/mib'
import TestWalkPanel from '@/components/network/TestWalkPanel'
import type { ApiError } from '@/services/api'

/** A value that only updates `ms` after the last change, so typing into the
 *  search box doesn't fire a request per keystroke. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return debounced
}

/** Copies text to the clipboard; if that's refused (no secure context), falls
 *  back to selecting el's text so the person can copy it themselves. Returns
 *  whether the clipboard copy itself succeeded. */
async function copyWithFallback(text: string, el: HTMLElement | null): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    if (el) {
      const range = document.createRange()
      range.selectNodeContents(el)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
    }
    return false
  }
}

interface TreeNodeProps {
  node: MibObject
  depth: number
  selectedOid: string | null
  expanded: Record<string, boolean>
  childrenByOid: Record<string, MibObject[]>
  loadingOid: string | null
  onToggle: (oid: string) => void
  onSelect: (oid: string) => void
}

function MibTreeNode({ node, depth, selectedOid, expanded, childrenByOid, loadingOid, onToggle, onSelect }: TreeNodeProps) {
  const isExpanded = !!expanded[node.oid]
  const kids = childrenByOid[node.oid]
  return (
    <div>
      <div
        className={`flex items-center gap-1 rounded px-1 py-1 text-sm ${selectedOid === node.oid ? 'bg-white/10' : 'hover:bg-white/5'}`}
        style={{ paddingLeft: `${depth * 16}px` }}
      >
        {node.has_children ? (
          <button
            type="button"
            className="shrink-0 rounded p-0.5 text-slate-500 hover:text-slate-300"
            onClick={() => onToggle(node.oid)}
            aria-label={isExpanded ? `Collapse ${node.name}` : `Expand ${node.name}`}
          >
            {isExpanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        ) : (
          <span className="w-4 shrink-0" />
        )}
        <button type="button" className="min-w-0 flex-1 truncate text-left text-slate-300" onClick={() => onSelect(node.oid)} title={node.oid}>
          {node.name}
        </button>
        <span className="shrink-0 text-[10px] uppercase tracking-wide text-slate-500">{node.kind}</span>
      </div>
      {isExpanded &&
        (kids ? (
          kids.map((c) => (
            <MibTreeNode
              key={c.oid}
              node={c}
              depth={depth + 1}
              selectedOid={selectedOid}
              expanded={expanded}
              childrenByOid={childrenByOid}
              loadingOid={loadingOid}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))
        ) : (
          <p className="py-1 text-xs text-slate-500" style={{ paddingLeft: `${(depth + 1) * 16}px` }}>
            {loadingOid === node.oid ? 'Loading…' : '—'}
          </p>
        ))}
    </div>
  )
}

/** The selected object's full detail: name, OID, type, description, enum and,
 *  for a table or row, its columns. Shown even when the object has no MIB
 *  definition (a raw OID), just with less to say. */
function ObjectDetail({ oid, detail, loading, error }: { oid: string; detail: MibObjectDetail | null; loading: boolean; error: string | null }) {
  const oidRef = useRef<HTMLSpanElement>(null)
  const [copied, setCopied] = useState(false)

  const copyOid = async () => {
    const ok = await copyWithFallback(detail?.oid ?? oid, oidRef.current)
    setCopied(ok)
    window.setTimeout(() => setCopied(false), 1500)
  }

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (error) return <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>

  const enumText = detail ? formatEnum(detail.enum) : ''

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-light text-white">{detail?.name ?? 'Unknown object'}</h2>
        <div className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-400">
          <span ref={oidRef} className="font-mono text-xs text-slate-300">
            {detail?.oid ?? oid}
          </span>
          <button type="button" onClick={() => void copyOid()} aria-label={copied ? 'Copied' : 'Copy OID'} className="text-slate-500 hover:text-slate-300">
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
          </button>
          {detail?.module && <span>· {detail.module}</span>}
        </div>
      </div>

      {!detail ? (
        <p className="text-sm text-slate-500">No MIB definition for this OID. You can still test-walk it below.</p>
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
            <dt className="text-slate-500">Kind</dt>
            <dd className="text-slate-200">{detail.kind}</dd>
            <dt className="text-slate-500">Type</dt>
            <dd className="text-slate-200">
              {detail.type_name || '—'} {detail.base_type && detail.base_type !== detail.type_name ? `(${detail.base_type})` : ''}
            </dd>
            <dt className="text-slate-500">Units</dt>
            <dd className="text-slate-200">{detail.units || '—'}</dd>
            <dt className="text-slate-500">Access</dt>
            <dd className="text-slate-200">{detail.access || '—'}</dd>
          </dl>

          <div>
            <p className="mb-1 text-xs uppercase tracking-widest text-slate-500">Description</p>
            <div className="max-h-40 overflow-y-auto whitespace-pre-wrap rounded-lg border border-white/10 bg-slate-900/40 p-3 text-sm text-slate-300">
              {detail.description || '—'}
            </div>
          </div>

          {enumText && (
            <div>
              <p className="mb-1 text-xs uppercase tracking-widest text-slate-500">Enumeration</p>
              <p className="text-sm text-slate-300">{enumText}</p>
            </div>
          )}

          {detail.columns && detail.columns.length > 0 && (
            <div>
              <p className="mb-1 text-xs uppercase tracking-widest text-slate-500">Columns</p>
              <div className="overflow-hidden rounded-lg border border-white/10">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                      <th className="px-3 py-2 font-medium">Name</th>
                      <th className="px-3 py-2 font-medium">OID</th>
                      <th className="px-3 py-2 font-medium">Type</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {detail.columns.map((c) => (
                      <tr key={c.oid}>
                        <td className="px-3 py-2 text-slate-200">{c.name}</td>
                        <td className="px-3 py-2 font-mono text-xs text-slate-400">{c.oid}</td>
                        <td className="px-3 py-2 text-slate-400">{c.type_name || c.base_type || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default function MibBrowser() {
  // Search
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebounced(search, 300)
  const [searchResults, setSearchResults] = useState<MibObject[]>([])
  const [searching, setSearching] = useState(false)

  // Tree
  const [roots, setRoots] = useState<MibObject[] | null>(null)
  const [childrenByOid, setChildrenByOid] = useState<Record<string, MibObject[]>>({})
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [loadingOid, setLoadingOid] = useState<string | null>(null)

  // Raw OID
  const [rawOid, setRawOid] = useState('')
  const [rawOidError, setRawOidError] = useState<string | null>(null)

  // Selected object / detail
  const [oid, setOid] = useState<string | null>(null)
  const [detail, setDetail] = useState<MibObjectDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)

  useEffect(() => {
    void mibChildren('').then(setRoots)
  }, [])

  useEffect(() => {
    const q = debouncedSearch.trim()
    if (q.length < 2) {
      setSearchResults([])
      return
    }
    let cancelled = false
    setSearching(true)
    mibSearch(q)
      .then((r) => {
        if (!cancelled) setSearchResults(r)
      })
      .finally(() => {
        if (!cancelled) setSearching(false)
      })
    return () => {
      cancelled = true
    }
  }, [debouncedSearch])

  useEffect(() => {
    if (!oid) {
      setDetail(null)
      setDetailError(null)
      return
    }
    let cancelled = false
    setDetailLoading(true)
    setDetailError(null)
    mibObject(oid)
      .then((d) => {
        if (!cancelled) setDetail(d)
      })
      .catch((err) => {
        if (cancelled) return
        setDetail(null)
        const e = err as ApiError
        if (e.status !== 404) setDetailError(e.message || 'Could not load this object')
      })
      .finally(() => {
        if (!cancelled) setDetailLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [oid])

  const toggle = async (nodeOid: string) => {
    setExpanded((e) => ({ ...e, [nodeOid]: !e[nodeOid] }))
    if (!childrenByOid[nodeOid]) {
      setLoadingOid(nodeOid)
      try {
        const kids = await mibChildren(nodeOid)
        setChildrenByOid((m) => ({ ...m, [nodeOid]: kids }))
      } finally {
        setLoadingOid((o) => (o === nodeOid ? null : o))
      }
    }
  }

  const submitRawOid = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = rawOid.trim()
    if (!isValidOid(trimmed)) {
      setRawOidError('Enter a numeric OID such as 1.3.6.1.2.1.1')
      return
    }
    setRawOidError(null)
    setOid(trimmed)
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">MIB browser</h1>
        <p className="mt-2 text-sm text-slate-400">Browse every object Sentinel&apos;s MIB library knows, and test-walk any OID against a real device.</p>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-4">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search by name, OID or description…"
              aria-label="Search MIB objects"
              className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
            />
          </div>

          {debouncedSearch.trim().length >= 2 && (
            <div className="card max-h-64 divide-y divide-white/10 overflow-y-auto">
              {searching ? (
                <p className="p-3 text-sm text-slate-400">Searching…</p>
              ) : searchResults.length === 0 ? (
                <p className="p-3 text-sm text-slate-500">No matches.</p>
              ) : (
                searchResults.map((r) => (
                  <button key={r.oid} type="button" className="block w-full p-3 text-left hover:bg-white/5" onClick={() => setOid(r.oid)}>
                    <div className="text-sm text-slate-200">{r.name} <span className="text-xs text-slate-500">({r.kind})</span></div>
                    <div className="text-xs text-slate-500">{r.module} · {r.oid}</div>
                  </button>
                ))
              )}
            </div>
          )}

          <div className="card max-h-[32rem] overflow-y-auto p-2">
            {roots === null ? (
              <p className="p-3 text-sm text-slate-400">Loading…</p>
            ) : (
              roots.map((r) => (
                <MibTreeNode
                  key={r.oid}
                  node={r}
                  depth={0}
                  selectedOid={oid}
                  expanded={expanded}
                  childrenByOid={childrenByOid}
                  loadingOid={loadingOid}
                  onToggle={(o) => void toggle(o)}
                  onSelect={setOid}
                />
              ))
            )}
          </div>

          <form onSubmit={submitRawOid} className="card flex flex-wrap items-center gap-2 p-4">
            <label className="text-sm text-slate-300" htmlFor="raw-oid">
              Raw OID
            </label>
            <input
              id="raw-oid"
              className="rd-input min-w-[180px] flex-1"
              placeholder="1.3.6.1.2.1.1.1"
              value={rawOid}
              onChange={(e) => setRawOid(e.target.value)}
            />
            <button type="submit" className="btn-secondary">
              Go
            </button>
            {rawOidError && <p className="w-full text-xs text-red-400">{rawOidError}</p>}
          </form>
        </div>

        <div className="space-y-4">
          {!oid ? (
            <div className="card p-8 text-center text-sm text-slate-500">
              Select an object from the tree or search, or enter a raw OID, to see its detail and test-walk it.
            </div>
          ) : (
            <>
              <div className="card p-6">
                <ObjectDetail oid={oid} detail={detail} loading={detailLoading} error={detailError} />
              </div>
              <TestWalkPanel oid={oid} kind={detail?.kind} />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

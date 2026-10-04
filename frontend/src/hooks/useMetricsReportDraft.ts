import { useCallback, useEffect, useMemo, useState } from 'react'
import { useScopePreview } from '@/hooks/useScopePreview'
import { MAX_REPORT_METRICS, type ReportScopeData } from '@/types/reports'
import {
  EMPTY_METRICS_SELECTION,
  EMPTY_NETWORK_SCOPE,
  networkScopeData,
  networkScopeError,
  reapplyMetrics,
  scopeNames,
  type MetricsSelection,
  type NetworkScopeDraft,
} from '@/utils/reportScope'

/**
 * The Metrics part of a report being built: the network scope, its live
 * preview, and the ordered metrics. The wizard and the quick dialog both use
 * it, so the two cannot drift. enabled is false while another report type is
 * chosen: nothing is fetched, and the picks are kept for coming back.
 */
export function useMetricsReportDraft(enabled: boolean) {
  const [scope, setScope] = useState<NetworkScopeDraft>(EMPTY_NETWORK_SCOPE)
  const [selection, setSelection] = useState<MetricsSelection>(EMPTY_METRICS_SELECTION)

  const scopeError = networkScopeError(scope)
  const scopeData = useMemo(() => networkScopeData(scope), [scope])
  const ready = enabled && scopeError === null
  const preview = useScopePreview(ready ? scope.scopeType : null, ready ? scopeData : null)

  // Every answer for a changed scope re-applies the defaults or, once the list
  // was edited, drops the metrics the scope no longer offers.
  const answer = preview.data
  useEffect(() => {
    if (answer) setSelection((cur) => reapplyMetrics(cur, answer))
  }, [answer])

  const setMetrics = useCallback((metrics: string[]) => {
    setSelection((cur) => ({ ...cur, metrics: metrics.slice(0, MAX_REPORT_METRICS), edited: true }))
  }, [])

  const reset = useCallback(() => {
    setScope(EMPTY_NETWORK_SCOPE)
    setSelection(EMPTY_METRICS_SELECTION)
  }, [])

  /** scope_data for the create call: the active tab's ids plus the metrics. */
  const payloadData = useMemo<ReportScopeData>(
    () => ({ ...scopeData, metrics: selection.metrics }),
    [scopeData, selection.metrics],
  )
  const names = useMemo(() => scopeNames(scope.sites, scope.devices), [scope.sites, scope.devices])

  return {
    scope,
    setScope,
    scopeError,
    scopeData,
    preview,
    /** The scope is complete, checked by the server, and its metrics are known. */
    scopeReady: scopeError === null && preview.data !== null,
    selection,
    setMetrics,
    metricsError: selection.metrics.length === 0 ? 'Choose at least one metric' : null,
    payloadData,
    names,
    reset,
  }
}

/**
 * Reporting windows offered on the dashboard's headline card. Kept in its own
 * module so the constant does not live inside a component file that a page
 * would otherwise have to import purely for the type.
 */
export type ReportPeriod = '24h' | '7d' | '30d'

export const REPORT_PERIODS: { key: ReportPeriod; label: string; heading: string; hours: number }[] = [
  { key: '24h', label: '24 hours', heading: 'Last 24 hours', hours: 24 },
  { key: '7d', label: '7 days', heading: 'Last 7 days', hours: 24 * 7 },
  { key: '30d', label: '30 days', heading: 'Last 30 days', hours: 24 * 30 },
]

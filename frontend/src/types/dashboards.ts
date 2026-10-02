import type { MetricPoint, MetricsRange } from '@/hooks/useMetrics'
import type { PortView, UnitFaceplate } from '@/hooks/usePorts'
import type { HealthMetric } from '@/hooks/useDeviceHealth'
import type { DeviceStatus, DeviceType } from '@/hooks/useDevices'

export type DashboardAccess = 'none' | 'view' | 'edit' | 'manage'

export type WidgetType =
  | 'label' | 'timeseries' | 'stat' | 'port_grid' | 'device_health' | 'site_power'
  | 'top_n' | 'event_log' | 'device_table' | 'open_incidents' | 'monitors'

export type WidgetState = 'ok' | 'no_access' | 'removed' | 'no_data'

export interface Dashboard {
  id: string
  name: string
  description: string
  site_id: string | null
  owner_id: string | null
  created_by: string | null
  version: number
  created_at: string
  updated_at: string
  site_name: string
  access: DashboardAccess
  published: boolean
  /** May change the name, widgets and layout now (published needs an admin). */
  can_edit: boolean
  can_share: boolean
  widget_count: number
}

export interface DashboardWidget {
  id: string
  type: WidgetType
  title: string
  x: number
  y: number
  w: number
  h: number
  /** Only sent to callers who can edit the dashboard. */
  config?: Record<string, unknown>
}

export interface DashboardDetail extends Dashboard {
  widgets: DashboardWidget[]
}

export interface WidgetResponse<T = unknown> {
  state: WidgetState
  data?: T
  hidden: number
  removed: number
  refresh_seconds: number
  generated_at: string
}

export interface PublicWidget {
  id: string
  type: WidgetType
  title: string
  x: number
  y: number
  w: number
  h: number
}

export interface PublicDashboard {
  name: string
  description: string
  version: number
  widgets: PublicWidget[]
}

export interface DashboardShare {
  user_id: string
  username: string
  email: string
  permission: 'readonly' | 'editable'
  created_at: string
}

export interface PublicLink {
  dashboard_id: string
  token: string
  created_by: string
  created_at: string
}

export interface PublicLinkInfo {
  link: PublicLink | null
  broad_widgets: { id: string; type: WidgetType; title: string }[]
}

export interface DeviceMetric {
  metric: string
  label: string
  unit: string
  instances: { instance: string; label: string }[]
}

// ---- Widget data (mirrors backend/internal/dashboards/widget_*.go) ----

export interface LabelData {
  text: string
  size: 's' | 'm' | 'l'
}

export interface SeriesLine {
  key: string
  label: string
  metric: string
  unit: string
  points: MetricPoint[]
}

export interface TimeseriesData {
  range: MetricsRange
  resolution: string
  step_seconds: number
  lines: SeriesLine[]
}

export interface StatData {
  label: string
  unit: string
  value: number
  level: 'ok' | 'warn' | 'crit'
  mode: 'latest' | 'average'
  range: MetricsRange
  spark?: MetricPoint[]
}

export interface PortGridData {
  device_id?: string
  device_name: string
  model: string
  ports: PortView[]
  faceplates: UnitFaceplate[]
}

export interface DeviceHealthData {
  device_id?: string
  device_name: string
  metrics: HealthMetric[]
}

export interface UPSCard {
  device_id?: string
  name: string
  status: DeviceStatus
  readings: Record<string, number>
  conditions: string[]
  low_battery_pct: number
  high_load_pct: number
}

export interface SitePowerData {
  upses: UPSCard[]
}

export interface TopNItem {
  device_id?: string
  device_name: string
  if_index: number
  port_name: string
  alias: string
  value: number
}

export interface TopNData {
  measure: 'traffic' | 'utilisation' | 'errors'
  unit: string
  range: MetricsRange
  items: TopNItem[]
}

export interface EventItem {
  time: string
  kind: string
  device_id?: string
  device_name: string
  if_index?: number
  port_label: string
  port_name: string
  subject: string
  condition: string
  severity: string
  incident_id?: string
  ended_at?: string
}

export interface EventLogData {
  items: EventItem[]
}

export interface DeviceRow {
  device_id?: string
  name: string
  host?: string
  type: DeviceType
  status: DeviceStatus
  vendor_model: string
  last_seen_at: string | null
  availability_30d: number | null
  open_incidents: number
}

export interface DeviceTableData {
  devices: DeviceRow[]
}

export interface IncidentRow {
  incident_id?: string
  subject_type: 'monitor' | 'device'
  subject_name: string
  site_name: string
  condition: string
  severity: string
  start_time: string
  duration_seconds: number
  device_id?: string
  monitor_id?: string
  port_if_index?: number
}

export interface OpenIncidentsData {
  incidents: IncidentRow[]
  total: number
}

export interface UptimeBucket {
  label: string
  status: 'up' | 'down' | 'partial' | 'nodata'
  uptime: number
}

export interface MonitorRow {
  monitor_id?: string
  name: string
  type: string
  status: string
  response_time_ms: number
  last_check_at: string | null
  buckets?: UptimeBucket[]
}

export interface AgentRow {
  agent_id?: string
  name: string
  status: string
  last_heartbeat: string | null
}

export interface MonitorsData {
  style: 'list' | 'bars'
  window: '24h' | '90d'
  monitors: MonitorRow[]
  agents: AgentRow[]
}

// The site map API's JSON (services.MapSnapshot, services.MapTraffic).

export interface MapEnd {
  device_id: string
  /** null: the device itself, port unknown. */
  if_index: number | null
  /** The faceplate label ("51", "1/4"); "" with no port. */
  label: string
  /** ifName ("eth10", "0/51"). */
  name: string
}

export interface MapDevice {
  id: string
  name: string
  /** router, switch, or another type an editor added. */
  type: string
  model: string
  /** Signed-in viewers only. */
  host?: string
  status: 'up' | 'down' | 'pending' | 'paused' | 'error' | string
  status_detail?: string
  down_since: string | null
  uptime_seconds: number | null
  /** Saved position, or null to lay it out automatically. */
  x: number | null
  y: number | null
  added: boolean
  /** End devices plugged into it (signed-in viewers, routers and switches). */
  connected?: number
  /** A router's internet (WAN) ports; a public map leaves name and reason "". */
  wan?: MapWAN[]
}

export interface MapWAN {
  if_index: number
  name: string
  speed_bps: number
  /** Why it's WAN ("Has a public address"), or "" when set by hand. */
  reason: string
}

export type LinkSource = 'manual' | 'port_setting' | 'lldp' | 'cdp' | 'guess'

export interface MapLink {
  key: string
  a: MapEnd
  b: MapEnd
  sources: LinkSource[] | string[]
  guess: boolean
  stale: boolean
  last_seen: string | null
  manual_id: string | null
  /** The rate end's port speed; 0 unknown. */
  speed_bps: number
  threshold_pct: number
  /** Which end's port the traffic figures come from. */
  rate_end: 'a' | 'b' | ''
  /**
   * A bundle (a LAG, or parallel cables between the same two devices): each
   * cable, oriented like the bundle and in A's port order. A bundle's key is
   * "bundle:" + theirs, its speed their sum, and its traffic theirs, added
   * up in the browser (bundleState): the traffic map carries member keys.
   */
  members?: MapLink[]
  /** A bundle's LAG name at each end ("lag 1", "Po1"; "" not a LAG there); null or absent: not a LAG. */
  lag?: { a: string; b: string } | null
}

export interface MapSnapshot {
  site_id: string
  site_name: string
  can_edit: boolean
  is_admin: boolean
  devices: MapDevice[]
  links: MapLink[]
  hidden_devices: MapDevice[]
  hidden_links: MapLink[]
  candidates: MapDevice[]
  generated_at: string
}

/** One link's traffic, from its rate end's device: in arrives there, out leaves. */
export interface LinkRate {
  in_bps: number | null
  out_bps: number | null
  oper_up: boolean | null
  at: string | null
}

export interface MapTraffic {
  site_id: string
  at: string
  /** By link key, a bundle's members each by their own, and routers' WAN ports by wanKey. */
  links: Record<string, LinkRate>
  /** Status of each device the fast poll looked at. */
  devices: Record<string, string>
}

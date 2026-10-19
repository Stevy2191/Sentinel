import { Camera, Monitor, Network, Phone, Printer, Wifi, type LucideIcon } from 'lucide-react'
import type { EndpointKind } from '@/hooks/useEndpoints'

/** Each kind's icon; Network is a "behind" group (an unmanaged switch). */
export const KIND_ICON: Record<EndpointKind | 'behind', LucideIcon> = {
  camera: Camera,
  phone: Phone,
  printer: Printer,
  ap: Wifi,
  device: Monitor,
  behind: Network,
}

export const KIND_LABEL: Record<EndpointKind, string> = {
  camera: 'Camera',
  phone: 'Phone',
  printer: 'Printer',
  ap: 'Access point',
  device: 'Computer or device',
}

/** Literal Tailwind classes per kind (see utils/colors.ts on why literal):
 *  the map node's fill and outline, and the icon's colour. */
export const KIND_TONE: Record<EndpointKind | 'behind', { node: string; icon: string }> = {
  camera: { node: 'fill-amber-500/15 stroke-amber-400/70', icon: 'text-amber-300' },
  phone: { node: 'fill-emerald-500/15 stroke-emerald-400/70', icon: 'text-emerald-300' },
  printer: { node: 'fill-violet-500/15 stroke-violet-400/70', icon: 'text-violet-300' },
  ap: { node: 'fill-sky-500/15 stroke-sky-400/70', icon: 'text-sky-300' },
  device: { node: 'fill-slate-500/15 stroke-slate-400/60', icon: 'text-slate-300' },
  behind: { node: 'fill-slate-500/10 stroke-slate-400/60', icon: 'text-slate-300' },
}

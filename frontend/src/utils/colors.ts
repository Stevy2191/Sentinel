// Colour definitions for the whole app. Every accent in the redesign comes from
// here so a hue is changed in one place rather than hunted through JSX.
//
// These are LITERAL Tailwind class strings on purpose. Tailwind only emits CSS
// for class names it finds spelled out in the source, so a template like
// `border-${key}-500/30` compiles to nothing at all. Anything that needs to be
// picked at runtime maps a key to a whole literal string, never to a fragment.
export const colors = {
  // Main status colours
  operational: {
    gradient: 'from-emerald-600 to-emerald-500',
    text: 'text-emerald-400',
    bg: 'bg-emerald-500/10',
    border: 'border-emerald-500/30',
  },
  warning: {
    gradient: 'from-yellow-600 to-yellow-500',
    text: 'text-yellow-400',
    bg: 'bg-yellow-500/10',
    border: 'border-yellow-500/30',
  },
  error: {
    gradient: 'from-red-600 to-red-500',
    text: 'text-red-400',
    bg: 'bg-red-500/10',
    border: 'border-red-500/30',
  },

  // Stat card colours
  responseTime: {
    gradient: 'from-blue-600 to-blue-500',
    text: 'text-blue-400',
    bg: 'bg-blue-500/10',
    border: 'border-blue-500/30',
  },
  incidents: {
    gradient: 'from-red-600 to-red-500',
    text: 'text-red-400',
    bg: 'bg-red-500/10',
    border: 'border-red-500/30',
  },
  agents: {
    gradient: 'from-amber-600 to-amber-500',
    text: 'text-amber-400',
    bg: 'bg-amber-500/10',
    border: 'border-amber-500/30',
  },

  // Monitor type colours
  dns: {
    gradient: 'from-purple-600 to-purple-500',
    text: 'text-purple-400',
    bg: 'bg-purple-500/10',
    border: 'border-purple-500/20',
  },
  http: {
    gradient: 'from-cyan-600 to-cyan-500',
    text: 'text-cyan-400',
    bg: 'bg-cyan-500/10',
    border: 'border-cyan-500/20',
  },
  ping: {
    gradient: 'from-yellow-600 to-yellow-500',
    text: 'text-yellow-400',
    bg: 'bg-yellow-500/10',
    border: 'border-yellow-500/20',
  },
  tcp: {
    gradient: 'from-orange-600 to-orange-500',
    text: 'text-orange-400',
    bg: 'bg-orange-500/10',
    border: 'border-orange-500/20',
  },
}

export type ColorKey = keyof typeof colors

/** Maps a monitor's type to its accent. Unknown types fall back to the HTTP cyan. */
export const getMonitorTypeColor = (type: string) => {
  switch (type?.toLowerCase()) {
    case 'dns':
      return colors.dns
    case 'http':
      return colors.http
    case 'ping':
      return colors.ping
    case 'tcp':
      return colors.tcp
    default:
      return colors.http
  }
}

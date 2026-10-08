import type { CSSProperties } from 'react'
import { useSites } from '@/hooks/useSites'
import { siteChoices } from '@/utils/siteChoices'

interface Props {
  id?: string
  /** A site id, or '' for no site. */
  value: string
  onChange: (siteId: string) => void
  /** The current site's name, offered when it is not among the user's sites. */
  currentName?: string | null
  className?: string
  style?: CSSProperties
}

/** A Site picker: "No site", then the sites this user can see. */
export default function SiteSelect({ id, value, onChange, currentName = null, className, style }: Props) {
  const { sites } = useSites()
  const choices = siteChoices(sites, value, currentName)
  return (
    <select id={id} className={className} style={style} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">No site</option>
      {choices.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  )
}

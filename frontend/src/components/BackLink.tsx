import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useBack } from '@/utils/navHistory'

/** A page's back link: to the page the user came from, named after it, or
 *  to fallback when they arrived from outside the app (a bookmark, an email
 *  link). children renders the label so each page keeps its own look. */
export default function BackLink({
  fallbackTo,
  fallbackLabel,
  className,
  children,
}: {
  fallbackTo: string
  fallbackLabel: string
  className?: string
  children: (label: string) => ReactNode
}) {
  const back = useBack({ to: fallbackTo, label: fallbackLabel })
  if (back.href) {
    return (
      <Link to={back.href} className={className}>
        {children(back.label)}
      </Link>
    )
  }
  return (
    <button type="button" onClick={back.go} className={className}>
      {children(back.label)}
    </button>
  )
}

import type { ReactNode } from 'react'

interface Props {
  title: string
  description?: string
  children: ReactNode
}

/** SettingsCard groups a related set of settings controls in a card. */
export default function SettingsCard({ title, description, children }: Props) {
  return (
    <div className="card p-5">
      <div className="mb-4">
        <h3 className="font-semibold">{title}</h3>
        {description && (
          <p className="text-sm text-neutral-500 dark:text-neutral-400">{description}</p>
        )}
      </div>
      <div className="space-y-4">{children}</div>
    </div>
  )
}

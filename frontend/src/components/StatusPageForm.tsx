import { useState } from 'react'
import type { ApiError } from '@/services/api'
import type { StatusPage, StatusPageInput } from '@/types'
import { validateStatusPageSlug } from '@/utils/validators'

export interface StatusPageFormValues {
  slug: string
  name: string
  description: string
  logo_url: string
  theme_color: string
  published: boolean
}

export const emptyStatusPageForm: StatusPageFormValues = {
  slug: '',
  name: '',
  description: '',
  logo_url: '',
  theme_color: '#10b981',
  published: true,
}

export function statusPageToForm(p: StatusPage): StatusPageFormValues {
  return {
    slug: p.slug,
    name: p.name,
    description: p.description,
    logo_url: p.logo_url,
    theme_color: p.theme_color || '#10b981',
    published: p.published,
  }
}

type Errors = Partial<Record<keyof StatusPageFormValues, string>>

const inputCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:opacity-60'

interface Props {
  initialValues?: Partial<StatusPageFormValues>
  onSubmit: (input: StatusPageInput) => Promise<void> | void
  isLoading?: boolean
  error?: ApiError | null
  submitLabel?: string
  slugReadOnly?: boolean
  existingSlugs?: string[]
  onCancel?: () => void
}

export default function StatusPageForm({
  initialValues,
  onSubmit,
  isLoading,
  error,
  submitLabel = 'Save Page',
  slugReadOnly = false,
  existingSlugs = [],
  onCancel,
}: Props) {
  const [values, setValues] = useState<StatusPageFormValues>({
    ...emptyStatusPageForm,
    ...initialValues,
  })
  const [errors, setErrors] = useState<Errors>({})

  const set = <K extends keyof StatusPageFormValues>(key: K, value: StatusPageFormValues[K]) =>
    setValues((v) => ({ ...v, [key]: value }))

  const validate = (): Errors => {
    const e: Errors = {}
    if (!slugReadOnly) {
      if (!validateStatusPageSlug(values.slug))
        e.slug = 'Slug must be 3–50 chars, letters/numbers/hyphens only'
      else if (existingSlugs.includes(values.slug.trim()))
        e.slug = 'That slug is already taken'
    }
    if (values.name.trim().length < 1 || values.name.trim().length > 255)
      e.name = 'Name is required (1–255 chars)'
    if (values.theme_color && !/^#([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$/.test(values.theme_color))
      e.theme_color = 'Must be a hex color like #10b981'
    return e
  }

  const handleSubmit = (ev: React.FormEvent) => {
    ev.preventDefault()
    const errs = validate()
    setErrors(errs)
    if (Object.keys(errs).length > 0) return
    const input: StatusPageInput = {
      slug: values.slug.trim(),
      name: values.name.trim(),
      description: values.description.trim() || undefined,
      logo_url: values.logo_url.trim() || undefined,
      theme_color: values.theme_color || undefined,
      published: values.published,
    }
    void onSubmit(input)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="card space-y-4 p-5">
        <label className="block">
          <span className="mb-1 block text-sm font-medium">
            Slug<span className="ml-0.5 text-error-500">*</span>
          </span>
          <input
            className={inputCls}
            value={values.slug}
            disabled={slugReadOnly}
            onChange={(e) => set('slug', e.target.value)}
            placeholder="acme-corp"
          />
          {errors.slug ? (
            <span className="mt-1 block text-xs text-error-600">{errors.slug}</span>
          ) : (
            <span className="mt-1 block text-xs text-neutral-500">
              {slugReadOnly
                ? 'Slug cannot be changed after creation'
                : 'Public URL: /public/status/<slug>'}
            </span>
          )}
        </label>

        <label className="block">
          <span className="mb-1 block text-sm font-medium">
            Name<span className="ml-0.5 text-error-500">*</span>
          </span>
          <input
            className={inputCls}
            value={values.name}
            onChange={(e) => set('name', e.target.value)}
            placeholder="Acme Corp Status"
          />
          {errors.name && <span className="mt-1 block text-xs text-error-600">{errors.name}</span>}
        </label>

        <label className="block">
          <span className="mb-1 block text-sm font-medium">Description</span>
          <textarea
            className={inputCls}
            rows={3}
            value={values.description}
            onChange={(e) => set('description', e.target.value)}
            placeholder="System status for Acme services"
          />
        </label>
      </div>

      <div className="card space-y-4 p-5">
        <h3 className="font-semibold">Branding</h3>
        <label className="block">
          <span className="mb-1 block text-sm font-medium">Logo URL</span>
          <input
            className={inputCls}
            value={values.logo_url}
            onChange={(e) => set('logo_url', e.target.value)}
            placeholder="https://example.com/logo.png"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-sm font-medium">Theme Color</span>
          <div className="flex items-center gap-3">
            <input
              type="color"
              value={values.theme_color}
              onChange={(e) => set('theme_color', e.target.value)}
              className="h-9 w-14 cursor-pointer rounded-md border border-neutral-300 dark:border-neutral-700"
            />
            <input
              className={`${inputCls} max-w-[140px] font-mono`}
              value={values.theme_color}
              onChange={(e) => set('theme_color', e.target.value)}
            />
          </div>
          {errors.theme_color && (
            <span className="mt-1 block text-xs text-error-600">{errors.theme_color}</span>
          )}
        </label>

        <div>
          <span className="mb-1 block text-sm font-medium">Visibility</span>
          <div className="flex gap-2">
            {[
              { v: true, label: 'Published' },
              { v: false, label: 'Draft' },
            ].map((o) => (
              <button
                key={o.label}
                type="button"
                onClick={() => set('published', o.v)}
                className={`rounded-md px-4 py-2 text-sm font-medium ${
                  values.published === o.v
                    ? 'bg-primary-600 text-white'
                    : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
                }`}
              >
                {o.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {error && (
        <div className="card border-error-300 p-4 text-sm text-error-700 dark:text-error-300">
          {error.message}
        </div>
      )}

      <div className="flex gap-2">
        <button type="submit" className="btn-primary" disabled={isLoading}>
          {isLoading ? 'Saving…' : submitLabel}
        </button>
        {onCancel && (
          <button type="button" className="btn-secondary" onClick={onCancel}>
            Cancel
          </button>
        )}
      </div>
    </form>
  )
}

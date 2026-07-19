import { RotateCcw } from 'lucide-react'

interface Props {
  label: string
  value: string
  defaultValue: string
  onChange: (hex: string) => void
}

const isHex = (s: string) => /^#([0-9a-fA-F]{6})$/.test(s)

export default function ColorPicker({ label, value, defaultValue, onChange }: Props) {
  const valid = isHex(value)
  return (
    <div>
      <span className="mb-1 block text-sm font-medium">{label}</span>
      <div className="flex items-center gap-3">
        <input
          type="color"
          value={valid ? value : defaultValue}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 w-14 cursor-pointer rounded-md border border-neutral-300 dark:border-neutral-700"
          aria-label={`${label} picker`}
        />
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={`w-28 rounded-md border px-3 py-2 font-mono text-sm dark:bg-neutral-800 ${
            valid
              ? 'border-neutral-300 dark:border-neutral-700'
              : 'border-error-400 text-error-600'
          }`}
        />
        <span
          className="h-8 w-8 rounded-md border border-neutral-300 dark:border-neutral-700"
          style={{ backgroundColor: valid ? value : defaultValue }}
        />
        <button
          type="button"
          className="btn-secondary !py-1.5"
          onClick={() => onChange(defaultValue)}
          title="Reset to default"
        >
          <RotateCcw className="h-4 w-4" /> Reset
        </button>
      </div>
      {!valid && <span className="mt-1 block text-xs text-error-600">Enter a valid hex color (e.g. #10b981)</span>}
    </div>
  )
}

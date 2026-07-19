import { useTheme, type ThemeMode } from '@/context/ThemeContext'

export default function Settings() {
  const { mode, setMode } = useTheme()
  const modes: ThemeMode[] = ['light', 'dark', 'auto']

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Customize appearance and branding
        </p>
      </div>

      <div className="card space-y-4 p-5">
        <h2 className="font-semibold">Appearance</h2>

        <div>
          <div className="mb-2 text-sm text-neutral-500">Theme</div>
          <div className="flex gap-2">
            {modes.map((m) => (
              <button
                key={m}
                onClick={() => setMode(m)}
                className={`rounded-md px-4 py-2 text-sm font-medium capitalize ${
                  mode === m
                    ? 'bg-primary-600 text-white'
                    : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
                }`}
              >
                {m}
              </button>
            ))}
          </div>
        </div>

        <div>
          <div className="mb-2 text-sm text-neutral-500">Font size</div>
          <select
            disabled
            className="rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800"
          >
            <option>Compact</option>
            <option>Normal</option>
            <option>Large</option>
          </select>
        </div>
      </div>

      <div className="card space-y-4 p-5">
        <h2 className="font-semibold">Branding</h2>
        <div>
          <div className="mb-2 text-sm text-neutral-500">Logo</div>
          <input type="file" disabled className="text-sm" />
        </div>
        <div className="flex gap-4">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">Primary color</span>
            <input type="color" defaultValue="#10b981" disabled className="h-9 w-16" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">Accent color</span>
            <input type="color" defaultValue="#3b82f6" disabled className="h-9 w-16" />
          </label>
        </div>
      </div>

      <button className="btn-primary" disabled>
        Save Changes
      </button>
    </div>
  )
}

import { useSites } from '@/hooks/useSites'
import { inputCls } from '@/utils/dashboards'

export default function SitePicker({ value, onChange, allowNone = false }: { value: string | null; onChange: (id: string | null) => void; allowNone?: boolean }) {
  const { sites } = useSites()
  return (
    <select className={inputCls} value={value ?? ''} onChange={(e) => onChange(e.target.value || null)}>
      <option value="">{allowNone ? 'Any site' : 'Choose a site…'}</option>
      {sites.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  )
}

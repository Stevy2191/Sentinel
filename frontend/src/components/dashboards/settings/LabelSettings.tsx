import { inputCls, str } from '@/utils/dashboards'

interface Props {
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}

export default function LabelSettings({ config, onChange }: Props) {
  return (
    <div className="space-y-3">
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Text</span>
        <textarea className={inputCls} rows={2} maxLength={200} value={str(config, 'text')} onChange={(e) => onChange({ ...config, text: e.target.value })} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Size</span>
        <select className={inputCls} value={str(config, 'size') || 'm'} onChange={(e) => onChange({ ...config, size: e.target.value })}>
          <option value="s">Small</option>
          <option value="m">Medium</option>
          <option value="l">Large</option>
        </select>
      </label>
    </div>
  )
}

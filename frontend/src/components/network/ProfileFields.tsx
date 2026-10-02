import type { ProfileFieldsValue } from '@/utils/metrics'

interface Props {
  value: ProfileFieldsValue
  onChange: (value: ProfileFieldsValue) => void
}

/** Name / Description / Match prefixes / Poll interval: the profile fields
 *  shared by the "New profile" inline form (Profiles.tsx) and the profile
 *  detail page's editable header (ProfileDetail.tsx). Purely controlled - no
 *  submit button, no API call - so each caller keeps its own save flow. */
export default function ProfileFields({ value, onChange }: Props) {
  const set = <K extends keyof ProfileFieldsValue>(k: K, v: ProfileFieldsValue[K]) => onChange({ ...value, [k]: v })
  return (
    <>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Name</span>
        <input className="rd-input w-full" value={value.name} onChange={(e) => set('name', e.target.value)} required />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Description</span>
        <textarea className="rd-input w-full" rows={2} value={value.description} onChange={(e) => set('description', e.target.value)} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Match prefixes (one per line)</span>
        <textarea
          className="rd-input w-full font-mono text-xs"
          rows={3}
          value={value.prefixes}
          onChange={(e) => set('prefixes', e.target.value)}
          placeholder="1.3.6.1.4.1.9.1"
        />
        <span className="block text-xs text-slate-500">Devices whose sysObjectID starts with one of these arcs get this profile.</span>
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Poll every</span>
        <select className="rd-select" value={value.pollMinutes} onChange={(e) => set('pollMinutes', Number(e.target.value))}>
          <option value={1}>1 minute</option>
          <option value={5}>5 minutes</option>
          <option value={15}>15 minutes</option>
        </select>
      </label>
    </>
  )
}

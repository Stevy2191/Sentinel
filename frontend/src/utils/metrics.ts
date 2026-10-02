// Pure helpers for metric profiles and the metric editor (no React, no network).
import type { ProfileMetric } from '@/hooks/useProfiles'
import type { MibObjectDetail } from '@/hooks/useMibs'

/** A new, unsaved metric draft: a scalar gauge with no rule, ready for the
 *  editor's fields to fill in. */
export function emptyMetric(): ProfileMetric {
  return {
    id: '',
    profile_id: '',
    name: '',
    key: '',
    source: 'scalar',
    kind: 'gauge',
    units: '',
    scale: 1,
    oid: '',
    oid2: '',
    precision_oid: '',
    filter_oid: '',
    filter_values: [],
    label_mode: 'index',
    label_oid: '',
    label_pointer_oid: '',
    label_target_oid: '',
    ok_states: [],
    state_names: null,
    rule_kind: '',
    rule_value: null,
    rule_hold_minutes: 0,
    rule_enabled: false,
    position: 0,
    created_at: '',
    updated_at: '',
  }
}

/** Seeds a metric draft from a MIB object (the "Make a metric from this"
 *  link in the MIB browser's test-walk panel): a table column picks the
 *  column source, anything else the scalar source; an object with named
 *  values becomes a status metric with those as its state names; a counter
 *  base type becomes a counter. Units carry over either way. */
export function metricFromMibObject(o: MibObjectDetail): Partial<ProfileMetric> {
  const partial: Partial<ProfileMetric> = {
    name: o.name,
    oid: o.oid,
    source: o.kind === 'column' ? 'column' : 'scalar',
    units: o.units,
  }
  if (o.enum && Object.keys(o.enum).length > 0) {
    partial.kind = 'status'
    partial.state_names = o.enum
  } else if (o.base_type === 'Counter32' || o.base_type === 'Counter64') {
    partial.kind = 'counter'
  } else {
    partial.kind = 'gauge'
  }
  return partial
}

/** Suggests a metric key from its name while the person hasn't typed one of
 *  their own: lowercase, runs of non-alphanumerics collapsed to a single
 *  underscore, leading non-letters dropped. The server has the final say -
 *  the exact key rule, the reserved if_/ups_ prefixes, uniqueness - this is
 *  only a starting point the person can still edit before saving. */
export function suggestKey(name: string): string {
  const key = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, 63)
  return key.replace(/^[^a-z]+/, '')
}

/** "above 90 % for 10 min", "below 5 for 5 min", "not OK", or "—" when the
 *  metric has no rule. Reflects the rule itself; whether it is enabled is a
 *  separate, visible toggle in the editor rather than folded into this text. */
export function ruleText(m: ProfileMetric): string {
  switch (m.rule_kind) {
    case 'above':
    case 'below':
      if (m.rule_value == null) return '—'
      return `${m.rule_kind} ${m.units ? `${m.rule_value} ${m.units}` : m.rule_value} for ${m.rule_hold_minutes} min`
    case 'not_ok':
      return 'not OK'
    default:
      return '—'
  }
}

/** A stable JSON string of a metric draft (object keys sorted, recursively),
 *  used to tell whether the draft has changed since it was last previewed:
 *  the editor's Save button stays disabled until this matches the hash taken
 *  at the moment a preview last came back with at least one row. */
export function draftHash(m: ProfileMetric): string {
  return stableStringify(m)
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value)
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`
  const obj = value as Record<string, unknown>
  const keys = Object.keys(obj).sort()
  return `{${keys.map((k) => `${JSON.stringify(k)}:${stableStringify(obj[k])}`).join(',')}}`
}

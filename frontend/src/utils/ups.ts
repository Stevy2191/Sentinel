export const BATTERY_STATUS: Record<number, string> = { 1: 'Battery status unknown', 3: 'Battery low', 4: 'Battery depleted' }

/** A UPS's readings as tiles, in display order; missing readings are left out. */
export function upsTiles(r: Record<string, number>): { key: string; label: string; value: string }[] {
  const out: { key: string; label: string; value: string }[] = []
  const add = (key: string, label: string, fmt: (n: number) => string) => {
    if (r[key] !== undefined) out.push({ key, label, value: fmt(r[key]) })
  }
  add('ups_charge_pct', 'Charge', (n) => `${Math.round(n)}%`)
  add('ups_runtime_min', 'Runtime left', (n) => `${Math.round(n)} min`)
  add('ups_load_pct', 'Load', (n) => `${Math.round(n)}%`)
  add('ups_input_v', 'Input', (n) => `${Math.round(n)} V`)
  add('ups_output_v', 'Output', (n) => `${Math.round(n)} V`)
  add('ups_battery_temp_c', 'Battery temperature', (n) => `${Math.round(n)} °C`)
  return out
}

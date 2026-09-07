/**
 * A small RFC 4180-style CSV reader, enough for monitor imports.
 *
 * Hand-rolled rather than pulled from npm because the requirement is narrow and
 * well defined: quoted fields (so `"production,critical"` stays one value),
 * doubled quotes as an escape, and CRLF or LF line endings. Anything more
 * exotic belongs in a spreadsheet, not here.
 */

/** Split CSV text into rows of raw string cells. Blank lines are dropped. */
export function parseCsv(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cell = ''
  let inQuotes = false

  // Strip a UTF-8 BOM, which spreadsheet exports routinely prepend and which
  // would otherwise become part of the first header name.
  const src = text.replace(/^\uFEFF/, '')

  const endCell = () => {
    row.push(cell)
    cell = ''
  }
  const endRow = () => {
    endCell()
    if (row.some((c) => c.trim() !== '')) rows.push(row)
    row = []
  }

  for (let i = 0; i < src.length; i++) {
    const ch = src[i]

    if (inQuotes) {
      if (ch === '"') {
        if (src[i + 1] === '"') {
          cell += '"'
          i++
        } else {
          inQuotes = false
        }
      } else {
        cell += ch
      }
      continue
    }

    switch (ch) {
      case '"':
        inQuotes = true
        break
      case ',':
        endCell()
        break
      case '\r':
        break // handled by the \n that follows
      case '\n':
        endRow()
        break
      default:
        cell += ch
    }
  }
  // Whatever is left after the last newline is a final row.
  if (cell !== '' || row.length > 0) endRow()

  return rows
}

/**
 * Parse CSV with a header row into objects keyed by lower-cased header name.
 * Returns an error message instead of throwing when the text isn't usable.
 */
export function parseCsvRecords(text: string): { records: Record<string, string>[]; error?: string } {
  const rows = parseCsv(text)
  if (rows.length === 0) return { records: [], error: 'The file is empty.' }

  const headers = rows[0].map((h) => h.trim().toLowerCase())
  if (rows.length === 1) return { records: [], error: 'The file has a header row but no data rows.' }

  const records = rows.slice(1).map((cells) => {
    const rec: Record<string, string> = {}
    headers.forEach((h, i) => {
      rec[h] = (cells[i] ?? '').trim()
    })
    return rec
  })
  return { records }
}

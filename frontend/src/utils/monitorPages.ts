// Reading a paginated list in full. Free of `@/` imports so it can be checked
// alone.

/** One page of a list, as GET /monitors returns it. */
export interface ListPage<T> {
  monitors: T[]
  pagination: { pages?: number }
}

/** fetchAllPages reads page 1, 2, ... up to the last page the server reports,
 *  so a list longer than one page is never cut short. A missing page count
 *  means one page; maxPages bounds it whatever the server says, so a wrong
 *  count cannot loop forever. The server pages by offset, so a row created or
 *  deleted mid-read can shift a boundary and repeat a row; with keyOf the
 *  first occurrence of each key is kept. */
export async function fetchAllPages<T>(
  getPage: (page: number) => Promise<ListPage<T>>,
  maxPages = 100,
  keyOf?: (item: T) => string
): Promise<T[]> {
  const all: T[] = []
  const seen = new Set<string>()
  for (let page = 1; page <= maxPages; page++) {
    const res = await getPage(page)
    for (const item of res.monitors) {
      if (keyOf) {
        const k = keyOf(item)
        if (seen.has(k)) continue
        seen.add(k)
      }
      all.push(item)
    }
    if (res.monitors.length === 0 || page >= (res.pagination.pages ?? 1)) break
  }
  return all
}

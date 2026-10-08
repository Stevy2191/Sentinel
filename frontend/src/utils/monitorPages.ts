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
 *  count cannot loop forever. */
export async function fetchAllPages<T>(
  getPage: (page: number) => Promise<ListPage<T>>,
  maxPages = 100
): Promise<T[]> {
  const all: T[] = []
  for (let page = 1; page <= maxPages; page++) {
    const res = await getPage(page)
    all.push(...res.monitors)
    if (res.monitors.length === 0 || page >= (res.pagination.pages ?? 1)) break
  }
  return all
}

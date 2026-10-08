// What a Site select offers. Free of `@/` imports so it can be checked alone.

export interface SiteChoice {
  id: string
  name: string
}

/** siteChoices is the user's sites A-Z, plus the current one when it is not
 *  among them (a monitor shared by someone whose site is not shared with
 *  you), so opening a form never silently drops a site. */
export function siteChoices(sites: SiteChoice[], currentId: string, currentName: string | null): SiteChoice[] {
  const list = [...sites].sort((a, b) => a.name.localeCompare(b.name))
  if (currentId && !list.some((s) => s.id === currentId)) {
    list.push({ id: currentId, name: currentName ?? 'Current site' })
  }
  return list
}

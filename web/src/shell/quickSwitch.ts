export interface SwitchItem { id:string; title:string; kind:string; detail:string; href:string }
export function searchItems(items: SwitchItem[], query: string, recent: string[]): SwitchItem[] {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  const rank = (item: SwitchItem) => {
    const index = recent.indexOf(item.href)
    return index < 0 ? Number.MAX_SAFE_INTEGER : index
  }
  return items.filter(item => words.every(word => `${item.title} ${item.kind} ${item.detail}`.toLocaleLowerCase().includes(word)))
    .sort((a,b) => rank(a)-rank(b) || a.title.localeCompare(b.title)).slice(0,60)
}
export function switchHref(path: string, search: string): string | undefined {
  if (path === '/packs') { const folder = new URLSearchParams(search).get('folder'); return folder ? `/packs?folder=${encodeURIComponent(folder)}` : undefined }
  return /^\/(packs\/(drafts\/)?[^/]+|graphs\/[^/]+|chats\/[^/]+)(\/evaluate|\/matrix)?$/.test(path) && path !== '/packs/new'
    ? path.replace(/\/(evaluate|matrix)$/, '') : undefined
}
export function readRecent(key: string): string[] {
  try { const value: unknown = JSON.parse(localStorage.getItem(key) ?? '[]'); return Array.isArray(value) ? value.filter((x):x is string => typeof x === 'string' && x.startsWith('/')).slice(0,20) : [] } catch { return [] }
}

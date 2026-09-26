import { useSyncExternalStore } from 'react'

export const CONNECTION_PREFERENCES_KEY = 'jpack-desk.connection-shortcuts.v1'
export const SHORTCUT_LIMIT = 5
const changed = 'desk-connection-shortcuts'
const validId = (value: unknown): value is string => typeof value === 'string' && /^[a-z][a-z0-9-]{0,47}$/.test(value)
export interface ConnectionPreferences { pinned: string[]; recent: string[] }
const empty: ConnectionPreferences = { pinned: [], recent: [] }
let snapshot = empty, lastRaw: string | null | undefined, volatile = false
function ids(value: unknown, limit: number): string[] { return Array.isArray(value) ? [...new Set(value.filter(validId))].slice(0, limit) : [] }
function read() {
 if (volatile) return snapshot
 try {
  const raw = localStorage.getItem(CONNECTION_PREFERENCES_KEY)
  if (raw !== lastRaw) {
   lastRaw = raw; snapshot = empty
   if (raw && raw.length <= 8192) {
    try { const value = JSON.parse(raw); if (value && typeof value === 'object') snapshot = { pinned: ids(value.pinned, SHORTCUT_LIMIT), recent: ids(value.recent, 20) } } catch { /* Invalid preferences do not affect available connections. */ }
   }
  }
 } catch { /* Unavailable browser storage retains this session's preferences. */ }
 return snapshot
}
function subscribe(listener: () => void) {
 const storage = (event: StorageEvent) => { if (event.key === CONNECTION_PREFERENCES_KEY || event.key === null) { volatile = false; lastRaw = undefined; listener() } }
 window.addEventListener('storage', storage); window.addEventListener(changed, listener)
 return () => { window.removeEventListener('storage', storage); window.removeEventListener(changed, listener) }
}
function save(next: ConnectionPreferences) {
 snapshot = next; lastRaw = JSON.stringify(next)
 try { localStorage.setItem(CONNECTION_PREFERENCES_KEY, lastRaw); volatile = false } catch { volatile = true }
 window.dispatchEvent(new Event(changed))
}
export function useConnectionPreferences() { return useSyncExternalStore(subscribe, read, () => empty) }
export function pinConnection(provider: string, pinned: boolean, advertised: string[]) {
 if (!validId(provider) || !advertised.includes(provider)) return
 const current = read(), next = current.pinned.filter(id => advertised.includes(id) && id !== provider)
 if (pinned && next.length >= SHORTCUT_LIMIT) return
 save({ ...current, pinned: pinned ? [...next, provider] : next })
}
export function rememberConnection(provider: string) {
 if (!validId(provider)) return
 const current = read(); save({ ...current, recent: [provider, ...current.recent.filter(id => id !== provider)].slice(0, 20) })
}
/** Callers supply only currently connected services. A preference never grants access. */
export function connectionShortcuts<T extends { provider: string }>(available: T[], preferences: ConnectionPreferences): T[] {
 const order = [...preferences.pinned, ...preferences.recent, ...available.map(item => item.provider)]
 const seen = new Set<string>(), byId = new Map(available.map(item => [item.provider, item]))
 return order.flatMap(id => { const item = byId.get(id); if (!item || seen.has(id)) return []; seen.add(id); return [item] }).slice(0, SHORTCUT_LIMIT)
}

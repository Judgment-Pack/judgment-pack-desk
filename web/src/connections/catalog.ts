import { useQueries, useQuery } from '@tanstack/react-query'
import { language } from '../i18n'
import { answer, deskFetch } from '../files/client'
import { CONNECTIONS_KEY, connectionStatusOptions, type ConnectionProvider } from './client'

export interface ConnectionDescriptor {
 id: ConnectionProvider
 auth: 'oauth' | 'local-folder' | 'credentials'
 registration: 'google-desktop' | 'automatic' | 'none' | 'form'
 selection: 'browser-picker' | 'mail-search' | 'source-search'
 queryRequired: boolean
 operations: string[]
 queryMode?: 'text' | 'prefix'
 protocol?: string
 presentation?: { icon: string; name: string; description: Localized; instructions: Localized }
 setup?: SetupField[]
 authorizationEndpoints?: string[]
 source?: { id: string; shape: 'command' | 'http' | 'mcp'; record: 'drive-v1' | 'mail-v1' | 'note-v1' | 'resource-v1' }
}
export type Localized = Record<string, string>
export interface SetupField { key: string; type: 'text' | 'password'; label: Localized; required: boolean }
export function localized(value: Localized | undefined): string { return value?.[language()] ?? value?.en ?? '' }
const identifier = (value: unknown): value is string => typeof value === 'string' && /^[a-z][a-z0-9-]{0,47}$/.test(value)
const text = (v: unknown, max: number): v is string => typeof v === 'string' && new TextEncoder().encode(v).length <= max && !/[\x00-\x08\x0b-\x1f\x7f]/.test(v)
const localizedValid = (v: unknown, max: number) => Boolean(v && typeof v === 'object' && !Array.isArray(v) && typeof (v as Localized).en === 'string' && Object.keys(v).length <= 32 && Object.entries(v).every(([key,value]) => key.length <= 32 && text(value, max)))
function validPresentation(item: ConnectionDescriptor) {
 return identifier(item.queryMode) && identifier(item.protocol) && item.presentation && validConnectionIcon(item.presentation.icon) && text(item.presentation.name,120) && item.presentation.name.length > 0 && localizedValid(item.presentation.description,512) && localizedValid(item.presentation.instructions,4096) && Array.isArray(item.setup) && item.setup.length <= 12 && new Set(item.setup.map(field => field.key)).size === item.setup.length && item.setup.every(field => field && identifier(field.key) && !['constructor','prototype'].includes(field.key) && identifier(field.type) && localizedValid(field.label,120) && typeof field.required === 'boolean') && Array.isArray(item.authorizationEndpoints) && item.authorizationEndpoints.length <= 4 && item.authorizationEndpoints.every(endpoint => {
  try { const u = new URL(endpoint); return endpoint.length <= 2048 && u.protocol === 'https:' && !u.username && !u.password && !u.hash && !u.search && !/[\\\x00-\x20]/.test(endpoint) } catch { return false }
 }) && item.source && identifier(item.source.id) && identifier(item.source.shape) && identifier(item.source.record)
}
function supported(item: ConnectionDescriptor) {
 if (!['text','prefix'].includes(item.queryMode ?? '') || item.protocol !== 'connection-v1' || !item.setup?.every(field => ['text','password'].includes(field.type))) return false
 if (!['oauth','local-folder','credentials'].includes(item.auth) || !['google-desktop','automatic','none','form'].includes(item.registration)) return false
 if (item.auth === 'oauth' && !item.authorizationEndpoints?.length) return false
 if (!['command','http','mcp'].includes(item.source!.shape)) return false
 if (item.registration === 'google-desktop' && !['google-drive','gmail'].includes(item.id)) return false
 if (item.source!.record === 'resource-v1') return !Object.hasOwn(handlers,item.id) && item.source!.id === item.id && item.selection === 'source-search' && ['status','search','select','disconnect'].every(op => item.operations.includes(op)) && (item.auth === 'oauth' ? ['automatic','none','form'].includes(item.registration) && ['connect','poll','cancel'].every(op => item.operations.includes(op)) : item.registration === 'form') && (item.registration !== 'form' || item.operations.includes('configure'))
 // Legacy source contracts keep their stronger provider-specific checks.
 const legacy = Object.hasOwn(handlers,item.id) ? handlers[item.id as keyof typeof handlers] : undefined
 return Boolean(legacy && item.auth === legacy.auth && (item.registration === legacy.registration || item.id === 'obsidian' && item.registration === 'form') && item.selection === legacy.selection && legacy.operations.every(op => item.operations.includes(op)) && item.source!.record === ({'google-drive':'drive-v1',gmail:'mail-v1',notion:'note-v1',obsidian:'note-v1'} as Record<string,string>)[item.id] && item.source!.id === (item.id === 'google-drive' ? 'drive' : item.id) && item.source!.shape === (item.id === 'notion' ? 'mcp' : item.id === 'obsidian' ? 'command' : 'http'))
}

// Local handler contracts, not an availability list. Unknown protocols never
// choose an endpoint, a component, or an operation for the browser to execute.
const handlers = {
 'google-drive': { auth: 'oauth', registration: 'google-desktop', selection: 'browser-picker', operations: ['status', 'configure', 'connect', 'pick', 'poll', 'cancel', 'disconnect'] },
 gmail: { auth: 'oauth', registration: 'google-desktop', selection: 'mail-search', operations: ['status', 'configure', 'connect', 'poll', 'cancel', 'disconnect', 'search', 'select'] },
 notion: { auth: 'oauth', registration: 'automatic', selection: 'source-search', operations: ['status', 'connect', 'poll', 'cancel', 'disconnect', 'search', 'select'] },
 obsidian: { auth: 'local-folder', registration: 'none', selection: 'source-search', operations: ['status', 'configure', 'disconnect', 'search', 'select'] },
} as const

export function parseConnectionCatalog(raw: unknown) {
 const value = raw as { version?: unknown; providers?: unknown; sources?: unknown }
 if (!value || (value.version !== 2 && value.version !== 3) || !Array.isArray(value.sources) || !Array.isArray(value.providers) || value.providers.length > 32) throw new Error('Invalid connection catalog')
 const seen = new Set<string>(), providers: ConnectionDescriptor[] = [], unsupported: ConnectionDescriptor[] = []
 for (const item of value.providers) {
  if (!item || typeof item.id !== 'string' || !/^[a-z][a-z0-9-]{0,47}$/.test(item.id) || seen.has(item.id) || typeof item.queryRequired !== 'boolean' || !Array.isArray(item.operations) || item.operations.length > 16 || !item.operations.every((op: unknown) => typeof op === 'string' && /^[a-z][a-z0-9-]{0,47}$/.test(op)) || new Set(item.operations).size !== item.operations.length) throw new Error('Invalid connection catalog')
  seen.add(item.id)
  if (value.version === 3) {
   if (!validPresentation(item)) throw new Error('Invalid connection catalog')
   if (supported(item)) providers.push(item); else unsupported.push(item)
   continue
  }
  if (!Object.hasOwn(handlers, item.id)) continue
  const handler = handlers[item.id as keyof typeof handlers]
  if (item.auth !== handler.auth || item.registration !== handler.registration || item.selection !== handler.selection || !handler.operations.every(op => item.operations.includes(op))) continue
  providers.push({ id: item.id, auth: item.auth, registration: item.registration, selection: item.selection, queryRequired: item.queryRequired, operations: item.operations })
 }
 const sources = value.sources as { id?: unknown; input?: unknown; mediaTypes?: unknown; maxBytes?: unknown }[]
 if (sources.length > 32 || new Set(sources.map(s => s?.id)).size !== sources.length || sources.some(s => !s || typeof s.id !== 'string' || !/^[a-z][a-z0-9-]{0,47}$/.test(s.id) || typeof s.input !== 'string' || !Array.isArray(s.mediaTypes) || !Number.isSafeInteger(s.maxBytes))) throw new Error('Invalid source catalog')
 const web = sources.some(s => s.id === 'web' && s.input === 'url' && s.maxBytes === 4 << 20 && ['text/html','text/plain','application/pdf'].every(type => (s.mediaTypes as string[]).includes(type)))
 const discovery = sources.some(s=>s.id==='web-discovery' && s.input==='url' && s.maxBytes===1<<20 && (s.mediaTypes as string[]).includes('application/vnd.jpack.web-discovery+json'))
 return { providers, web, discovery, ...(unsupported.length ? { unsupported } : {}) }
}

export function useConnections(enabled: boolean) {
 const catalog = useQuery({
  queryKey: [...CONNECTIONS_KEY, 'catalog'], enabled, retry: false, staleTime: 30_000,
  queryFn: async ({ signal }) => parseConnectionCatalog(await answer<unknown>(await deskFetch('/api/connections/catalog', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', signal }))),
 })
 // A failed refresh must not keep previously advertised actions active.
 const descriptors = enabled && !catalog.isError ? catalog.data?.providers ?? [] : []
 const statuses = useQueries({ queries: descriptors.map(provider => connectionStatusOptions(provider.id, true, provider.source?.record === 'resource-v1')) })
 const entries = descriptors.map((descriptor, index) => ({ descriptor, status: statuses[index]! }))
 return { ...catalog, entries, discovery: Boolean(enabled && !catalog.isError && catalog.data?.discovery), unsupported: enabled && !catalog.isError ? catalog.data?.unsupported ?? [] : [], web: Boolean(enabled && !catalog.isError && catalog.data?.web), loading: enabled && catalog.isPending }
}

export function validConnectionIcon(icon: unknown): icon is string {
 if (icon === '') return true
 if (typeof icon !== 'string' || icon.length > 22000 || !icon.startsWith('data:image/png;base64,')) return false
 try { const value = atob(icon.slice(22)); const data = Uint8Array.from(value,c=>c.charCodeAt(0)); const view = new DataView(data.buffer); return data.length >= 24 && value.slice(0,8) === '\x89PNG\r\n\x1a\n' && value.slice(12,16) === 'IHDR' && view.getUint32(16)>0 && view.getUint32(16)<=128 && view.getUint32(20)>0 && view.getUint32(20)<=128 } catch { return false }
}

import { useQueries, useQuery } from '@tanstack/react-query'
import { answer, deskFetch } from '../files/client'
import { CONNECTIONS_KEY, connectionStatusOptions, type ConnectionProvider } from './client'

export interface ConnectionDescriptor {
 id: ConnectionProvider
 auth: 'oauth' | 'local-folder'
 registration: 'google-desktop' | 'automatic' | 'none'
 selection: 'browser-picker' | 'mail-search' | 'source-search'
 queryRequired: boolean
 operations: string[]
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
 if (!value || value.version !== 2 || !Array.isArray(value.sources) || !Array.isArray(value.providers) || value.providers.length > 32) throw new Error('Invalid connection catalog')
 const seen = new Set<string>(), providers: ConnectionDescriptor[] = []
 for (const item of value.providers) {
  if (!item || typeof item.id !== 'string' || !/^[a-z][a-z0-9-]{0,47}$/.test(item.id) || seen.has(item.id) || typeof item.queryRequired !== 'boolean' || !Array.isArray(item.operations) || item.operations.length > 16 || !item.operations.every((op: unknown) => typeof op === 'string' && /^[a-z][a-z0-9-]{0,47}$/.test(op)) || new Set(item.operations).size !== item.operations.length) throw new Error('Invalid connection catalog')
  seen.add(item.id)
  if (!Object.hasOwn(handlers, item.id)) continue
  const handler = handlers[item.id as ConnectionProvider]
  if (item.auth !== handler.auth || item.registration !== handler.registration || item.selection !== handler.selection || !handler.operations.every(op => item.operations.includes(op))) continue
  providers.push({ id: item.id, auth: item.auth, registration: item.registration, selection: item.selection, queryRequired: item.queryRequired, operations: item.operations })
 }
 const sources = value.sources as { id?: unknown; input?: unknown; mediaTypes?: unknown; maxBytes?: unknown }[]
 if (sources.length > 32 || new Set(sources.map(s => s?.id)).size !== sources.length || sources.some(s => !s || typeof s.id !== 'string' || !/^[a-z][a-z0-9-]{0,47}$/.test(s.id) || typeof s.input !== 'string' || !Array.isArray(s.mediaTypes) || !Number.isSafeInteger(s.maxBytes))) throw new Error('Invalid source catalog')
 const web = sources.some(s => s.id === 'web' && s.input === 'url' && s.maxBytes === 4 << 20 && ['text/html','text/plain','application/pdf'].every(type => (s.mediaTypes as string[]).includes(type)))
 return { providers, web }
}

export function useConnections(enabled: boolean) {
 const catalog = useQuery({
  queryKey: [...CONNECTIONS_KEY, 'catalog'], enabled, retry: false, staleTime: 30_000,
  queryFn: async ({ signal }) => parseConnectionCatalog(await answer<unknown>(await deskFetch('/api/connections/catalog', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', signal }))),
 })
 // A failed refresh must not keep previously advertised actions active.
 const descriptors = enabled && !catalog.isError ? catalog.data?.providers ?? [] : []
 const statuses = useQueries({ queries: descriptors.map(provider => connectionStatusOptions(provider.id)) })
 const entries = descriptors.map((descriptor, index) => ({ descriptor, status: statuses[index]! }))
 return { ...catalog, entries, web: Boolean(enabled && !catalog.isError && catalog.data?.web), loading: enabled && catalog.isPending }
}

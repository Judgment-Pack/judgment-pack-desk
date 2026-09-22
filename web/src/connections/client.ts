import { useQuery } from '@tanstack/react-query'
import { deskFetch, answer } from '../files/client'
import { sourceMessage } from '../i18n/source'
export type SourceProvider = string
export type ConnectionProvider = string
export interface SourceSelection { resourceId: string; grant: string }
export interface SourcePreview { id: string; title: string; url: string; description?: string }
export interface SourceSearch { selectionContext: string; items: SourcePreview[]; more: boolean; nextPageToken?: string }
export interface MailSelection { messageId: string; grant: string }
export interface MailPreview { id: string; subject: string; from: string; date: string }
export interface MailSearch { selectionContext: string; messages: MailPreview[]; nextPageToken?: string }
export interface ConnectionStatus { version: 1; provider: ConnectionProvider; state: 'setup-required'|'not-connected'|'connected'|'blocked'|'unavailable'; account?: { id: string; email: string; name: string }; maxFileBytes: number; maxFiles: number }
export interface DriveSelection { fileId: string; grant: string }
export interface ConnectionFlow { id: string; state: 'pending'|'complete'|'failed'|'canceled'; url?: string; error?: string; selections?: DriveSelection[] }
export const CONNECTIONS_KEY = ['gateway-connections'] as const
export class ConnectionRequestError extends Error {
 constructor(readonly code: string, readonly provider: ConnectionProvider) {
  super(connectionError(code, provider)); this.name = 'ConnectionRequestError'
 }
 get reconnectRequired() { return ['reconnect-required', 'connect-required', 'registration-expired'].includes(this.code) }
}
export function connectionFailure(cause: unknown, provider: ConnectionProvider): ConnectionRequestError {
 if (cause instanceof ConnectionRequestError) return cause
 // Adapter refusals arrive as gateway diagnostics. Recognize only these exact
 // protocol code tokens; never use translated display copy to drive recovery.
 const tokens = cause instanceof Error ? cause.message.split(/[^a-z-]+/) : []
 const code = ['reconnect-required', 'connect-required', 'registration-expired', 'source-incomplete', 'source-changed', 'file-too-large', 'blocked-by-policy'].find(code => tokens.includes(code))
 return new ConnectionRequestError(code ?? 'retrieval-failed', provider)
}
export function connectionError(code: string, provider: ConnectionProvider = 'google-drive'): string {
 if (!['google-drive','gmail','notion','obsidian'].includes(provider)) {
  if (['reconnect-required','connect-required','registration-expired'].includes(code)) return sourceMessage('Reconnect {{provider}} to continue.', {provider})
  if (code === 'blocked-by-policy') return sourceMessage('Managed by your organization')
  if (code === 'canceled') return sourceMessage('Canceled.')
  return sourceMessage('{{provider}} could not complete this request. Try again.', {provider})
 }
 if (provider === 'notion' || provider === 'obsidian') {
  if (code === 'wrong-account') return sourceMessage('Choose the account already connected, or disconnect it first.')
  if (code === 'blocked-by-policy') return sourceMessage('Managed by your organization')
  if (code === 'authorization-in-progress') return sourceMessage('Finish or cancel the current sign-in first.')
  if (code === 'too-many-selections' || code === 'too-many-files') return sourceMessage('Attach up to four files at a time.')
  if (code === 'file-too-large') return sourceMessage('This source exceeds the connection size limit. Choose a smaller source.')
  if (code === 'source-changed') return sourceMessage('This source changed while being read. Search again and reselect it.')
  if (code === 'source-incomplete') return sourceMessage('Notion returned incomplete content. Choose a smaller page or upload an export.')
  if (code === 'registration-expired') return sourceMessage('The Notion registration expired. Reconnect to register again.')
  if (code === 'invalid-vault') return sourceMessage('Choose an existing Obsidian vault folder containing an .obsidian folder.')
  if (code === 'callback-unavailable') return sourceMessage('The sign-in callback port is in use. Close other Desk windows and try again.')
  if (code === 'canceled') return sourceMessage('Canceled.')
  if (code === 'connect-required' || code === 'reconnect-required') return sourceMessage('Reconnect {{provider}} to continue.', { provider: provider === 'notion' ? 'Notion' : 'Obsidian' })
  if (code === 'selection-expired') return sourceMessage('This selection expired. Search again and reselect your sources.')
  if (code === 'unsupported-file') return sourceMessage('This source is not available to read with this connection.')
  return sourceMessage('{{provider}} could not complete this request. Try again.', { provider: provider === 'notion' ? 'Notion' : 'Obsidian' })
 }
 switch (code) {
 case 'setup-required': return sourceMessage('Configure Google registration in Admin → Connections before signing in.')
 case 'wrong-account': return sourceMessage('Choose the Google account already connected, or disconnect it first.')
 case 'reconnect-required': case 'connect-required': if (provider === 'gmail') return sourceMessage('Reconnect Gmail to continue.'); return sourceMessage('Reconnect Google Drive to continue.')
 case 'authorization-in-progress': return sourceMessage('Finish or cancel the current Google sign-in first.')
 case 'selection-expired': return sourceMessage('This selection expired. Search again and reselect your sources.')
 case 'too-many-files': return sourceMessage('Attach up to four files at a time.')
 case 'canceled': return sourceMessage('Canceled.')
 case 'disconnect-first': if (provider === 'gmail') return sourceMessage('Disconnect Gmail before changing its setup.'); return sourceMessage('Disconnect Google Drive before changing its setup.')
 case 'blocked-by-policy': return sourceMessage('Managed by your organization')
 default: if (provider === 'gmail') return sourceMessage('Gmail could not complete this request. Try connecting again.'); return sourceMessage('Google Drive could not complete this request. Try connecting again.')
 }
}
export async function connectionCall<T>(method: string, params: object = {}, signal?: AbortSignal, provider: ConnectionProvider = 'google-drive'): Promise<T> {
 if (!/^[a-z][a-z0-9-]{0,47}$/.test(provider) || !/^[a-z][a-z0-9-]{0,47}$/.test(method)) throw new Error('Invalid connection request')
 const result = await answer<T & { error?: string }>(await deskFetch(`/api/connections/${provider === 'google-drive' ? '' : `${provider}/`}${method}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(params), signal }))
 if (result.error) throw new ConnectionRequestError(result.error, provider)
 return result
}
export function useDriveStatus(enabled = true, provider: ConnectionProvider = 'google-drive') {
 return useQuery(connectionStatusOptions(provider, enabled))
}
export function connectionStatusOptions(provider: ConnectionProvider, enabled = true) {
 return { queryKey: provider === 'google-drive' ? CONNECTIONS_KEY : [...CONNECTIONS_KEY, provider], queryFn: ({ signal }: { signal: AbortSignal }) => connectionCall<ConnectionStatus>('status', {}, signal, provider), enabled, retry: false, staleTime: 30_000 }
}
/** Open synchronously from the user's click. Only a declared authorization
 * endpoint reaches this tab; credentials and the callback are gateway-owned. */
export async function authorizeDrive(mode: 'connect'|'pick', signal: AbortSignal, provider: ConnectionProvider = 'google-drive', authorizationEndpoints?: string[]): Promise<DriveSelection[]> {
 const tab = window.open('about:blank', '_blank')
 if (!tab) throw new Error(sourceMessage('Allow pop-ups to open the sign-in window.'))
 tab.opener = null
 let id: string | undefined, done = false
 try {
  const flow = await connectionCall<ConnectionFlow>(mode, {}, signal, provider); id = flow.id
  const url = new URL(flow.url ?? '')
  if (url.username || url.password || url.protocol !== 'https:' || url.hash || !(authorizationEndpoints ?? [provider === 'notion' ? 'https://mcp.notion.com/authorize' : 'https://accounts.google.com/o/oauth2/v2/auth']).includes(url.origin + url.pathname) || !/^[a-f0-9]{64}$/.test(id)) throw new Error(connectionError('invalid-response', provider))
  signal.throwIfAborted(); tab.location.href = url.href
  const until = Date.now() + 5 * 60_000
  while (Date.now() < until) {
   await new Promise<void>((resolve, reject) => {
    const cancel = () => { clearTimeout(timer); reject(signal.reason) }
    const timer = setTimeout(() => { signal.removeEventListener('abort', cancel); resolve() }, 1000)
    if (signal.aborted) { clearTimeout(timer); reject(signal.reason) } else signal.addEventListener('abort', cancel, { once: true })
   })
   const result = await connectionCall<ConnectionFlow>('poll', { id }, signal, provider)
   if (result.state === 'complete') { done = true; return result.selections ?? [] }
   if (result.state !== 'pending') { done = true; throw new ConnectionRequestError(result.error ?? 'canceled', provider) }
   if (tab.closed) throw new Error(sourceMessage('Canceled.'))
  }
  throw new Error(connectionError('expired', provider))
 } finally {
  tab.close()
  if (id && !done) void connectionCall('cancel', { id }, undefined, provider).catch(() => {})
 }
}

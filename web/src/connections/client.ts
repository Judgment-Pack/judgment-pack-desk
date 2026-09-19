import { useQuery } from '@tanstack/react-query'
import { deskFetch, answer } from '../files/client'
import { sourceMessage } from '../i18n/source'
export type ConnectionProvider = 'google-drive' | 'gmail'
export interface MailSelection { messageId: string; grant: string }
export interface MailPreview { id: string; subject: string; from: string; date: string }
export interface MailSearch { selectionContext: string; messages: MailPreview[]; nextPageToken?: string }
export interface ConnectionStatus { version: 1; provider: ConnectionProvider; state: 'setup-required'|'not-connected'|'connected'|'blocked'|'unavailable'; account?: { id: string; email: string; name: string }; maxFileBytes: number; maxFiles: number }
export interface DriveSelection { fileId: string; grant: string }
export interface ConnectionFlow { id: string; state: 'pending'|'complete'|'failed'|'canceled'; url?: string; error?: string; selections?: DriveSelection[] }
export const CONNECTIONS_KEY = ['gateway-connections'] as const
export function connectionError(code: string, provider: ConnectionProvider = 'google-drive'): string {
 switch (code) {
 case 'setup-required': if (provider === 'gmail') return sourceMessage('Connect Gmail from the attachment menu.'); return sourceMessage('Connect Google Drive from the attachment menu.')
 case 'wrong-account': return sourceMessage('Choose the Google account already connected, or disconnect it first.')
 case 'reconnect-required': case 'connect-required': if (provider === 'gmail') return sourceMessage('Reconnect Gmail to continue.'); return sourceMessage('Reconnect Google Drive to continue.')
 case 'authorization-in-progress': return sourceMessage('Finish or cancel the current Google sign-in first.')
 case 'too-many-files': return sourceMessage('Attach up to four files at a time.')
 case 'canceled': return sourceMessage('Canceled.')
 case 'disconnect-first': if (provider === 'gmail') return sourceMessage('Disconnect Gmail before changing its setup.'); return sourceMessage('Disconnect Google Drive before changing its setup.')
 case 'blocked-by-policy': return sourceMessage('Managed by your organization')
 default: if (provider === 'gmail') return sourceMessage('Gmail could not complete this request. Try connecting again.'); return sourceMessage('Google Drive could not complete this request. Try connecting again.')
 }
}
export async function connectionCall<T>(method: string, params: object = {}, signal?: AbortSignal, provider: ConnectionProvider = 'google-drive'): Promise<T> {
 const result = await answer<T & { error?: string }>(await deskFetch(`/api/connections/${provider === 'gmail' ? 'gmail/' : ''}${method}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(params), signal }))
 if (result.error) throw new Error(connectionError(result.error, provider))
 return result
}
export function useDriveStatus(enabled = true, provider: ConnectionProvider = 'google-drive') {
 return useQuery({ queryKey: provider === 'gmail' ? [...CONNECTIONS_KEY, 'gmail'] : CONNECTIONS_KEY, queryFn: () => connectionCall<ConnectionStatus>('status', {}, undefined, provider), enabled, retry: false, staleTime: 30_000 })
}
/** Open synchronously from the user's click. Only a Google authorization URL
 * reaches this tab; credentials and the callback are gateway-owned. */
export async function authorizeDrive(mode: 'connect'|'pick', signal: AbortSignal, provider: ConnectionProvider = 'google-drive'): Promise<DriveSelection[]> {
 const tab = window.open('about:blank', '_blank')
 if (!tab) throw new Error(sourceMessage('Allow pop-ups to open Google sign-in.'))
 tab.opener = null
 let id: string | undefined, done = false
 try {
  const flow = await connectionCall<ConnectionFlow>(mode, {}, signal, provider); id = flow.id
  const url = new URL(flow.url ?? '')
  if (url.origin !== 'https://accounts.google.com' || url.pathname !== '/o/oauth2/v2/auth' || !/^[a-f0-9]{64}$/.test(id)) throw new Error(connectionError('invalid-response', provider))
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
   if (result.state !== 'pending') { done = true; throw new Error(connectionError(result.error ?? 'canceled', provider)) }
   if (tab.closed) throw new Error(sourceMessage('Canceled.'))
  }
  throw new Error(connectionError('expired', provider))
 } finally {
  tab.close()
  if (id && !done) void connectionCall('cancel', { id }, undefined, provider).catch(() => {})
 }
}

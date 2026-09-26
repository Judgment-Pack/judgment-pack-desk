import { useQuery } from '@tanstack/react-query'
import { chassisUrl, deskFetch } from '../files/client'
import { sourceMessage } from '../i18n/source'
import type { CodexEffort } from './agent'

export interface Provider { id: string; authMethod: string; agent: string; configured: boolean; enabled: boolean; engineReady: boolean; requiredVersion: string; availability?: string; loginMethods: string[] }
export interface ProviderStatus { provider: string; authMethod: string; agent: string; runtime: string; account: string; plan?: string; login?: { id: string; state: string; expiresAt: string } }
export interface ProviderModel { id: string; name: string; efforts: CodexEffort[]; defaultEffort: CodexEffort }
export interface ProviderChallenge { method: string; id: string; url: string; code?: string; expiresAt: string }
const errors: Record<string,string> = {
  'runtime-install-failed': sourceMessage('Desk could not prepare the ChatGPT connection. Check your internet connection and try again.'),
  'provider-busy': sourceMessage('Another Codex operation is active. Try again when it finishes.'),
  'provider-unavailable': sourceMessage('Codex is unavailable. Check its installation in Desk.'),
  'sign-in-required': sourceMessage('Connect your ChatGPT account in Assistant settings.'),
  'login-unavailable': sourceMessage('This sign-in attempt is no longer available. Start again.'),
  'already-connected': sourceMessage('Disconnect the current ChatGPT account before signing in again.')
}
export class ProviderError extends Error {
  constructor(public code: string) { super(errors[code] ?? sourceMessage('The ChatGPT connection could not complete this request. Try again.')) }
}
export async function providerRequest<T>(action: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await deskFetch(chassisUrl(action === 'catalog' ? '/api/model-providers' : `/api/model-providers/openai/${action}`), {
    method: body === undefined ? 'GET' : 'POST', signal, cache: 'no-store',
    ...(body === undefined ? {} : { headers: { 'Content-Type':'application/json' }, body:JSON.stringify(body) })
  })
  const value = await response.json()
  if (!response.ok) throw new ProviderError(typeof value?.error === 'string' ? value.error : 'unavailable')
  return value as T
}
export function useProviderCatalog(enabled = true) {
  return useQuery({ queryKey:['model-providers'], queryFn:({signal})=>providerRequest<{providers:Provider[]}>('catalog',undefined,signal), enabled, retry:false, staleTime:30_000 })
}
export function useProviderStatus(enabled = true) {
  return useQuery({ queryKey:['model-provider','openai','status'], queryFn:({signal})=>providerRequest<ProviderStatus>('status',undefined,signal), enabled, retry:false, staleTime:5_000,
    refetchInterval: query => query.state.data?.login?.state === 'pending' ? 1500 : 15_000, refetchIntervalInBackground:false })
}
export function useProviderModels(enabled = true) {
  return useQuery({ queryKey:['model-provider','openai','models'], queryFn:({signal})=>providerRequest<{models:ProviderModel[]}>('models',undefined,signal), enabled, retry:false, staleTime:30_000, refetchOnWindowFocus:false })
}

import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { assistantReady, useAssistantSlot } from './useAssistantSlot'

const connected = { provider:'openai', authMethod:'subscription', agent:'codex', runtime:'available', account:'connected' }
const config = effectiveConfig(undefined, undefined, undefined, { path:'/fixture/desk.json', present:true,
  decoded:decodeDeskConfig(JSON.stringify({deskConfigVersion:1,assistant:{engine:'codex',agent:{provider:'openai',authMethod:'subscription',model:'test-model',tools:['get_schema']}}}), 'desk') })
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
function setup(response: () => Response | Promise<Response>, cached = false) {
  const client = testQueryClient()
  if (cached) client.setQueryData(['model-provider','openai','status'], connected)
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    expect(new URL(url, 'http://localhost').pathname).toBe('/api/model-providers/openai/status')
    expect(init?.method).toBe('GET')
    return response()
  })
  vi.stubGlobal('fetch', fetcher)
  const hook = renderHook(useAssistantSlot, {wrapper:({children}:{children:ReactNode}) =>
    <QueryClientProvider client={client}><DeskConfigFixture value={config}>{children}</DeskConfigFixture></QueryClientProvider>})
  return {...hook, fetcher}
}
it('does not turn a busy first read into a signed-out claim and recovers automatically', async () => {
  let busy = true
  const {result, fetcher} = setup(() => busy ? Response.json({error:'provider-busy'},{status:409}) : Response.json(connected))
  await waitFor(() => expect(result.current.recovery).toBe('retry'))
  expect(result.current.unusable).toContain('Another Codex operation')
  expect(result.current.unusable).not.toContain('Connect your ChatGPT account')
  expect(assistantReady(result.current)).toBe(false)
  busy = false
  await waitFor(() => expect(assistantReady(result.current)).toBe(true), {timeout:3500})
  expect(fetcher.mock.calls.length).toBeGreaterThan(1)
  expect(result.current.recovery).toBeUndefined()
})
it('retains a previously verified connection when a background status read is busy', async () => {
  const {result, fetcher} = setup(() => Response.json({error:'provider-busy'},{status:409}), true)
  expect(assistantReady(result.current)).toBe(true)
  await act(async () => result.current.retryKey())
  await waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
  await waitFor(() => expect(result.current.retrying).toBe(false))
  expect(assistantReady(result.current)).toBe(true)
  expect(result.current.unusable).toBeUndefined()
})
it('keeps pending checks distinct from missing credentials', () => {
  const {result} = setup(() => new Promise(() => {}))
  expect(result.current.unusable).toBe('Checking the ChatGPT connection…')
  expect(result.current.recovery).toBeUndefined()
  expect(assistantReady(result.current)).toBe(false)
})
it.each(['unavailable','unknown','login-pending'])('does not call a %s account signed out', async account => {
  const {result} = setup(() => Response.json({...connected,account}))
  await waitFor(() => expect(result.current.retrying).toBe(false))
  expect(assistantReady(result.current)).toBe(false)
  expect(result.current.unusable).not.toContain('Connect your ChatGPT account')
  expect(result.current.recovery).toBe(account === 'login-pending' ? 'configure' : 'retry')
})
it('offers a check rather than reauthentication after a failed read, even with cached data', async () => {
  let failed = true
  const {result} = setup(() => failed ? Response.json({error:'provider-unavailable'},{status:503}) : Response.json(connected), true)
  await act(async () => result.current.retryKey())
  await waitFor(() => expect(result.current.recovery).toBe('retry'))
  expect(assistantReady(result.current)).toBe(false)
  expect(result.current.unusable).not.toContain('Connect your ChatGPT account')
  failed = false
  await act(async () => result.current.retryKey())
  await waitFor(() => expect(assistantReady(result.current)).toBe(true))
})
it('requires connection after an explicit signed-out response replaces cached connected status', async () => {
  const {result} = setup(() => Response.json({...connected,account:'signed-out'}), true)
  await act(async () => result.current.retryKey())
  await waitFor(() => expect(result.current.recovery).toBe('configure'))
  expect(result.current.unusable).toBe('Connect your ChatGPT account in Connections > AI.')
  expect(assistantReady(result.current)).toBe(false)
})
it('honors an explicit sign-in-required refusal', async () => {
  const {result} = setup(() => Response.json({error:'sign-in-required'},{status:409}))
  await waitFor(() => expect(result.current.recovery).toBe('configure'))
  expect(result.current.unusable).toContain('Connect your ChatGPT account')
})

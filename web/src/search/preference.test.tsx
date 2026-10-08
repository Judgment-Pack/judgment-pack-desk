/**
 * This desk's research policy (`jpack-search.json`): Research says "Saved" only
 * of a save whose bytes the desk read back as written, and never before a save
 * has landed.
 */
import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { useSearchPreference } from './connections'

afterEach(() => { vi.unstubAllGlobals() })

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

/** A project with no `jpack-search.json` yet, whose write answers with `written(content)`. */
function serves(written: (content: string) => string) {
  const puts: string[] = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const path = new URL(url, 'http://localhost').pathname
    if (path === '/api/files') return json({ root: '/p', files: [] })
    if (path === '/api/file' && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { path: string; content: string }
      puts.push(body.content)
      const content = written(body.content)
      return json({ path: body.path, bytes: content.length, sha256: 'b'.repeat(64), content })
    }
    if (path === '/api/file') return json({ error: 'no such file', code: 'not-found' }, 404)
    return json({ error: 'not available' }, 404)
  })
  return puts
}

function hook() {
  const client = testQueryClient()
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return renderHook(() => useSearchPreference(), { wrapper })
}

it('says nothing is saved until a save lands, and saved once its bytes read back as written', async () => {
  const puts = serves((content) => content)
  const { result } = hook()
  await waitFor(() => expect(result.current.data?.value.mode).toBe('auto'))
  expect(result.current.saved).toBe(false)
  await act(async () => { await result.current.save({ version: 1, connection: null, mode: 'provided' }) })
  expect(puts).toHaveLength(1)
  await waitFor(() => expect(result.current.saved).toBe(true))
  expect(result.current.saveError).toBeNull()
})

it('is not saved where the bytes the desk read back are not the bytes it wrote', async () => {
  serves((content) => content.replace('provided', 'auto'))
  const { result } = hook()
  await waitFor(() => expect(result.current.data?.value.mode).toBe('auto'))
  await act(async () => {
    await expect(result.current.save({ version: 1, connection: null, mode: 'provided' })).rejects.toThrow('Search settings could not be verified.')
  })
  await waitFor(() => expect(result.current.saveError).toBeTruthy())
  expect(result.current.saved).toBe(false)
})

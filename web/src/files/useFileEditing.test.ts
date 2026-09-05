/**
 * The save discipline, driven as the hook it now is.
 *
 * These were `AuthorView`'s rules and were held only through its rendering.
 * Two editors hold them now, so they are asserted where they live — against a
 * wire-shaped stub of the chassis, because that is the shape the client reads.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { FileContent } from './client'
import { upsertListed, useFileEditing } from './useFileEditing'

const PATH = 'packs/vendor-onboarding.pack.json'
const LOADED = '{"id": "vendor-onboarding"}'
const EDITED = '{"id": "vendor-onboarding", "version": "0.2.0"}'
const LOADED_SHA = 'a'.repeat(64)
const SAVED_SHA = 'b'.repeat(64)

interface Call {
  url: string
  method: string
  body?: Record<string, unknown>
}

function chassis(answer: (call: Call) => { status: number; body: unknown }) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const call: Call = {
        url: String(input),
        method: init?.method ?? 'GET',
        body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined
      }
      calls.push(call)
      const { status, body } = answer(call)
      return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' }
      })
    })
  )
  return calls
}

function harness() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } }
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children)
  return { queryClient, ...renderHook(() => useFileEditing(), { wrapper }) }
}

const landed = (content: string): FileContent => ({
  path: PATH,
  bytes: content.length,
  sha256: SAVED_SHA,
  content
})

afterEach(() => vi.unstubAllGlobals())

describe('the save', () => {
  it('sends the base digest and hands the read-back back to the caller', async () => {
    const calls = chassis(() => ({ status: 200, body: landed(EDITED) }))
    const { result } = harness()
    const saved: FileContent[] = []
    act(() =>
      result.current.save({
        path: PATH,
        content: EDITED,
        baseSha256: LOADED_SHA,
        onSaved: (fresh) => saved.push(fresh)
      })
    )
    await waitFor(() => expect(result.current.outcome).toBeDefined())
    expect(calls[0]!.method).toBe('PUT')
    expect(calls[0]!.body!.baseSha256).toBe(LOADED_SHA)
    expect(calls[0]!.body!.content).toBe(EDITED)
    expect(saved.map((file) => file.content)).toEqual([EDITED])
    expect(result.current.verified).toBe(true)
  })

  it('compares the read-back to the submitted snapshot and not to the live buffer', async () => {
    // The chassis reads the file back off the disk after the rename. What that
    // proves is about the bytes that were *sent*; typing after a save must not
    // turn a true "verified" into a false "does not match".
    chassis(() => ({ status: 200, body: landed(EDITED) }))
    const { result } = harness()
    act(() => result.current.save({ path: PATH, content: EDITED, baseSha256: LOADED_SHA }))
    await waitFor(() => expect(result.current.outcome).toBeDefined())
    expect(result.current.outcome!.submitted).toBe(EDITED)
    expect(result.current.verified).toBe(true)
  })

  it('reports a read-back that is not what was sent', async () => {
    chassis(() => ({ status: 200, body: landed('something else entirely') }))
    const { result } = harness()
    act(() => result.current.save({ path: PATH, content: EDITED, baseSha256: LOADED_SHA }))
    await waitFor(() => expect(result.current.outcome).toBeDefined())
    expect(result.current.verified).toBe(false)
  })

  it('does not install its read-back over a newer answer', async () => {
    // A watcher refetch that completed while the PUT was in flight is newer
    // than this answer, and installing over it would replace a fresher read
    // and clear the invalidation that fetched it.
    chassis(() => ({ status: 200, body: landed(EDITED) }))
    const { result, queryClient } = harness()
    queryClient.setQueryData(['desk-file', PATH], {
      path: PATH,
      bytes: 1,
      sha256: 'c'.repeat(64),
      content: 'watcher'
    })
    act(() => result.current.save({ path: PATH, content: EDITED, baseSha256: LOADED_SHA }))
    // The watcher's answer lands *after* the save was issued. `dataUpdatedAt`
    // is a millisecond clock, so the two writes are separated by one rather
    // than landing on the same tick and comparing equal.
    await new Promise((resolve) => setTimeout(resolve, 2))
    act(() => {
      queryClient.setQueryData(['desk-file', PATH], {
        path: PATH,
        bytes: 2,
        sha256: 'd'.repeat(64),
        content: 'newer'
      })
    })
    await waitFor(() => expect(result.current.outcome).toBeDefined())
    expect((queryClient.getQueryData(['desk-file', PATH]) as FileContent).content).toBe('newer')
  })

  it('patches the listing with what landed where nothing newer arrived', async () => {
    chassis(() => ({ status: 200, body: landed(EDITED) }))
    const { result, queryClient } = harness()
    queryClient.setQueryData(['desk-files'], { root: '/p', files: [] })
    act(() => result.current.save({ path: PATH, content: EDITED, baseSha256: LOADED_SHA }))
    await waitFor(() => expect(result.current.outcome).toBeDefined())
    const listing = queryClient.getQueryData(['desk-files']) as { files: { path: string }[] }
    expect(listing.files.map((file) => file.path)).toEqual([PATH])
  })

  it('carries a 409 through as the refusal it is', async () => {
    chassis(() => ({
      status: 409,
      body: {
        error: 'the file changed',
        path: PATH,
        expectedSha256: LOADED_SHA,
        actualSha256: SAVED_SHA,
        exists: true,
        code: 'stale'
      }
    }))
    const { result } = harness()
    act(() => result.current.save({ path: PATH, content: EDITED, baseSha256: LOADED_SHA }))
    await waitFor(() => expect(result.current.write.error).toBeTruthy())
    expect(result.current.outcome).toBeUndefined()
  })
})

describe('reload', () => {
  it('is a direct read and hands the caller what is on disk now', async () => {
    // `refetch()` reports success from cache when the watcher's broad
    // `cancelQueries()` cancels the request in flight, so its success is not
    // proof that anything was fetched.
    const calls = chassis(() => ({ status: 200, body: landed(LOADED) }))
    const { result, queryClient } = harness()
    const fresh: FileContent[] = []
    act(() =>
      result.current.reload(PATH, (file) => {
        fresh.push(file)
        return true
      })
    )
    await waitFor(() => expect(fresh).toHaveLength(1))
    expect(calls[0]!.method).toBe('GET')
    expect(calls[0]!.url).toContain('/api/file?')
    expect((queryClient.getQueryData(['desk-file', PATH]) as FileContent).content).toBe(LOADED)
  })

  it('reports a read that failed rather than handing back nothing', async () => {
    chassis(() => ({ status: 404, body: { error: 'no such file' } }))
    const { result } = harness()
    const fresh: FileContent[] = []
    act(() =>
      result.current.reload(PATH, (file) => {
        fresh.push(file)
        return true
      })
    )
    await waitFor(() => expect(result.current.reloadError).toBeDefined())
    expect(fresh).toHaveLength(0)
  })
})

describe('upsertListed', () => {
  it('replaces an entry in place and sorts a new one in', () => {
    const files = [
      { path: 'a.json', bytes: 1, sha256: 'x' },
      { path: 'c.json', bytes: 3, sha256: 'z' }
    ]
    expect(upsertListed(files, { path: 'a.json', bytes: 9, sha256: 'y' })[0]!.bytes).toBe(9)
    expect(upsertListed(files, { path: 'b.json', bytes: 2, sha256: 'y' }).map((f) => f.path)).toEqual([
      'a.json',
      'b.json',
      'c.json'
    ])
  })
})

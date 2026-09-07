/**
 * The one configuration write this page makes.
 *
 * **What is asserted here is what leaves the browser and what the page does
 * with the answer** — the chassis' own half is `internal/desk/assistant_test.go`,
 * where the file is composed, decoded and written. Two properties are the
 * reason this exists at all:
 *
 * - **The request names no path and carries `ifMatch`.** A write that omitted
 *   the digest would be a page overwriting whatever it found, on the one file
 *   that names the endpoint a credential is presented to.
 * - **A refused write invalidates nothing.** Re-reading after a 422 would be
 *   this page telling itself that something happened.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { FileRequestError, StaleWrite } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { updateAssistantConfig } from './client'
import { useUpdateAssistantConfig } from './queries'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const GEMINI = {
  endpoint: {
    url: 'https://api.example.invalid/',
    kind: 'gemini',
    model: 'a-model',
    tools: ['validate']
  },
  engine: 'vercel',
  thinking: 'on'
}

const WRITTEN = {
  path: '/home/someone/.config/jpack-desk/desk.json',
  sha256: 'b'.repeat(64),
  assistant: GEMINI,
  created: false,
  // The write moves the endpoint and never the credential; this is how the
  // page learns whether somebody has to enter one for the new destination.
  keyRebindRequired: false
}

/** One chassis answer, and every request it was sent. */
function respond(status: number, body: unknown): { calls: RequestInit[]; urls: string[] } {
  const calls: RequestInit[] = []
  const urls: string[] = []
  vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
    urls.push(url)
    calls.push(init)
    return {
      ok: status >= 200 && status < 300,
      status,
      statusText: '',
      text: async () => JSON.stringify(body)
    }
  })
  return { calls, urls }
}

describe('the desk-level assistant write', () => {
  it('PUTs the object and the digest to the one route, with the session token', async () => {
    const seen = respond(200, WRITTEN)
    const answered = await updateAssistantConfig({
      assistant: GEMINI,
      ifMatch: 'a'.repeat(64)
    })
    expect(answered).toEqual(WRITTEN)

    expect(seen.urls).toHaveLength(1)
    expect(seen.urls[0]).toContain('/api/desk-config?')
    expect(seen.urls[0]).toContain('token=')
    expect(seen.calls[0]!.method).toBe('PUT')

    const sent = JSON.parse(String(seen.calls[0]!.body)) as Record<string, unknown>
    // Two members, and **no path**: this route writes one file, the one on
    // that machine, and a body that could name another would be a way to
    // write anywhere with the desk's own authority.
    expect(Object.keys(sent).sort()).toEqual(['assistant', 'ifMatch'])
    expect(sent.assistant).toEqual(GEMINI)
    expect(sent.ifMatch).toBe('a'.repeat(64))
  })

  it('raises the chassis code for an object the shared decoder refuses', async () => {
    respond(422, {
      error: 'the configuration this would write is not one this desk reads',
      code: 'desk-config-refused',
      problems: [{ key: 'assistant.endpoint.apiKey', reason: 'a key is never stored' }]
    })
    // The code, not the sentence: the message is written for a person reading
    // a diagnostic and is improved when it reads badly.
    await expect(
      updateAssistantConfig({ assistant: GEMINI, ifMatch: '' })
    ).rejects.toMatchObject({ code: 'desk-config-refused', status: 422 })
    await expect(
      updateAssistantConfig({ assistant: GEMINI, ifMatch: '' })
    ).rejects.toBeInstanceOf(FileRequestError)
  })

  it('raises a stale write, with both digests, for a file that moved', async () => {
    respond(409, {
      error: 'the desk-level configuration on disk is not the one this page read',
      code: 'desk-config-changed',
      path: '/home/someone/.config/jpack-desk/desk.json',
      expectedSha256: 'a'.repeat(64),
      actualSha256: 'c'.repeat(64),
      exists: true
    })
    // The same digest discipline the file API uses: the page can show what
    // happened rather than overwrite a change nobody saw.
    const failure = await updateAssistantConfig({ assistant: GEMINI, ifMatch: 'a'.repeat(64) })
      .then(() => undefined)
      .catch((cause: unknown) => cause)
    expect(failure).toBeInstanceOf(StaleWrite)
    expect((failure as StaleWrite).code).toBe('desk-config-changed')
    expect((failure as StaleWrite).expectedSha256).toBe('a'.repeat(64))
    expect((failure as StaleWrite).actualSha256).toBe('c'.repeat(64))
  })
})

/** A button that writes, so the hook runs where a page would run it. */
function Writer({ ifMatch }: { ifMatch: string }) {
  const write = useUpdateAssistantConfig()
  return (
    <>
      <button onClick={() => write.mutate({ assistant: GEMINI, ifMatch })}>write</button>
      <output>{write.isSuccess ? 'written' : write.isError ? 'refused' : 'idle'}</output>
    </>
  )
}

describe('the write hook', () => {
  it('re-reads the configuration after a write that landed', async () => {
    respond(200, WRITTEN)
    const client = testQueryClient()
    // A cached read, so an invalidation is observable as one rather than as an
    // absence.
    client.setQueryData(DESK_CONFIG_QUERY_KEY, { marker: 'the value read before' })
    render(
      <QueryClientProvider client={client}>
        <Writer ifMatch={'a'.repeat(64)} />
      </QueryClientProvider>
    )
    screen.getByRole('button', { name: 'write' }).click()
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('written'))
    // **Invalidated rather than written into**, because what is cached is the
    // *effective* configuration — two files layered, with each section's
    // source — and a write answers with the assistant slot alone.
    await waitFor(() =>
      expect(client.getQueryState(DESK_CONFIG_QUERY_KEY)?.isInvalidated).toBe(true)
    )
  })

  it('re-reads nothing after a write that was refused', async () => {
    respond(422, { error: 'refused', code: 'desk-config-refused', problems: [] })
    const client = testQueryClient()
    client.setQueryData(DESK_CONFIG_QUERY_KEY, { marker: 'the value read before' })
    render(
      <QueryClientProvider client={client}>
        <Writer ifMatch={''} />
      </QueryClientProvider>
    )
    screen.getByRole('button', { name: 'write' }).click()
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('refused'))
    // Nothing changed on disk, so nothing is re-read: an invalidation here
    // would be this page telling itself that something happened.
    expect(client.getQueryState(DESK_CONFIG_QUERY_KEY)?.isInvalidated).toBe(false)
    expect(client.getQueryData(DESK_CONFIG_QUERY_KEY)).toEqual({
      marker: 'the value read before'
    })
  })
})

describe('the answer to a write that moved the endpoint', () => {
  it('carries the desk s request for a new key rather than hiding it', async () => {
    // **The credential does not follow the configuration.** A key is bound to
    // the scheme, host and wire protocol it was entered for; a write that
    // changes any of those leaves the stored key in place and unusable, and
    // the probe and the relay refuse with `assistant-key-unbound` rather than
    // presenting it somewhere new. The page is told at the moment of the write
    // instead of discovering it by making a request that fails.
    respond(200, { ...WRITTEN, keyRebindRequired: true })
    const answered = await updateAssistantConfig({
      assistant: GEMINI,
      ifMatch: 'a'.repeat(64)
    })
    expect(answered.keyRebindRequired).toBe(true)
  })

  it('raises the chassis code where a bound key is asked to travel elsewhere', async () => {
    respond(409, {
      error:
        'the key on this machine was entered for https://first.example over "gemini", ' +
        'and this desk is configured for https://second.example over "gemini"',
      code: 'assistant-key-unbound'
    })
    // A 409 with a body is read as a stale write by the shared envelope, and
    // the code is what a caller branches on either way.
    await expect(
      updateAssistantConfig({ assistant: GEMINI, ifMatch: '' })
    ).rejects.toMatchObject({ code: 'assistant-key-unbound' })
  })
})

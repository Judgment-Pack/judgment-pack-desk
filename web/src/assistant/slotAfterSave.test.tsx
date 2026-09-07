/**
 * A save reaches the surfaces that read the slot, without a reload.
 *
 * **The other suites use a configuration fixture, and that is exactly what
 * this one must not do.** A fixture is a value handed to the tree; what is
 * being asserted here is that the *query* behind the real provider is
 * invalidated by the write and read again — the mechanism by which the tab's
 * status line and Describe it come to name the model somebody just chose. A
 * fixture would hold that mechanism constant and prove nothing about it.
 *
 * So the real `DeskConfigProvider` runs over a stubbed chassis: the file it
 * reads changes when the write lands, exactly as a file on disk does, and the
 * slot is read through `useAssistantSlot` — the one hook every consumer uses.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigProvider, useEffectiveConfig } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { AssistantSection } from './AssistantSection'
import { useAssistantSlot } from './useAssistantSlot'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'the-model-in-the-file',
  tools: ['validate']
}

/**
 * A chassis whose desk-level file is rewritten by the write, as one on disk is.
 *
 * The read answers whatever the file currently is, so a query that is
 * invalidated sees the new value and one that is not sees the old — which is
 * the difference this suite exists to measure.
 */
function stubDesk(): { writes: number } {
  const state = { writes: 0 }
  let assistant: Record<string, unknown> = {
    endpoint: ENDPOINT,
    engine: 'vercel',
    thinking: 'off'
  }
  let digest = 'a'.repeat(64)
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    if (url.includes('/api/desk-config') && method === 'PUT') {
      state.writes += 1
      const sent = JSON.parse(String(init?.body)) as { assistant: Record<string, unknown> }
      assistant = sent.assistant
      digest = 'b'.repeat(64)
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({
            path: DESK_PATH,
            sha256: digest,
            assistant,
            created: false,
            keyRebindRequired: false
          })
      }
    }
    if (url.includes('/api/desk-config')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({
            path: DESK_PATH,
            present: true,
            sha256: digest,
            content: JSON.stringify({ deskConfigVersion: 1, assistant })
          })
      }
    }
    if (url.includes('/api/assistant/key')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () => JSON.stringify({ present: false, fingerprint: '', origin: '', kind: '' })
      }
    }
    // The project file, which this desk has none of in these cases.
    return {
      ok: false,
      status: 404,
      statusText: '',
      text: async () => JSON.stringify({ error: 'no such file' })
    }
  })
  return state
}

/**
 * The digest the form would send, read from the same place the form reads it.
 *
 * Rendered so a case can wait for a re-read to have **landed** rather than for
 * the request to have been made: a read that has been issued is not a read the
 * form has, and a Save in between would state the digest from before it.
 */
function DigestReading() {
  const { desk } = useEffectiveConfig()
  return <p id="desk-digest">{desk?.sha256 ?? 'none'}</p>
}

/** What every surface that reads the slot reads. */
function SlotReading() {
  const slot = useAssistantSlot()
  return (
    <output>
      {slot.state} · {slot.endpoint?.model ?? 'none'} · {slot.engine} · {slot.thinking}
    </output>
  )
}

function renderDesk() {
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigProvider>
        <DigestReading />
        <SlotReading />
        <AssistantSection id="assistant" title="Assistant" />
      </DeskConfigProvider>
    </QueryClientProvider>
  )
}

describe('what a save reaches', () => {
  it('changes the slot every surface reads, with no reload and no remount', async () => {
    const state = stubDesk()
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'configured · the-model-in-the-file · vercel · off'
      )
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('combobox', { name: 'Thinking' }))
    fireEvent.click(await screen.findByRole('option', { name: 'ultra' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // **The whole assertion.** The write invalidates the desk-configuration
    // query, the read runs again, and the value every consumer of the slot
    // reads is the one on disk — without anything reloading the page.
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'configured · the-model-chosen · vercel · ultra'
      )
    )
    expect(state.writes).toBe(1)
  })

  it('changes nothing anywhere when the write was refused', async () => {
    // A refused write changed nothing on disk, so re-reading after one would
    // be this page telling itself that something happened.
    stubDesk()
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url.includes('/api/desk-config') && method === 'PUT') {
        return {
          ok: false,
          status: 422,
          statusText: '',
          text: async () =>
            JSON.stringify({
              error: 'refused',
              code: 'desk-config-refused',
              problems: [{ key: 'assistant.endpoint.model', reason: 'must be a non-empty string' }]
            })
        }
      }
      if (url.includes('/api/desk-config')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              path: DESK_PATH,
              present: true,
              sha256: 'a'.repeat(64),
              content: JSON.stringify({
                deskConfigVersion: 1,
                assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' }
              })
            })
        }
      }
      if (url.includes('/api/assistant/key')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({ present: false, fingerprint: '', origin: '', kind: '' })
        }
      }
      return { ok: false, status: 404, statusText: '', text: async () => '{}' }
    })
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'configured · the-model-in-the-file · vercel · off'
      )
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('must be a non-empty string')).toBeTruthy()
    expect(screen.getByRole('status').textContent).toBe(
      'configured · the-model-in-the-file · vercel · off'
    )
  })
})

describe('Reload after a file that moved', () => {
  /**
   * A chassis whose file somebody else edits, and which refuses a write that
   * states the digest from before that edit.
   *
   * **The fixture-driven suite cannot measure this.** What Reload has to do is
   * *read the file again*, and a form over a fixture would show the same thing
   * whether it read or not — which is exactly how a Reload that only cleared
   * its own notice passed a mutation row. So this drives the real provider and
   * counts the reads, and then asserts the digest the **next** write states.
   */
  function stubMoving(): { reads: number; sent: string[] } {
    const state = { reads: 0, sent: [] as string[] }
    let digest = 'a'.repeat(64)
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url.includes('/api/desk-config') && method === 'PUT') {
        const body = JSON.parse(String(init?.body)) as { ifMatch: string }
        state.sent.push(body.ifMatch)
        if (body.ifMatch !== digest) {
          return {
            ok: false,
            status: 409,
            statusText: '',
            text: async () =>
              JSON.stringify({
                error: 'the desk-level configuration on disk is not the one this page read',
                code: 'desk-config-changed',
                path: DESK_PATH,
                expectedSha256: body.ifMatch,
                actualSha256: digest,
                exists: true
              })
          }
        }
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              path: DESK_PATH,
              sha256: digest,
              assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' },
              created: false,
              keyRebindRequired: false
            })
        }
      }
      if (url.includes('/api/desk-config')) {
        state.reads += 1
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              path: DESK_PATH,
              present: true,
              sha256: digest,
              content: JSON.stringify({
                deskConfigVersion: 1,
                assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' }
              })
            })
        }
      }
      if (url.includes('/api/assistant/key')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({ present: false, fingerprint: '', origin: '', kind: '' })
        }
      }
      return { ok: false, status: 404, statusText: '', text: async () => '{}' }
    })
    /** Somebody else writes the file. */
    ;(state as unknown as { move: () => void }).move = () => {
      digest = 'c'.repeat(64)
    }
    return state
  }

  it('reads the file again, so the next write states a digest that is true', async () => {
    const state = stubMoving() as { reads: number; sent: string[]; move: () => void }
    renderDesk()
    // Waited on the *rendered* configuration rather than on the request:
    // a read that has been made is not a read that has reached the form, and
    // Save with no digest yet does nothing at all — which is a different
    // failure wearing this one's clothes.
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    const before = state.reads

    state.move()
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'chosen-and-unsaved' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/changed on disk. Nothing was written/)).toBeTruthy()
    expect(state.sent).toEqual(['a'.repeat(64)])
    // Nothing was read on the refusal: a refused write changed nothing.
    expect(state.reads).toBe(before)

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    // **The assertion the row is for.** Reload reads the file again — a button
    // that only cleared its own notice would leave the next Save stating the
    // same stale digest, and the author pressing it twice. Waited on the value
    // the form holds, not on the request: the request having been made says
    // nothing about the form having the answer.
    await waitFor(() => expect(state.reads).toBe(before + 1))
    await waitFor(() =>
      expect(document.querySelector('#desk-digest')?.textContent).toBe('c'.repeat(64))
    )

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(state.sent).toHaveLength(2))
    expect(state.sent[1]).toBe('c'.repeat(64))
    // And the value typed before the refusal survived both.
    expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('chosen-and-unsaved')
  })
})

describe('a write that landed while the read after it did not', () => {
  /**
   * A chassis whose write always succeeds and whose **read** can be made to
   * fail or to hang.
   *
   * This is the case the first version of this suite could not see: the status
   * line was updated by a second `GET`, so a write that landed under a read
   * that failed left the tab describing the endpoint that had just been
   * replaced, under a form that said "Saved".
   */
  function stubWriteThen(read: 'ok' | 'fails' | 'hangs'): { writes: number } {
    const state = { writes: 0 }
    let assistant: Record<string, unknown> = {
      endpoint: ENDPOINT,
      engine: 'vercel',
      thinking: 'off'
    }
    let written = false
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url.includes('/api/desk-config') && method === 'PUT') {
        state.writes += 1
        const sent = JSON.parse(String(init?.body)) as { assistant: Record<string, unknown> }
        assistant = sent.assistant
        written = true
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              path: DESK_PATH,
              sha256: 'b'.repeat(64),
              assistant,
              created: false,
              keyRebindRequired: false
            })
        }
      }
      if (url.includes('/api/desk-config')) {
        // The first read always works — the page has to have something to
        // start from — and every read after the write behaves as the case says.
        if (written && read === 'hangs') return new Promise(() => {})
        if (written && read === 'fails') {
          return {
            ok: false,
            status: 503,
            statusText: '',
            text: async () => JSON.stringify({ error: 'the desk could not read it' })
          }
        }
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              path: DESK_PATH,
              present: true,
              sha256: written ? 'b'.repeat(64) : 'a'.repeat(64),
              content: JSON.stringify({ deskConfigVersion: 1, assistant })
            })
        }
      }
      if (url.includes('/api/assistant/key')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({ present: false, fingerprint: '', origin: '', kind: '', configuredOrigin: '', bound: false })
        }
      }
      return { ok: false, status: 404, statusText: '', text: async () => '{}' }
    })
    return state
  }

  it('reflects the slot the write answered with while the read is still in flight', async () => {
    // **The write's own answer, and nothing waiting on a second request.** It
    // carries the slot the chassis read back off the disk and the digest the
    // next write states, so a read that never comes back leaves neither the
    // status line nor the next Save stranded.
    const state = stubWriteThen('hangs')
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'configured · the-model-chosen · vercel · off'
      )
    )
    expect(state.writes).toBe(1)
    await waitFor(() =>
      expect(document.querySelector('#desk-digest')?.textContent).toBe('b'.repeat(64))
    )
  })

  it('reports the slot as unavailable, never as none, where the read refused', async () => {
    // **The false `none`.** The chassis confirmed a write; the read after it
    // answered with a refusal; `loadDeskConfig` resolves to the built-in
    // defaults, and every consumer of the slot was then told that no assistant
    // was configured — an absence this page had not established, about a file
    // it could not open. It is its own state now, and the tab says so.
    const state = stubWriteThen('fails')
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('unavailable · none · vercel · off')
    )
    expect(screen.getByRole('status').textContent).not.toContain('configured ·')
    expect(state.writes).toBe(1)
  })

  it('follows the chassis for the key row even where the configuration refused', async () => {
    // **The row read the page's copy of the file first**, and that copy is
    // exactly what goes missing here: it said "save an endpoint first" while a
    // perfectly good key read beside it named the endpoint and carried this
    // desk's verdict about it. Every state comes from the answer now.
    stubWriteThen('fails')
    vi.stubGlobal('fetch', withStoredKey(globalThis.fetch as typeof fetch))
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('unavailable · none · vercel · off')
    )
    // The chassis says there is an endpoint and the key is for it, so the row
    // says that — rather than overruling it with a file it could not read.
    expect(screen.getByText(/which is where this desk is configured/)).toBeTruthy()
    expect(screen.queryByText(/Save an endpoint above before storing a key/)).toBeNull()
  })

  it('reports the state as unverified where the read after it failed', async () => {
    // **The other branch the review admitted, and the one that is right here.**
    // A read that *answered* is newer information about the same file than the
    // write's own answer, and this desk's standing doctrine is that a file it
    // could not read is a file it says nothing about — not one it describes
    // from memory. So the write's value is not defended against it: the page
    // says it cannot read its own configuration and refuses to write again,
    // which is a louder and truer thing than a status line quietly holding a
    // value nothing on disk has been seen to confirm.
    const state = stubWriteThen('fails')
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/has not seen them/)).toBeTruthy()
    expect(
      (screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled
    ).toBe(true)
    expect(state.writes).toBe(1)
    // And nothing claims the write did not happen either: the slot is not
    // reported as some third state invented for the occasion.
    expect(document.querySelector('#desk-digest')?.textContent).toBe('none')
  })

  it('still re-reads, for the parts a write cannot speak about', async () => {
    // The write answers about one slot; the cache holds two files layered,
    // every section's source and the problems each file carries. Setting the
    // slot is not a reason to stop reading the rest.
    const state = stubWriteThen('ok')
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'the-model-chosen' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe(
        'configured · the-model-chosen · vercel · off'
      )
    )
    expect(state.writes).toBe(1)
  })
})

describe('removing the endpoint', () => {
  it('leaves the desk with none, and the key still kept here', async () => {
    // **Driven through the real provider**, because what is being asserted is
    // that the *slot* went to None — which the fixture-backed suite cannot
    // see, since a fixture is a value handed to the tree rather than one the
    // write changes.
    const state = stubDesk()
    // A key stored for the endpoint that is about to go, so the row has
    // something to keep saying afterwards.
    vi.stubGlobal('fetch', withStoredKey(globalThis.fetch as typeof fetch))
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toContain('the-model-in-the-file')
    )
    fireEvent.click(screen.getByRole('button', { name: 'Remove endpoint' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove it' }))
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('none · none · vercel · off')
    )
    expect(state.writes).toBe(1)
    // The two are separate: the endpoint went and the key did not.
    expect(await screen.findByText('stored on this machine — sk-a…wxyz')).toBeTruthy()
    expect(screen.getByText(/Save an endpoint above before storing a key/)).toBeTruthy()
    expect(screen.getByText('none — no endpoint configured')).toBeTruthy()
  })
})

/**
 * The same chassis, answering the key read with one that is stored and bound —
 * **and answering it as the chassis would once the endpoint has gone.**
 *
 * The key row reads `configuredOrigin` and `bound` from this answer and from
 * nothing else, so a stub that went on naming an endpoint after a removal
 * would be a stub disagreeing with the desk it stands for.
 */
function withStoredKey(inner: typeof fetch): typeof fetch {
  let configured = true
  return (async (url: string, init?: RequestInit) => {
    const address = String(url)
    if (address.includes('/api/desk-config') && (init?.method ?? 'GET') === 'PUT') {
      const sent = JSON.parse(String(init?.body)) as { assistant: { endpoint: unknown } }
      configured = sent.assistant.endpoint !== null
    }
    if (address.includes('/api/assistant/key')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({
            present: true,
            fingerprint: 'sk-a…wxyz',
            origin: 'https://api.example.invalid',
            kind: 'openai-compatible',
            configuredOrigin: configured ? 'https://api.example.invalid' : '',
            configuredKind: configured ? 'openai-compatible' : '',
            bound: configured
          })
      }
    }
    return inner(address as never, init as never)
  }) as unknown as typeof fetch
}

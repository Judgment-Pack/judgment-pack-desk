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

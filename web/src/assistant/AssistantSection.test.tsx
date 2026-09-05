/**
 * Admin › Assistant, driven against a stub of the chassis it calls.
 *
 * The assertions are about **what the page says and what it sends**, on the
 * wire: which request each control makes, what it renders from the answer, and
 * — the one that matters most — that the key the reader typed goes into the
 * store request and appears nowhere else, in no rendered text and in no
 * subsequent request.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { AssistantSection, ASSISTANT_STANDING_SENTENCE } from './AssistantSection'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: ['get_schema', 'validate']
}

/** One effective configuration whose desk-level file carries an endpoint. */
function configured(endpoint: unknown = ENDPOINT): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: true,
    decoded: decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint } }),
      'desk'
    )
  })
}

/** The default: a desk-level file that is there and configures no assistant. */
function unconfigured(): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: false,
    note: `no desk-level configuration file at ${DESK_PATH}`
  })
}

/** Every request the page made, and a scripted answer for each route. */
function stubChassis(answers: {
  key?: { present: boolean; fingerprint: string }
  keyStatus?: number
  keyError?: { error: string; code: string }
  probe?: unknown
  probeStatus?: number
}): { sent: { method: string; url: string; body?: string }[] } {
  const sent: { method: string; url: string; body?: string }[] = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    sent.push({ method, url, body: init?.body as string | undefined })
    if (url.includes('/api/assistant/probe')) {
      return {
        ok: (answers.probeStatus ?? 200) < 400,
        status: answers.probeStatus ?? 200,
        statusText: '',
        text: async () => JSON.stringify(answers.probe ?? {})
      }
    }
    if (url.includes('/api/assistant/key')) {
      if (answers.keyError !== undefined && method === 'PUT') {
        return {
          ok: false,
          status: answers.keyStatus ?? 400,
          statusText: '',
          text: async () => JSON.stringify(answers.keyError)
        }
      }
      const state =
        method === 'PUT'
          ? { present: true, fingerprint: 'sk-a…wxyz' }
          : method === 'DELETE'
            ? { present: false, fingerprint: '' }
            : (answers.key ?? { present: false, fingerprint: '' })
      return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(state) }
    }
    return { ok: true, status: 200, statusText: '', text: async () => '{}' }
  })
  return { sent }
}

function renderSection(value: EffectiveConfig = unconfigured()) {
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigFixture value={value}>
        <AssistantSection id="assistant" title="Assistant" />
      </DeskConfigFixture>
    </QueryClientProvider>
  )
}

describe('the Assistant section', () => {
  it('carries the standing sentence character for character', () => {
    stubChassis({})
    renderSection()
    expect(screen.getByText(ASSISTANT_STANDING_SENTENCE)).toBeTruthy()
    // The three claims that sentence exists to make, spelled out here so that
    // softening any of them fails rather than reads as an edit.
    expect(ASSISTANT_STANDING_SENTENCE).toContain('proposes edits')
    expect(ASSISTANT_STANDING_SENTENCE).toContain('never saves a file')
    expect(ASSISTANT_STANDING_SENTENCE).toContain('never decides an outcome')
    expect(ASSISTANT_STANDING_SENTENCE).toContain('never written into a project')
  })

  it('describes the three deployment states as text, and never as controls', () => {
    stubChassis({})
    const { container } = renderSection()
    // Read off the list itself rather than by text: "None" is also the word
    // the two-settings sentence above uses, and a text query that matched
    // both would pass without the list existing at all.
    const states = Array.from(container.querySelectorAll('dl.fields dt')).map(
      (term) => term.textContent
    )
    expect(states).toEqual(['None', 'Bring your own', 'Supplied'])
    // Supplied is an ordinary endpoint. That is the whole claim, and it is
    // made in words on the page rather than left to be inferred.
    expect(screen.getByText(/ordinary endpoint, the same code path/)).toBeTruthy()
    // None of the three is selectable, and none is a disabled control — an
    // affordance that will never enable lies about what the page can do.
    expect(screen.queryByRole('radio')).toBeNull()
    expect(screen.queryByRole('option')).toBeNull()
    expect(container.querySelectorAll('[disabled]')).toHaveLength(0)
  })

  it('says no endpoint is configured, and does not claim there is no key', () => {
    // **It used to say "none — no assistant, and no key".** The key is
    // independent of the endpoint: removing the endpoint from the file does
    // not remove the key from this machine, so that line asserted something
    // the page had not established and could be flatly false.
    stubChassis({ key: { present: true, fingerprint: 'sk-a…wxyz' } })
    renderSection()
    expect(screen.getByText('none — no endpoint configured')).toBeTruthy()
    expect(screen.queryByText(/no assistant, and no key/)).toBeNull()
    expect(screen.getByText(/No endpoint is configured/)).toBeTruthy()
    expect(screen.getByText(/The key and the endpoint are separate/)).toBeTruthy()
  })

  it('still reports a stored key where no endpoint is configured', async () => {
    stubChassis({ key: { present: true, fingerprint: 'sk-a…wxyz' } })
    renderSection()
    expect(await screen.findByText('stored on this machine — sk-a…wxyz')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove key' })).toBeTruthy()
  })

  it('shows the configured endpoint, its protocol, its model and its tools', () => {
    stubChassis({})
    const { container } = renderSection(configured())
    // The status line, read off the paragraph it is in: the same phrase
    // appears in the prose that explains the two settings, so a bare text
    // query would match both and prove neither.
    const status = Array.from(container.querySelectorAll('p')).find((paragraph) =>
      paragraph.textContent?.startsWith('Assistant:')
    )
    expect(status, 'the section states what the assistant is').toBeDefined()
    expect(status!.textContent).toContain('a model endpoint')
    expect(screen.getByText('https://api.example.invalid/v1')).toBeTruthy()
    expect(screen.getByText('openai-compatible')).toBeTruthy()
    expect(screen.getByText('a-model')).toBeTruthy()
    expect(screen.getByText('get_schema · validate')).toBeTruthy()
    // And where it came from, which is the desk-level file by construction.
    expect(screen.getByText(/source: desk file/)).toBeTruthy()
    expect(screen.getByText(DESK_PATH)).toBeTruthy()
  })

  it('says an empty tool list means the assistant may call nothing', () => {
    stubChassis({})
    renderSection(configured({ ...ENDPOINT, tools: [] }))
    expect(screen.getByText('none — the assistant may call no tool')).toBeTruthy()
  })

  it('says the key is not read yet before the chassis has answered', () => {
    // A read that has not answered is not "no key stored": it is a page that
    // has not been told, and saying otherwise is stating what was not observed.
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderSection()
    expect(screen.getByText('not read yet')).toBeTruthy()
  })

  it('reports a stored key by its fingerprint, and offers to remove it', async () => {
    stubChassis({ key: { present: true, fingerprint: 'sk-a…wxyz' } })
    renderSection()
    expect(await screen.findByText('stored on this machine — sk-a…wxyz')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove key' })).toBeTruthy()
  })

  it('offers no removal where there is nothing to remove', async () => {
    stubChassis({ key: { present: false, fingerprint: '' } })
    renderSection()
    expect(await screen.findByText('none stored on this machine')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Remove key' })).toBeNull()
  })

  it('says a key is present but too short to show, rather than showing a blank', async () => {
    // `present` with an empty fingerprint is a real state — a key of eight
    // characters would be disclosed in full by four-and-four — and rendering
    // it as a stored key with nothing beside it looks like a bug.
    stubChassis({ key: { present: true, fingerprint: '' } })
    renderSection()
    expect(
      await screen.findByText(/too short to show any of it without showing all of it/)
    ).toBeTruthy()
  })

  it('sends the typed key on a store, and never renders it afterwards', async () => {
    const { sent } = stubChassis({ key: { present: false, fingerprint: '' } })
    const { container } = renderSection()
    await screen.findByText('none stored on this machine')

    const field = container.querySelector('input') as HTMLInputElement
    expect(field.type).toBe('password')
    fireEvent.change(field, { target: { value: 'sk-a-real-looking-key-wxyz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Store key' }))

    await waitFor(() =>
      expect(sent.some((request) => request.method === 'PUT')).toBe(true)
    )
    const put = sent.find((request) => request.method === 'PUT')!
    expect(put.url).toContain('/api/assistant/key')
    expect(JSON.parse(put.body!)).toEqual({ key: 'sk-a-real-looking-key-wxyz' })

    // The field is cleared and the page shows the fingerprint the chassis
    // answered with — never the value it was handed.
    await waitFor(() => expect(field.value).toBe(''))
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    expect(await screen.findByText('stored on this machine — sk-a…wxyz')).toBeTruthy()
  })

  it('removes a key on request, and says so', async () => {
    const { sent } = stubChassis({ key: { present: true, fingerprint: 'sk-a…wxyz' } })
    renderSection()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove key' }))
    await waitFor(() => expect(sent.some((request) => request.method === 'DELETE')).toBe(true))
    expect(await screen.findByText('none stored on this machine')).toBeTruthy()
  })

  it('clears the field before the request, so a failure retains nothing', async () => {
    // **The failure case, which is the one that mattered.** The field was
    // cleared by the success callback only, so a network error or a refusal
    // left the plaintext sitting in a password input and in React state until
    // somebody noticed. It is copied and cleared synchronously now, before
    // the request is made.
    stubChassis({
      key: { present: false, fingerprint: '' },
      keyError: { error: 'a key may not contain a control character', code: 'bad-request' }
    })
    const { container } = renderSection()
    await screen.findByText('none stored on this machine')
    const field = container.querySelector('input') as HTMLInputElement
    fireEvent.change(field, { target: { value: 'sk-a-real-looking-key-wxyz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Store key' }))

    // Empty immediately, not after the answer arrives.
    expect(field.value).toBe('')
    expect(await screen.findByText(/a key may not contain a control character/)).toBeTruthy()
    // And nowhere in the rendered page either.
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    expect(field.value).toBe('')
  })

  it('reports a refused store as one that did not happen', async () => {
    stubChassis({
      key: { present: false, fingerprint: '' },
      keyError: { error: 'a key may not contain a control character', code: 'bad-request' }
    })
    const { container } = renderSection()
    await screen.findByText('none stored on this machine')
    fireEvent.change(container.querySelector('input')!, { target: { value: 'bad\nkey' } })
    fireEvent.click(screen.getByRole('button', { name: 'Store key' }))
    expect(await screen.findByText(/a key may not contain a control character/)).toBeTruthy()
    // And the page still says no key is stored, because none is.
    expect(screen.getByText('none stored on this machine')).toBeTruthy()
  })

  it('asks the desk to probe, sending no destination of its own', async () => {
    // The request carries nothing but the token. If it named a URL, anything
    // holding the token could point the desk — and the key it holds — at a
    // host of its choosing.
    const { sent } = stubChassis({
      probe: { reachable: true, status: 200, latencyMs: 240, diagnostic: '' }
    })
    renderSection(configured())
    fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
    await waitFor(() => expect(sent.some((request) => request.method === 'POST')).toBe(true))
    const probe = sent.find((request) => request.method === 'POST')!
    expect(probe.url).toContain('/api/assistant/probe')
    expect(probe.body).toBeUndefined()
    expect(probe.url).not.toContain('api.example.invalid')
  })

  it('renders a reachable answer with its status and its latency', async () => {
    stubChassis({ probe: { reachable: true, status: 200, latencyMs: 240, diagnostic: '' } })
    renderSection(configured())
    fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
    expect(await screen.findByText(/reachable · answered 200 · 240 ms/)).toBeTruthy()
  })

  it('renders a refused credential as not reachable, from the fixed vocabulary', async () => {
    // **Not the endpoint's own sentence.** It used to be quoted verbatim with
    // the key substituted out; a body under the endpoint's control can carry
    // a derived representation of the credential that no substitution finds,
    // so the desk discards the body and answers one word from a closed list.
    stubChassis({
      probe: { reachable: false, status: 401, latencyMs: 88, diagnostic: 'unauthorized' }
    })
    renderSection(configured())
    fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
    expect(await screen.findByText(/not reachable · answered 401/)).toBeTruthy()
    expect(screen.getByText(/the endpoint did not accept the key/)).toBeTruthy()
  })

  it('renders every word of the vocabulary as a sentence a person can read', async () => {
    for (const [diagnostic, says] of [
      ['forbidden', 'the endpoint refused this request'],
      ['not-found', 'nothing is at that address'],
      ['timeout', 'no answer within ten seconds'],
      ['tls', 'the secure connection could not be established'],
      ['refused', 'nothing is listening there'],
      ['dns', 'that host name did not resolve'],
      ['unexpected-status', 'the endpoint answered something unexpected']
    ] as const) {
      stubChassis({ probe: { reachable: false, status: 0, latencyMs: 5, diagnostic } })
      renderSection(configured())
      fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
      expect(await screen.findByText(new RegExp(says)), diagnostic).toBeTruthy()
      cleanup()
    }
  })

  it('says no answer arrived where the status is zero', async () => {
    stubChassis({
      probe: {
        reachable: false,
        status: 0,
        latencyMs: 10_000,
        diagnostic: 'timeout'
      }
    })
    renderSection(configured())
    fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
    expect(await screen.findByText(/no answer arrived/)).toBeTruthy()
    // Never "answered 0", which is a status nothing sends.
    expect(screen.queryByText(/answered 0/)).toBeNull()
  })

  it('reports a probe the desk refused, naming which state refused it', async () => {
    stubChassis({
      probeStatus: 409,
      probe: {
        error: 'no key is stored on this machine, so there is nothing to present to the endpoint',
        code: 'assistant-no-key'
      }
    })
    renderSection(configured())
    fireEvent.click(screen.getByRole('button', { name: 'Check reachability' }))
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
  })

  it('names the four tools it may be given, and says what each of them is', () => {
    stubChassis({})
    renderSection()
    expect(
      screen.getByText('get_schema, get_example, validate, experimental_evaluate')
    ).toBeTruthy()
    expect(screen.getByText(/consults no reviewed set and decides no outcome/)).toBeTruthy()
  })

  it('offers a paste block with no key member in it, and says why', () => {
    stubChassis({})
    const { container } = renderSection()
    const pasted = container.querySelector('figure.json code')!.textContent!
    const json = JSON.parse(pasted) as { assistant: { endpoint: Record<string, unknown> } }
    expect(Object.keys(json.assistant.endpoint).sort()).toEqual([
      'kind',
      'model',
      'tools',
      'url'
    ])
    expect(screen.getByText(/The key is not in that block/)).toBeTruthy()
  })

  it('says nothing to the reader about a chassis, bytes or a path', () => {
    stubChassis({})
    const { container } = renderSection()
    const text = container.textContent ?? ''
    for (const jargon of ['chassis', 'bytes', 'os.Root', 'endpoint handler', 'HTTP']) {
      expect(text, `the section says ${jargon} to the reader`).not.toContain(jargon)
    }
  })
})

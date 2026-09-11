/**
 * Admin › Assistant, driven against a stub of the chassis it calls.
 *
 * The assertions are about **what the page says and what it sends**, on the
 * wire: which request each control makes, what it renders from the answer, and
 * — the one that matters most — that the key the reader typed goes into the
 * store request and appears nowhere else, in no rendered text and in no
 * subsequent request.
 *
 * The form's own cases are in `endpointForm.test.tsx`; what is here about the
 * form is that it is *on* the section, and that the key line and the write
 * answer meet: `keyRebindRequired` moves the line without waiting for a read.
 */
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { AssistantSection } from './AssistantSection'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function testConnection() {
  const button = await screen.findByRole('button', { name: 'Test connection' }) as HTMLButtonElement
  await waitFor(() => expect(button.disabled).toBe(false))
  fireEvent.click(button)
}

const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'
const DIGEST = 'a'.repeat(64)

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: ['get_schema', 'validate']
}

/**
 * The answer a desk with a key stored for `ENDPOINT` gives.
 *
 * **`bound` and `configuredOrigin` are the chassis' own**, so these fixtures
 * state them rather than deriving them: the page no longer computes either,
 * and a fixture that computed them would be testing the arithmetic this change
 * removed. The Go side exercises both spellings of a default port in
 * `TestKeyReadCarriesThisDesksOwnBindingVerdict`.
 */
const BOUND = {
  present: true,
  fingerprint: 'sk-a…wxyz',
  origin: 'https://api.example.invalid',
  kind: 'openai-compatible',
  configuredOrigin: 'https://api.example.invalid',
  configuredKind: 'openai-compatible',
  bound: true
}

/** The same key, and a desk configured for somewhere it may not go. */
const ELSEWHERE = {
  ...BOUND,
  origin: 'https://first.example.invalid',
  bound: false
}

const NO_KEY = {
  present: false,
  fingerprint: '',
  origin: '',
  kind: '',
  configuredOrigin: 'https://api.example.invalid',
  configuredKind: 'openai-compatible',
  bound: false
}

/** No endpoint configured: the desk has no origin to name. */
const NO_ENDPOINT = { ...NO_KEY, configuredOrigin: '', configuredKind: '' }

/** One effective configuration whose desk-level file carries an endpoint. */
function configured(endpoint: unknown = ENDPOINT): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: true,
    sha256: DIGEST,
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
    sha256: '',
    note: `no desk-level configuration file at ${DESK_PATH}`
  })
}

/** Every request the page made, and a scripted answer for each route. */
function stubChassis(answers: {
  key?: {
    present: boolean
    fingerprint: string
    origin: string
    kind: string
    configuredOrigin: string
    configuredKind?: string
    bound: boolean
  }
  keyStatus?: number
  keyError?: { error: string; code: string }
  probe?: unknown
  probeStatus?: number
  /** What the endpoint's own model listing answers, for the other half. */
  listing?: unknown
  /** What `PUT /api/desk-config` answers, where a case saves. */
  written?: unknown
  writtenStatus?: number
  /** Called synchronously, inside `fetch`, before anything is awaited. */
  onRequest?: (method: string, url: string) => void
}): { sent: { method: string; url: string; body?: string }[] } {
  const sent: { method: string; url: string; body?: string }[] = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    sent.push({ method, url, body: init?.body as string | undefined })
    // **Before any await.** The question is what the page holds at the instant
    // the request begins, not what it holds once the answer has come back and
    // a render has flushed.
    answers.onRequest?.(method, url)
    if (url.includes('/api/assistant/relay/')) {
      // The listing half of one press. This file is about the probe and the
      // key; what the list does with the rows is `endpointForm.test.tsx`'s.
      return new Response(JSON.stringify(answers.listing ?? { data: [] }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    }
    if (url.includes('/api/assistant/probe')) {
      return {
        ok: (answers.probeStatus ?? 200) < 400,
        status: answers.probeStatus ?? 200,
        statusText: '',
        text: async () => JSON.stringify(answers.probe ?? {})
      }
    }
    if (url.includes('/api/desk-config')) {
      const status = answers.writtenStatus ?? 200
      return {
        ok: status < 400,
        status,
        statusText: '',
        text: async () => JSON.stringify(answers.written ?? {})
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
          ? BOUND
          : method === 'DELETE'
            ? NO_KEY
            : (answers.key ?? NO_KEY)
      return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(state) }
    }
    return { ok: true, status: 200, statusText: '', text: async () => '{}' }
  })
  return { sent }
}

function renderSection(
  value: EffectiveConfig = unconfigured(),
  client = testQueryClient()
) {
  return render(
    <QueryClientProvider client={client}>
      <DeskConfigFixture value={value}>
        <AssistantSection id="assistant" title="Assistant" />
      </DeskConfigFixture>
    </QueryClientProvider>
  )
}

/** The one password field, which is the key's and nothing else's. */
function keyField(container: HTMLElement): HTMLInputElement | null {
  return container.querySelector('input[type="password"]')
}

/** Every value React Query is currently holding on behalf of a mutation. */
function retainedVariables(client: QueryClient): unknown[] {
  return client
    .getMutationCache()
    .getAll()
    .map((mutation) => mutation.state.variables)
    .filter((variables) => variables !== undefined)
}

describe('the Assistant section', () => {
  it('renders as a card: the file it is in, its state, and no paragraph', () => {
    // The three paragraphs that stood here — a standing sentence, three
    // deployment states and a note about the one branching member — were prose
    // about a slot the card now states in four facts. What is left is the
    // states, each of which is one answer out of a fixed set.
    stubChassis({})
    const { container } = renderSection()
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('Assistant')
    const keys = Array.from(container.querySelectorAll('dt')).map((each) => each.textContent)
    expect(keys).toEqual(['Location', 'Status'])
    expect(screen.getByText(DESK_PATH)).toBeTruthy()
    expect(screen.getByText('not present — defaults in use')).toBeTruthy()
    // Still not three shapes: the form has one endpoint, and the deployment
    // states were never a choice on it.
    expect(screen.queryByRole('radio')).toBeNull()
  })

  it('asks for the key where a person would look for it, and says where it lives', () => {
    // **Second on the form, after the provider**, because that is the order
    // somebody setting this up works in — and the hint answers the question the
    // old wording left them to guess at.
    stubChassis({ key: BOUND })
    const { container } = renderSection()
    const labels = Array.from(container.querySelectorAll('label')).map((each) => each.textContent)
    expect(labels.slice(0, 3)).toEqual(['Provider', 'API key', 'Endpoint URL'])
    expect(
      screen.getByText('Stored on this computer only, never in the project. Readable by your user account only.')
    ).toBeTruthy()
  })

  it('still reports a stored key where no endpoint is configured', async () => {
    stubChassis({ key: BOUND })
    renderSection()
    expect(await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove key' })).toBeTruthy()
  })

  it('says the key is not read yet before the chassis has answered', () => {
    // A read that has not answered is not "no key stored": it is a page that
    // has not been told, and saying otherwise is stating what was not observed.
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderSection()
    expect(screen.getByText('Not read yet')).toBeTruthy()
  })

  it('reports a stored key by its fingerprint, and offers to remove it', async () => {
    stubChassis({ key: BOUND })
    renderSection()
    expect(await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove key' })).toBeTruthy()
  })

  it('offers no removal where there is nothing to remove', async () => {
    stubChassis({ key: NO_KEY })
    renderSection()
    expect(await screen.findByText('No key stored')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Remove key' })).toBeNull()
  })

  it('says a key is present but too short to show, rather than showing a blank', async () => {
    // `present` with an empty fingerprint is a real state — a key of eight
    // characters would be disclosed in full by four-and-four — and rendering
    // it as a stored key with nothing beside it looks like a bug.
    stubChassis({ key: { ...BOUND, fingerprint: '' } })
    renderSection()
    expect(
      await screen.findByText(/too short to show any of it without showing all of it/)
    ).toBeTruthy()
  })

  it('sends the typed key on a store, and never renders it afterwards', async () => {
    const { sent } = stubChassis({ key: NO_KEY })
    const { container } = renderSection(configured())
    await screen.findByText('No key stored')

    const field = keyField(container)!
    expect(field.type).toBe('password')
    fireEvent.change(field, { target: { value: 'sk-a-real-looking-key-wxyz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))

    await waitFor(() =>
      expect(sent.some((request) => request.url.includes('/api/assistant/key') && request.method === 'PUT')).toBe(true)
    )
    const put = sent.find(
      (request) => request.url.includes('/api/assistant/key') && request.method === 'PUT'
    )!
    expect(JSON.parse(put.body!)).toEqual({ key: 'sk-a-real-looking-key-wxyz' })

    // The field is cleared and the page shows the fingerprint the chassis
    // answered with — never the value it was handed.
    await waitFor(() => expect(field.value).toBe(''))
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    expect(await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')).toBeTruthy()
  })

  it('removes a key on request, and says so', async () => {
    const { sent } = stubChassis({ key: BOUND })
    renderSection(configured())
    fireEvent.click(await screen.findByRole('button', { name: 'Remove key' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm removal' }))
    await waitFor(() => expect(sent.some((request) => request.method === 'DELETE')).toBe(true))
    expect(await screen.findByText('No key stored')).toBeTruthy()
  })

  it('has emptied the field at the instant the request is made', async () => {
    // **The assertion that was missing.** The old one read the field after
    // `fireEvent` had flushed a render, which says nothing about the moment
    // `fetch` was called — and React batches state updates, so a `setState`
    // immediately before the request had not taken effect when it began. The
    // field is uncontrolled and cleared on the node now, and this looks at it
    // from inside `fetch`.
    let atTheRequest: string | undefined
    let container: HTMLElement | undefined
    stubChassis({
      key: NO_KEY,
      onRequest: (method, url) => {
        if (method !== 'PUT' || !url.includes('/api/assistant/key')) return
        atTheRequest = keyField(container!)?.value
      }
    })
    const rendered = renderSection(configured())
    container = rendered.container
    await screen.findByText('No key stored')
    const field = keyField(container)!
    fireEvent.change(field, { target: { value: 'sk-a-real-looking-key-wxyz' } })
    expect(field.value).toBe('sk-a-real-looking-key-wxyz')
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))

    await waitFor(() => expect(atTheRequest).toBeDefined())
    expect(atTheRequest, 'the field still held the key when the request began').toBe('')
  })

  it('clears the field before the request, so a failure retains nothing', async () => {
    // **The failure case, which is the one that mattered.** The field was
    // cleared by the success callback only, so a network error or a refusal
    // left the plaintext sitting in a password input and in React state until
    // somebody noticed. It is copied and cleared synchronously now, before
    // the request is made.
    stubChassis({
      key: NO_KEY,
      keyError: { error: 'a key may not contain a control character', code: 'bad-request' }
    })
    const { container } = renderSection(configured())
    await screen.findByText('No key stored')
    const field = keyField(container)!
    fireEvent.change(field, { target: { value: 'sk-a-real-looking-key-wxyz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))

    // Empty immediately, not after the answer arrives.
    expect(field.value).toBe('')
    expect(await screen.findByText(/a key may not contain a control character/)).toBeTruthy()
    // And nowhere in the rendered page either.
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    expect(field.value).toBe('')
  })

  it('retains the key in no mutation, on success or on failure', async () => {
    // **Where a credential lives is the question, and React Query is a place
    // it can live.** A mutation keeps what it was called with for as long as
    // its state does, so a failed store left the plaintext in the mutation
    // cache beside the error. The key is not a mutation variable at all now —
    // resetting the observer did not clear the cache's copy — and this reads
    // the cache rather than trusting either arrangement.
    for (const failing of [false, true]) {
      const client = testQueryClient()
      stubChassis(
        failing
          ? { key: NO_KEY, keyError: { error: 'refused', code: 'bad-request' } }
          : { key: NO_KEY }
      )
      const { container } = renderSection(configured(), client)
      await screen.findByText('No key stored')
      fireEvent.change(keyField(container)!, {
        target: { value: 'sk-a-real-looking-key-wxyz' }
      })
      fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
      await waitFor(() =>
        expect(
          retainedVariables(client),
          failing ? 'after a failed store' : 'after a successful store'
        ).toEqual([])
      )
      // And nowhere in the cache's serialized state either.
      expect(JSON.stringify(client.getMutationCache().getAll())).not.toContain(
        'sk-a-real-looking-key-wxyz'
      )
      cleanup()
    }
  })

  it('reports a refused store as one that did not happen', async () => {
    stubChassis({
      key: NO_KEY,
      keyError: { error: 'a key may not contain a control character', code: 'bad-request' }
    })
    const { container } = renderSection(configured())
    await screen.findByText('No key stored')
    fireEvent.change(keyField(container)!, { target: { value: 'bad\nkey' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
    expect(await screen.findByText(/a key may not contain a control character/)).toBeTruthy()
    // And the page still says no key is stored, because none is.
    expect(screen.getByText('No key stored')).toBeTruthy()
  })

  it('asks the desk to probe, sending no destination of its own', async () => {
    // The request carries nothing but the token. If it named a URL, anything
    // holding the token could point the desk — and the key it holds — at a
    // host of its choosing.
    const { sent } = stubChassis({
      key: BOUND,
      probe: { reachable: true, status: 200, latencyMs: 240, diagnostic: '' }
    })
    renderSection(configured())
    await testConnection()
    await waitFor(() =>
      expect(sent.some((request) => request.url.includes('/api/assistant/probe'))).toBe(true)
    )
    const probe = sent.find((request) => request.url.includes('/api/assistant/probe'))!
    expect(probe.method).toBe('POST')
    expect(probe.body).toBeUndefined()
    expect(probe.url).not.toContain('api.example.invalid')
  })

  it('says connected and how many models the endpoint has, on one press', async () => {
    // **Two requests and one line.** The probe says whether the address and the
    // credential work; the listing says what there is to enable. They are one
    // question to a reader, so they are one button.
    const { sent } = stubChassis({
      key: BOUND,
      probe: { reachable: true, status: 200, latencyMs: 240, diagnostic: '' },
      listing: { data: [{ id: 'a-model' }, { id: 'a-second-model' }] }
    })
    renderSection(configured())
    await testConnection()
    expect(await screen.findByText('Connected · 2 models available')).toBeTruthy()
    await waitFor(() =>
      expect(sent.filter((each) => each.url.includes('/api/assistant/relay/'))).toHaveLength(1)
    )
  })

  it('renders a refused credential as not reachable, from the fixed vocabulary', async () => {
    // **Not the endpoint's own sentence.** It used to be quoted verbatim with
    // the key substituted out; a body under the endpoint's control can carry
    // a derived representation of the credential that no substitution finds,
    // so the desk discards the body and answers one word from a closed list.
    stubChassis({
      key: BOUND,
      probe: { reachable: false, status: 401, latencyMs: 88, diagnostic: 'unauthorized' }
    })
    renderSection(configured())
    await testConnection()
    expect(
      await screen.findByText(/Not connected · answered 401 · the endpoint did not accept the key/)
    ).toBeTruthy()
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
      stubChassis({ key: BOUND, probe: { reachable: false, status: 0, latencyMs: 5, diagnostic } })
      renderSection(configured())
      await testConnection()
      expect(await screen.findByText(new RegExp(says)), diagnostic).toBeTruthy()
      cleanup()
    }
  })

  it('says no answer arrived where the status is zero', async () => {
    stubChassis({
      key: BOUND,
      probe: {
        reachable: false,
        status: 0,
        latencyMs: 10_000,
        diagnostic: 'timeout'
      }
    })
    renderSection(configured())
    await testConnection()
    expect(await screen.findByText(/no answer arrived/)).toBeTruthy()
    // Never "answered 0", which is a status nothing sends.
    expect(screen.queryByText(/answered 0/)).toBeNull()
  })

  it('reports a probe the desk refused, naming which state refused it', async () => {
    stubChassis({
      key: BOUND,
      probeStatus: 409,
      probe: {
        error: 'no key is stored on this machine, so there is nothing to present to the endpoint',
        code: 'assistant-no-key'
      }
    })
    renderSection(configured())
    await testConnection()
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
  })

  it('names the five tools it may be given, as five grants and not as prose', async () => {
    stubChassis({})
    renderSection(configured())
    for (const tool of [
      'get_schema',
      'list_examples',
      'get_example',
      'validate',
      'experimental_evaluate'
    ]) {
      expect(screen.getByRole('checkbox', { name: tool }), tool).toBeTruthy()
    }
    expect(screen.getByText('All read-only. Untick one to hide it from the assistant.')).toBeTruthy()
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

describe('the key row and the endpoint it is bound to', () => {
  it('offers an explicit key save that saves the endpoint first', async () => {
    // Storing a key requires an endpoint to bind it to, so a second button here
    // could only produce a refusal. The field itself stays, because Connect —
    // the primary action — saves the endpoint first and then stores it.
    stubChassis({ key: NO_ENDPOINT })
    const { container } = renderSection()
    await screen.findByText('No key stored')
    expect(screen.getByText(/Save API key saves the endpoint first/)).toBeTruthy()
    expect(keyField(container)).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Save API key' })).toBeTruthy()
  })

  it('names the key by what it is, and never by the host it goes to', async () => {
    // The host is on the line below, where a mismatch names both halves. A
    // label that carried it made the one field on the form read as five.
    stubChassis({ key: NO_KEY })
    renderSection(configured())
    expect(await screen.findByText('No key stored')).toBeTruthy()
    expect(screen.getByLabelText('API key')).toBeTruthy()
    expect(screen.queryByText(/No key is stored for/)).toBeNull()
  })

  it('says a stored key is stored, with its fingerprint and its provider', async () => {
    stubChassis({ key: BOUND })
    const { container } = renderSection(configured())
    expect(await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')).toBeTruthy()
    // **And there is no field at all.** An empty masked box beside a working
    // key invites somebody to wonder what is in it and to type into it by
    // accident; what a person wants there is Replace and Remove.
    expect(keyField(container)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Save API key' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Replace key' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove key' })).toBeTruthy()
  })

  it('opens the one field there is when a replacement is asked for', async () => {
    stubChassis({ key: BOUND })
    const { container } = renderSection(configured())
    fireEvent.click(await screen.findByRole('button', { name: 'Replace key' }))
    expect(keyField(container)).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Save API key' })).toBeTruthy()
  })

  it('names both hosts where the stored key was entered for another one', async () => {
    // Both halves, because a reader has to be able to see which of the two
    // moved — the endpoint they just saved, or a key entered for elsewhere.
    stubChassis({ key: ELSEWHERE })
    const { container } = renderSection(configured())
    expect(await screen.findByText(/nothing will be sent/)).toBeTruthy()
    expect(screen.getByText('https://first.example.invalid')).toBeTruthy()
    expect(screen.getByLabelText('API key')).toBeTruthy()
    expect(keyField(container)).not.toBeNull()
  })

  it('reads the binding off the desk s verdict and computes none of its own', async () => {
    // The origin and the kind here are exactly the configured endpoint's, so
    // any comparison this page could write would say "bound". The desk says
    // otherwise — which is the case the browser's own URL folding could never
    // have produced — and the row reports what it was told.
    stubChassis({ key: { ...BOUND, bound: false } })
    renderSection(configured())
    expect(await screen.findByText(/nothing will be sent/)).toBeTruthy()
  })

  it('asks for the key again the moment a write says the endpoint moved', async () => {
    // **Without waiting for a read.** The chassis says `keyRebindRequired` at
    // the instant the endpoint moves; a row that waited for the key query to
    // be re-fetched would go on saying the key is bound for as long as that
    // took, which is the page reporting a state it has been told is false.
    stubChassis({
      key: BOUND,
      written: {
        path: DESK_PATH,
        sha256: 'b'.repeat(64),
        assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' },
        created: false,
        keyRebindRequired: true
      }
    })
    renderSection(configured())
    expect(await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    expect(await screen.findByText(/nothing will be sent/)).toBeTruthy()
  })

  it('stops asking once a key has been stored for the new endpoint', async () => {
    stubChassis({
      key: BOUND,
      written: {
        path: DESK_PATH,
        sha256: 'b'.repeat(64),
        assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' },
        created: false,
        keyRebindRequired: true
      }
    })
    const { container } = renderSection(configured())
    await screen.findByText('Stored — sk-a…wxyz, for OpenAI-compatible')
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await screen.findByText(/nothing will be sent/)
    fireEvent.change(keyField(container)!, { target: { value: 'sk-another-real-looking-key' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
    // The store answers with the binding the chassis now holds, and the line
    // goes back to reading it.
    await waitFor(() => expect(screen.queryByText(/nothing will be sent/)).toBeNull())
  })
})

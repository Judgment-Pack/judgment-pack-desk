/**
 * The form, driven against a stub of the one route it writes to.
 *
 * **Three properties are why this file exists**, and each is asserted on the
 * wire rather than in the rendering:
 *
 * - **the digest goes with the write**, so a file somebody edited between this
 *   page's read and its Save refuses the write rather than losing their edit;
 * - **the body carries the members the schema declares and nothing else**,
 *   whatever anybody typed into a field;
 * - **a refusal is the decoder's sentence**, rendered against the field its key
 *   path names, and a stale write keeps every value that was typed.
 *
 * **Connect is the fourth**, and it is an ordering rather than a route: the
 * endpoint write, and then the key store, with each refusal stopping what comes
 * after it.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { narrationIn } from '../admin/narration'
import type { AssistantKeyState } from './client'
import { EndpointForm } from './EndpointForm'
import { PREFILLED_URL } from './endpointDraft'

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
const NEXT = 'b'.repeat(64)

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: ['get_schema', 'validate']
}

const WRITTEN = {
  path: DESK_PATH,
  sha256: NEXT,
  assistant: { endpoint: ENDPOINT, engine: 'vercel', thinking: 'off' },
  created: false,
  keyRebindRequired: false
}

/**
 * What the key route answers, for a suite that drives a form the key row is now
 * part of.
 *
 * It is the chassis' own verdict and never computed here — which is the rule
 * the row itself keeps — so the fixture states `bound` rather than deriving it
 * from the two origins beside it.
 */
function keyState(bound: boolean, present = bound): AssistantKeyState {
  return {
    present,
    fingerprint: present ? 'sk-a…wxyz' : '',
    origin: present ? 'https://api.example.invalid' : '',
    kind: present ? 'openai-compatible' : '',
    configuredOrigin: 'https://api.example.invalid',
    configuredKind: 'openai-compatible',
    bound
  }
}

/** The answer the stubs below give the key route, set by `renderForm`. */
let keyAnswer: AssistantKeyState = keyState(true)

/** How the key store answered last, and what it was sent. */
let keyStore: { status: number; body?: unknown } = { status: 200 }

function configured(
  assistant: unknown = { endpoint: ENDPOINT },
  sha256: string | undefined = DIGEST
): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: true,
    sha256,
    decoded: decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, assistant }), 'desk')
  })
}

/** No file yet: the digest is the empty string, which is a digest a write may state. */
function noFile(): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: false,
    sha256: '',
    note: 'no desk-level configuration file'
  })
}

/** The desk-level read never answered, so this page has no digest at all. */
function unread(): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: false,
    readFailure: { reason: 'nothing answered', responseReceived: false, source: 'browser' }
  })
}

function stubWrites(
  answers: { status?: number; body?: unknown }[]
): { sent: { url: string; method: string; body?: string }[] } {
  const sent: { url: string; method: string; body?: string }[] = []
  let index = 0
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    sent.push({ url, method, body: init?.body as string | undefined })
    if (url.includes('/api/assistant/key')) {
      if (method === 'GET') {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      return {
        ok: keyStore.status < 400,
        status: keyStore.status,
        statusText: '',
        text: async () => JSON.stringify(keyStore.body ?? keyState(true))
      }
    }
    if (!url.includes('/api/desk-config')) {
      return { ok: true, status: 200, statusText: '', text: async () => '{}' }
    }
    const answer = answers[Math.min(index, answers.length - 1)]!
    index += 1
    const status = answer.status ?? 200
    return {
      ok: status < 400,
      status,
      statusText: '',
      text: async () => JSON.stringify(answer.body ?? WRITTEN)
    }
  })
  return { sent }
}

function renderForm(
  value: EffectiveConfig = configured(),
  bound = true,
  unavailable = false
) {
  keyAnswer = keyState(bound)
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigFixture value={value}>
        <EndpointForm unavailable={unavailable} />
      </DeskConfigFixture>
    </QueryClientProvider>
  )
}

/** The primary action, whatever it is called in the state under test. */
const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
const saveApiKey = () => fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
const typeKey = (value: string) =>
  fireEvent.change(screen.getByLabelText('API key'), { target: { value } })

/** The one write this suite is ever about. */
function theWrite(sent: { url: string; method: string; body?: string }[]) {
  const put = sent.find(
    (request) => request.url.includes('/api/desk-config') && request.method === 'PUT'
  )
  expect(put, 'the form wrote the desk-level configuration').toBeDefined()
  return JSON.parse(put!.body!) as { assistant: Record<string, unknown>; ifMatch: string }
}

describe('what a save sends', () => {
  it('states the digest this page read, and names no file', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(body.ifMatch).toBe(DIGEST)
    // Two members and no path: this route writes one file, and a body that
    // could name another would be a way to write anywhere with the desk's own
    // authority.
    expect(Object.keys(body).sort()).toEqual(['assistant', 'ifMatch'])
  })

  it('states the empty digest where the read said there is no file', async () => {
    const { sent } = stubWrites([{ body: { ...WRITTEN, created: true } }])
    renderForm(noFile())
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://api.example.invalid/v1' }
    })
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect(theWrite(sent).ifMatch).toBe('')
    expect(await screen.findByText(/the file was created/)).toBeTruthy()
  })

  it('writes nothing at all where this page never learned the digest', async () => {
    const { sent } = stubWrites([{}])
    renderForm(unread())
    expect((screen.getByRole('button', { name: 'Save settings' }) as HTMLButtonElement).disabled).toBe(
      true
    )
    expect(screen.getByText(/a write states the bytes it replaces/)).toBeTruthy()
    save()
    await Promise.resolve()
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('carries no key-shaped member even where a field is filled with one', async () => {
    // The fields are the schema's six; the body is composed by naming them.
    // Typing `apiKey` into one puts the *text* into a member the schema
    // declares, and creates no member of that name anywhere.
    const { sent } = stubWrites([{ status: 422, body: { error: 'refused', code: 'desk-config-refused', problems: [] } }])
    renderForm()
    fireEvent.change(screen.getByLabelText('Other model… (type an id)'), {
      target: { value: 'apiKey' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(Object.keys(body.assistant).sort()).toEqual(['endpoint', 'thinking'])
    expect(
      Object.keys(body.assistant.endpoint as Record<string, unknown>).sort()
    ).toEqual(['kind', 'model', 'models', 'tools', 'url'])
    // The text is where it was typed, and it is not a member name.
    expect((body.assistant.endpoint as { models: string[] }).models).toContain('apiKey')
    const names = [...JSON.stringify(body).matchAll(/"([^"]+)":/g)].map((match) => match[1])
    expect(names).not.toContain('apiKey')
  })

  it('sends the tools that are ticked, and an empty list where none is', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    for (const tool of ['get_schema', 'validate']) {
      fireEvent.click(screen.getByRole('checkbox', { name: tool }))
    }
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect((theWrite(sent).assistant.endpoint as { tools: string[] }).tools).toEqual([])
  })
})

describe('the fields', () => {
  it('opens on the file, and takes a later read while nothing has been typed', async () => {
    // The desk-level read has usually not answered at first render.
    stubWrites([{}])
    const { rerender } = render(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={effectiveConfig(undefined)}>
          <EndpointForm unavailable={false} />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe(PREFILLED_URL['openai-compatible'])
    rerender(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={configured()}>
          <EndpointForm unavailable={false} />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe(ENDPOINT.url)
    )
  })

  it('offers the address a protocol documents when the protocol is chosen', async () => {
    stubWrites([{}])
    renderForm(noFile())
    fireEvent.click(screen.getByRole('combobox', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Google Gemini' }))
    await waitFor(() =>
      expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe(
        PREFILLED_URL.gemini
      )
    )
  })

  it('refuses a URL in the decoder s own words, and sends nothing', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'http://api.example.invalid/v1' }
    })
    // The sentence is the file reader's, verbatim — not a paraphrase written
    // for the form.
    expect(
      await screen.findByText(/a key sent in clear text over a network is a key given away/)
    ).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save settings' }) as HTMLButtonElement).disabled).toBe(
      true
    )
    save()
    await Promise.resolve()
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('saves an endpoint with no model chosen, as the null the schema spells', async () => {
    // **The whole order this form is in depends on this save going through.** A
    // model is picked from the list the endpoint itself offers, and that list
    // cannot be read until there is an endpoint saved and a key bound to it —
    // so the first save has to be allowed to name no model at all. `null` and
    // not `''`: the empty string is a value the decoder refuses, so writing it
    // would compose a file this page's own reader rejects.
    const { sent } = stubWrites([{ body: { ...WRITTEN, created: true } }])
    renderForm(noFile())
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://api.example.invalid/v1' }
    })
    expect((screen.getByRole('button', { name: 'Save settings' }) as HTMLButtonElement).disabled).toBe(false)
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect((theWrite(sent).assistant.endpoint as { model: unknown }).model).toBeNull()
  })

  it('enables a model that was typed, trimmed, exactly as it was spelled', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    fireEvent.change(screen.getByLabelText('Other model… (type an id)'), {
      target: { value: '  a-typed-model  ' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect((theWrite(sent).assistant.endpoint as { models: string[] }).models).toEqual([
      'a-model',
      'a-typed-model'
    ])
  })

  it('refuses a configured query the relay reserves, in the same words', async () => {
    stubWrites([{}])
    renderForm()
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://api.example.invalid/v1?alt=sse' }
    })
    expect(
      await screen.findByText(/a name the relay itself may add/)
    ).toBeTruthy()
  })

  it('offers exactly the tiers the file admits', async () => {
    stubWrites([{}])
    renderForm()
    fireEvent.click(screen.getByRole('combobox', { name: 'Thinking' }))
    const offered = (await screen.findAllByRole('option')).map((option) => option.textContent)
    expect(offered).toEqual(['off', 'standard', 'deep'])
  })

})

describe('a write the desk refused', () => {
  it('shows each problem against the field its key path names', async () => {
    stubWrites([
      {
        status: 422,
        body: {
          error: 'the configuration this would write is not one this desk reads',
          code: 'desk-config-refused',
          problems: [
            { key: 'assistant.endpoint.model', reason: 'must be a non-empty string' },
            {
              key: 'assistant.endpoint.models',
              reason: 'must be an array of strings; found null'
            },
            { key: 'assistant.endpoint.url', reason: 'must be an absolute URL' },
            { key: 'assistant.endpoint.tools', reason: 'is not a tool the assistant may call' }
          ]
        }
      }
    ])
    renderForm()
    save()
    // Beside the field, in the decoder's own sentence.
    const url = await screen.findByLabelText('Endpoint URL')
    await waitFor(() => expect(url.getAttribute('aria-describedby')).toBeTruthy())
    expect(url.getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByText('must be an absolute URL')).toBeTruthy()
    // The set and its default are both members of this form, and each lands in
    // the Models list rather than being rendered whole with no field.
    expect(screen.getByText('must be a non-empty string')).toBeTruthy()
    expect(screen.getByText('must be an array of strings; found null')).toBeTruthy()
    expect(screen.getByText('is not a tool the assistant may call')).toBeTruthy()
    expect(screen.queryByText(/This configuration was refused/)).toBeNull()
  })

  it('renders whole any problem whose field is not on this form', async () => {
    // A problem with no field here is still a problem with the file this write
    // would have made; dropping it would leave a refusal with no sentence.
    stubWrites([
      {
        status: 422,
        body: {
          error: 'refused',
          code: 'desk-config-refused',
          problems: [{ key: 'identity.provider.issuer', reason: 'must be an https URL' }]
        }
      }
    ])
    renderForm()
    save()
    expect(await screen.findByText(/identity.provider.issuer: must be an https URL/)).toBeTruthy()
  })

  it('says a refusal that carries no problems in the desk s own sentence', async () => {
    stubWrites([
      {
        status: 409,
        body: {
          error: 'the key on this machine was entered for somewhere else',
          code: 'assistant-key-unbound'
        }
      }
    ])
    renderForm()
    save()
    expect(await screen.findByText(/entered for somewhere else/)).toBeTruthy()
  })
})

describe('a file that moved underneath the page', () => {
  it('says nothing was written, offers Reload, and offers no overwrite', async () => {
    stubWrites([
      {
        status: 409,
        body: {
          error: 'the desk-level configuration on disk is not the one this page read',
          code: 'desk-config-changed',
          path: DESK_PATH,
          expectedSha256: DIGEST,
          actualSha256: NEXT,
          exists: true
        }
      }
    ])
    renderForm()
    save()
    expect(await screen.findByText(/changed on disk. Nothing was written/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy()
    // There is no override on this route, and offering one would be a client
    // with an unstated concurrency story.
    expect(screen.queryByRole('button', { name: /Overwrite/ })).toBeNull()
  })

  it('keeps every value that was typed when Reload takes the file again', async () => {
    stubWrites([
      {
        status: 409,
        body: {
          error: 'changed',
          code: 'desk-config-changed',
          path: DESK_PATH,
          expectedSha256: DIGEST,
          actualSha256: NEXT,
          exists: true
        }
      }
    ])
    renderForm()
    fireEvent.change(screen.getByLabelText('Other model… (type an id)'), {
      target: { value: 'a-model-being-chosen' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    save()
    await screen.findByText(/changed on disk. Nothing was written/)
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() =>
      expect(screen.queryByText(/changed on disk. Nothing was written/)).toBeNull()
    )
    // The whole point: the read is taken again and the draft is not.
    expect(
      (screen.getByRole('checkbox', { name: 'a-model-being-chosen' }) as HTMLInputElement).checked
    ).toBe(true)
  })
})

describe('Test connection: the probe and the listing, in one press', () => {
  /** A `fetch` that answers both halves and records what it was asked. */
  function servesCheck(
    listing: unknown,
    options: { listingStatus?: number; probe?: unknown; probeStatus?: number } = {}
  ): { urls: string[]; inits: RequestInit[] } {
    const urls: string[] = []
    const inits: RequestInit[] = []
    vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
      urls.push(url)
      inits.push(init)
      if (url.includes('/api/assistant/key')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      if (url.includes('/api/assistant/probe')) {
        const status = options.probeStatus ?? 200
        return {
          ok: status < 400,
          status,
          statusText: '',
          text: async () =>
            JSON.stringify(
              options.probe ?? { reachable: true, status: 200, latencyMs: 12, diagnostic: '' }
            )
        }
      }
      if (!url.includes('/api/assistant/relay/')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(WRITTEN) }
      }
      return new Response(JSON.stringify(listing), {
        status: options.listingStatus ?? 200,
        headers: { 'content-type': 'application/json' }
      })
    })
    return { urls, inits }
  }

  const GEMINI = configured({
    endpoint: { ...ENDPOINT, kind: 'gemini', url: 'https://api.example.invalid' }
  })
  const LISTED = {
    models: [
      {
        name: 'models/gemini-2.5-pro',
        displayName: 'Gemini 2.5 Pro',
        supportedGenerationMethods: ['generateContent']
      },
      {
        name: 'models/gemini-2.5-flash',
        displayName: 'Gemini 2.5 Flash',
        supportedGenerationMethods: ['generateContent']
      }
    ]
  }
  /** Press it, once the key read has answered and the button is offered. */
  const test = async () =>
    await testConnection()
  const relayed = (urls: string[]) => urls.filter((url) => url.includes('/api/assistant/relay/'))
  const probed = (urls: string[]) => urls.filter((url) => url.includes('/api/assistant/probe'))

  it('reads down in the order somebody sets one of these up', async () => {
    // **The labels, in document order.** Test connection sits right after the
    // key and the address, because that is the moment a person has the
    // question it answers — and Models sits under it, because the answer is
    // what fills the list.
    const named = (container: HTMLElement, selector: string) =>
      Array.from(container.querySelectorAll(selector))
        .map((each) => each.textContent?.trim())
        .filter((text): text is string => text !== undefined && text !== '')

    // With no key stored, the key's own field is on the form and sits second.
    servesCheck(LISTED)
    const first = renderForm(GEMINI, false)
    await screen.findByText(/Save an API key for this endpoint to test/)
    // Each field's own label and each group's legend. The individual grants —
    // a model row, a tool box — carry `.checkbox` and are not what this reads.
    expect(named(first.container, 'legend, label:not(.checkbox)')).toEqual([
      'Provider',
      'API key',
      'Endpoint URL',
      'Models',
      'Search models',
      'Other model… (type an id)',
      'Tools the assistant may use',
      'Thinking'
    ])
    cleanup()

    // With one stored, the key row is a state line and the button is offered —
    // between the address and the list it fills in.
    servesCheck(LISTED)
    const second = renderForm(GEMINI, true)
    await screen.findByRole('button', { name: 'Test connection' })
    const order = named(second.container, 'legend, label:not(.checkbox), button')
    expect(order.indexOf('Test connection')).toBeGreaterThan(order.indexOf('Endpoint URL'))
    expect(order.indexOf('Test connection')).toBeLessThan(order.indexOf('Models'))
    expect(order.indexOf('Models')).toBeLessThan(order.indexOf('Tools the assistant may use'))
    expect(order.indexOf('Tools the assistant may use')).toBeLessThan(order.indexOf('Thinking'))
    expect(order.indexOf('Thinking')).toBeLessThan(order.indexOf('Save settings'))
  })

  it('asks nothing at all until it is pressed', async () => {
    // The listing used to arrive on its own the instant a key was stored,
    // which is this desk asking somebody's endpoint a question nobody put.
    const seen = servesCheck(LISTED)
    renderForm(GEMINI, true)
    await screen.findByRole('button', { name: 'Test connection' })
    expect(relayed(seen.urls)).toHaveLength(0)
    expect(probed(seen.urls)).toHaveLength(0)
  })

  it('makes both requests on one press, and names no destination of its own', async () => {
    const seen = servesCheck(LISTED)
    renderForm(GEMINI, true)
    await test()
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
    await waitFor(() => expect(probed(seen.urls)).toHaveLength(1))
    // The listing is a suffix through the relay; the probe carries nothing.
    expect(relayed(seen.urls)[0]).toContain('/api/assistant/relay/v1/v1beta/models')
    expect(relayed(seen.urls)[0]).not.toContain('api.example.invalid')
    expect(probed(seen.urls)[0]).not.toContain('api.example.invalid')
  })

  it('says connected and how many models there are', async () => {
    servesCheck(LISTED)
    renderForm(GEMINI, true)
    await test()
    expect(await screen.findByText('Connected · 2 models available')).toBeTruthy()
  })

  it('says the endpoint listed none rather than leaving the count unsaid', async () => {
    servesCheck({ models: [] })
    renderForm(GEMINI, true)
    await test()
    expect(await screen.findByText('Connected · the endpoint listed no models')).toBeTruthy()
  })

  it('reports a probe that did not reach, in the probe s own words', async () => {
    servesCheck(LISTED, {
      probe: { reachable: false, status: 401, latencyMs: 88, diagnostic: 'unauthorized' }
    })
    renderForm(GEMINI, true)
    await test()
    expect(
      await screen.findByText('Not connected · answered 401 · the endpoint did not accept the key')
    ).toBeTruthy()
  })

  it('reports a listing that was refused, beside a probe that reached', async () => {
    servesCheck({ error: 'sk-a-real-looking-key-wxyz' }, { listingStatus: 401 })
    const { container } = renderForm(GEMINI, true)
    await test()
    expect(await screen.findByText(/the models could not be listed/)).toBeTruthy()
    expect(await screen.findByText(/the endpoint did not accept the key/)).toBeTruthy()
    // Nothing the endpoint wrote is repeated, on either half.
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
  })

  it('reports a probe route the desk refused, in the desk s own sentence', async () => {
    servesCheck(LISTED, {
      probeStatus: 409,
      probe: {
        error: 'no key is stored on this machine, so there is nothing to present to the endpoint',
        code: 'assistant-no-key'
      }
    })
    renderForm(GEMINI, true)
    await test()
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
  })

  it('refuses by name where the protocol would have to name a model and none is', async () => {
    // **Anthropic's probe is a generation call.** With nothing enabled it would
    // put a request naming the empty string on the wire and read whatever came
    // back as a verdict about the endpoint. It is not one.
    const seen = servesCheck(LISTED)
    renderForm(
      configured({
        endpoint: {
          ...ENDPOINT,
          kind: 'anthropic',
          url: 'https://api.example.invalid',
          model: null,
          models: []
        }
      }),
      true
    )
    await test()
    expect(await screen.findByText('Choose a model to test this provider.')).toBeTruthy()
    // And nothing at all left this page.
    expect(probed(seen.urls)).toHaveLength(0)
    expect(relayed(seen.urls)).toHaveLength(0)
  })

  it('is disabled before there is an endpoint saved and a key stored', async () => {
    servesCheck(LISTED)
    renderForm(GEMINI, false)
    expect(await screen.findByText(/Save an API key for this endpoint to test/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('is disabled while the form says another endpoint', async () => {
    servesCheck(LISTED)
    renderForm(GEMINI, true)
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    expect(await screen.findByText(/this asks the endpoint that is saved/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('drops the answer when the endpoint the form says moves', async () => {
    // A reading left standing after a provider or a URL changed is a reading of
    // somewhere else, offered against a form that no longer says that host.
    servesCheck(LISTED)
    renderForm(GEMINI, true)
    await test()
    await screen.findByText('Connected · 2 models available')
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    await waitFor(() => expect(screen.queryByText(/models available/)).toBeNull())
  })

  it('does not resurrect an answer when the endpoint is changed away and back', async () => {
    // **Hidden is not cleared.** Rows kept in state while the identity differed
    // came back the moment the URL was typed back — a stale listing on screen
    // with no request behind it and nothing saying when it was taken.
    const seen = servesCheck(LISTED)
    renderForm(GEMINI, true)
    await test()
    await screen.findByText('Connected · 2 models available')
    const original = (screen.getByLabelText('Endpoint URL') as HTMLInputElement).value
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    await waitFor(() => expect(screen.queryByText(/models available/)).toBeNull())
    fireEvent.change(screen.getByLabelText('Endpoint URL'), { target: { value: original } })
    // The button is back, and nothing was asked in the meantime.
    expect(await screen.findByRole('button', { name: 'Test connection' })).toBeTruthy()
    expect(screen.queryByText(/models available/)).toBeNull()
    expect(relayed(seen.urls)).toHaveLength(1)
  })

  it('does not test automatically after saving a key', async () => {
    // Saving establishes storage, not a successful connection. Testing is
    // a separate, explicit action.
    const seen = servesCheck(LISTED)
    renderForm(GEMINI, false)
    await screen.findByText(/Save an API key for this endpoint to test/)
    typeKey('sk-a-key')
    saveApiKey()
    await screen.findByText('API key saved on this computer.')
    expect(relayed(seen.urls)).toHaveLength(0)
    expect(probed(seen.urls)).toHaveLength(0)
  })
})

describe('Models: the set, the default, and an id nobody listed', () => {
  function servesListing(listing: unknown): { urls: string[]; inits: RequestInit[] } {
    const urls: string[] = []
    const inits: RequestInit[] = []
    vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
      urls.push(url)
      inits.push(init)
      if (url.includes('/api/assistant/key')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      if (url.includes('/api/assistant/probe')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({ reachable: true, status: 200, latencyMs: 12, diagnostic: '' })
        }
      }
      if (!url.includes('/api/assistant/relay/')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(WRITTEN) }
      }
      return new Response(JSON.stringify(listing), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    })
    return { urls, inits }
  }

  const LISTED = {
    data: [
      { id: 'a-listed-model' },
      { id: 'a-second-listed-model' },
      { id: 'a-third-listed-model' }
    ]
  }
  /** Press it, once the key read has answered and the button is offered. */
  const test = async () =>
    await testConnection()
  const box = (id: string) => screen.getByRole('checkbox', { name: id }) as HTMLInputElement
  const defaults = () => screen.getAllByRole('radio') as HTMLInputElement[]
  const other = () => screen.getByLabelText('Other model… (type an id)')
  const add = () => fireEvent.click(screen.getByRole('button', { name: 'Add' }))

  it('shows the file s own set before anything has been asked', async () => {
    servesListing(LISTED)
    renderForm(
      configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model', 'another'] } }),
      true
    )
    expect(box('a-model').checked).toBe(true)
    expect(box('another').checked).toBe(true)
    // And nothing the endpoint might have listed, because nothing asked it.
    expect(screen.queryByRole('checkbox', { name: 'a-listed-model' })).toBeNull()
    expect(other()).toBeTruthy()
  })

  it('fills the list from what came back, with the file s own set still on it', async () => {
    servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    await test()
    expect(await screen.findByRole('checkbox', { name: 'a-listed-model' })).toBeTruthy()
    // The saved id is not one the endpoint listed, and is still on the list:
    // dropping it would silently disable a model on the next Save.
    expect(box('a-model').checked).toBe(true)
    expect(box('a-listed-model').checked).toBe(false)
  })

  it('selecting a default also enables that model', async () => {
    servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    await test()
    await screen.findByRole('checkbox', { name: 'a-listed-model' })
    const chosen = defaults().filter((radio) => radio.checked)
    expect(chosen).toHaveLength(1)
    // A row nothing enables cannot be made the default: that is the state the
    // decoder refuses by name.
    fireEvent.click(screen.getByRole('radio', { name: 'Default model: a-listed-model' }))
    expect(box('a-listed-model').checked).toBe(true)
    expect((screen.getByRole('radio', { name: 'Default model: a-listed-model' }) as HTMLInputElement).checked).toBe(true)
  })

  it('writes the set and the default it was left with', async () => {
    const seen = servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    await test()
    await screen.findByRole('checkbox', { name: 'a-listed-model' })
    fireEvent.click(box('a-listed-model'))
    fireEvent.click(box('a-second-listed-model'))
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { model: string; models: string[] } }
    }
    expect(body.assistant.endpoint.models).toEqual([
      'a-model',
      'a-listed-model',
      'a-second-listed-model'
    ])
    expect(body.assistant.endpoint.model).toBe('a-model')
  })

  it('moves the default off a model that was just unticked', async () => {
    // A non-empty set with no default is a configuration the decoder refuses,
    // and two clicks that each looked reasonable used to compose one.
    const seen = servesListing(LISTED)
    renderForm(
      configured({
        endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model', 'a-kept-model'] }
      }),
      true
    )
    fireEvent.click(box('a-model'))
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { model: string | null; models: string[] } }
    }
    expect(body.assistant.endpoint.models).toEqual(['a-kept-model'])
    expect(body.assistant.endpoint.model).toBe('a-kept-model')
  })

  it('writes the null the schema spells where the last model is unticked', async () => {
    const seen = servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    fireEvent.click(box('a-model'))
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { model: string | null; models: string[] } }
    }
    expect(body.assistant.endpoint.models).toEqual([])
    expect(body.assistant.endpoint.model).toBeNull()
  })

  it('changes the default to another enabled model', async () => {
    const seen = servesListing(LISTED)
    renderForm(
      configured({
        endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model', 'a-second-model'] }
      }),
      true
    )
    const second = defaults()[1]!
    fireEvent.click(second)
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { model: string } }
    }
    expect(body.assistant.endpoint.model).toBe('a-second-model')
  })

  it('adds an id nobody listed, through Other and through nothing else', async () => {
    // A listing is first-page-only, an endpoint may refuse to list at all, and
    // a gateway may route on a name of its own. None of those may stop an
    // author enabling a model they know the name of.
    const seen = servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    fireEvent.change(other(), { target: { value: 'a-model-nobody-listed' } })
    add()
    expect(box('a-model-nobody-listed').checked).toBe(true)
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { models: string[] } }
    }
    expect(body.assistant.endpoint.models).toEqual(['a-model', 'a-model-nobody-listed'])
  })

  it('refuses an id already in the set, in the decoder s own words', async () => {
    servesListing(LISTED)
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    fireEvent.change(other(), { target: { value: 'a-model' } })
    expect(await screen.findByText(/is listed twice; each model appears once/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('refuses an id the decoder would not take, and offers none either', async () => {
    // Two halves of one rule, and the rule is asked rather than restated: the
    // picker never offers a whitespace-only id, and typing one gets the
    // decoder's own sentence.
    servesListing({ data: [{ id: '   ' }, { id: 'a-listed-model' }] })
    renderForm(configured({ endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model'] } }), true)
    await test()
    await screen.findByRole('checkbox', { name: 'a-listed-model' })
    expect(screen.getAllByRole('checkbox').map((each) => each.getAttribute('name'))).not.toContain(
      '   '
    )
    fireEvent.change(other(), { target: { value: '   ' } })
    expect((screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('enables what was listed under its id, and never under its label', async () => {
    // The two differ on two of the three protocols, and a form that saved what
    // it showed would write a name no endpoint answers to.
    const seen = servesListing({
      data: [{ id: 'a-listed-model', display_name: 'A Listed Model' }]
    })
    renderForm(
      configured({
        endpoint: { ...ENDPOINT, kind: 'anthropic', model: 'a-model', models: ['a-model'] }
      }),
      true
    )
    await test()
    const listed = await screen.findByRole('checkbox', { name: /a-listed-model/ })
    // The label is what a person reads, and it is on the row.
    expect(listed.closest('label')!.textContent).toContain('A Listed Model')
    fireEvent.click(listed)
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as {
      assistant: { endpoint: { models: string[]; model: string } }
    }
    expect(body.assistant.endpoint.models).toEqual(['a-model', 'a-listed-model'])
    expect(body.assistant.endpoint.model).toBe('a-model')
  })
})


describe('removing the endpoint', () => {
  it('is offered only where there is one, and never as the primary action', () => {
    stubWrites([{}])
    renderForm(noFile())
    expect(screen.queryByRole('button', { name: 'Remove endpoint' })).toBeNull()
    cleanup()
    stubWrites([{}])
    renderForm()
    const remove = screen.getByRole('button', { name: 'Remove endpoint' })
    // Quiet, beside the primary Save. A destructive action whose primary
    // button is the destructive one is a client with no story about a
    // mis-click.
    expect(remove.className).not.toContain('primary')
  })

  it('says what it means for the key before it does anything', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    fireEvent.click(screen.getByRole('button', { name: 'Remove endpoint' }))
    expect(screen.getByText(/The key stays on this computer/)).toBeTruthy()
    // Nothing is written by asking.
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Keep it' }))
    await waitFor(() => expect(screen.queryByText(/The key stays on this computer/)).toBeNull())
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('writes the null the schema spells, and keeps how it would run', async () => {
    const { sent } = stubWrites([
      {
        body: {
          path: DESK_PATH,
          sha256: NEXT,
          assistant: { endpoint: null, engine: 'vercel', thinking: 'ultra' },
          created: false,
          keyRebindRequired: true
        }
      }
    ])
    renderForm(configured({ endpoint: ENDPOINT, engine: 'vercel', thinking: 'ultra' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove endpoint' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove it' }))
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(body.assistant).toEqual({ endpoint: null, thinking: 'ultra' })
    // The digest, exactly as an ordinary Save states it: this is the same
    // conditional commit and not a second, looser write.
    expect(body.ifMatch).toBe(DIGEST)
    expect(await screen.findByText(/no assistant endpoint configured/)).toBeTruthy()
  })
})

describe('Save API key: the endpoint and its key, in the one order the chassis admits', () => {
  /** Both writes, in the order they were made. */
  const writes = (sent: { url: string; method: string; body?: string }[]) =>
    sent
      .filter((request) => request.method === 'PUT')
      .map((request) => (request.url.includes('/api/assistant/key') ? 'key' : 'endpoint'))

  it('is the primary action until a key is bound for the endpoint that is saved', async () => {
    stubWrites([{}])
    renderForm(noFile(), false)
    expect(await screen.findByRole('button', { name: 'Save API key' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeTruthy()
    cleanup()
    stubWrites([{}])
    renderForm(configured(), true)
    expect(await screen.findByRole('button', { name: 'Save settings' })).toBeTruthy()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save API key' })).toBeNull())
  })

  it('writes the endpoint, then stores the key, and says both landed', async () => {
    // One action, two routes, and neither route changed: a key is kept bound to
    // the endpoint that is *configured*, so this is the only order in which a
    // first-time setup can work at all.
    const { sent } = stubWrites([{}])
    keyStore = { status: 200 }
    renderForm(noFile(), false)
    await screen.findByRole('button', { name: 'Save API key' })
    typeKey('sk-a-real-looking-key-wxyz')
    saveApiKey()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint', 'key']))
    expect(await screen.findByText('API key saved on this computer.')).toBeTruthy()
    // The key was sent once, as its own member, and to its own route.
    const store = sent.find(
      (request) => request.url.includes('/api/assistant/key') && request.method === 'PUT'
    )!
    expect(JSON.parse(store.body!)).toEqual({ key: 'sk-a-real-looking-key-wxyz' })
    // **And there is no field left to hold it.** A key is stored, so what the
    // row offers is Replace and Remove; an empty masked box beside a working
    // key invites somebody to wonder what is in it.
    await waitFor(() => expect(screen.queryByLabelText('API key')).toBeNull())
    expect(screen.getByRole('button', { name: 'Replace key' })).toBeTruthy()
  })

  it('sends no key at all where the endpoint write was refused', async () => {
    // The refusal order that matters. A key stored against an endpoint whose
    // write was refused would be bound to whatever the file said before, which
    // is the opposite of what the person asked for.
    const { sent } = stubWrites([
      {
        status: 422,
        body: {
          error: 'refused',
          code: 'desk-config-refused',
          problems: [{ key: 'assistant.endpoint.url', reason: 'must be an https: URL' }]
        }
      }
    ])
    renderForm(noFile(), false)
    await screen.findByRole('button', { name: 'Save API key' })
    typeKey('sk-a-real-looking-key-wxyz')
    saveApiKey()
    expect(await screen.findByText('must be an https: URL')).toBeTruthy()
    expect(writes(sent)).toEqual(['endpoint'])
  })

  it('leaves the endpoint saved where the key store was refused, and says so', async () => {
    const { sent } = stubWrites([{}])
    keyStore = {
      status: 409,
      body: { error: 'no endpoint is configured to bind a key to', code: 'assistant-key-unbound' }
    }
    renderForm(noFile(), false)
    await screen.findByRole('button', { name: 'Save API key' })
    typeKey('sk-a-real-looking-key-wxyz')
    saveApiKey()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint', 'key']))
    // The endpoint write landed and is not undone; the key refusal is its own
    // sentence, where the key is.
    expect(await screen.findByText(/no endpoint is configured to bind a key to/)).toBeTruthy()
    keyStore = { status: 200 }
  })

  it('saves the endpoint alone where nothing was typed into the key field', async () => {
    const { sent } = stubWrites([{}])
    renderForm(noFile(), false)
    await screen.findByText('No key stored')
    expect((screen.getByRole('button', { name: 'Save API key' }) as HTMLButtonElement).disabled).toBe(true)
    save()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint']))
  })
})

describe('what this form no longer offers', () => {
  it('has no Engine field at all', async () => {
    stubWrites([{}])
    renderForm()
    await screen.findByRole('button', { name: 'Save settings' })
    // The slot has one member, so there is nothing to choose. The member is
    // still decodable, with a migration; this form simply never writes one.
    expect(screen.queryByRole('combobox', { name: 'Engine' })).toBeNull()
    expect(screen.queryByLabelText('Engine')).toBeNull()
  })

  it('writes no engine member, whatever the file it opened on carried', async () => {
    const { sent } = stubWrites([{}])
    renderForm(configured({ endpoint: ENDPOINT, engine: 'builtin', thinking: 'on' }))
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(Object.keys(body.assistant).sort()).toEqual(['endpoint', 'thinking'])
    // The tier the file named survives, because it is a decision somebody made.
    expect(body.assistant.thinking).toBe('on')
  })
})

describe('the narration guard, over the states only the form can reach', () => {
  /**
   * **Admin's sweep renders configurations; these are the states a *control*
   * produces**, and they were outside it. A stale-write panel, a decoder's
   * refusal beside a field, and a removal confirmation are each a sentence
   * this desk writes, and each was written after the page that carries the
   * sweep had already been rendered.
   */
  const swept = (container: HTMLElement) =>
    narrationIn(container).map((each) => `${each.where}: ${each.says}`)

  it('carries no paragraph on a stale write', async () => {
    stubWrites([
      {
        status: 409,
        body: {
          error: 'changed',
          code: 'desk-config-changed',
          path: DESK_PATH,
          expectedSha256: DIGEST,
          actualSha256: NEXT,
          exists: true
        }
      }
    ])
    const { container } = renderForm()
    save()
    await screen.findByText(/changed on disk. Nothing was written/)
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })

  it('carries no paragraph on a refusal rendered against its fields', async () => {
    stubWrites([
      {
        status: 422,
        body: {
          error: 'refused',
          code: 'desk-config-refused',
          problems: [
            { key: 'assistant.endpoint.url', reason: 'must be an https: URL' },
            { key: 'somewhere.else', reason: 'unknown key' }
          ]
        }
      }
    ])
    const { container } = renderForm()
    save()
    await screen.findByText(/This configuration was refused/)
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })

  it('carries no paragraph while a removal is being confirmed', () => {
    stubWrites([{}])
    const { container } = renderForm()
    fireEvent.click(screen.getByRole('button', { name: 'Remove endpoint' }))
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })

  it('carries no paragraph over a file this desk could not read', () => {
    stubWrites([{}])
    const { container } = renderForm(noFile(), false, true)
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })

  it('carries no paragraph over a list the endpoint answered', async () => {
    // The states the Models list and Test connection reach are the form's own,
    // and the sweep on Admin renders configurations rather than presses.
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.includes('/api/assistant/key')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      if (url.includes('/api/assistant/probe')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({ reachable: true, status: 200, latencyMs: 12, diagnostic: '' })
        }
      }
      if (url.includes('/api/assistant/relay/')) {
        return new Response(
          JSON.stringify({ data: [{ id: 'a-listed-model' }, { id: 'a-second-listed-model' }] }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      }
      return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(WRITTEN) }
    })
    keyAnswer = keyState(true)
    const { container } = renderForm()
    await testConnection()
    await screen.findByText('Connected · 2 models available')
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })

  it('carries no paragraph where the check refused before it asked', async () => {
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.includes('/api/assistant/key')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(WRITTEN) }
    })
    keyAnswer = keyState(true)
    const { container } = renderForm(
      configured({ endpoint: { ...ENDPOINT, kind: 'anthropic', model: null, models: [] } })
    )
    await testConnection()
    await screen.findByText('Choose a model to test this provider.')
    expect(swept(container), swept(container).join(' | ')).toEqual([])
  })
})

describe('API key save and connection readiness', () => {
  it('disables saving an empty key and testing a missing or unsaved key', async () => {
    const { sent } = stubWrites([{}])
    renderForm(configured(), false)
    await screen.findByText('No key stored')
    const store = screen.getByRole('button', { name: 'Save API key' }) as HTMLButtonElement
    const test = screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement
    expect(store.disabled).toBe(true)
    expect(test.disabled).toBe(true)
    typeKey('   ')
    expect(store.disabled).toBe(true)
    typeKey('sk-new-key')
    expect(store.disabled).toBe(false)
    expect(test.disabled).toBe(true)
    fireEvent.click(test)
    expect(sent.filter((request) => request.method !== 'GET')).toHaveLength(0)
  })

  it('waits for key storage to succeed, closes replacement, and requires an explicit test', async () => {
    const { sent } = stubWrites([{}])
    const original = globalThis.fetch
    let finish!: (response: Response) => void
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      if (url.includes('/api/assistant/key') && init?.method === 'PUT') {
        return new Promise<Response>((resolve) => { finish = resolve })
      }
      return original(url, init)
    })
    renderForm()
    fireEvent.click(await screen.findByRole('button', { name: 'Replace key' }))
    typeKey('sk-new-key')
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
    await waitFor(() => expect(finish).toBeDefined())
    expect(screen.queryByText('API key saved on this computer.')).toBeNull()
    expect((screen.getByRole('button', { name: 'Saving API key…' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('')
    finish(new Response(JSON.stringify(keyState(true))))
    await screen.findByText('API key saved on this computer.')
    expect(screen.queryByLabelText('API key')).toBeNull()
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(false)
    expect(screen.getByText('Connection not tested.')).toBeTruthy()
    expect(sent.some((request) => request.url.includes('/probe') || request.url.includes('/relay/'))).toBe(false)
  })

  it('never reports a failed key save as saved and allows cancelling a replacement', async () => {
    stubWrites([{}])
    keyStore = { status: 409, body: { error: 'key store unavailable', code: 'assistant-key-unbound' } }
    renderForm()
    fireEvent.click(await screen.findByRole('button', { name: 'Replace key' }))
    typeKey('sk-new-key')
    fireEvent.click(screen.getByRole('button', { name: 'Save API key' }))
    await screen.findByText('key store unavailable')
    expect(screen.queryByText('API key saved on this computer.')).toBeNull()
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByLabelText('API key')).toBeNull()
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(false)
    keyStore = { status: 200 }
  })

  it('does not test a stored key that the server says is bound elsewhere', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    keyAnswer = keyState(false, true)
    await screen.findByText(/nothing will be sent/)
    const button = screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.click(button)
    expect(sent.some((request) => request.url.includes('/probe'))).toBe(false)
  })

  it('prevents duplicate tests and waits for both the probe and listing', async () => {
    stubWrites([{}])
    const original = globalThis.fetch
    let probe!: (response: Response) => void
    let listing!: (response: Response) => void
    let requests = 0
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      if (url.includes('/api/assistant/probe')) {
        requests++
        return new Promise<Response>((resolve) => { probe = resolve })
      }
      if (url.includes('/api/assistant/relay/')) {
        requests++
        return new Promise<Response>((resolve) => { listing = resolve })
      }
      return original(url, init)
    })
    renderForm()
    await testConnection()
    const button = screen.getByRole('button', { name: 'Testing connection…' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.click(button)
    await waitFor(() => expect(requests).toBe(2))
    listing(new Response(JSON.stringify({ data: [{ id: 'test-model' }] })))
    await screen.findByRole('checkbox', { name: 'test-model' })
    expect(button.disabled).toBe(true)
    probe(new Response(JSON.stringify({ reachable: true, status: 200, latencyMs: 12, diagnostic: '' })))
    await screen.findByText('Connected · 1 model available')
    expect((screen.getByRole('button', { name: 'Test connection' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('keeps enabled models in the saved configuration when search hides them', async () => {
    const { sent } = stubWrites([{}])
    renderForm(configured({ endpoint: { ...ENDPOINT, models: ['a-model', 'another-model'] } }))
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search models' }), { target: { value: 'another' } })
    expect(screen.queryByRole('checkbox', { name: 'a-model' })).toBeNull()
    expect(screen.getByRole('checkbox', { name: 'another-model' })).toBeTruthy()
    save()
    await waitFor(() => expect(sent.some((request) => request.method === 'PUT')).toBe(true))
    expect(theWrite(sent).assistant.endpoint).toMatchObject({ model: 'a-model', models: ['a-model', 'another-model'] })
  })
})

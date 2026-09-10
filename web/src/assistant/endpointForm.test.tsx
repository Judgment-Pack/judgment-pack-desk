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
const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save' }))
const connect = () => fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
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
    fireEvent.change(screen.getByLabelText('Type a model id'), { target: { value: 'a-model' } })
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect(theWrite(sent).ifMatch).toBe('')
    expect(await screen.findByText(/the file was created/)).toBeTruthy()
  })

  it('writes nothing at all where this page never learned the digest', async () => {
    const { sent } = stubWrites([{}])
    renderForm(unread())
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(
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
    fireEvent.change(screen.getByLabelText('Type a model id'), { target: { value: 'apiKey' } })
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(Object.keys(body.assistant).sort()).toEqual(['endpoint', 'thinking'])
    expect(
      Object.keys(body.assistant.endpoint as Record<string, unknown>).sort()
    ).toEqual(['kind', 'model', 'tools', 'url'])
    // The text is where it was typed, and it is not a member name.
    expect((body.assistant.endpoint as { model: string }).model).toBe('apiKey')
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
    expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe('')
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
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(
      true
    )
    save()
    await Promise.resolve()
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('refuses an empty model in the decoder s own words, and sends nothing', async () => {
    // **The same rule the URL gets, applied to the member beside it.** The
    // decoder refuses an empty model, so a form that sent one would be
    // composing a write its own reader rejects — and on a fresh desk that is
    // the very first thing anybody would press.
    const { sent } = stubWrites([{}])
    renderForm(noFile())
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://api.example.invalid/v1' }
    })
    expect(await screen.findByText(/must be a non-empty string/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    save()
    await Promise.resolve()
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
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
            { key: 'assistant.endpoint.tools', reason: 'is not a tool the assistant may call' }
          ]
        }
      }
    ])
    renderForm()
    save()
    // Beside the field, in the decoder's own sentence.
    const model = await screen.findByLabelText('Type a model id')
    await waitFor(() =>
      expect(model.getAttribute('aria-describedby')).toBeTruthy()
    )
    expect(screen.getByText('must be a non-empty string')).toBeTruthy()
    expect(screen.getByText('is not a tool the assistant may call')).toBeTruthy()
    expect(model.getAttribute('aria-invalid')).toBe('true')
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
    fireEvent.change(screen.getByLabelText('Type a model id'), { target: { value: 'a-model-being-chosen' } })
    save()
    await screen.findByText(/changed on disk. Nothing was written/)
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() =>
      expect(screen.queryByText(/changed on disk. Nothing was written/)).toBeNull()
    )
    // The whole point: the read is taken again and the draft is not.
    expect((screen.getByLabelText('Type a model id') as HTMLInputElement).value).toBe('a-model-being-chosen')
  })
})

describe('the model, and the list the endpoint offers', () => {
  /** A `fetch` that answers the listing and records what it was asked. */
  function servesListing(
    body: unknown,
    status = 200
  ): { urls: string[]; inits: RequestInit[] } {
    const urls: string[] = []
    const inits: RequestInit[] = []
    vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
      urls.push(url)
      inits.push(init)
      if (url.includes('/api/assistant/key')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(keyAnswer) }
      }
      if (!url.includes('/api/assistant/relay/')) {
        return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(WRITTEN) }
      }
      return new Response(JSON.stringify(body), {
        status,
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
      }
    ]
  }
  const listed = () => screen.findByRole('combobox', { name: 'Model' })
  const noList = () => screen.queryByRole('combobox', { name: 'Model' })
  const relayed = (urls: string[]) => urls.filter((url) => url.includes('/api/assistant/relay/'))

  it('asks nothing at all where the key is not bound to the saved endpoint', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, false)
    expect(await screen.findByText(/Connect first/)).toBeTruthy()
    expect(relayed(seen.urls)).toHaveLength(0)
    expect(noList()).toBeNull()
  })

  it('asks as soon as a key is bound, with no button to press', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
    // The button that used to stand here is gone: the answer to "which model"
    // is a list the endpoint already knows, and pressing something first is a
    // step with no decision in it.
    expect(screen.queryByRole('button', { name: 'List models' })).toBeNull()
    expect(await listed()).toBeTruthy()
  })

  it('asks through the relay, by a suffix, and never builds an address', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
    const listing = relayed(seen.urls)[0]!
    expect(listing).toContain('/api/assistant/relay/v1/v1beta/models')
    expect(listing).not.toContain('api.example.invalid')
  })

  it('asks once, and does not ask again on every render', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    await listed()
    fireEvent.change(screen.getByLabelText('Type a model id'), { target: { value: 'a' } })
    fireEvent.change(screen.getByLabelText('Type a model id'), { target: { value: 'ab' } })
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
  })

  it('fills a picker with what came back, showing the label', async () => {
    servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(await listed())
    expect(await screen.findByRole('option', { name: 'Gemini 2.5 Pro' })).toBeTruthy()
  })

  it('preselects the model the file already names', async () => {
    servesListing(LISTED)
    renderForm(
      configured({
        endpoint: {
          ...ENDPOINT,
          kind: 'gemini',
          url: 'https://api.example.invalid',
          model: 'gemini-2.5-pro'
        }
      }),
      true
    )
    // The trigger renders the label of the option whose value is selected, so
    // a preselected row is the label on screen without the list being opened.
    expect((await listed()).textContent).toContain('Gemini 2.5 Pro')
  })

  it('saves the id it was listed under, and never the label', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(await listed())
    fireEvent.click(await screen.findByRole('option', { name: 'Gemini 2.5 Pro' }))
    // The field beside the list takes the id, because the id is what the
    // endpoint answers to and the label is what a person reads.
    await waitFor(() =>
      expect((screen.getByLabelText('Type a model id') as HTMLInputElement).value).toBe(
        'gemini-2.5-pro'
      )
    )
    // **And the list survives being picked from.** Choosing a model changes the
    // model and not the endpoint, so the rows are still about the endpoint on
    // screen — only a provider or a URL moving makes them a list from somewhere
    // else.
    expect(noList()).not.toBeNull()
    save()
    await waitFor(() => expect(seen.urls.some((url) => url.includes('/api/desk-config'))).toBe(true))
    const put = seen.inits.find((init) => init.method === 'PUT')!
    const body = JSON.parse(String(put.body)) as { assistant: { endpoint: { model: string } } }
    expect(body.assistant.endpoint.model).toBe('gemini-2.5-pro')
  })

  it('keeps the field usable for a model the first page does not carry', async () => {
    // A listing is first-page-only and an endpoint may refuse to list at all.
    // Neither may stop an author configuring a model they know the name of.
    servesListing(LISTED)
    renderForm(GEMINI, true)
    await listed()
    fireEvent.change(screen.getByLabelText('Type a model id'), {
      target: { value: 'a-model-not-listed' }
    })
    expect((screen.getByLabelText('Type a model id') as HTMLInputElement).value).toBe(
      'a-model-not-listed'
    )
    expect(screen.getByText(/Anything else is typed in below/)).toBeTruthy()
  })

  it('asks the endpoint that is saved, never the one being typed', async () => {
    // **The review's exact sequence.** A saved, bound OpenAI-compatible
    // endpoint; select Gemini without saving. It used to send `v1beta/models`
    // to the still-saved OpenAI endpoint — a request composed for one
    // destination and sent to another.
    const seen = servesListing({ data: [{ id: 'a-stub-model' }] })
    renderForm(configured(), true)
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
    expect(relayed(seen.urls)[0]).toContain('/api/assistant/relay/v1/models')
    fireEvent.click(screen.getByRole('combobox', { name: 'Provider' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Google Gemini' }))
    expect(await screen.findByText(/this asks the endpoint that is saved/)).toBeTruthy()
    // Nothing further was asked: the draft is no longer the saved endpoint.
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
  })

  it('asks nothing while only the URL has been typed over', async () => {
    const seen = servesListing({ data: [{ id: 'a-stub-model' }] })
    renderForm(configured(), true)
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid/v1' }
    })
    expect(await screen.findByText(/this asks the endpoint that is saved/)).toBeTruthy()
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(1))
  })

  it('clears the rows when the endpoint the form says moves', async () => {
    // A picker left standing after a provider or a URL changed is a list of
    // models from somewhere else, offered against a form that no longer says so.
    servesListing(LISTED)
    renderForm(GEMINI, true)
    await listed()
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    await waitFor(() => expect(noList()).toBeNull())
  })

  it('does not resurrect a listing when the endpoint is changed away and back', async () => {
    // **Hidden is not cleared.** Rows kept in state while the identity differed
    // came back the moment the URL was typed back — an arbitrarily stale
    // listing on screen with no request behind it and nothing on the page
    // saying when it was taken. What replaces the resurrection is a fresh ask,
    // because the draft is the saved endpoint again.
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    await listed()
    const original = (screen.getByLabelText('Endpoint URL') as HTMLInputElement).value
    fireEvent.change(screen.getByLabelText('Endpoint URL'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    await waitFor(() => expect(noList()).toBeNull())
    expect(relayed(seen.urls)).toHaveLength(1)
    fireEvent.change(screen.getByLabelText('Endpoint URL'), { target: { value: original } })
    // A second request, not the first one's rows put back on screen.
    await waitFor(() => expect(relayed(seen.urls)).toHaveLength(2))
    expect(await listed()).toBeTruthy()
  })

  it('offers no option for an id the decoder refuses, and still refuses one typed', async () => {
    // Two halves of one rule. The picker never offers a whitespace-only id — it
    // would save cleanly into the field and produce a 422 on the next Save —
    // and typing the same value still gets the decoder's own sentence against
    // the field, because the chassis is what decides.
    servesListing({
      models: [
        { name: 'models/   ', supportedGenerationMethods: ['generateContent'] },
        {
          name: 'models/gemini-2.5-pro',
          displayName: 'Gemini 2.5 Pro (stub)',
          supportedGenerationMethods: ['generateContent']
        }
      ]
    })
    renderForm(GEMINI, true)
    fireEvent.click(await listed())
    const offered = (await screen.findAllByRole('option')).map((option) => option.textContent)
    expect(offered).toEqual(['Gemini 2.5 Pro (stub)'])
  })

  it('falls back to the typed field on a refused listing, in the probe s words', async () => {
    servesListing({ error: 'sk-a-real-looking-key-wxyz' }, 401)
    const { container } = renderForm(GEMINI, true)
    expect(await screen.findByText(/the endpoint did not accept the key/)).toBeTruthy()
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    // No picker, because there is nothing to pick from — and the field is
    // still there, which is what makes a refused listing survivable.
    expect(noList()).toBeNull()
    expect(screen.getByLabelText('Type a model id')).toBeTruthy()
  })

  it('says the endpoint listed none rather than showing an empty picker unexplained', async () => {
    servesListing({ models: [] })
    renderForm(GEMINI, true)
    expect(await screen.findByText('The endpoint listed no models.')).toBeTruthy()
    expect(noList()).toBeNull()
    expect(screen.getByLabelText('Type a model id')).toBeTruthy()
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

describe('Connect: the endpoint and its key, in the one order the chassis admits', () => {
  /** Both writes, in the order they were made. */
  const writes = (sent: { url: string; method: string; body?: string }[]) =>
    sent
      .filter((request) => request.method === 'PUT')
      .map((request) => (request.url.includes('/api/assistant/key') ? 'key' : 'endpoint'))

  it('is the primary action until a key is bound for the endpoint that is saved', async () => {
    stubWrites([{}])
    renderForm(configured(), false)
    expect(await screen.findByRole('button', { name: 'Connect' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    cleanup()
    stubWrites([{}])
    renderForm(configured(), true)
    expect(await screen.findByRole('button', { name: 'Save' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Connect' })).toBeNull()
  })

  it('writes the endpoint, then stores the key, and says both landed', async () => {
    // One action, two routes, and neither route changed: a key is kept bound to
    // the endpoint that is *configured*, so this is the only order in which a
    // first-time setup can work at all.
    const { sent } = stubWrites([{}])
    keyStore = { status: 200 }
    renderForm(configured(), false)
    await screen.findByRole('button', { name: 'Connect' })
    typeKey('sk-a-real-looking-key-wxyz')
    connect()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint', 'key']))
    expect(await screen.findByText(/the key is stored on this computer/)).toBeTruthy()
    // The key was sent once, as its own member, and to its own route.
    const store = sent.find(
      (request) => request.url.includes('/api/assistant/key') && request.method === 'PUT'
    )!
    expect(JSON.parse(store.body!)).toEqual({ key: 'sk-a-real-looking-key-wxyz' })
    // And it is gone from the field the instant it was handed to the request.
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('')
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
    renderForm(configured(), false)
    await screen.findByRole('button', { name: 'Connect' })
    typeKey('sk-a-real-looking-key-wxyz')
    connect()
    expect(await screen.findByText('must be an https: URL')).toBeTruthy()
    expect(writes(sent)).toEqual(['endpoint'])
  })

  it('leaves the endpoint saved where the key store was refused, and says so', async () => {
    const { sent } = stubWrites([{}])
    keyStore = {
      status: 409,
      body: { error: 'no endpoint is configured to bind a key to', code: 'assistant-key-unbound' }
    }
    renderForm(configured(), false)
    await screen.findByRole('button', { name: 'Connect' })
    typeKey('sk-a-real-looking-key-wxyz')
    connect()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint', 'key']))
    // The endpoint write landed and is not undone; the key refusal is its own
    // sentence, where the key is.
    expect(await screen.findByText(/no endpoint is configured to bind a key to/)).toBeTruthy()
    keyStore = { status: 200 }
  })

  it('saves the endpoint alone where nothing was typed into the key field', async () => {
    const { sent } = stubWrites([{}])
    renderForm(configured(), false)
    expect(await screen.findByText(/Enter the key above to store it with the endpoint/)).toBeTruthy()
    connect()
    await waitFor(() => expect(writes(sent)).toEqual(['endpoint']))
  })
})

describe('what this form no longer offers', () => {
  it('has no Engine field at all', async () => {
    stubWrites([{}])
    renderForm()
    await screen.findByRole('button', { name: 'Save' })
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
})

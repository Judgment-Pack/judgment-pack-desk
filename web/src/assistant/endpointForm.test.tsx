/**
 * The form, driven against a stub of the one route it writes to.
 *
 * **Three properties are why this file exists**, and each is asserted on the
 * wire rather than in the rendering:
 *
 * - **the digest goes with the write**, so a file somebody edited between this
 *   page's read and its Save refuses the write rather than losing their edit;
 * - **the body carries the six members and nothing else**, whatever anybody
 *   typed into a field;
 * - **a refusal is the decoder's sentence**, rendered against the field its key
 *   path names, and a stale write keeps every value that was typed.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
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

function renderForm(value: EffectiveConfig = configured(), bound = false) {
  const written: unknown[] = []
  const rendered = render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigFixture value={value}>
        <EndpointForm bound={bound} onWritten={(answer) => written.push(answer)} />
      </DeskConfigFixture>
    </QueryClientProvider>
  )
  return { ...rendered, written }
}

const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save' }))

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
    fireEvent.change(screen.getByLabelText('Endpoint'), {
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
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(
      true
    )
    expect(screen.getByText(/has not seen them/)).toBeTruthy()
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
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'apiKey' } })
    save()
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(Object.keys(body.assistant).sort()).toEqual(['endpoint', 'engine', 'thinking'])
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
          <EndpointForm bound={false} onWritten={() => {}} />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    expect((screen.getByLabelText('Endpoint') as HTMLInputElement).value).toBe('')
    rerender(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={configured()}>
          <EndpointForm bound={false} onWritten={() => {}} />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect((screen.getByLabelText('Endpoint') as HTMLInputElement).value).toBe(ENDPOINT.url)
    )
  })

  it('offers the address a protocol documents when the protocol is chosen', async () => {
    stubWrites([{}])
    renderForm(noFile())
    fireEvent.click(screen.getByRole('combobox', { name: 'Wire protocol' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Gemini' }))
    await waitFor(() =>
      expect((screen.getByLabelText('Endpoint') as HTMLInputElement).value).toBe(
        PREFILLED_URL.gemini
      )
    )
  })

  it('refuses a URL in the decoder s own words, and sends nothing', async () => {
    const { sent } = stubWrites([{}])
    renderForm()
    fireEvent.change(screen.getByLabelText('Endpoint'), {
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

  it('refuses a configured query the relay reserves, in the same words', async () => {
    stubWrites([{}])
    renderForm()
    fireEvent.change(screen.getByLabelText('Endpoint'), {
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
    expect(offered).toEqual(['off', 'on', 'ultra'])
  })

  it('says what the chosen tier puts on this protocol s wire', async () => {
    stubWrites([{}])
    renderForm({ ...configured({ endpoint: { ...ENDPOINT, kind: 'gemini' } }) })
    expect(screen.getByText(/thinkingBudget/)).toBeTruthy()
    expect(screen.getByText(/this desk’s choice inside a documented field/)).toBeTruthy()
  })

  it('names the two things the SDK-backed engine cannot do', async () => {
    stubWrites([{}])
    renderForm()
    expect(screen.getByText(/narrowed/)).toBeTruthy()
    expect(screen.getByText(/empty signed thought part/)).toBeTruthy()
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
    const model = await screen.findByLabelText('Model')
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
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'a-model-being-chosen' } })
    save()
    await screen.findByText(/changed on disk. Nothing was written/)
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() =>
      expect(screen.queryByText(/changed on disk. Nothing was written/)).toBeNull()
    )
    // The whole point: the read is taken again and the draft is not.
    expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('a-model-being-chosen')
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

  it('offers the listing only where the key is bound to the saved endpoint', () => {
    servesListing(LISTED)
    renderForm(GEMINI, false)
    expect(
      (screen.getByRole('button', { name: 'List models' }) as HTMLButtonElement).disabled
    ).toBe(true)
    expect(screen.getByText(/its key stored before this desk can ask it/)).toBeTruthy()
    cleanup()
    servesListing(LISTED)
    renderForm(GEMINI, true)
    expect(
      (screen.getByRole('button', { name: 'List models' }) as HTMLButtonElement).disabled
    ).toBe(false)
  })

  it('asks through the relay, by a suffix, and never builds an address', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    await waitFor(() =>
      expect(seen.urls.some((url) => url.includes('/api/assistant/relay/'))).toBe(true)
    )
    const listing = seen.urls.find((url) => url.includes('/api/assistant/relay/'))!
    expect(listing).toContain('/api/assistant/relay/v1/v1beta/models')
    expect(listing).not.toContain('api.example.invalid')
  })

  it('fills a picker with what came back, showing the label', async () => {
    servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    const picker = await screen.findByRole('combobox', { name: 'Models this endpoint listed' })
    fireEvent.click(picker)
    expect(await screen.findByRole('option', { name: 'Gemini 2.5 Pro' })).toBeTruthy()
  })

  it('saves the id it was listed under, and never the label', async () => {
    const seen = servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    const picker = await screen.findByRole('combobox', { name: 'Models this endpoint listed' })
    fireEvent.click(picker)
    fireEvent.click(await screen.findByRole('option', { name: 'Gemini 2.5 Pro' }))
    // The field beside the list takes the id, because the id is what the
    // endpoint answers to and the label is what a person reads.
    await waitFor(() =>
      expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('gemini-2.5-pro')
    )
    // **And the list survives being picked from.** Choosing a model changes
    // the model and not the endpoint, so the rows are still about the endpoint
    // on screen — only a kind or a URL moving makes them a list from
    // somewhere else.
    expect(screen.getByRole('combobox', { name: 'Models this endpoint listed' })).toBeTruthy()
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
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    await screen.findByRole('combobox', { name: 'Models this endpoint listed' })
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'a-model-not-listed' } })
    expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('a-model-not-listed')
    expect(screen.getByText(/A model that is not here is typed into the field/)).toBeTruthy()
  })

  it('asks the endpoint that is saved, never the one being typed', async () => {
    // **The review's exact sequence.** A saved, bound OpenAI-compatible
    // endpoint; select Gemini without saving; press List models. It used to
    // send `v1beta/models` to the still-saved OpenAI endpoint — a request
    // composed for one destination and sent to another.
    const seen = servesListing({ data: [{ id: 'a-stub-model' }] })
    renderForm(configured(), true)
    expect(
      (screen.getByRole('button', { name: 'List models' }) as HTMLButtonElement).disabled
    ).toBe(false)
    fireEvent.click(screen.getByRole('combobox', { name: 'Wire protocol' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Gemini' }))
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'List models' }) as HTMLButtonElement).disabled
      ).toBe(true)
    )
    expect(screen.getByText(/this asks the endpoint that is saved/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    await Promise.resolve()
    expect(seen.urls.filter((url) => url.includes('/api/assistant/relay/'))).toHaveLength(0)
  })

  it('disables the listing while only the URL has been typed over', async () => {
    const seen = servesListing({ data: [{ id: 'a-stub-model' }] })
    renderForm(configured(), true)
    fireEvent.change(screen.getByLabelText('Endpoint'), {
      target: { value: 'https://elsewhere.example.invalid/v1' }
    })
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'List models' }) as HTMLButtonElement).disabled
      ).toBe(true)
    )
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    await Promise.resolve()
    expect(seen.urls.filter((url) => url.includes('/api/assistant/relay/'))).toHaveLength(0)
  })

  it('clears the rows when the endpoint the form says moves', async () => {
    // A picker left standing after a kind or a URL changed is a list of models
    // from somewhere else, offered against a form that no longer says so.
    servesListing(LISTED)
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    await screen.findByRole('combobox', { name: 'Models this endpoint listed' })
    fireEvent.change(screen.getByLabelText('Endpoint'), {
      target: { value: 'https://elsewhere.example.invalid' }
    })
    await waitFor(() =>
      expect(screen.queryByRole('combobox', { name: 'Models this endpoint listed' })).toBeNull()
    )
  })

  it('offers no option for an id the decoder refuses, and still refuses one typed', async () => {
    // Two halves of one rule. The picker never offers a whitespace-only id —
    // it would save cleanly into the field and produce a 422 on the next Save
    // — and typing the same value still gets the decoder's own sentence
    // against the field, because the chassis is what decides.
    servesListing(
      {
        models: [
          { name: 'models/   ', supportedGenerationMethods: ['generateContent'] },
          {
            name: 'models/gemini-2.5-pro',
            displayName: 'Gemini 2.5 Pro (stub)',
            supportedGenerationMethods: ['generateContent']
          }
        ]
      }
    )
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    const picker = await screen.findByRole('combobox', { name: 'Models this endpoint listed' })
    fireEvent.click(picker)
    const offered = (await screen.findAllByRole('option')).map((option) => option.textContent)
    expect(offered).toEqual(['Gemini 2.5 Pro (stub)'])
  })

  it('reports a refused listing in the probe s words, and never the body', async () => {
    servesListing({ error: 'sk-a-real-looking-key-wxyz' }, 401)
    const { container } = renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    expect(await screen.findByText(/the endpoint did not accept the key/)).toBeTruthy()
    expect(container.textContent).not.toContain('sk-a-real-looking-key-wxyz')
    // And no picker, because there is nothing to pick from.
    expect(screen.queryByRole('combobox', { name: 'Models this endpoint listed' })).toBeNull()
  })

  it('says the endpoint listed none rather than showing an empty picker unexplained', async () => {
    servesListing({ models: [] })
    renderForm(GEMINI, true)
    fireEvent.click(screen.getByRole('button', { name: 'List models' }))
    expect(await screen.findByText('The endpoint listed none.')).toBeTruthy()
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
    expect(screen.getByText(/The key stays on this machine/)).toBeTruthy()
    // Nothing is written by asking.
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Keep it' }))
    await waitFor(() => expect(screen.queryByText(/The key stays on this machine/)).toBeNull())
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('writes the null the schema spells, and keeps how it would run', async () => {
    const { sent } = stubWrites([
      {
        body: {
          path: DESK_PATH,
          sha256: NEXT,
          assistant: { endpoint: null, engine: 'builtin', thinking: 'ultra' },
          created: false,
          keyRebindRequired: true
        }
      }
    ])
    renderForm(configured({ endpoint: ENDPOINT, engine: 'builtin', thinking: 'ultra' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove endpoint' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove it' }))
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const body = theWrite(sent)
    expect(body.assistant).toEqual({ endpoint: null, engine: 'builtin', thinking: 'ultra' })
    // The digest, exactly as an ordinary Save states it: this is the same
    // conditional commit and not a second, looser write.
    expect(body.ifMatch).toBe(DIGEST)
    expect(await screen.findByText(/no assistant endpoint configured/)).toBeTruthy()
  })
})

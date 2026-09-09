/**
 * The model listing: what it asks for, and what it makes of the answer.
 *
 * **The load-bearing case is where the request went**, and it is asserted at
 * the network global rather than at the capability: the whole claim is that
 * the page names a suffix and the desk builds the address, so a test that
 * inspected the capability's argument would prove the argument and not the
 * address.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DIAGNOSTIC_SAYS } from './client'
import {
  LISTING_SUFFIX,
  NOT_A_LISTING,
  listModels,
  listingRefusal,
  modelRows
} from './modelListing'
import { bindModelCall } from './session'

afterEach(() => {
  vi.unstubAllGlobals()
})

const GEMINI_BODY = {
  models: [
    {
      name: 'models/gemini-2.5-pro',
      displayName: 'Gemini 2.5 Pro',
      supportedGenerationMethods: ['generateContent', 'countTokens']
    },
    {
      name: 'models/text-embedding-004',
      displayName: 'Text Embedding 004',
      supportedGenerationMethods: ['embedContent']
    },
    {
      name: 'models/gemini-flash-latest',
      supportedGenerationMethods: ['generateContent']
    }
  ]
}

/** A `fetch` that answers one listing and remembers every request. */
function serves(
  body: unknown,
  options: { status?: number; text?: string } = {}
): { urls: string[]; inits: RequestInit[] } {
  const urls: string[] = []
  const inits: RequestInit[] = []
  vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
    urls.push(url)
    inits.push(init)
    const status = options.status ?? 200
    return new Response(options.text ?? JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' }
    })
  })
  return { urls, inits }
}

describe('where the listing request goes', () => {
  it('goes to the relay, by a suffix, and never to the endpoint', async () => {
    const seen = serves(GEMINI_BODY)
    await listModels('gemini', bindModelCall('gemini'))
    expect(seen.urls).toHaveLength(1)
    // The desk's own route, carrying nothing of its own — and no trace of the
    // configured endpoint, which the page does not hold.
    expect(seen.urls[0]).toContain('/api/assistant/relay/v1/v1beta/models')
    expect(seen.urls[0]).not.toContain('token=')
    expect(seen.urls[0]).not.toContain('googleapis')
    expect(seen.urls[0]).not.toContain('https://')
  })

  it('carries no query of its own, on the one family that admits a pair', async () => {
    // Gemini's first page needs none, and `pageToken` is refused by the relay
    // and by the configured URL alike: later pages are not supported at all.
    const seen = serves(GEMINI_BODY)
    await listModels('gemini', bindModelCall('gemini'))
    const url = new URL(seen.urls[0]!, 'http://desk.invalid')
    expect([...url.searchParams.keys()]).toEqual([])
  })

  it('asks with a GET and no body, and no credential but this desk’s own', async () => {
    const seen = serves({ data: [] })
    await listModels('openai-compatible', bindModelCall('openai-compatible'))
    expect(seen.inits[0]!.method).toBe('GET')
    expect(seen.inits[0]!.body).toBeUndefined()
    const headers = seen.inits[0]!.headers as Record<string, string>
    // **One header, and it is the session this page holds** — the relay is a
    // gated chassis route. What must not be here is a credential for the
    // *endpoint*: that one is on this machine and is attached by the chassis.
    expect(Object.keys(headers)).toEqual(['Authorization'])
    expect(headers.Authorization).toMatch(/^Bearer /)
  })

  it('uses each protocol s own listing path', async () => {
    for (const [kind, suffix] of Object.entries(LISTING_SUFFIX)) {
      const seen = serves({ data: [], models: [] })
      await listModels(kind as keyof typeof LISTING_SUFFIX, bindModelCall(kind as never))
      expect(seen.urls[0], kind).toBe(`/api/assistant/relay/v1/${suffix}`)
    }
  })
})

describe('what the listing makes of an answer', () => {
  it('reads Gemini s shape, stripping the prefix and keeping the display name', () => {
    expect(modelRows('gemini', GEMINI_BODY)).toEqual([
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro' },
      // No display name: the id is what a reader gets, rather than a blank.
      { id: 'gemini-flash-latest', label: 'gemini-flash-latest' }
    ])
  })

  it('drops a Gemini model that cannot generate content', () => {
    // The endpoint's own declaration does the filtering. Offering an embedding
    // model would be a picker producing a session no request succeeds in.
    expect(modelRows('gemini', GEMINI_BODY).map((row) => row.id)).not.toContain(
      'text-embedding-004'
    )
  })

  it('reads the OpenAI-compatible shape, where the id is the whole of it', () => {
    expect(modelRows('openai-compatible', { data: [{ id: 'a-model' }, { id: 'b-model' }] })).toEqual([
      { id: 'a-model', label: 'a-model' },
      { id: 'b-model', label: 'b-model' }
    ])
  })

  it('reads the Anthropic shape, where a display name sits beside the id', () => {
    expect(
      modelRows('anthropic', {
        data: [{ id: 'claude-x', display_name: 'Claude X' }, { id: 'claude-y' }]
      })
    ).toEqual([
      { id: 'claude-x', label: 'Claude X' },
      { id: 'claude-y', label: 'claude-y' }
    ])
  })

  it('skips an entry with no id it could save', () => {
    // A row nobody can save is not a row, and a blank in a list reads as a bug.
    expect(modelRows('openai-compatible', { data: [{ id: '' }, {}, null, 3, { id: 'a' }] })).toEqual([
      { id: 'a', label: 'a' }
    ])
    expect(modelRows('gemini', { models: [{ name: 'models/' }, { name: 'x' }] })).toEqual([])
  })

  it('skips an id the configuration decoder would refuse, whitespace included', () => {
    // **The rule is imported and not restated.** A copy is how the picker came
    // to offer `"   "`: the file's reader trims and the copy did not, so the
    // option saved cleanly into the field and produced a 422 on the next Save.
    expect(
      modelRows('openai-compatible', { data: [{ id: '   ' }, { id: '\t\n' }, { id: 'a' }] })
    ).toEqual([{ id: 'a', label: 'a' }])
    expect(
      modelRows('anthropic', { data: [{ id: ' ', display_name: 'Looks fine' }] })
    ).toEqual([])
    expect(
      modelRows('gemini', {
        models: [
          { name: 'models/   ', supportedGenerationMethods: ['generateContent'] },
          { name: 'models/ok', supportedGenerationMethods: ['generateContent'] }
        ]
      })
    ).toEqual([{ id: 'ok', label: 'ok' }])
  })

  it('offers the id trimmed, because that is what would be saved', () => {
    expect(
      modelRows('gemini', {
        models: [
          { name: 'models/  a-model  ', supportedGenerationMethods: ['generateContent'] }
        ]
      })
    ).toEqual([{ id: 'a-model', label: 'a-model' }])
    // A picker that showed one string and wrote another is a picker whose
    //choice cannot be checked against the file afterwards.
    expect(modelRows('openai-compatible', { data: [{ id: '  a-model  ' }] })).toEqual([
      { id: 'a-model', label: 'a-model' }
    ])
  })

  it('makes nothing of a body that is not a listing at all', () => {
    for (const body of [null, 3, 'text', {}, { data: 'not an array' }]) {
      expect(modelRows('openai-compatient' as never, body)).toEqual([])
    }
  })
})

describe('a listing that did not answer with one', () => {
  it('reports the status and one word from the probe s vocabulary', async () => {
    for (const [status, word] of [
      [401, 'unauthorized'],
      [403, 'forbidden'],
      [404, 'not-found'],
      [500, 'unexpected-status'],
      [409, 'unexpected-status']
    ] as const) {
      serves({}, { status })
      const failure = await listModels('gemini', bindModelCall('gemini')).catch(
        (cause: Error) => cause
      )
      expect((failure as Error).message, String(status)).toBe(
        `the model listing was refused — answered ${status}, ${DIAGNOSTIC_SAYS[word]}`
      )
    }
  })

  it('never repeats a word the endpoint wrote, on any status', async () => {
    // The probe's ruling, one layer along: a body an endpoint controls can
    // carry a derived representation of the credential that no substitution
    // finds, so the body is not read on a refusal at all.
    serves({}, { status: 500, text: 'sk-a-real-looking-key-wxyz leaked here' })
    const failure = await listModels('gemini', bindModelCall('gemini')).catch(
      (cause: Error) => cause
    )
    expect((failure as Error).message).not.toContain('sk-a-real-looking-key-wxyz')
    expect((failure as Error).message).toBe(listingRefusal(500))
  })

  it('says so in a fixed sentence where the answer is not JSON', async () => {
    // `JSON.parse` quotes the text it failed on, and that text is the body.
    serves({}, { text: 'not json: sk-a-real-looking-key-wxyz' })
    const failure = await listModels('gemini', bindModelCall('gemini')).catch(
      (cause: Error) => cause
    )
    expect((failure as Error).message).toBe(NOT_A_LISTING)
    expect((failure as Error).message).not.toContain('sk-a-real-looking-key-wxyz')
  })
})

describe('a listing the desk itself refused', () => {
  it('says so without reading the body, on the status the desk answers on', async () => {
    // **`assistant-listing-refused` arrives as a 502 with the desk's own
    // envelope in it, and this page still does not read it.** The body on a
    // refused listing is sometimes the desk's and sometimes the endpoint's —
    // the relay forwards a 4xx verbatim — and no header tells them apart, so
    // reading the `code` would be reading a body under the endpoint's control.
    // What is said is what both readings have in common.
    serves({ error: 'sk-a-real-looking-key-wxyz', code: 'assistant-listing-refused' }, { status: 502 })
    const failure = await listModels('gemini', bindModelCall('gemini')).catch(
      (cause: Error) => cause
    )
    expect((failure as Error).message).toContain('answered 502')
    expect((failure as Error).message).toContain('will not put on the page')
    expect((failure as Error).message).not.toContain('sk-a-real-looking-key-wxyz')
    expect((failure as Error).message).not.toContain('assistant-listing-refused')
  })

  it('produces no rows at all from a refusal, whatever the body carried', async () => {
    // The other half of the HIGH finding: the page never renders a listing it
    // did not get, and a refused one yields no options for anything to copy.
    serves({ data: [{ id: 'sk-a-real-looking-key-wxyz' }] }, { status: 502 })
    await expect(listModels('openai-compatible', bindModelCall('openai-compatible'))).rejects.toThrow()
  })

  it('names the busy refusal on its own status', async () => {
    serves({}, { status: 503 })
    const failure = await listModels('gemini', bindModelCall('gemini')).catch(
      (cause: Error) => cause
    )
    expect((failure as Error).message).toContain('as many requests to the endpoint as it will')
  })
})

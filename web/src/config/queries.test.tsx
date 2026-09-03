/**
 * The config read, and the one property it must have: **it never rejects.**
 *
 * A 404, a body that is not JSON, a file the chassis refused, and no `fetch`
 * stub at all all resolve to the built-in defaults with the reason kept for
 * Admin. The last one is load-bearing: `testing/harness.tsx` stubs no fetch,
 * so a shell query that rejected would land an unhandled rejection in every
 * future integration test, from a query that has nothing to do with the case
 * under test.
 *
 * **They do not all resolve to the *same* answer**, and that is the second
 * property. A 404 is an absent file and lands in `note`, which nothing warns
 * about; everything else is a file that exists and was not read, and lands in
 * `readFailure`, which Admin and the status strip both show. Collapsing the
 * two made a 413, a permission error and a dead socket indistinguishable from
 * a project that simply has no configuration.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DESK_DEFAULTS } from './deskConfig'
import { loadDeskConfig } from './queries'

afterEach(() => vi.unstubAllGlobals())

/** One chassis answer, in the shape `GET /api/file` gives. */
function respond(status: number, body: unknown) {
  vi.stubGlobal('fetch', async () => ({
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    text: async () => (typeof body === 'string' ? body : JSON.stringify(body))
  }))
}

describe('the desk-config read', () => {
  it('is the defaults, with no banner and no error, when the file is absent', async () => {
    respond(404, { error: 'no such file' })
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(effective.problems).toEqual([])
    expect(effective.note).toContain('no such file')
  })

  it('resolves to the defaults when there is no fetch stub at all', async () => {
    // The harness stubs none. `src/testing/setup.ts` installs a rejecting
    // default precisely so this case is exercised rather than dialling out.
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    // Not `note`: nothing answered, so nothing said the file is absent.
    expect(effective.readFailure!.reason).toContain('no fetch stub')
    expect(effective.readFailure!.responseReceived).toBe(false)
    expect(effective.note).toBeUndefined()
  })

  it('separates a file that is absent from a file that could not be read', async () => {
    respond(404, { error: 'no such file' })
    const absent = await loadDeskConfig()
    expect(absent.note).toContain('no such file')
    expect(absent.readFailure).toBeUndefined()

    for (const status of [403, 413, 500]) {
      respond(status, { error: `refused with ${status}` })
      const unread = await loadDeskConfig()
      expect(unread.config, `${status} is still the defaults`).toEqual(DESK_DEFAULTS)
      expect(unread.readFailure!.reason, `${status} is a read failure`).toContain(
        `refused with ${status}`
      )
      expect(unread.note, `${status} is not an absence`).toBeUndefined()
    }
  })

  it('resolves to the defaults, naming the parse error, when the file is not JSON', async () => {
    respond(200, { path: 'jpack-desk.json', bytes: 3, sha256: '', content: 'not json' })
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(effective.problems[0]!.reason).toContain('not JSON')
  })

  it('resolves to the defaults, naming the key, when a key is unknown', async () => {
    respond(200, {
      path: 'jpack-desk.json',
      bytes: 0,
      sha256: '',
      content: JSON.stringify({ deskConfigVersion: 1, colour: 'blue' })
    })
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(effective.problems.map((problem) => problem.key)).toEqual(['colour'])
  })

  it('applies a well-formed file', async () => {
    respond(200, {
      path: 'jpack-desk.json',
      bytes: 0,
      sha256: '',
      content: JSON.stringify({ deskConfigVersion: 1, organization: { name: 'Acme Co.' } })
    })
    const effective = await loadDeskConfig()
    expect(effective.config.organization.name).toBe('Acme Co.')
    expect(effective.sources.organization).toBe('project file')
  })

  it('carries the provenance of a failed read rather than inferring it', () => {
    // Three cases, three provenances, and none of them read off a sibling:
    // the chassis answered and its `{error}` is quoted; a response arrived
    // that this desk cannot use, so the sentence is the desk's; or nothing
    // answered at all.
    return (async () => {
      respond(413, { error: 'the file is too large to read' })
      const refused = await loadDeskConfig()
      expect(refused.readFailure).toEqual({
        reason: 'the file is too large to read',
        responseReceived: true,
        status: 413,
        source: 'chassis'
      })

      vi.stubGlobal('fetch', async () => {
        throw new TypeError('Failed to fetch')
      })
      const unreachable = await loadDeskConfig()
      expect(unreachable.readFailure!.responseReceived).toBe(false)
      expect(unreachable.readFailure!.source).toBe('browser')
      expect(unreachable.readFailure!.status).toBeUndefined()
      expect(unreachable.readFailure!.reason).toContain('Failed to fetch')
    })()
  })

  it('calls a malformed 200 an answer, because it is one', async () => {
    // The regression. A `200` whose body is not the envelope this API promises
    // used to throw a plain `Error`, which lost the status — so provenance
    // read off "is there a status?" put it in the transport-failure bucket and
    // Admin said the request never got an answer. It did.
    respond(200, 'not the envelope this API promises')
    const malformed = await loadDeskConfig()
    expect(malformed.config).toEqual(DESK_DEFAULTS)
    expect(malformed.readFailure!.responseReceived).toBe(true)
    expect(malformed.readFailure!.status).toBe(200)
    // The sentence is the desk's own: the chassis sent nothing to quote.
    expect(malformed.readFailure!.source).toBe('desk')
    expect(malformed.readFailure!.reason).toContain('not JSON')
    expect(malformed.note).toBeUndefined()
  })

  it('resolves to the defaults when the fetch itself throws', async () => {
    vi.stubGlobal('fetch', async () => {
      throw new TypeError('Failed to fetch')
    })
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    // A socket that never answered is not a project without a file.
    expect(effective.readFailure!.reason).toContain('Failed to fetch')
    expect(effective.note).toBeUndefined()
  })
})

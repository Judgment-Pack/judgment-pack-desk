/**
 * The config read, and the one property it must have: **it never rejects.**
 *
 * A 404, a body that is not JSON, a file the chassis refused, and no `fetch`
 * stub at all are all the same answer here — the built-in defaults, with the
 * reason kept for Admin. The last one is load-bearing: `testing/harness.tsx`
 * stubs no fetch, so a shell query that rejected would land an unhandled
 * rejection in every future integration test, from a query that has nothing to
 * do with the case under test.
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
    expect(effective.note).toContain('no fetch stub')
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

  it('resolves to the defaults when the fetch itself throws', async () => {
    vi.stubGlobal('fetch', async () => {
      throw new TypeError('Failed to fetch')
    })
    const effective = await loadDeskConfig()
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(effective.note).toContain('Failed to fetch')
  })
})

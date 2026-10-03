/**
 * The standing of a release comes from the review of the desk the page is on:
 * the startup desk names no desk, and a named desk names itself on the
 * request, as every other call does. Each desk answers with its own lock.
 */
import { createHash } from 'node:crypto'
import { afterEach, expect, it, vi } from 'vitest'

const sha = (text: string) => 'sha256:' + createHash('sha256').update(Buffer.from(text, 'utf8')).digest('hex')
const bytes = '{"title":"Alpha"}\n'
const named = 'c'.repeat(32)

/** A review whose lock pins `locked` for alpha. */
const reviewLocking = (locked: string) => ({ status: 'valid', locked: true, findings: [], diagnostics: [], contents: {}, token: 'f'.repeat(64), files: [
  { kind: 'config', path: 'jpack.json', lock: 'same', locked: 'sha256:config', now: { state: 'text', digest: 'sha256:config' } },
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'same', locked, now: { state: 'text', digest: locked } }
] })

afterEach(() => { sessionStorage.clear(); vi.resetModules(); vi.unstubAllGlobals() })

/** The standing of `bytes` as alpha, read on the desk the page is on. */
async function standingOn(desk: string | undefined) {
  if (desk) sessionStorage.setItem('jpack.active-desk', desk)
  vi.resetModules()
  ;(await import('../mcp/session')).giveThisPageASessionForTesting('session')
  const asked: { url: string; desk: string | undefined }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init: RequestInit) => {
    const header = (init.headers as Record<string, string>)['X-Jpack-Desk']
    asked.push({ url, desk: header })
    // The startup desk's lock pins these bytes; the named desk's pins others.
    return new Response(JSON.stringify(reviewLocking(header === named ? sha('{"title":"Other"}\n') : sha(bytes))), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
  const { readReleaseStanding } = await import('./reviewedSet')
  return { standing: await readReleaseStanding(bytes, 'alpha'), asked }
}

it('reads the startup desk’s review, naming no desk', async () => {
  const { standing, asked } = await standingOn(undefined)
  expect(asked).toEqual([{ url: '/api/review', desk: undefined }])
  expect(standing).toEqual({ state: 'reviewed', bytes: 'same', findings: [] })
})

it('reads a named desk’s review, naming that desk', async () => {
  const { standing, asked } = await standingOn(named)
  expect(asked).toEqual([{ url: '/api/review', desk: named }])
  expect(standing).toEqual({ state: 'draft', bytes: 'other', findings: [] })
})

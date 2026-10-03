/**
 * Jobs' "Review this release": whether the pack bytes a release is made from
 * are in the project's reviewed set (ADR-0009, question 4). The comparison is
 * of the release's exact bytes with the lock's own entry for the release's
 * decision id, from the review step's one reading, and the runtime's findings
 * for that pack are shown in the review step's words.
 */
import { createHash } from 'node:crypto'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import type { Review, ReviewFile, ReviewFinding } from '../packs/review/client'
import type { Release } from './client'
import { ReleaseStanding } from './ReleaseStanding'
import { readReleaseStanding, standingOf, type ReleaseStanding as Standing } from './reviewedSet'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

/** The digest of a text's UTF-8 bytes, computed apart from the code under test. */
const sha = (text: string) => 'sha256:' + createHash('sha256').update(Buffer.from(text, 'utf8')).digest('hex')
/** The bytes a release is made from: not ASCII, not compact, with a final newline. */
const bytes = '{\n  "title": "Décision · 審查",\n  "version": "1"\n}\n'
const other = '{"title":"Other"}\n'
const release = { id: 'release_1', packId: 'https://example.com/judgment-packs/alpha', pack: bytes, packVersion: '1', tests: 'passed' } as Release

const pack = (id: string, locked: string | undefined, now = locked): ReviewFile => ({ kind: 'pack', id, path: `packs/${id}.json`, lock: !locked ? 'none' : locked === now ? 'same' : 'other', ...(locked ? { locked } : {}), digest: now, now: { state: 'text', digest: now } })
/** A review of a locked project with alpha and beta, as the review step reads it. */
function review(alpha: string | undefined, { beta = sha(other), alphaNow = alpha, findings = [], ...rest }: { beta?: string; alphaNow?: string } & Partial<Review> = {}): Review {
  return { status: findings.length ? 'invalid' : 'valid', locked: true, diagnostics: [], contents: {}, token: 'f'.repeat(64), findings, files: [
    { kind: 'config', path: 'jpack.json', lock: 'same', locked: 'sha256:config', digest: 'sha256:config', now: { state: 'text', digest: 'sha256:config' } },
    pack('alpha', alpha, alphaNow), pack('beta', beta)
  ], ...rest }
}
const noLock: Review = { status: 'error', locked: false, findings: [], contents: {}, token: 'f'.repeat(64), diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock at jpack.lock.json.' }], files: [
  { kind: 'config', path: 'jpack.json', lock: 'none', now: { state: 'text', digest: 'sha256:config' } }, pack('alpha', undefined)
] }
const configDrift: ReviewFinding = { name: 'config-drift', path: 'jpack.json', detail: 'The configuration’s own bytes differ from the reviewed set.' }
const alphaDrift: ReviewFinding = { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/alpha.json', detail: 'The pack document’s bytes differ from the reviewed set.' }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let answer: () => Response
beforeEach(() => {
  answer = () => json(200, review(sha(bytes)))
  vi.mocked(deskFetch).mockImplementation(async url => {
    if (url !== '/api/review') throw new Error(`unexpected ${String(url)}`)
    return answer()
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('the comparison', () => {
  it('is reviewed only where the lock pins the exact bytes the release is made from', async () => {
    expect(await readReleaseStanding(bytes, 'alpha')).toEqual({ state: 'reviewed', bytes: 'same', findings: [] })
    expect(deskFetch).toHaveBeenCalledWith('/api/review', expect.anything())
  })

  it('compares the exact bytes: not reformatted, not trimmed, not another encoding', async () => {
    const latin1 = 'sha256:' + createHash('sha256').update(Buffer.from(bytes, 'latin1')).digest('hex')
    for (const near of [sha(JSON.stringify(JSON.parse(bytes))), sha(bytes.trim()), sha(bytes.replaceAll('\n', '\r\n')), latin1]) {
      answer = () => json(200, review(near))
      expect(await readReleaseStanding(bytes, 'alpha')).toEqual({ state: 'draft', bytes: 'other', findings: [] })
    }
  })

  it('compares with the lock’s entry for the release’s decision id, and no other', async () => {
    answer = () => json(200, review(sha(other), { beta: sha(bytes) }))
    expect(await readReleaseStanding(bytes, 'alpha')).toEqual({ state: 'draft', bytes: 'other', findings: [] })
    expect(await readReleaseStanding(bytes, 'beta')).toEqual({ state: 'reviewed', bytes: 'same', findings: [] })
    // The pack document's own id keys nothing in the lock.
    expect(await readReleaseStanding(bytes, release.packId)).toEqual({ state: 'draft', bytes: 'none', findings: [] })
  })

  it('is a draft where the lock pins other bytes for the id, or none', () => {
    expect(standingOf(review(sha(other)), 'alpha', sha(bytes))).toEqual({ state: 'draft', bytes: 'other', findings: [] })
    expect(standingOf(review(undefined), 'alpha', sha(bytes))).toEqual({ state: 'draft', bytes: 'none', findings: [] })
    // The file now holds the release's bytes, and the lock does not: the
    // file's own digest is not the lock's entry.
    expect(standingOf(review(sha(other), { alphaNow: sha(bytes), findings: [alphaDrift] }), 'alpha', sha(bytes))).toEqual({ state: 'draft', bytes: 'other', findings: [alphaDrift] })
    expect(standingOf(review(undefined, { alphaNow: sha(bytes) }), 'alpha', sha(bytes))).toEqual({ state: 'draft', bytes: 'none', findings: [] })
  })

  it('says so where the project keeps no lock', () => {
    expect(standingOf(noLock, 'alpha', sha(bytes))).toEqual({ state: 'no-lock' })
  })

  it('holds every id where the project file changed after the lock, whatever the bytes', () => {
    for (const [locked, bytesAre] of [[sha(bytes), 'same'], [sha(other), 'other'], [undefined, 'none']] as const) {
      expect(standingOf(review(locked, { findings: [configDrift] }), 'alpha', sha(bytes))).toEqual({ state: 'config-drift', bytes: bytesAre, findings: [configDrift] })
    }
  })

  it('carries the runtime’s findings for that pack, and none about another', () => {
    const gamma: ReviewFinding = { name: 'lock-entry-missing', kind: 'pack', id: 'gamma', path: 'packs/gamma.json' }
    const betaDrift: ReviewFinding = { ...alphaDrift, id: 'beta', path: 'packs/beta.json' }
    // The pack was edited after the release was checked: the release's bytes
    // are the locked ones, and the runtime finds the file changed.
    expect(standingOf(review(sha(bytes), { alphaNow: sha(other), findings: [alphaDrift, betaDrift, gamma] }), 'alpha', sha(bytes))).toEqual({ state: 'reviewed', bytes: 'same', findings: [alphaDrift] })
  })

  it('does not say where the review cannot be read', async () => {
    expect(standingOf(review(sha(bytes), { blocked: 'packs/alpha.json is larger than 1048576 bytes, which is more than Desk shows', files: [], token: undefined }), 'alpha', sha(bytes)))
      .toEqual({ state: 'unreadable', reason: 'packs/alpha.json is larger than 1048576 bytes, which is more than Desk shows' })
    expect(standingOf(review(sha(bytes), { status: 'error', diagnostics: [{ code: 'JPS-LOCK-JSON', message: 'The reviewed-set lock jpack.lock.json is not acceptable JSON.' }] }), 'alpha', sha(bytes)))
      .toEqual({ state: 'unreadable', reason: 'The reviewed-set lock jpack.lock.json is not acceptable JSON.' })
    answer = () => json(409, { code: 'bad_request', error: 'This project’s runtime reads /elsewhere/jpack.json, which JPACK_CONFIG names, and not this project’s jpack.json, so Desk does not review or lock it here.' })
    await expect(readReleaseStanding(bytes, 'alpha')).rejects.toThrow('JPACK_CONFIG names')
    answer = () => json(200, { status: 'valid' })
    await expect(readReleaseStanding(bytes, 'alpha')).rejects.toThrow('The review could not be loaded')
    // A lock entry that is not a digest's text is not an answer to compare with.
    const odd = review(sha(bytes)); (odd.files[1] as unknown as { locked: unknown }).locked = 5
    answer = () => json(200, odd)
    await expect(readReleaseStanding(bytes, 'alpha')).rejects.toThrow('The review could not be loaded')
  })
})

function show(packId = 'alpha') {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter><ReleaseStanding release={release} packId={packId} /></MemoryRouter></QueryClientProvider>)
}
const shown = async (state: Standing['state']) => {
  await waitFor(() => expect(document.querySelector('[data-standing]')?.getAttribute('data-standing')).toBe(state))
  return document.querySelector('[data-standing]')!.textContent ?? ''
}
/** The standing's own label, beside its heading. */
const label = () => document.querySelector('[data-label]')?.textContent
const meaning = '“In the reviewed set” means that the project’s jpack.lock.json pins these exact bytes for this decision id. It does not say the pack is right, or who reviewed it. It is shown, not enforced: the job is created the same way either way.'
const runner = 'Runner locks each release on its own and runs it under that lock, so the audit record of every run of this job says reviewed: true. That field is about Runner’s lock of this release, not this project’s reviewed set.'
const reviewLink = () => screen.queryByRole('link', { name: 'Review and lock' })

describe('the display', () => {
  it('shows a release in the reviewed set, and what the runtime finds about its pack', async () => {
    show()
    expect(screen.getByRole('status').textContent).toBe('Comparing the pack bytes of this release with the project’s lock…')
    const text = await shown('reviewed')
    expect(label()).toBe('In the reviewed set')
    expect(text).toContain('Desk compared the SHA-256 of the pack bytes this release is made from with the project’s lock. The lock pins these exact bytes for alpha.')
    expect(text).toContain('The runtime’s packs verify finds nothing about alpha in the project now.')
    expect(screen.getByText(meaning)).toBeTruthy(); expect(screen.getByText(runner)).toBeTruthy()
    expect(reviewLink()).toBeNull()
  })

  it.each([
    ['other bytes', sha(other), 'The lock pins other bytes for alpha.'],
    ['no bytes', undefined, 'The lock pins no bytes for alpha.']
  ])('shows a draft where the lock pins %s for the id', async (_, locked, says) => {
    answer = () => json(200, review(locked, { findings: locked ? [alphaDrift] : [{ name: 'lock-entry-missing', kind: 'pack', id: 'alpha', path: 'packs/alpha.json' }] }))
    show()
    const text = await shown('draft')
    expect(label()).toBe('Draft'); expect(text).toContain(says)
    expect(text).toContain('This release is made from a draft: bytes that are not in the project’s reviewed set.')
    expect(text).toContain('What the runtime’s packs verify finds in the project now:')
    expect(text).toContain(locked ? 'Changed since the last lock' : 'New, never locked')
    expect(reviewLink()?.getAttribute('href')).toBe('/packs/_review')
    expect(screen.getByText(meaning)).toBeTruthy(); expect(screen.getByText(runner)).toBeTruthy()
  })

  it('shows a project with no lock', async () => {
    answer = () => json(200, noLock)
    show()
    const text = await shown('no-lock')
    expect(label()).toBe('No lock')
    expect(text).toContain('This project keeps no reviewed-set lock, so the pack bytes of this release are in no reviewed set.')
    expect(text).not.toContain('Desk compared')
    expect(reviewLink()).toBeTruthy()
  })

  it('shows a project file changed after the lock, with what the lock pins for the id', async () => {
    answer = () => json(200, review(sha(bytes), { findings: [configDrift] }))
    show()
    const text = await shown('config-drift')
    expect(label()).toBe('Project file changed')
    expect(text).toContain('The lock pins these exact bytes for alpha.')
    expect(text).toContain('The project’s jpack.json changed after that lock, so until the next lock the runtime refuses every deciding run by decision id in this project, alpha included.')
    expect(text).toContain('The project file changed; every decision waits for a lock')
    expect(text).not.toContain('This release is made from a draft')
    expect(reviewLink()).toBeTruthy()
  })

  it('says it does not say where the review cannot be read, with the desk’s reason', async () => {
    answer = () => json(409, { code: 'bad_request', error: 'This project’s runtime reads /elsewhere/jpack.json, which JPACK_CONFIG names, and not this project’s jpack.json, so Desk does not review or lock it here.' })
    show()
    const text = await shown('unreadable')
    expect(label()).toBe('Not known')
    expect(text).toContain('Desk could not read the project’s review, so this does not say whether the pack bytes of this release are in its reviewed set.')
    expect(text).toContain('JPACK_CONFIG names')
    expect(text).not.toContain('Desk compared')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText(meaning)).toBeTruthy()
  })
})

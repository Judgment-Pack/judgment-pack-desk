/**
 * Review and lock on the page: what it reads, what it shows, and that it
 * locks only on the owner's confirmation, sending back the token of exactly
 * the reading it showed.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../../files/client'
import { McpContext } from '../../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../../testing/harness'
import { PacksPane } from '../PacksPane'
import { confirmLock, readReview, StaleReview, type Review } from './client'
import { ReviewAndLockView } from './ReviewAndLockView'
import { ReviewProvider } from './ReviewContext'

vi.mock(import('../../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const token = 'f'.repeat(64)
const config = '{"configVersion":"5","packs":{"alpha":{"path":"packs/a.json"},"beta":{"path":"packs/b.json"}}}'
const before = '{"id":"alpha","title":"Before"}'
const after = '{"id":"alpha","title":"After"}'
const beta = '{"id":"beta","title":"Beta"}'
/** A side showing text, whose text the review carries in its contents. */
const text = (value: string) => ({ state: 'text' as const, digest: 'sha256:' + value })
const contents = Object.fromEntries([config, before, after, beta].map(value => ['sha256:' + value, value]))
const differing: Review = { status: 'invalid', locked: true, diagnostics: [], token, contents, findings: [
  { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json', detail: 'The pack document’s bytes differ.' },
  { name: 'document-drift', kind: 'pack', id: 'beta', path: 'packs/b.json' }
], files: [
  { kind: 'config', path: 'jpack.json', lock: 'same', now: text(config) },
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'other', earlier: text(before), now: text(after) },
  { kind: 'pack', id: 'beta', path: 'packs/b.json', lock: 'other', earlier: { state: 'no-copy' }, now: text(beta) }
] }
const first: Review = { status: 'error', locked: false, findings: [], token, contents, diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock.' }], files: [
  { kind: 'config', path: 'jpack.json', lock: 'none', now: text(config) },
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'none', now: text(after) },
  { kind: 'pack', id: 'beta', path: 'packs/b.json', lock: 'none', now: text(beta) }
] }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let reviews: Review[] = []
let posted: unknown[] = []
let lockAnswer: () => Response
beforeEach(() => {
  posted = []
  lockAnswer = () => json(200, { files: 3, copies: 'stored' })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (url === '/api/review/lock') {
      posted.push({ method: init?.method, type: (init?.headers as Record<string, string>)['Content-Type'], body: JSON.parse(String(init?.body)) })
      return lockAnswer()
    }
    return json(200, reviews.length > 1 ? reviews.shift() : reviews[0])
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  render(<QueryClientProvider client={testQueryClient()}><MemoryRouter><ReviewProvider><ReviewAndLockView /></ReviewProvider></MemoryRouter></QueryClientProvider>)
}
const file = (path: string) => document.querySelector(`[data-file="${path}"]`) as HTMLElement

describe('the review client', () => {
  it('refuses an answer that is not a review', async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { status: 'valid' }))
    await expect(readReview()).rejects.toThrow('The review could not be loaded')
  })
  it('sends the review’s token as JSON, and tells a stale confirmation apart', async () => {
    lockAnswer = () => json(409, { code: 'stale', error: 'The project changed after you reviewed it.' })
    await expect(confirmLock(token)).rejects.toBeInstanceOf(StaleReview)
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { token } }])
    lockAnswer = () => json(500, { code: 'internal', error: 'The runtime did not lock the project.' })
    await expect(confirmLock(token)).rejects.toThrow('The runtime did not lock the project.')
  })
})

describe('the review page', () => {
  it('shows every file, each finding in plain words, and a diff only where the desk kept the locked bytes', async () => {
    reviews = [differing]
    show()
    expect(await screen.findAllByText('Changed since the last lock')).toHaveLength(2)
    expect(screen.getByText('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')).toBeTruthy()
    expect(screen.getByText(/A change to jpack.json holds every pack/)).toBeTruthy()
    expect(within(file('jpack.json')).getByText('Matches the last lock')).toBeTruthy()
    expect(within(file('packs/a.json')).getByText('The pack document’s bytes differ.')).toBeTruthy()
    // Alpha's locked bytes are kept, so its changes are drawn; beta's are not.
    expect(within(file('packs/a.json')).getByRole('region', { name: 'Pack changes' })).toBeTruthy()
    expect(within(file('packs/b.json')).queryByRole('region', { name: 'Pack changes' })).toBeNull()
    expect(within(file('packs/b.json')).getByText('Desk has no earlier copy of the locked version to compare with.')).toBeTruthy()
    expect(file('packs/b.json').textContent).toContain(beta)
  })

  it('locks nothing until the owner confirms, then sends back the token of what it showed', async () => {
    reviews = [differing]
    show()
    const button = await screen.findByRole('button', { name: 'Confirm and lock 2 differences' })
    expect(screen.getByText('This lock covers 3 files: jpack.json and every pack and graph it declares.')).toBeTruthy()
    expect(posted).toEqual([])
    fireEvent.click(button)
    expect((await screen.findByRole('status')).textContent).toContain('Locked. These 3 files are now this project’s reviewed set.')
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { token } }])
  })

  it('shows the step again when the project changed after the review', async () => {
    reviews = [differing, { ...differing, findings: differing.findings.slice(0, 1) }]
    lockAnswer = () => json(409, { code: 'stale', error: 'stale' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm and lock 2 differences' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The project changed after you reviewed it, so nothing was locked. Review the changes again.')
    await screen.findByRole('button', { name: 'Confirm and lock 1 difference' })
  })

  it('reads a first lock as a full review: every file, open', async () => {
    reviews = [first]
    show()
    await screen.findByText('This project has no reviewed-set lock yet, so every file below is new to it. Read each one before you lock.')
    expect(screen.getByText('There is no reviewed-set lock.')).toBeTruthy()
    for (const [path, text] of [['jpack.json', config], ['packs/a.json', after], ['packs/b.json', beta]] as const) {
      expect(within(file(path)).getByText('Not in a lock yet')).toBeTruthy()
      expect(file(path).querySelector('details')?.open).toBe(true)
      expect(file(path).textContent).toContain(text)
    }
    expect(screen.getByRole('button', { name: 'Confirm and lock 3 files' })).toBeTruthy()
  })

  it('offers no lock when every file matches, when a file cannot be shown, or without a token', async () => {
    reviews = [{ ...first, status: 'valid', locked: true, diagnostics: [], files: first.files.map(item => ({ ...item, lock: 'same' as const })) }]
    show()
    await screen.findByText('Every file matches the reviewed set. There is nothing to lock.')
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
    cleanup()
    // The desk gives no token for a reading it cannot show; the page offers
    // no lock for one even if a token came.
    reviews = [{ ...first, blocked: 'packs/b.json is larger than 1048576 bytes, which is more than Desk shows', files: [first.files[0]!, first.files[1]!, { ...first.files[2]!, now: { state: 'not-shown' } }] }]
    show()
    expect((await screen.findByRole('alert')).textContent).toContain('Desk cannot lock this project here until it can show you every file a lock would cover.')
    expect(screen.getByText('The current file is too large to show here.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
  })
})

describe('the collection', () => {
  it('shows each pack’s finding beside its name, and the way to review them', async () => {
    reviews = [differing]
    const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [{ id: 'alpha' }, { id: 'beta' }, { id: 'gamma' }] }) }) })
    render(<QueryClientProvider client={testQueryClient()}><MemoryRouter><McpContext.Provider value={connected({ client: stub.client })}><ReviewProvider><PacksPane /></ReviewProvider></McpContext.Provider></MemoryRouter></QueryClientProvider>)
    const alpha = await screen.findByRole('link', { name: /alpha/ })
    await waitFor(() => expect(alpha.textContent).toContain('Changed since the last lock'))
    expect(screen.getByRole('link', { name: /gamma/ }).textContent).not.toContain('Changed since the last lock')
    expect(screen.getByRole('link', { name: 'Review and lock' }).getAttribute('href')).toBe('/packs/_review')
  })
})

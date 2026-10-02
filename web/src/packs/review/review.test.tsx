/**
 * Review and lock on the page: what it reads, what it shows, and that it
 * locks only on the owner's confirmation, sending back exactly the set the
 * review showed.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

const set = { config: 'sha256:' + '0'.repeat(64), entries: [
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', digest: 'sha256:' + '1'.repeat(64) },
  { kind: 'pack', id: 'beta', path: 'packs/b.json', digest: 'sha256:' + '2'.repeat(64) }
] }
const before = '{"id":"alpha","title":"Before"}'
const after = '{"id":"alpha","title":"After"}'
const differing: Review = { status: 'invalid', locked: true, diagnostics: [], set, findings: [
  { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json', detail: 'The pack document’s bytes differ.', earlier: { state: 'text', text: before }, now: { state: 'text', text: after } },
  { name: 'document-drift', kind: 'pack', id: 'beta', path: 'packs/b.json', earlier: { state: 'no-copy' }, now: { state: 'text', text: '{}' } }
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

describe('the review client', () => {
  it('refuses an answer that is not a review', async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { status: 'valid' }))
    await expect(readReview()).rejects.toThrow('The review could not be loaded')
  })
  it('sends the confirmed set as JSON, and tells a stale confirmation apart', async () => {
    lockAnswer = () => json(409, { code: 'stale', error: 'The project changed after you reviewed it.' })
    await expect(confirmLock(set)).rejects.toBeInstanceOf(StaleReview)
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { set } }])
    lockAnswer = () => json(500, { code: 'internal', error: 'The runtime did not lock the project.' })
    await expect(confirmLock(set)).rejects.toThrow('The runtime did not lock the project.')
  })
})

describe('the review page', () => {
  it('shows each finding in plain words, with a diff only where the desk kept the locked bytes', async () => {
    reviews = [differing]
    show()
    expect(await screen.findAllByText('Changed since the last lock')).toHaveLength(2)
    expect(screen.getByText('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')).toBeTruthy()
    expect(screen.getByText(/A change to jpack.json holds every pack/)).toBeTruthy()
    expect(screen.getByText('The pack document’s bytes differ.')).toBeTruthy()
    // Alpha's locked bytes are kept, so its changes are drawn; beta's are not.
    expect(screen.getAllByRole('region', { name: 'Pack changes' })).toHaveLength(1)
    expect(screen.getByText('Desk has no earlier copy of the locked version to compare with.')).toBeTruthy()
  })

  it('locks nothing until the owner confirms, then sends back exactly the set it showed', async () => {
    reviews = [differing]
    show()
    const button = await screen.findByRole('button', { name: 'Confirm and lock 2 differences' })
    expect(screen.getByText('This lock covers 3 files: jpack.json and every pack and graph it declares.')).toBeTruthy()
    expect(posted).toEqual([])
    fireEvent.click(button)
    expect((await screen.findByRole('status')).textContent).toContain('Locked. These 3 files are now this project’s reviewed set.')
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { set } }])
  })

  it('shows the step again when the project changed after the review', async () => {
    reviews = [differing, { ...differing, findings: differing.findings.slice(0, 1) }]
    lockAnswer = () => json(409, { code: 'stale', error: 'stale' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm and lock 2 differences' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The project changed after you reviewed it, so nothing was locked. Review the changes again.')
    await screen.findByRole('button', { name: 'Confirm and lock 1 difference' })
  })

  it('offers no lock when every file matches, or when a file could not be read', async () => {
    reviews = [{ status: 'valid', locked: true, diagnostics: [], set, findings: [] }]
    show()
    await screen.findByText('Every file matches the reviewed set. There is nothing to lock.')
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
    cleanup()
    reviews = [{ ...differing, set: null, unreadable: 'packs/b.json could not be read' }]
    show()
    expect((await screen.findByRole('alert')).textContent).toContain('Desk could not read every file a lock would cover')
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
  })

  it('offers the first lock of a project with none, and says what the runtime said', async () => {
    reviews = [{ status: 'error', locked: false, findings: [], set, diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock.' }] }]
    show()
    await screen.findByText('This project has no reviewed-set lock yet.')
    expect(screen.getByText('There is no reviewed-set lock.')).toBeTruthy()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm and lock' })).toBeTruthy())
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

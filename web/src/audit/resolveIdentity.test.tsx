/**
 * The decision record's question on this project's identity, where it was
 * written in another folder that no longer holds it (issue #309): the
 * question and its two answers; each answer's confirmation, which alone sends
 * the token the record gave for that answer; what the answer did, kept
 * across the check the panel runs after it; a refusal, in Desk's words; and
 * the client's checks of what the chassis answers.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditIdentity, isAuditRecord, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const moved = 'ab'.repeat(32)
const copy = 'cd'.repeat(32)
const report: AuditReport = { status: 'valid', lines: 1, bytes: 400, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 1, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-supplied' }, signedRecords: 0, unsignedRecords: 1,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 1, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } },
  segments: [{ firstLine: 1, lastLine: 1 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: [], doesNotEstablish: [] }
type Reported = Extract<AuditRecord, { state: 'report' }>
const asked: Reported = { state: 'report', runtime: '0.27.1', report, keys: { state: 'startup' }, identity: { state: 'unresolved', moved, copy } }
const QUESTION = 'This project’s identity was written in another folder, which no longer holds it. Desk cannot tell whether this folder is that one, moved here, or a copy of it, so it makes, rotates and recovers no signing key under that identity until you say which.'

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let answer: (body: { choice: string }) => Response
let checks: number
let posts: unknown[]
beforeEach(() => {
  checks = 0
  posts = []
  records = [asked]
  answer = body => json(200, { state: body.choice })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url) === '/api/audit/verify') {
      checks++
      return json(200, records.length > 1 ? records.shift()! : records[0]!)
    }
    if (String(url) === '/api/project/identity' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { choice: string }
      posts.push(body)
      return answer(body)
    }
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  return render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}
const region = () => within(screen.getByRole('region', { name: 'This project’s identity' }))

describe('the question on this project’s identity', () => {
  it('asks which this folder is, and sends nothing until an answer is confirmed', async () => {
    show()
    expect((await screen.findByText(QUESTION)).getAttribute('role')).toBe('alert')
    fireEvent.click(region().getByRole('button', { name: 'This folder was moved here' }))
    const dialog = await screen.findByRole('dialog', { name: 'Was this folder moved here?' })
    expect(within(dialog).getByText('Desk names this folder by the project’s identity from now on, and decides what a stopped key creation or rotation left under it when it next starts. Answer this only if this is the folder the identity was written in, and no copy of it is in use.')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(posts).toEqual([])
  })

  it('sends the moved answer with its own token, says what it did, and checks the trail again', async () => {
    records = [asked, { ...asked, identity: undefined }]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'This folder was moved here' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'This folder was moved here' }))
    expect(await screen.findByText('This folder keeps the project’s identity from now on. Desk decides what a stopped key creation or rotation left under it when it next starts.')).toBeTruthy()
    expect(posts).toEqual([{ choice: 'moved', token: moved }])
    expect(checks).toBe(2)
    expect(screen.queryByText(QUESTION)).toBeNull()
  })

  it('sends the copy answer with its own token, after saying what a copy keeps', async () => {
    records = [asked, { ...asked, identity: undefined }]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'This is a copy' }))
    const dialog = await screen.findByRole('dialog', { name: 'Is this folder a copy?' })
    expect(within(dialog).getByText('Desk gives this folder an identity of its own. What Desk keeps under the identity it was copied with, its signing key and stamping settings, stays as it is for the folder that identity was written in. This folder’s jpack.json still names that signing key until you change it.')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'This is a copy' }))
    expect(await screen.findByText('This folder has an identity of its own from now on.')).toBeTruthy()
    expect(posts).toEqual([{ choice: 'copy', token: copy }])
  })

  it('says a refusal in Desk’s words', async () => {
    const stale = "This project's identity changed after the decision record showed it, so nothing was changed. Check the decision record again."
    answer = () => json(409, { error: stale, code: 'stale' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'This is a copy' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'This is a copy' }))
    expect((await screen.findByText(stale)).getAttribute('role')).toBe('alert')
    expect(screen.queryByText('This folder has an identity of its own from now on.')).toBeNull()
  })

  it('takes the question only as the chassis gives it', () => {
    expect(isAuditIdentity({ state: 'unresolved', moved, copy })).toBe(true)
    for (const other of [{ state: 'unresolved', moved, copy: moved }, { state: 'unresolved', moved }, { state: 'resolved', moved, copy }, { state: 'unresolved', moved: 'x', copy }]) {
      expect(isAuditIdentity(other)).toBe(false)
    }
    expect(isAuditRecord(asked)).toBe(true)
    expect(isAuditRecord({ ...asked, identity: { state: 'unresolved', moved } })).toBe(false)
  })
})

/**
 * Desk's archive of keys on the decision record (the maintainer's decision of
 * 2026-10-08: Desk never removes a signing key on its own): every archived
 * file, with why; Remove on each file that is there, whose confirmation alone
 * sends the token the record gave for it; what the removal did, kept across
 * the check the panel runs after it; a refusal, in Desk's words; the archive
 * beside every answer, a refusal among them; and the client's checks of what
 * the chassis answers.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { ARCHIVE_SENTENCES, isAuditArchive, isAuditRecord, type ArchivedKey, type AuditArchive, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const desk = 'ab'.repeat(16)
const trail = '32'.repeat(16)
const promoted: ArchivedKey = { scope: 'desk', identity: desk, file: `${trail}-1-20261008T120001.000000000Z.seed`, kind: 'seed', trail, sequence: 1,
  at: '2026-10-08T12:00:01Z', rule: 'rotation-promoted', why: 'The runtime handed this key’s signing over to the next key after record 1.', own: true, token: 'cd'.repeat(32) }
const gone: ArchivedKey = { scope: 'runner', identity: desk, file: 'none-none-20261008T120002.000000000Z.keys.jsonl', kind: 'keys.jsonl',
  at: '2026-10-08T12:00:02Z', rule: 'creation-stopped', why: 'Desk’s journal names this file, and it is not in the archive now.', missing: true }
const archive: AuditArchive = { entries: [promoted, gone], more: 3 }
const report: AuditReport = { status: 'valid', lines: 1, bytes: 400, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 1, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-supplied' }, signedRecords: 0, unsignedRecords: 1,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 1, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } },
  segments: [{ firstLine: 1, lastLine: 1 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: [], doesNotEstablish: [] }
const listed: AuditRecord = { state: 'report', runtime: '0.27.1', report, keys: { state: 'none' }, archive }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let answer: () => Response
let checks: number
let posts: unknown[]
beforeEach(() => {
  checks = 0
  posts = []
  records = [listed]
  answer = () => json(200, { state: 'removed' })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url) === '/api/audit/verify') {
      checks++
      return json(200, records.length > 1 ? records.shift()! : records[0]!)
    }
    if (String(url) === '/api/audit/key/archive/remove' && init?.method === 'POST') {
      posts.push(JSON.parse(String(init.body)))
      return answer()
    }
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  return render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}
const region = () => within(screen.getByRole('region', { name: 'Archived keys' }))

describe('the archive of keys', () => {
  it('lists each archived file, why it is there, and offers Remove only where it is there', async () => {
    show()
    await screen.findByRole('region', { name: 'Archived keys' })
    const items = within(region().getByRole('list', { name: 'What Desk archived' })).getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(within(items[0]!).getByText('A signing key')).toBeTruthy()
    expect(within(items[0]!).getByText(`Kept under ${desk}, this desk’s name`)).toBeTruthy()
    expect(within(items[0]!).getByText(`Trail ${trail}, record 1`)).toBeTruthy()
    expect(within(items[0]!).getByText('Archived 2026-10-08T12:00:01Z')).toBeTruthy()
    expect(within(items[0]!).getByText(promoted.why)).toBeTruthy()
    expect(within(items[0]!).getByRole('button', { name: 'Remove' })).toBeTruthy()
    expect(within(items[1]!).getByText('A list of public keys')).toBeTruthy()
    expect(within(items[1]!).getByText(`Kept for Runner under ${desk}`)).toBeTruthy()
    expect(within(items[1]!).getByText('This file is not in the archive now.')).toBeTruthy()
    expect(within(items[1]!).queryByRole('button')).toBeNull()
    expect(region().getByText('And 3 more, older, not listed.')).toBeTruthy()
  })

  it('sends nothing until a removal is confirmed', async () => {
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    const dialog = await screen.findByRole('dialog', { name: 'Remove this archived file?' })
    expect(within(dialog).getByText('Desk removes this file from its archive of keys for good, and cannot bring it back.')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(posts).toEqual([])
  })

  it('sends the file with its own token, says what it did, and checks the trail again', async () => {
    records = [listed, { ...listed, archive: { entries: [gone] } }]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove for good' }))
    expect(await screen.findByText(`The archived file ${promoted.file} was removed.`)).toBeTruthy()
    expect(posts).toEqual([{ scope: 'desk', identity: desk, file: promoted.file, token: promoted.token }])
    expect(checks).toBe(2)
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull()
  })

  it('says a refusal in Desk’s words', async () => {
    const stale = 'That archived file changed after the decision record showed it, so nothing was removed. Check the decision record again.'
    answer = () => json(409, { error: stale, code: 'stale' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove for good' }))
    expect((await screen.findByText(stale)).getAttribute('role')).toBe('alert')
  })

  it('is listed where the project keeps no trail, and beside a refusal to check', async () => {
    records = [{ state: 'no-trail', archive }]
    show()
    expect(await screen.findByRole('region', { name: 'Archived keys' })).toBeTruthy()
    cleanup()
    for (const status of [409, 500]) {
      vi.mocked(deskFetch).mockImplementation(async url => String(url) === '/api/audit/verify'
        ? json(status, { error: 'Desk could not check it.', code: 'internal', archive })
        : json(404, { error: 'not here' }))
      show()
      expect(await screen.findByRole('region', { name: 'Archived keys' }), String(status)).toBeTruthy()
      cleanup()
    }
  })

  it('takes the archive only as the chassis gives it', () => {
    expect(isAuditArchive(archive)).toBe(true)
    expect(isAuditArchive({ entries: [] })).toBe(true)
    for (const other of [
      { entries: [{ ...promoted, token: undefined }] },
      { entries: [{ ...gone, token: 'cd'.repeat(32) }] },
      { entries: [{ ...promoted, file: '../outside.seed' }] },
      { entries: [{ ...promoted, kind: 'other' }] },
      { entries: [{ ...promoted, identity: 'x' }] },
      { entries: [{ ...promoted, scope: 'elsewhere' }] },
      { entries: [{ ...promoted, why: '' }] },
      { entries: [promoted], more: -1 }
    ]) {
      expect(isAuditArchive(other), JSON.stringify(other)).toBe(false)
    }
    expect(isAuditRecord(listed)).toBe(true)
    expect(isAuditRecord({ ...listed, archive: { entries: [{ ...promoted, token: 'x' }] } })).toBe(false)
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/archive.go'), 'utf8')
    expect(ARCHIVE_SENTENCES).toHaveLength(6)
    for (const sentence of ARCHIVE_SENTENCES) {
      for (const part of sentence.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, sentence).toBe(part)
      }
    }
  })
})

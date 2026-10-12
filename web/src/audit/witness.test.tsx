/**
 * The witness on the decision record (ADR-0012): none by default, with
 * section 5's rows; a witness set, its cursor, the head Desk fetched, and the
 * runtime's reading of the chain, current or historical; the witness's own
 * words where it refused, and Desk's only words where it did not answer as
 * asked; adding a witness, checked and then confirmed, with the
 * confirmation's sentences and the trail identity before anything is kept,
 * and the submitter's token sent once and never shown again; "Send now",
 * removal, and the one countersigned check the owner asks for, which is kept
 * nowhere. The client's checks of what the chassis answers, and that each of
 * Desk's own sentences about a witness is one the chassis says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, isAuditReport, isAuditWitnessHolder, isWitnessAttempt, WITNESS_REASONS, type AuditRecord, type AuditReport, type AuditWitness, type AuditWitnessHolder } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const trail = '9a5ef41d74e7d7e003c8a34cff056351'
const key = '3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29'
const removeToken = 'ab'.repeat(48)
const checkToken = 'cd'.repeat(48)
const SUBMITTER = 'eyJ.submitter-token.never-shown'
const coverage: AuditReport['coverage'] = { legacyPrefix: 0, chained: 3, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked', detail: 'no public key was supplied' },
  signedRecords: 0, unsignedRecords: 0, checkpointed: { status: 'through', through: 3 }, witnessed: 3, unwitnessed: 0, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' },
  countersigned: { status: 'through', through: 3 } }
// What runtime 0.30.0 reports of a witness's chain read to the head Desk
// fetched, from Desk's own continuation.
const current: AuditWitness = { keysSupplied: 1, statementsRead: 3, statementsChecked: 3, began: 'continued', continuedAfter: 0, status: 'read', reading: 'current',
  headIndex: 1, highestIndex: 1, latestCheckpoint: { index: 1, sequence: 3, witnessedAt: '2026-10-12T00:00:00Z' }, conflicts: [], conflictsTotal: 0, retired: false,
  countersignedAt: '2026-10-12T00:00:00Z', continuationSaved: true }
const report: AuditReport = { status: 'valid', lines: 3, bytes: 3533, snapshotBetweenWrites: true, coverage, segments: [{ firstLine: 1, lastLine: 3 }], segmentsTotal: 1,
  discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0, establishes: [], doesNotEstablish: [], trail, witness: current }
const settings = { url: 'https://witness.example', publicKey: key, intervalMinutes: 60, setAt: 1791201600 }
const set: AuditWitnessHolder = { state: 'set', settings, trail, cursor: { through: 3, index: 1, digest: 'sha256:' + '0'.repeat(63) + '3', at: 1791205200 },
  head: { state: 'fetched', status: 200, index: 1 }, passed: true, removeToken,
  last: { at: 1791205200, outcome: 'countersigned', trail, from: 2, first: 3, through: 3, lines: 1, status: 200, index: 1 } }
type Reported = Extract<AuditRecord, { state: 'report' }>
const none: Reported = { state: 'report', runtime: '0.30.0', report: { ...report, coverage: { ...coverage, checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 3, countersigned: { status: 'not-checked' } }, witness: undefined },
  keys: { state: 'startup' }, signing: { state: 'no-key' }, stamping: { state: 'none' } }
const withWitness: Reported = { ...none, report, witness: set }

const CONFIRMATION = [
  'Choosing a witness is a trust decision.',
  'Each submission sends the witness the trail identity, sequences and record digests, never a record’s contents.',
  'Its reads are open: whoever holds one of the trail’s checkpoints can follow the trail there.',
  'Only the last line of a submission is countersigned.',
  'Whoever holds the token can poison the trail at the witness for good: a checkpoint signed at a sequence no honest one will exceed is not unsigned by revoking the token, and the trail is then retired, not repaired.',
  'The witness’s operator registers the trail first.'
]

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let fetched: string[]
let posts: { url: string; body: Record<string, unknown> }[]
let answers: Record<string, () => Response>
beforeEach(() => {
  fetched = []
  posts = []
  records = [none]
  answers = {
    '/api/audit/witness/check': () => json(200, { token: checkToken, shown: { url: 'https://witness.example', publicKey: key, intervalMinutes: 60 }, trail }),
    '/api/audit/witness': () => json(200, { state: 'set', settings }),
    '/api/audit/witness/remove': () => json(200, { state: 'none' }),
    '/api/audit/witness/send': () => json(200, { attempt: { ...set.last, requested: true } })
  }
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url).startsWith('/api/audit/verify')) {
      fetched.push(String(url))
      if (String(url).includes('countersignedThrough=')) {
        return json(200, { ...withWitness, report: { ...report, status: 'invalid', findingsTotal: 1,
          findings: [{ name: 'countersigned-coverage-missing', line: 0, detail: 'records 4 to 5 are not countersigned' }], requiredCountersigned: { through: 5, status: 'unmet' } } })
      }
      return json(200, records.length > 1 ? records.shift()! : records[0]!)
    }
    if (init?.method === 'POST' && answers[String(url)]) {
      posts.push({ url: String(url), body: JSON.parse(String(init.body)) })
      return answers[String(url)]!()
    }
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  return render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}
const region = () => within(screen.getByRole('region', { name: 'Witness' }))
/** The witness section, once the decision record has answered. */
const loaded = async () => within(await screen.findByRole('region', { name: 'Witness' }))
/** The witness section's text, apart from ADR-0012's rows: Desk's own words. */
function desksOwnWords(): string {
  const section = screen.getByRole('region', { name: 'Witness' }).cloneNode(true) as HTMLElement
  section.querySelector('[aria-label="What a witness establishes"]')?.remove()
  return section.textContent ?? ''
}

describe('witness', () => {
  it('sets no witness by default, and says ADR-0012’s rows beside it', async () => {
    show()
    await loaded()
    expect(await screen.findByText('No witness is set for this desk, so Desk submits nothing.')).toBeTruthy()
    const rows = within(region().getByLabelText('What a witness establishes'))
    expect(rows.getAllByRole('term').map(term => term.textContent)).toEqual([
      'A checkpoint statement, verified under the pinned key', 'The same, held against the trail', 'The chain read to a head Desk fetched',
      'A conflict statement', 'A retirement statement', 'The submitter’s token', 'Desk’s cursor and its record of submissions'])
    expect(rows.getAllByRole('definition')[0]!.textContent).toBe('Establishes: the key’s holder was given this checkpoint and signed it as statement index of its chain for the trail, and states that it did so at witnessedAt Does not establish: that the checkpoint names a real record; that it extends the one before; that the submitter was the trail’s operator; that witnessedAt is true; anything against a witness that colludes, or whose key is stolen')
    expect(region().queryByRole('button', { name: 'Send now' })).toBeNull()
    expect(screen.queryByText('Witness countersignature')).toBeNull()
    // Never Desk's own claim of verification, witnessing, proof or evidence.
    expect(desksOwnWords()).not.toMatch(/\b(verified|witnessed|proof|proves|evidence)\b/i)
  })

  it('shows a witness set: its cursor, the head fetched, the runtime’s current reading and countersigned coverage', async () => {
    records = [withWitness]
    show()
    await loaded()
    expect(await screen.findByText('Countersigned through record 3')).toBeTruthy()
    expect(region().getByText(/^The witness countersigned this desk’s trail through record 3, statement index 1;/)).toBeTruthy()
    expect(region().getByText('Desk fetched the witness’s head for this trail, statement index 1, and passed it to the check.')).toBeTruthy()
    const reading = within(region().getByLabelText('The runtime’s reading of the witness’s chain'))
    expect(reading.getByText('Current, to the head Desk fetched, index 1')).toBeTruthy()
    expect(reading.getByText('After index 0, from Desk’s own continuation')).toBeTruthy()
    expect(reading.getByText('Index 1, record 3, stated at 2026-10-12T00:00:00Z')).toBeTruthy()
    expect(region().getByText(/the witness countersigned the checkpoint at record 3, statement index 1, and Desk’s cursor moved to it\./)).toBeTruthy()
    expect(screen.getByText('Desk also gave the runtime the witness’s statements it keeps for this trail, and the witness’s key you pinned.')).toBeTruthy()
    expect(region().getByText(trail)).toBeTruthy()
    expect(desksOwnWords()).not.toMatch(/\b(verified|witnessed|proof|proves|evidence)\b/i)
  })

  it('says only that Desk could not reach the witness, and reads the chain as far as it was supplied', async () => {
    records = [{ ...withWitness, report: { ...report, witness: { ...current, reading: 'historical', headIndex: null } },
      witness: { ...set, head: { state: 'unreached', status: 503 }, last: { at: 1791205200, outcome: 'unreached', trail, from: 3, first: 4, through: 4, lines: 1 } } }]
    show()
    expect(await (await loaded()).findByText('Desk could not reach the witness (HTTP 503).')).toBeTruthy()
    expect(region().getByText('Historical: read only as far as it was supplied, with no head')).toBeTruthy()
    expect(region().getByText(/by Desk’s clock: Desk could not reach the witness\.$/)).toBeTruthy()
  })

  it('shows the witness’s refusals in its own words, the cursor unmoved', async () => {
    const sentence = 'Another record was offered for sequence 4, which the witness holds.'
    records = [{ ...withWitness, witness: { ...set, head: { state: 'refused', status: 404, reason: 'unknown-trail', error: 'The witness holds no statement for this trail.' },
      last: { at: 1791205200, outcome: 'refused', trail, from: 3, first: 4, through: 5, lines: 2, status: 409, reason: 'conflict', error: sentence } } }]
    show()
    const head = await (await loaded()).findByText('The witness holds no statement for this trail.')
    expect(head.closest('p')!.textContent).toBe('The witness answered HTTP 404: unknown-trail The witness holds no statement for this trail.')
    const last = region().getByText(sentence).closest('p')!
    expect(last.textContent).toMatch(/the witness answered HTTP 409: conflict Another record was offered for sequence 4, which the witness holds\.$/)
    expect(region().getByText(/^Desk’s cursor did not move\./)).toBeTruthy()
  })

  it('adds a witness behind its confirmation, sending the token once and never showing it', async () => {
    show()
    fireEvent.click(await (await loaded()).findByRole('button', { name: 'Add a witness' }))
    const form = within(screen.getByRole('form', { name: 'Witness' }))
    fireEvent.change(form.getByLabelText('The witness’s URL'), { target: { value: 'https://witness.example' } })
    fireEvent.change(form.getByLabelText('The witness’s public key'), { target: { value: key } })
    const token = form.getByLabelText('The submitter’s token') as HTMLInputElement
    expect(token.type).toBe('password')
    fireEvent.change(token, { target: { value: SUBMITTER } })
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Set this witness?' }))
    expect(dialog.getByRole('list', { name: 'What setting a witness means' }).querySelectorAll('li').length).toBe(CONFIRMATION.length)
    for (const sentence of CONFIRMATION) expect(dialog.getByText(sentence)).toBeTruthy()
    expect(dialog.getByText(trail)).toBeTruthy()
    // Checked with no token; nothing kept yet.
    expect(posts).toEqual([{ url: '/api/audit/witness/check', body: { url: 'https://witness.example', publicKey: key, intervalMinutes: 60 } }])
    records = [withWitness]
    fireEvent.click(dialog.getByRole('button', { name: 'Set the witness' }))
    await waitFor(() => expect(posts.map(post => post.url)).toEqual(['/api/audit/witness/check', '/api/audit/witness']))
    expect(posts[1]!.body).toEqual({ url: 'https://witness.example', publicKey: key, intervalMinutes: 60, submitterToken: SUBMITTER, token: checkToken })
    expect(await screen.findByText('The witness is set. Desk submits this desk’s new checkpoints to it at the interval, while it is running.')).toBeTruthy()
    expect(document.body.innerHTML).not.toContain(SUBMITTER)
  })

  it('sends now and removes, each checking the decision record again', async () => {
    records = [withWitness]
    show()
    fireEvent.click(await (await loaded()).findByRole('button', { name: 'Send now' }))
    await waitFor(() => expect(posts.map(post => post.url)).toEqual(['/api/audit/witness/send']))
    expect(posts[0]!.body).toEqual({})
    await waitFor(() => expect(fetched).toHaveLength(2))
    fireEvent.click(await (await loaded()).findByRole('button', { name: 'Remove the witness' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Remove this desk’s witness?' }))
    expect(dialog.getByText('The statements the witness signed, its key and Desk’s cursor are kept as they are, in the witness’s folder.')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Remove the witness' }))
    await waitFor(() => expect(posts.at(-1)).toEqual({ url: '/api/audit/witness/remove', body: { token: removeToken } }))
  })

  it('asks for the countersigned coverage once, and keeps nothing of it', async () => {
    records = [withWitness]
    show()
    const form = within(await (await loaded()).findByRole('form', { name: 'Check the countersigned coverage once' }))
    fireEvent.change(form.getByLabelText('Countersigned through record'), { target: { value: '5' } })
    fireEvent.click(form.getByRole('button', { name: 'Check once' }))
    const said = await form.findByRole('status')
    expect(said.textContent).toBe('The runtime did not find every record through record 5 countersigned. records 4 to 5 are not countersigned')
    expect(fetched).toEqual(['/api/audit/verify', '/api/audit/verify?countersignedThrough=5'])
    // The panel itself is as it was: its reading has no requirement.
    expect(screen.getByText('Countersigned through record 3')).toBeTruthy()
  })
})

describe('witness client', () => {
  it('reads the chassis’s word on a witness, each state with what it carries', () => {
    expect(isAuditWitnessHolder(set)).toBe(true)
    expect(isAuditWitnessHolder({ state: 'none' })).toBe(true)
    expect(isAuditWitnessHolder({ state: 'unread', problem: 'Desk could not read it.' })).toBe(true)
    expect(isAuditWitnessHolder({ ...set, settings: { ...settings, publicKey: 'nothex' } })).toBe(false)
    expect(isAuditWitnessHolder({ ...set, removeToken: 'short' })).toBe(false)
    expect(isAuditWitnessHolder({ ...set, head: { state: 'refused', status: 404 } })).toBe(false)
    expect(isAuditWitnessHolder({ ...set, cursor: { through: 3, index: 1, at: 1 } })).toBe(false)
    expect(isWitnessAttempt({ at: 1, outcome: 'refused', status: 409 })).toBe(false)
    expect(isWitnessAttempt({ at: 1, outcome: 'other' })).toBe(false)
    expect(isAuditRecord(withWitness)).toBe(true)
    expect(isAuditRecord({ state: 'no-trail', witness: set })).toBe(false)
  })

  it('reads the runtime’s witness section and countersigned coverage, of its values', () => {
    expect(isAuditReport(report)).toBe(true)
    expect(isAuditReport({ ...report, witness: { ...current, began: 'late' } })).toBe(false)
    expect(isAuditReport({ ...report, witness: { ...current, began: 'continued', continuedAfter: undefined } })).toBe(false)
    expect(isAuditReport({ ...report, witness: { ...current, conflicts: [1, 2], conflictsTotal: 1 } })).toBe(false)
    expect(isAuditReport({ ...report, coverage: { ...coverage, countersigned: { status: 'through' } } })).toBe(false)
    expect(isAuditReport({ ...report, requiredCountersigned: { through: 0, status: 'met' } })).toBe(false)
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/witness.go'), 'utf8')
    expect(WITNESS_REASONS).toHaveLength(33)
    for (const reason of WITNESS_REASONS) {
      for (const part of reason.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, reason).toBe(part)
      }
    }
    // Desk's only words for a witness that did not answer as asked.
    expect(source).toContain('witnessUnreachedWords = "Desk could not reach the witness"')
  })
})

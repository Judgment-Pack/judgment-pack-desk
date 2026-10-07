/**
 * Repairing a desk's trail from the decision record (ADR-0010, section 4,
 * "Repair"; question 8): offered only where the panel offers it; the
 * confirmation's three sentences, verbatim, before anything runs; only the
 * confirmation sends the panel's token; after a repair, the decision record
 * checked again, segmented, with Desk's one sentence; a stale refusal and the
 * runtime's refusal, each in its own words; and what a repair answered,
 * dropped when the owner checks again or opens the panel again. The client's
 * checks of what the chassis answers, and that each of Desk's own sentences
 * about a repair is one the chassis says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, isAuditRepair, REPAIR_REASONS, repairTrail, RepairRefused, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const token = 'cd'.repeat(48)
const digest = 'sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e'
const coverage: AuditReport['coverage'] = { legacyPrefix: 0, chained: 3, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked', detail: 'no public key was supplied' },
  signedRecords: 0, unsignedRecords: 0, checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 3, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } }
// What runtime 0.27.1 reported over a trail whose fourth line was cut, and
// after its repair, as the panel answers them.
const torn: AuditReport = { status: 'invalid', lines: 3, bytes: 2864, snapshotBetweenWrites: true, coverage,
  segments: [{ firstLine: 1, lastLine: 3 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0,
  findings: [{ name: 'incomplete-last-line', line: 4, detail: 'the trail ends in 8 bytes with no newline: a write that did not complete' }], findingsTotal: 1,
  establishes: [], doesNotEstablish: ['That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it.'] }
const NOT_INTACT = 'That the history is intact across a discontinuity: a repair keeps the damaged line in place and links over it, so what the damaged line held is not part of any segment.'
const segmented: AuditReport = { ...torn, status: 'segmented', lines: 5, coverage: { ...coverage, chained: 4, damaged: 1, unwitnessed: 4 },
  segments: [{ firstLine: 1, lastLine: 3 }, { firstLine: 5, lastLine: 5 }], segmentsTotal: 2,
  discontinuities: [{ line: 5, reason: 'incomplete-last-line', damagedLine: 4, bytes: 8, digest }], discontinuitiesTotal: 1, findings: [], findingsTotal: 0,
  establishes: ['The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to.'],
  doesNotEstablish: [...torn.doesNotEstablish, NOT_INTACT] }
type Reported = Extract<AuditRecord, { state: 'report' }>
const offered: Reported = { state: 'report', runtime: '0.27.1', report: torn, keys: { state: 'startup' }, signing: { state: 'no-key' }, repair: { state: 'available', line: 4, token } }
const repaired: Reported = { ...offered, report: segmented, repair: undefined }
const answer = { state: 'repaired', discontinuity: { line: 5, reason: 'incomplete-last-line', damagedLine: 4, bytes: 8, digest } }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let repair: () => Response
let checks: number
let posts: { body: unknown; headers: HeadersInit | undefined }[]
beforeEach(() => {
  checks = 0
  posts = []
  records = [offered, repaired]
  repair = () => json(200, answer)
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url) === '/api/audit/verify') {
      checks++
      return json(200, records.length > 1 ? records.shift()! : records[0]!)
    }
    if (String(url) === '/api/audit/repair' && init?.method === 'POST') {
      posts.push({ body: JSON.parse(String(init.body)), headers: init.headers })
      return repair()
    }
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  return render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}
const region = () => within(screen.getByRole('region', { name: 'Repairing the trail' }))
const REPAIRED = 'The trail now has a new segment after the damaged line; the lost line is not restored.'
const CONFIRMATION = [
  'Repair starts a new segment and keeps the damaged bytes.',
  'It never restores the lost line.',
  'Until it is done, every deciding run is refused.'
]
async function confirmRepair() {
  fireEvent.click(await screen.findByRole('button', { name: 'Repair the trail' }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Run the repair' }))
}

describe('repairing the trail', () => {
  it('is offered where the report names an incomplete last line, and says before anything runs what a repair does and does not do', async () => {
    show()
    const button = await screen.findByRole('button', { name: 'Repair the trail' })
    expect(region().getByText('The runtime’s report names line 4, the trail’s last, as incomplete. Desk repairs the trail only when you ask, never on its own.')).toBeTruthy()
    fireEvent.click(button)
    const dialog = await screen.findByRole('dialog', { name: 'Repair this desk’s trail?' })
    expect(within(dialog).getByText('Desk has the runtime repair the trail once, with jpack audit repair, where the trail is still as the decision record showed it.')).toBeTruthy()
    expect(within(within(dialog).getByRole('list', { name: 'What a repair does and does not do' })).getAllByRole('listitem').map(item => item.textContent)).toEqual(CONFIRMATION)
    // Opening it sends nothing; cancelling sends nothing either.
    expect(posts).toEqual([])
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(posts).toEqual([])
    expect(checks).toBe(1)
  })

  it('is not offered where the panel offers none, and says nothing of a repair', async () => {
    for (const record of [{ ...offered, report: { ...torn, status: 'valid', findings: [], findingsTotal: 0 }, repair: undefined }, repaired] satisfies Reported[]) {
      records = [record]
      show()
      expect(await screen.findByText('Checked by jpack 0.27.1.')).toBeTruthy()
      expect(screen.queryByRole('region', { name: 'Repairing the trail' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Repair the trail' })).toBeNull()
      cleanup()
    }
  })

  it('says why it offers no repair where the panel offers none, with no button and no promise', async () => {
    const reason = 'This project\'s jpack.json says audit.chain false, and the runtime repairs only a chained trail, so Desk offers no repair.'
    records = [{ ...offered, repair: { state: 'unavailable', line: 4, reason } }]
    show()
    expect((await screen.findByText(/^The runtime’s report names line 4/)).textContent).toBe(`The runtime’s report names line 4, the trail’s last, as incomplete. ${reason}`)
    expect(screen.queryByRole('button', { name: 'Repair the trail' })).toBeNull()
    expect(screen.queryByText(/every deciding run is refused/)).toBeNull()
    expect(screen.queryByText(/only when you ask/)).toBeNull()
    expect(posts).toEqual([])
  })

  it('sends the panel’s token on confirmation, checks the trail again, and shows it segmented with Desk’s one sentence', async () => {
    show()
    await confirmRepair()
    expect((await screen.findByText(REPAIRED)).getAttribute('role')).toBe('status')
    expect(posts).toEqual([{ body: { token }, headers: { 'Content-Type': 'application/json' } }])
    expect(checks).toBe(2)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Repair the trail' })).toBeNull()
    // The report checked again, in the runtime's words: segmented, its
    // segments, and the discontinuity with its damaged line and digest.
    const status = document.querySelector('[data-status]')!
    expect([status.getAttribute('data-status'), status.textContent]).toEqual(['segmented', 'Every check the runtime made passed. A repair started a new segment, and the history is not intact across it.'])
    expect(within(screen.getByRole('region', { name: 'Segments' })).getAllByRole('listitem').map(item => item.textContent)).toEqual(['Lines 1 to 3', 'Lines 5 to 5'])
    const discontinuity = within(screen.getByRole('region', { name: 'Discontinuities' })).getByRole('listitem')
    expect(discontinuity.textContent).toBe(`At line 5, a repair names line 4 as damaged. incomplete-last-line ${digest}`)
    expect(screen.getByText(NOT_INTACT)).toBeTruthy()
  })

  it('shows a stale refusal in Desk’s words, and checks the trail again', async () => {
    const stale = 'The trail changed after the decision record showed it, so nothing was repaired. Check the decision record again.'
    repair = () => json(409, { error: stale, code: 'stale' })
    records = [offered, { ...offered, repair: { state: 'available', line: 4, token: 'ef'.repeat(48) } }]
    show()
    await confirmRepair()
    expect((await screen.findByText(stale)).getAttribute('role')).toBe('alert')
    expect(checks).toBe(2)
    expect(screen.queryByText(REPAIRED)).toBeNull()
    expect(screen.queryByRole('list', { name: 'What the runtime said of the repair' })).toBeNull()
    // The fresh offer is the one shown.
    fireEvent.click(await screen.findByRole('button', { name: 'Repair the trail' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Run the repair' }))
    await waitFor(() => expect(posts.map(post => (post.body as { token: string }).token)).toEqual([token, 'ef'.repeat(48)]))
  })

  it('shows the runtime’s refusal in its own words, beside Desk’s, and checks the trail again', async () => {
    const said = { code: 'JPS-AUDIT-REPAIR-UNCHAINED', message: 'This project\'s audit member says chain false, and a repair starts a chained segment.' }
    repair = () => json(409, { error: 'The runtime did not report a repair. It said:', code: 'bad-request', diagnostics: [said] })
    records = [offered]
    show()
    await confirmRepair()
    expect((await screen.findByText('The runtime did not report a repair. It said:')).getAttribute('role')).toBe('alert')
    const words = within(screen.getByRole('list', { name: 'What the runtime said of the repair' })).getAllByRole('listitem')
    expect(words.map(item => [item.textContent, item.getAttribute('lang')])).toEqual([[`${said.code} ${said.message}`, 'en']])
    expect(checks).toBe(2)
    expect(screen.queryByText(REPAIRED)).toBeNull()
  })

  it('says what the repair answered beside the runtime’s refusal to check the trail again', async () => {
    const said = { code: 'JPS-AUDIT-TRAIL-READ', message: 'The project\'s trail …/evaluations.jsonl could not be opened.' }
    records = [offered, { state: 'unverified', runtime: '0.27.1', diagnostics: [said], keys: { state: 'startup' }, signing: { state: 'no-key' } }]
    show()
    await confirmRepair()
    expect((await screen.findByText(REPAIRED)).getAttribute('role')).toBe('status')
    expect(screen.getByText('The runtime did not check the trail.')).toBeTruthy()
    expect(checks).toBe(2)
    expect(screen.queryByRole('button', { name: 'Repair the trail' })).toBeNull()
  })

  it('drops what the last repair answered when the owner checks again', async () => {
    records = [offered, repaired, repaired]
    show()
    await confirmRepair()
    expect(await screen.findByText(REPAIRED)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(checks).toBe(3))
    await waitFor(() => expect(screen.queryByText(REPAIRED)).toBeNull())
  })

  it('drops what the last repair answered when the panel is opened again', async () => {
    records = [offered, repaired, repaired]
    const client = testQueryClient()
    const view = (visible: boolean) => <QueryClientProvider client={client}><DecisionRecord visible={visible} /></QueryClientProvider>
    const { rerender } = render(view(true))
    await confirmRepair()
    expect(await screen.findByText(REPAIRED)).toBeTruthy()
    rerender(view(false))
    rerender(view(true))
    await waitFor(() => expect(checks).toBe(3))
    await waitFor(() => expect(screen.queryByText(REPAIRED)).toBeNull())
  })
})

describe('the repair client', () => {
  it('reads the panel’s offer, and refuses what is not one', () => {
    const unavailable = { state: 'unavailable', line: 4, reason: 'Why not.' }
    expect(isAuditRepair({ state: 'available', line: 4, token })).toBe(true)
    expect(isAuditRepair(unavailable)).toBe(true)
    expect(isAuditRecord(offered)).toBe(true)
    expect(isAuditRecord({ ...offered, repair: unavailable })).toBe(true)
    for (const value of [{ state: 'available', line: 4 }, { state: 'available', token }, { state: 'available', line: 0, token },
      { state: 'available', line: 4, token: token.slice(2) }, { state: 'available', line: 4, token: 'cd'.repeat(32) }, { state: 'available', line: 4, token: token.toUpperCase() },
      { state: 'available', line: '4', token }, { state: 'available', line: 4, token, reason: 'Why not.' }, { line: 4, token },
      { ...unavailable, reason: '' }, { ...unavailable, token }, { state: 'unavailable', line: 4 }, { ...unavailable, state: 'offered' }, 'repair']) {
      expect(isAuditRepair(value), JSON.stringify(value)).toBe(false)
      expect(isAuditRecord({ ...offered, repair: value }), JSON.stringify(value)).toBe(false)
    }
    // A refusal to check carries no offer.
    expect(isAuditRecord({ state: 'unverified', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'Why.' }], repair: { state: 'available', line: 4, token } })).toBe(false)
  })

  it('posts the token, reads a repair, and refuses an answer that is not one', async () => {
    expect(await repairTrail(token)).toEqual(answer)
    expect(vi.mocked(deskFetch)).toHaveBeenCalledWith('/api/audit/repair', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
    const unread = 'Desk could not read what the repair answered. The decision record, checked again, shows what the trail holds now.'
    for (const body of [{ ...answer, state: 'done' }, { state: 'repaired' }, { ...answer, discontinuity: { ...answer.discontinuity, digest: undefined } },
      { ...answer, discontinuity: { ...answer.discontinuity, line: 4 } }, { ...answer, discontinuity: { ...answer.discontinuity, reason: '' } }]) {
      repair = () => json(200, body)
      await expect(repairTrail(token), JSON.stringify(body)).rejects.toThrow(unread)
    }
    repair = () => new Response('not json', { status: 500 })
    await expect(repairTrail(token)).rejects.toThrow(unread)
    const said = { code: 'JPS-AUDIT-REPAIR-NOTHING', message: 'The trail\'s last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage.' }
    repair = () => json(409, { error: 'The runtime did not report a repair. It said:', code: 'bad-request', diagnostics: [said, { code: '', message: 'not one' }] })
    const refused = await repairTrail(token).catch((error: unknown) => error)
    expect(refused).toBeInstanceOf(RepairRefused)
    expect((refused as RepairRefused).message).toBe('The runtime did not report a repair. It said:')
    // A list with anything in it that is not the runtime's diagnostic is not passed on.
    expect((refused as RepairRefused).diagnostics).toEqual([])
    repair = () => json(409, { error: 'The runtime did not report a repair. It said:', code: 'bad-request', diagnostics: [said] })
    expect(((await repairTrail(token).catch((error: unknown) => error)) as RepairRefused).diagnostics).toEqual([said])
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    // Each sentence's words between its placeholders, as the chassis's Go
    // source spells them.
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/audit_repair.go'), 'utf8')
    expect(REPAIR_REASONS).toHaveLength(17)
    for (const reason of REPAIR_REASONS) {
      for (const part of reason.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, reason).toBe(part)
      }
    }
  })
})

/**
 * The decision-record panel (ADR-0010, sections 4 and 6): the one sentence an
 * older runtime gets, and nothing beside it; the runtime's report, with its
 * sentences verbatim; the runtime's refusal; Desk's own refusal; a project
 * that keeps no trail; and that it runs on opening and on request, never on a
 * timer.
 */
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, readAuditRecord, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const establishes = ['The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to.']
const doesNotEstablish = [
  'The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.',
  'Who wrote any record: no public key was supplied, so no signature was checked.'
]
const coverage = { legacyPrefix: 2, chained: 3, unchained: 1, uncovered: 4, damaged: 0,
  signed: { status: 'not-checked', detail: 'no public key was supplied' }, signedRecords: 0, unsignedRecords: 0,
  checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 3,
  stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } }
const valid: AuditReport = { status: 'valid', lines: 10, bytes: 2856, snapshotBetweenWrites: true, coverage,
  segments: [{ firstLine: 1, lastLine: 10 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0,
  findings: [], findingsTotal: 0, establishes, doesNotEstablish }
const invalid: AuditReport = { ...valid, status: 'invalid', establishes: [],
  findings: [{ name: 'incomplete-last-line', line: 4, detail: 'the trail ends in 8 bytes with no newline: a write that did not complete' }], findingsTotal: 140 }
const segmented: AuditReport = { ...valid, status: 'segmented', segments: [{ firstLine: 1, lastLine: 3 }, { firstLine: 5, lastLine: 6 }], segmentsTotal: 2,
  discontinuities: [{ line: 5, reason: 'incomplete-last-line', damagedLine: 4, bytes: 8, digest: 'sha256:952cdc0f' }], discontinuitiesTotal: 1 }
const held: AuditReport = { ...valid, snapshotBetweenWrites: false, coverage: { ...coverage,
  signed: { status: 'through', through: 7 }, signedRecords: 7, unsignedRecords: 2,
  checkpointed: { status: 'through', through: 6 }, witnessed: 6, unwitnessed: 3,
  stamped: { status: 'none' } } }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let answers: (() => Response)[]
let asked: number
beforeEach(() => {
  asked = 0
  answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: valid } satisfies AuditRecord)]
  vi.mocked(deskFetch).mockImplementation(async url => {
    if (String(url) !== '/api/audit/verify') return json(404, { error: 'not here' })
    asked++
    return (answers.length > 1 ? answers.shift()! : answers[0]!)()
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.useRealTimers() })

function show(client: QueryClient = testQueryClient()) {
  return render(<QueryClientProvider client={client}><DecisionRecord /></QueryClientProvider>)
}
const panel = () => screen.getByTestId('decision-record')

describe('the decision-record client', () => {
  it('refuses an answer that is not one', async () => {
    for (const body of [{ state: 'report' }, { state: 'report', report: { ...valid, coverage: undefined } }, { state: 'report', report: { ...valid, establishes: [1] } },
      { state: 'unverified', diagnostics: [] }, { state: 'older-runtime' }, { state: 'chained' }, { state: 'report', runtime: 6, report: valid }]) {
      expect(isAuditRecord(body), JSON.stringify(body)).toBe(false)
    }
    answers = [() => json(200, { state: 'report', report: { ...valid, findings: [{ name: 'x' }] } })]
    await expect(readAuditRecord()).rejects.toThrow('The decision record could not be loaded')
  })
})

describe('the decision-record panel', () => {
  it('says one sentence with an older runtime, and calls nothing done that it cannot do', async () => {
    answers = [() => json(200, { state: 'older-runtime', runtime: '0.25.0', floor: '0.26.0' })]
    show()
    const line = await screen.findByText('This runtime (jpack 0.25.0) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack 0.26.0 or later.')
    expect(line.tagName).toBe('P')
    expect(panel().querySelectorAll('p, li, dt, dd, button')).toHaveLength(1)
    expect(panel().textContent).not.toMatch(/\b(chained|signed|witnessed|stamped)\b/i)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows the runtime’s report: status, coverage, segments, and its sentences verbatim, in English', async () => {
    show()
    expect(await screen.findByText('Every check the runtime made passed.')).toBeTruthy()
    expect(screen.getByText('Desk ran this on your machine, over your trail, with no keys and no checkpoints: it checked no signature, no held checkpoint and no stamp. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.')).toBeTruthy()
    const facts = within(panel().querySelector('dl')!)
    const value = (term: string) => facts.getByText(term).nextElementSibling?.textContent
    expect(value('Lines')).toBe('10')
    expect(value('Lines before the first chained line')).toBe('2')
    expect(value('Chained lines')).toBe('3')
    expect(value('Unchained lines a later chained line commits to')).toBe('1')
    expect(value('Lines nothing commits to')).toBe('4')
    expect(value('Lines a repair names as damaged')).toBe('0')
    expect(value('Signatures')).toBe('Not checked: Desk gave no public key')
    expect(value('Held checkpoints')).toBe('Not checked: Desk gave no held checkpoint')
    expect(value('Records a held checkpoint witnesses')).toBe('0')
    expect(value('Records no held checkpoint witnesses')).toBe('3')
    expect(value('Stamps')).toBe('Not checked: Desk gave no time-stamping roots')
    expect(facts.queryByText('Records with a valid signature of their own')).toBeNull()
    expect(within(screen.getByRole('region', { name: 'Segments' })).getByText('Lines 1 to 10')).toBeTruthy()
    expect(screen.getByText('No check failed.')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Discontinuities' })).toBeNull()
    const says = screen.getByRole('region', { name: 'What this establishes' }).querySelector('ul')!
    expect([...says.querySelectorAll('li')].map(item => item.textContent)).toEqual(establishes)
    expect(says.getAttribute('lang')).toBe('en')
    const doesNot = screen.getByRole('region', { name: 'What this does not establish' }).querySelector('ul')!
    expect([...doesNot.querySelectorAll('li')].map(item => item.textContent)).toEqual(doesNotEstablish)
    expect(doesNot.getAttribute('lang')).toBe('en')
    expect(screen.getByText('Checked by jpack 0.26.0.')).toBeTruthy()
    expect(screen.queryByText(/lock on the trail/)).toBeNull()
  })

  it('shows each finding by name, and how many the runtime did not list', async () => {
    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: invalid })]
    show()
    expect(await screen.findByText('The trail failed a check the runtime made.')).toBeTruthy()
    const findings = within(screen.getByRole('region', { name: 'Findings' }))
    const item = findings.getByText('incomplete-last-line').closest('li')!
    expect(item.textContent).toBe('incomplete-last-line Line 4: the trail ends in 8 bytes with no newline: a write that did not complete')
    expect(findings.getByText('Listed: 1 of 140.')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'What this establishes' })).toBeNull()
  })

  it('shows segments and the discontinuity between them', async () => {
    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: segmented })]
    show()
    expect(await screen.findByText(/A repair started a new segment/)).toBeTruthy()
    expect(within(screen.getByRole('region', { name: 'Segments' })).getAllByRole('listitem').map(item => item.textContent)).toEqual(['Lines 1 to 3', 'Lines 5 to 6'])
    expect(within(screen.getByRole('region', { name: 'Discontinuities' })).getByRole('listitem').textContent).toBe('At line 5, a repair names line 4 as damaged. incomplete-last-line sha256:952cdc0f')
  })

  it('says how far each held input reaches, where the report gives it, and a snapshot taken with no lock', async () => {
    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: held })]
    show()
    await screen.findByText('Every check the runtime made passed.')
    const facts = within(panel().querySelector('dl')!)
    const value = (term: string) => facts.getByText(term).nextElementSibling?.textContent
    expect(value('Signatures')).toBe('Signed through record 7')
    expect(value('Records with a valid signature of their own')).toBe('7')
    expect(value('Chained records without one')).toBe('2')
    expect(value('Held checkpoints')).toBe('Witnessed through record 6')
    expect(value('Stamps')).toBe('No trusted stamp covers a record')
    expect(screen.getByText('The runtime could take no lock on the trail here, so its last line may be a write still in progress.')).toBeTruthy()
  })

  it('shows the runtime’s refusal to check, in its words', async () => {
    answers = [() => json(200, { state: 'unverified', runtime: '0.26.0', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'The project’s trail does not exist yet: no record has been written.' }] })]
    show()
    expect(await screen.findByText('The runtime did not check the trail.')).toBeTruthy()
    const said = screen.getByRole('list', { name: 'What the runtime said' })
    expect(said.textContent).toBe('JPS-AUDIT-TRAIL-READ The project’s trail does not exist yet: no record has been written.')
    expect(said.querySelector('li')!.getAttribute('lang')).toBe('en')
  })

  it('shows Desk’s refusal where it does not check here, and runs nothing more', async () => {
    const refusal = 'This project\'s runtime reads /elsewhere/jpack.json, which JPACK_CONFIG names, and not this project\'s jpack.json, so Desk does not check its decision record here.'
    answers = [() => json(409, { error: refusal, code: 'bad-request' })]
    show()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('Desk does not check the decision record here. ' + refusal)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows an error as one, and asks again only when told', async () => {
    answers = [() => json(500, { error: 'The decision record could not be checked: its audit verify did not answer as documented.', code: 'internal' }), () => json(200, { state: 'report', runtime: '0.26.0', report: valid })]
    show()
    expect((await screen.findByRole('alert')).textContent).toContain('its audit verify did not answer as documented')
    expect(asked).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Every check the runtime made passed.')).toBeTruthy()
    expect(asked).toBe(2)
  })

  it('says a project keeps no trail, and nothing else', async () => {
    answers = [() => json(200, { state: 'no-trail' })]
    show()
    expect(await screen.findByText('This project keeps no trail: its jpack.json declares no audit directory, so the runtime records none of its deciding runs.')).toBeTruthy()
    expect(panel().querySelectorAll('p, li, dt, dd, button')).toHaveLength(1)
  })

  it('runs when it opens and when the owner asks again, never on a timer', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const client = testQueryClient()
    const first = show(client)
    await screen.findByText('Every check the runtime made passed.')
    expect(asked).toBe(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(6 * 60 * 60 * 1000) })
    // What TanStack Query listens for: the page becoming visible again, and
    // the network coming back.
    window.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('offline'))
    window.dispatchEvent(new Event('online'))
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(asked).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(asked).toBe(2))
    // Opened again, it runs again, though the answer it had is not stale.
    first.unmount()
    show(client)
    await waitFor(() => expect(asked).toBe(3))
  })
})

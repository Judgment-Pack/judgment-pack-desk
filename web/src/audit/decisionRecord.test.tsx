/**
 * The decision-record panel (ADR-0010, sections 1, 4 and 6): the one sentence
 * an older runtime gets, and nothing beside it; the runtime's report, with its
 * sentences verbatim; the keys Desk passed, shown for a holder, and the
 * runtime's word on the key; the runtime's refusal; Desk's own refusal; a
 * project that keeps no trail; and that it runs on opening and on request,
 * never on a timer.
 */
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { followsTheProject } from '../mcp/projectChange'
import { AUDIT_KEY, isAuditRecord, readAuditRecord, type AuditRecord, type AuditReport } from './client'
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

// A key runtime 0.26.0 generated, and the keyId it printed for it.
const deskKey = { publicKey: '882a7f2be72a4b0c0a03b590300c72e8ed3fab24355a6950f9e6399814c67350', keyId: '4ba1de706a3baa4d8f5456340604190a', at: 0 }
const nextKey = { publicKey: '272ad95977cf9721551ead07d2c9563cb7403b3688a8c23508b6a02ba4302623', keyId: '1cba17c1fa03b21777f6bf729e3f1a2b', at: 7 }
const signedReport: AuditReport = { ...valid, coverage: { ...coverage, signed: { status: 'through', through: 10 }, signedRecords: 10, unsignedRecords: 0 },
  signatures: { lines: 10, unreadable: 1, rotations: 0, keysSupplied: 1, revocations: 0, firstKey: deskKey.keyId, keyInForce: deskKey.keyId } }
const passedCheck = { state: 'check', status: 'passed', detail: 'Chained records are signed with key 4ba1de706a3baa4d8f5456340604190a, named by the audit member\'s signingKey.' } as const
const KEYED_STATEMENT = 'Desk ran this on your machine, over your trail, with the public keys it keeps for this desk and no checkpoints: it checked the signatures against those keys, and no held checkpoint and no stamp. It is not evidence to anyone who does not trust you: you hold the key. A holder runs the same command on a copy, with what it holds.'
const KEYLESS_STATEMENT = 'Desk ran this on your machine, over your trail, with no keys and no checkpoints: it checked no signature, no held checkpoint and no stamp. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.'

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

function show(client: QueryClient = testQueryClient(), visible = true) {
  return render(<QueryClientProvider client={client}><DecisionRecord visible={visible} /></QueryClientProvider>)
}
const panel = () => screen.getByTestId('decision-record')

describe('the decision-record client', () => {
  it('refuses an answer that is not one', async () => {
    for (const body of [{ state: 'report' }, { state: 'report', report: { ...valid, coverage: undefined } }, { state: 'report', report: { ...valid, establishes: [1] } },
      { state: 'unverified', diagnostics: [] }, { state: 'older-runtime' }, { state: 'chained' }, { state: 'report', runtime: 6, report: valid },
      { state: 'unverified', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: '' }] }, { state: 'unverified', diagnostics: [{ code: '', message: 'Refused.' }] },
      // A count missing, or not a count.
      { state: 'report', report: { ...valid, lines: undefined } }, { state: 'report', report: { ...valid, findingsTotal: undefined } },
      { state: 'report', report: { ...valid, segmentsTotal: -1 } }, { state: 'report', report: { ...valid, coverage: { ...valid.coverage, legacyPrefix: undefined } } },
      { state: 'report', report: { ...valid, coverage: { ...valid.coverage, unwitnessed: 1.5 } } },
      // A protection with no status, or through no record.
      { state: 'report', report: { ...valid, coverage: { ...valid.coverage, signed: { detail: 'no public key was supplied' } } } },
      { state: 'report', report: { ...valid, coverage: { ...valid.coverage, stamped: { status: 'through' } } } },
      // Lists longer than their totals, and findings that disagree with the status.
      { state: 'report', report: { ...invalid, findingsTotal: 0 } }, { state: 'report', report: { ...valid, findingsTotal: 1 } },
      { state: 'report', report: { ...valid, segmentsTotal: 0 } }, { state: 'report', report: { ...segmented, discontinuitiesTotal: 0 } },
      { state: 'report', report: { ...invalid, findings: [...invalid.findings, ...invalid.findings], findingsTotal: 1 } },
      { state: 'report', report: { ...invalid, findings: [{ name: '', line: 4, detail: '' }] } }]) {
      expect(isAuditRecord(body), JSON.stringify(body)).toBe(false)
    }
    answers = [() => json(200, { state: 'report', report: { ...valid, findings: [{ name: 'x' }] } })]
    await expect(readAuditRecord()).rejects.toThrow('The decision record could not be loaded')
    for (const report of [valid, invalid, segmented, held]) expect(isAuditRecord({ state: 'report', report }), report.status).toBe(true)
  })

  it('reads the keys Desk kept and the runtime’s word on them, and refuses what is not one', () => {
    for (const extra of [
      { keys: { state: 'kept', public: [deskKey] }, signing: passedCheck },
      { keys: { state: 'kept', public: [deskKey, nextKey] }, signing: { state: 'check', status: 'failed', detail: 'refused' } },
      { keys: { state: 'none' }, signing: { state: 'no-key' } },
      { keys: { state: 'startup' }, signing: { state: 'check', status: 'skipped' } },
      { keys: { state: 'unread', problem: 'Desk could not read the public keys it keeps for this desk: it is not a regular file.' } },
      { signing: { state: 'unread', diagnostics: [{ code: 'JPS-PROJECT-CONFIG-VERSION', message: 'Refused.' }] } },
      { signing: { state: 'unread', problem: 'Its packs validate did not answer as documented.' } }
    ]) {
      expect(isAuditRecord({ state: 'report', report: signedReport, ...extra }), JSON.stringify(extra)).toBe(true)
      expect(isAuditRecord({ state: 'unverified', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'None yet.' }], ...extra }), JSON.stringify(extra)).toBe(true)
    }
    for (const extra of [
      { keys: { state: 'kept', public: [] } },
      { keys: { state: 'kept' } },
      { keys: { state: 'kept', public: [{ ...deskKey, publicKey: deskKey.publicKey.toUpperCase() }] } },
      { keys: { state: 'kept', public: [{ ...deskKey, publicKey: deskKey.publicKey.slice(2) }] } },
      { keys: { state: 'kept', public: [{ ...deskKey, keyId: deskKey.keyId.slice(1) }] } },
      { keys: { state: 'kept', public: [{ ...deskKey, at: 1 }] } },
      { keys: { state: 'kept', public: [deskKey, { ...nextKey, at: 0 }] } },
      { keys: { state: 'kept', public: [deskKey, { ...nextKey, at: -1 }] } },
      { keys: { state: 'unread' } },
      { keys: { state: 'unread', problem: '' } },
      { keys: { state: 'signed' } },
      { keys: 'kept' },
      { signing: { state: 'check', status: 'maybe' } },
      { signing: { state: 'check' } },
      { signing: { state: 'check', status: 'passed', detail: 5 } },
      { signing: { state: 'unread' } },
      { signing: { state: 'unread', diagnostics: [] } },
      { signing: { state: 'unread', diagnostics: [{ code: '', message: 'Refused.' }] } },
      { signing: { state: 'signed' } }
    ]) {
      expect(isAuditRecord({ state: 'report', report: valid, ...extra }), JSON.stringify(extra)).toBe(false)
    }
    for (const signatures of [{ ...signedReport.signatures, keyInForce: undefined }, { ...signedReport.signatures, firstKey: '' }, { ...signedReport.signatures, unreadable: -1 }, { ...signedReport.signatures, rotations: undefined }]) {
      expect(isAuditRecord({ state: 'report', report: { ...signedReport, signatures } }), JSON.stringify(signatures)).toBe(false)
    }
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
    expect(screen.getByText(KEYLESS_STATEMENT)).toBeTruthy()
    // An answer with no word on the keys says nothing of them.
    expect(screen.queryByRole('region', { name: 'Signing key' })).toBeNull()
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

  it('says which keys it passed, shows each for a holder in order, and the runtime’s word on the key', async () => {
    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: signedReport, keys: { state: 'kept', public: [deskKey, nextKey] }, signing: passedCheck } satisfies AuditRecord)]
    show()
    expect(await screen.findByText(KEYED_STATEMENT)).toBeTruthy()
    expect(screen.queryByText(KEYLESS_STATEMENT)).toBeNull()
    const signing = within(screen.getByRole('region', { name: 'Signing key' }))
    expect(signing.getByText(/^Desk keeps this desk’s signing key in its own configuration folder, outside the project/)).toBeTruthy()
    const shown = [...screen.getByRole('region', { name: 'Signing key' }).querySelectorAll('pre')].map(block => [block.getAttribute('aria-label'), block.textContent])
    // Each key, by its place and the record it signs after.
    expect(shown).toEqual([[`Public key 1, keyId ${deskKey.keyId}, signing from the first record`, deskKey.publicKey],
      [`Public key 2, keyId ${nextKey.keyId}, signing the records after record 7`, nextKey.publicKey]])
    expect(signing.getByRole('button', { name: `Copy Public key 1, keyId ${deskKey.keyId}, signing from the first record` })).toBeTruthy()
    const check = screen.getByRole('region', { name: 'Signing key' }).querySelector('[data-check]')!
    expect(check.getAttribute('data-check')).toBe('passed')
    expect(check.textContent).toBe('The runtime’s check of the key: Passed. ' + passedCheck.detail)
    expect(check.querySelector('span')!.getAttribute('lang')).toBe('en')
    const facts = within(panel().querySelector('dl')!)
    const value = (term: string) => facts.getByText(term).nextElementSibling?.textContent
    expect(value('Signatures')).toBe('Signed through record 10')
    expect(value('Key in force at the end of the trail')).toBe(deskKey.keyId)
    expect(value('Signature lines the runtime could not read')).toBe('1')
  })

  it('says where Desk keeps no key, or could not read the ones it keeps, and passes none', async () => {
    for (const [keys, says] of [
      [{ state: 'none' }, 'Desk keeps no signing key for this desk, so it passed no public key.'],
      [{ state: 'startup' }, 'Desk keeps no signing key for the project it was started on, so it passed no public key.'],
      [{ state: 'unread', problem: 'Desk could not read the public keys it keeps for this desk: line 1 is not in the form Desk writes.' }, 'Desk could not read the public keys it keeps for this desk: line 1 is not in the form Desk writes.']
    ] as const) {
      answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: valid, keys, signing: { state: 'no-key' } } satisfies AuditRecord)]
      show()
      expect(await screen.findByText(KEYLESS_STATEMENT)).toBeTruthy()
      const signing = screen.getByRole('region', { name: 'Signing key' })
      expect(within(signing).getByText(says)).toBeTruthy()
      expect(signing.querySelector('pre')).toBeNull()
      expect(within(signing).getByText('The runtime’s check of the key: no key is named for this project, so its runtime signs no record.')).toBeTruthy()
      if (keys.state === 'unread') expect(within(signing).getByRole('alert').textContent).toBe(says)
      cleanup()
    }
  })

  it('shows a key the runtime refuses, and a check it did not make, in its words', async () => {
    const refused = 'The signing key the audit member\'s signingKey names, Desk\'s signing folder/abc.seed, is refused, so records are written unsigned: the signing key can be read or written by its group or by other users.'
    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: valid, keys: { state: 'kept', public: [deskKey] }, signing: { state: 'check', status: 'failed', detail: refused } } satisfies AuditRecord)]
    show()
    await screen.findByText(KEYED_STATEMENT)
    expect(panel().querySelector('[data-check]')!.textContent).toBe('The runtime’s check of the key: Failed. ' + refused)
    cleanup()

    answers = [() => json(200, { state: 'unverified', runtime: '0.26.0', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'None yet.' }], keys: { state: 'kept', public: [deskKey] },
      signing: { state: 'unread', diagnostics: [{ code: 'JPS-PROJECT-CONFIG-VERSION', message: 'The configuration is refused.' }] } } satisfies AuditRecord)]
    show()
    await screen.findByText('The runtime did not check the trail.')
    const signing = within(screen.getByRole('region', { name: 'Signing key' }))
    expect(signing.getByText('The runtime did not say whether a key signs this project’s records.')).toBeTruthy()
    const said = signing.getByRole('list', { name: 'What the runtime said of the key' })
    expect(said.textContent).toBe('JPS-PROJECT-CONFIG-VERSION The configuration is refused.')
    expect(said.querySelector('li')!.getAttribute('lang')).toBe('en')
    expect(screen.getByRole('region', { name: 'Signing key' }).querySelectorAll('pre')).toHaveLength(1)
    cleanup()

    answers = [() => json(200, { state: 'report', runtime: '0.26.0', report: valid, signing: { state: 'unread', problem: 'Its packs validate did not answer as documented.' } } satisfies AuditRecord)]
    show()
    await screen.findByText(KEYLESS_STATEMENT)
    expect(within(screen.getByRole('region', { name: 'Signing key' })).getByText('Its packs validate did not answer as documented.')).toBeTruthy()
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

  it('runs when it becomes visible and when the owner asks again, never on a timer, focus or reconnect', async () => {
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
    await screen.findByText('Every check the runtime made passed.')
    // Kept mounted and hidden, it runs nothing; shown again, it runs again,
    // though the answer it had is not stale.
    first.rerender(<QueryClientProvider client={client}><DecisionRecord visible={false} /></QueryClientProvider>)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(asked).toBe(2)
    first.rerender(<QueryClientProvider client={client}><DecisionRecord visible /></QueryClientProvider>)
    await waitFor(() => expect(asked).toBe(3))
    await screen.findByText('Every check the runtime made passed.')
    // Mounted again, it runs again.
    first.unmount()
    show(client)
    await waitFor(() => expect(asked).toBe(4))
  })

  it('runs nothing while hidden from the start', async () => {
    show(testQueryClient(), false)
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 50)) })
    expect(asked).toBe(0)
  })

  it('is not rerun by an invalidation of every query, and is left out of the project’s', async () => {
    const client = testQueryClient()
    show(client)
    await screen.findByText('Every check the runtime made passed.')
    // The project's own invalidation, which McpProvider sends on a file
    // change or a reconnect, leaves it as it was, and reaches the others.
    client.setQueryData(['another'], 'answer')
    await act(async () => { await client.invalidateQueries({ predicate: followsTheProject }) })
    expect(client.getQueryState(AUDIT_KEY)?.isInvalidated).toBe(false)
    expect(client.getQueryState(['another'])?.isInvalidated).toBe(true)
    // And a blanket invalidation, which reruns every active query, does not
    // rerun this one.
    await act(async () => { await client.invalidateQueries() })
    expect(asked).toBe(1)
  })

})

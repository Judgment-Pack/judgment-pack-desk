/**
 * Stamping from the decision record (ADR-0010, section 3; question 5): no
 * authority by default; section 7's row for a stamp, verbatim; the settings
 * as Desk keeps them, unread, or none; setting an authority, checked by Desk
 * and then confirmed, with the confirmation's sentences, verbatim, before
 * anything is kept; removing it, confirmed; "Stamp now", and the decision
 * record checked again after it; the records pending a stamp, the stamps
 * the runtime checked and the last stamp run, in the runtime's words where it
 * refused; and, where the runtime did not check the stamps, the sequence the
 * last run named labelled as the authority's answer to Desk's request. The
 * client's checks of what the chassis answers, and that each of Desk's own
 * sentences about stamping is one the chassis says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { formatDate } from '../i18n'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, isAuditReport, isAuditStamping, isStampRun, STAMPING_REASONS, stampNow, type AuditRecord, type AuditReport, type AuditStamping, type StampRun } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const trail = '9a5ef41d74e7d7e003c8a34cff056351'
const removeToken = 'ab'.repeat(48)
const checkToken = 'cd'.repeat(48)
const rootDigest = 'sha256:' + '1f'.repeat(32)
const listDigest = 'sha256:' + '2e'.repeat(32)
const ROOTS = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n'
const coverage: AuditReport['coverage'] = { legacyPrefix: 0, chained: 3, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked', detail: 'no public key was supplied' },
  signedRecords: 0, unsignedRecords: 0, checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 3, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } }
const unchecked: AuditReport = { status: 'valid', lines: 3, bytes: 3533, snapshotBetweenWrites: true, coverage, segments: [{ firstLine: 1, lastLine: 3 }], segmentsTotal: 1,
  discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0, establishes: [], doesNotEstablish: [] }
// What runtime 0.27.1 reported over three records, the first two stamped,
// with the roots given, naming the trail by its identity.
const checked: AuditReport = { ...unchecked, trail, coverage: { ...coverage, stamped: { status: 'through', through: 2 } },
  stamps: { lines: 2, unreadable: 0, trusted: 2, revocationChecked: 0, revocationNotChecked: 2, coveredBy: '2026-10-07T13:29:22Z',
    lag: { records: 2, maxSeconds: 14.607222481, maxSequence: 2, minSeconds: 10.92565753, minSequence: 1, atAfterStamp: false, atUnreadable: 0 } } }
const settings = { authority: 'https://tsa.example/stamp', intervalMinutes: 60, policies: ['1.3.6.1.4.1.99999.1'], roots: [{ subject: 'CN=test time-stamping root', sha256: rootDigest }],
  crls: [{ sha256: listDigest, lists: 1 }], setAt: 1791201600 }
const stamped: StampRun = { at: 1791205200, status: 'stamped', trail, sequence: 2, stampedAt: '2026-10-07T13:29:07Z', existedBy: '2026-10-07T13:29:07Z', policy: '1.3.6.1.4.1.99999.1' }
type Reported = Extract<AuditRecord, { state: 'report' }>
const none: Reported = { state: 'report', runtime: '0.27.1', report: unchecked, keys: { state: 'startup' }, signing: { state: 'no-key' }, stamping: { state: 'none' } }
// The chassis's word on the last run's checkpoint (issue #312): the stamps
// the runtime checked reach the very checkpoint the run named.
const set: Reported = { ...none, report: checked, stamping: { state: 'set', settings, removeToken, passed: true, pending: 1, last: stamped, lastChecked: { checked: true } } }
const LABEL = 'The last stamp run named the checkpoint at record 2: that is the authority’s answer to Desk’s request, not a stamp the runtime checked.'
// What the chassis says where it does not (stamping.go, `lastRunChecked`).
const REWRITTEN = 'The record at that sequence now is not the one the run named, as where a trail was put back to an earlier point and written again since.'
const OTHER_TRAIL = 'The stamps the runtime checked are of another trail than the one the run named.'

const ESTABLISHES = 'the checkpoint, and every line before it, existed by the authority’s stated time, as far as that authority is independent of the operator'
const DOES_NOT = 'when any record was made: a stamp is an upper bound on existence; anything against an authority that colludes; revocation, where no supplied list speaks for it; anything after the last checkpoint stamped'
const CONFIRMATION = [
  'Choosing an authority is a trust decision.',
  'Each stamp sends this authority the SHA-256 digest of the trail’s checkpoint, a nonce and a request for its certificate, and nothing else of the trail.',
  'Nothing on the decision path waits for a stamp: a deciding run is recorded at once, and stamped later, while Desk is running.'
]

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let checks: number
let posts: { url: string; body: unknown }[]
let answers: Record<string, () => Response>
beforeEach(() => {
  checks = 0
  posts = []
  records = [none]
  answers = {
    '/api/audit/stamping/check': () => json(200, { token: checkToken, shown: { ...settings, setAt: undefined } }),
    '/api/audit/stamping': () => json(200, { state: 'set', settings }),
    '/api/audit/stamping/remove': () => json(200, { state: 'none' }),
    '/api/audit/stamping/stamp': () => json(200, { run: { ...stamped, requested: true } })
  }
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url) === '/api/audit/verify') {
      checks++
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
const region = () => within(screen.getByRole('region', { name: 'Stamping' }))
const when = (seconds: number) => formatDate(seconds * 1000, { dateStyle: 'medium', timeStyle: 'medium' })

async function fillForm(fields: { authority?: string; minutes?: string; roots?: string; policies?: string } = {}) {
  fireEvent.click(await screen.findByRole('button', { name: 'Set an authority' }))
  const form = within(screen.getByRole('form', { name: 'Time-stamping authority' }))
  fireEvent.change(form.getByLabelText('Authority’s address'), { target: { value: fields.authority ?? 'https://tsa.example/stamp' } })
  fireEvent.change(form.getByLabelText('Interval, in minutes'), { target: { value: fields.minutes ?? '60' } })
  fireEvent.change(form.getByLabelText('Root certificates (PEM)'), { target: { value: fields.roots ?? ROOTS } })
  fireEvent.change(form.getByLabelText('Policy OIDs (optional)'), { target: { value: fields.policies ?? '1.3.6.1.4.1.99999.1' } })
  return form
}

describe('stamping', () => {
  it('sets no authority by default, and says section 7’s row for a stamp beside the settings', async () => {
    show()
    expect(await screen.findByText('No time-stamping authority is set for this desk, so Desk stamps nothing.')).toBeTruthy()
    const words = within(region().getByLabelText('What a stamp establishes'))
    expect(words.getAllByRole('term').map(term => term.textContent)).toEqual(['Establishes', 'Does not establish'])
    expect(words.getAllByRole('definition').map(definition => definition.textContent)).toEqual([ESTABLISHES, DOES_NOT])
    expect(region().queryByRole('button', { name: 'Stamp now' })).toBeNull()
    expect(region().queryByRole('button', { name: 'Remove the authority' })).toBeNull()
    expect(screen.getByText(/with no keys and no checkpoints: it checked no signature, no held checkpoint and no stamp\./)).toBeTruthy()
    // Never Desk's own claim of trust, proof or certification.
    expect(region().queryByText(/\b(trusted|proven|certified)\b/i)).toBeNull()
  })

  it('checks a proposal, states before anything is kept what an authority means, and keeps it only on the confirmation', async () => {
    records = [none, set]
    show()
    const form = await fillForm()
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set this time-stamping authority?' })
    expect(posts).toEqual([{ url: '/api/audit/stamping/check', body: { authority: 'https://tsa.example/stamp', intervalMinutes: 60, roots: ROOTS, policies: ['1.3.6.1.4.1.99999.1'], crls: [] } }])
    expect(within(dialog).getByText('Desk keeps these settings in its own configuration folder, outside the project, and passes the address to jpack audit stamp as --tsa: jpack.json is not changed.')).toBeTruthy()
    expect(within(within(dialog).getByRole('list', { name: 'What setting an authority means' })).getAllByRole('listitem').map(item => item.textContent)).toEqual(CONFIRMATION)
    expect(within(dialog).getByText('CN=test time-stamping root')).toBeTruthy()
    // Cancelling keeps nothing.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(posts.map(post => post.url)).toEqual(['/api/audit/stamping/check'])
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Set the authority' }))
    expect((await screen.findByText('The time-stamping authority is set. Desk stamps this desk’s trail at the interval, while it is running.')).getAttribute('role')).toBe('status')
    expect(posts[2]).toEqual({ url: '/api/audit/stamping', body: { authority: 'https://tsa.example/stamp', intervalMinutes: 60, roots: ROOTS, policies: ['1.3.6.1.4.1.99999.1'], crls: [], token: checkToken } })
    expect(checks).toBe(2)
    expect(await screen.findByRole('button', { name: 'Stamp now' })).toBeTruthy()
  })

  it('sends each revocation list’s bytes in base64, read as bytes', async () => {
    show()
    const form = await fillForm({ policies: '' })
    const bytes = new Uint8Array([0x30, 0x82, 0x00, 0xff, 0x0a])
    fireEvent.change(form.getByLabelText('Revocation lists (optional)'), { target: { files: [new File([bytes], 'list.crl')] } })
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    await screen.findByRole('dialog')
    expect(posts[0]!.body).toEqual({ authority: 'https://tsa.example/stamp', intervalMinutes: 60, roots: ROOTS, policies: [], crls: ['MIIA/wo='] })
  })

  it('shows Desk’s refusal of a proposal in its words, and keeps nothing', async () => {
    const words = 'Give the interval as a whole number of minutes from 5 to 1440.'
    answers['/api/audit/stamping/check'] = () => json(400, { error: words, code: 'bad-request' })
    show()
    const form = await fillForm({ minutes: '4' })
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    expect((await screen.findByText(words)).getAttribute('role')).toBe('alert')
    expect(posts.map(post => [post.url, (post.body as { intervalMinutes: number }).intervalMinutes])).toEqual([['/api/audit/stamping/check', 4]])
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('says a stale confirmation in Desk’s words, and keeps nothing', async () => {
    const stale = 'The stamping settings changed after Desk showed them, so nothing was changed. Check the decision record again.'
    answers['/api/audit/stamping'] = () => json(409, { error: stale, code: 'stale' })
    show()
    const form = await fillForm()
    fireEvent.click(form.getByRole('button', { name: 'Review' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Set the authority' }))
    expect((await screen.findByText(stale)).getAttribute('role')).toBe('alert')
    expect(checks).toBe(1)
  })

  it('shows the settings, the records pending, the stamps the runtime checked and the last run', async () => {
    records = [set]
    show()
    expect(await screen.findByRole('button', { name: 'Stamp now' })).toBeTruthy()
    const authority = within(region().getByLabelText('Time-stamping authority'))
    expect(authority.getByText('https://tsa.example/stamp')).toBeTruthy()
    expect(authority.getByText('60 minutes')).toBeTruthy()
    expect(within(region().getByRole('list', { name: 'Root certificates' })).getByText('CN=test time-stamping root')).toBeTruthy()
    const pending = within(region().getByLabelText('Pending'))
    expect([pending.getByRole('term').textContent, pending.getByRole('definition').textContent]).toEqual(['Records pending a stamp', '1'])
    const stamps = within(region().getByLabelText('Stamps the runtime checked'))
    expect(stamps.getByText('2026-10-07T13:29:22Z')).toBeTruthy()
    expect(stamps.getByText('14.607222481 s, record 2')).toBeTruthy()
    expect(stamps.getByText('10.92565753 s, record 1')).toBeTruthy()
    expect(region().getByText(`Last stamp run, ${when(1791205200)} by Desk’s clock: the authority stamped the checkpoint at record 2, stating it existed by 2026-10-07T13:29:07Z, under policy 1.3.6.1.4.1.99999.1.`)).toBeTruthy()
    expect(region().queryByText('The runtime did not check the stamps.')).toBeNull()
    // The stamps the runtime checked reach the record the run named: no label.
    expect(region().queryByText(/the authority’s answer to Desk’s request/)).toBeNull()
    expect(screen.getByText(/with the time-stamping roots you gave and no keys or checkpoints: it checked the stamps against those roots/)).toBeTruthy()
  })

  // Line audit, finding 7: where the report does not say how many records
  // follow the last one stamped (a repair named a line damaged), the count
  // is of lines, and said as one.
  it('says the lines pending a stamp as lines, where the report does not say how many records', async () => {
    records = [{ ...set, stamping: { ...set.stamping!, pending: undefined, pendingLines: 2 } }]
    show()
    const pending = within(await screen.findByLabelText('Pending'))
    expect([pending.getByRole('term').textContent, pending.getByRole('definition').textContent]).toEqual(['Lines pending a stamp', '2'])
    expect(region().queryByText('Records pending a stamp')).toBeNull()
  })

  it('runs one stamp on request, and checks the decision record again', async () => {
    const after: Reported = { ...set, stamping: { ...set.stamping!, pending: 0, last: { ...stamped, sequence: 3, requested: true } } }
    records = [set, after]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Stamp now' }))
    await waitFor(() => expect(checks).toBe(2))
    expect(posts).toEqual([{ url: '/api/audit/stamping/stamp', body: {} }])
    expect(await screen.findByText(/the authority stamped the checkpoint at record 3/)).toBeTruthy()
  })

  it('says a refusal of Stamp now in Desk’s words', async () => {
    const busy = 'A stamp run for this desk is in progress, so Desk starts no other. The decision record shows its outcome once it ends.'
    answers['/api/audit/stamping/stamp'] = () => json(409, { error: busy, code: 'bad-request' })
    records = [set]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Stamp now' }))
    expect((await screen.findByText(busy)).getAttribute('role')).toBe('alert')
  })

  it('shows a refused run in the runtime’s words, and says when Desk tries again', async () => {
    const said = { code: 'JPS-AUDIT-STAMP-UNREACHABLE', message: 'the time-stamping authority could not be asked, or did not answer: the request was not answered. Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the same checkpoint.' }
    records = [{ ...set, stamping: { ...set.stamping!, last: { at: 1791205200, status: 'refused', diagnostics: [said] } } }]
    show()
    expect(await screen.findByText(`Last stamp run, ${when(1791205200)} by Desk’s clock: the runtime did not stamp. It said:`)).toBeTruthy()
    const words = within(screen.getByRole('list', { name: 'What the runtime said of the stamp' })).getAllByRole('listitem')
    expect(words.map(item => [item.textContent, item.getAttribute('lang')])).toEqual([[`${said.code} ${said.message}`, 'en']])
    expect(screen.getByText('Desk tries again at the next interval.')).toBeTruthy()
  })

  it('labels the sequence of the last run as the authority’s answer where the runtime did not check the stamps', async () => {
    records = [{ ...none, stamping: { state: 'none', last: stamped } }]
    show()
    const said = await screen.findByText('The runtime did not check the stamps.', { exact: false })
    expect(said.textContent).toBe('The runtime did not check the stamps. no time-stamping roots were supplied')
    expect(said.querySelector('[lang="en"]')!.textContent).toBe('no time-stamping roots were supplied')
    expect(screen.getByText('The last stamp run named the checkpoint at record 2: that is the authority’s answer to Desk’s request, not a stamp the runtime checked.')).toBeTruthy()
  })

  it('labels the record the last run named wherever the chassis says no stamp the runtime checked reaches it, with its reason', async () => {
    for (const [stampedState, reason] of [[{ status: 'none' }, 'No stamp the runtime checked covers a record of this trail.'], [{ status: 'through', through: 1 }, 'The stamps the runtime checked reach record 1, before the checkpoint the run named.']] as const) {
      records = [{ ...set, report: { ...checked, coverage: { ...checked.coverage, stamped: stampedState } }, stamping: { ...set.stamping!, lastChecked: { checked: false, reason } } }]
      show()
      expect(await screen.findByText(`${LABEL} ${reason}`), JSON.stringify(stampedState)).toBeTruthy()
      expect(screen.queryByText('The runtime did not check the stamps.')).toBeNull()
      cleanup()
    }
  })

  // Line audit, finding 5, and the second line audit's finding N4 (issue
  // #312): the page decides nothing of it itself. Wherever the chassis does
  // not say the stamps the runtime checked reach the very checkpoint the run
  // named, by trail, sequence and record, the label stays, with the chassis's
  // reason: another trail's stamps, whatever their sequence; and the same
  // trail and sequence holding another record now, stamped from outside Desk,
  // where the report's coverage alone would read as reaching it.
  it('labels the checkpoint the last run named wherever the chassis does not say a stamp the runtime checked reaches it', async () => {
    const moved = 'c962ef5fa560f62c67bd4c1c29e011e3'
    for (const [report, reason] of [[{ ...checked, trail: moved }, OTHER_TRAIL], [{ ...checked, trail: moved, coverage: { ...checked.coverage, stamped: { status: 'through', through: 9 } } }, OTHER_TRAIL],
      [{ ...checked, coverage: { ...checked.coverage, stamped: { status: 'through', through: 2 } } }, REWRITTEN]] as const) {
      records = [{ ...set, report, stamping: { ...set.stamping!, lastChecked: { checked: false, reason } } }]
      show()
      expect(await screen.findByText(`${LABEL} ${reason}`), reason).toBeTruthy()
      cleanup()
    }
    // Where the chassis gives no word on it, the label stays.
    records = [{ ...set, stamping: { ...set.stamping!, lastChecked: undefined } }]
    show()
    expect(await screen.findByText(LABEL)).toBeTruthy()
    cleanup()
    records = [{ ...set, report: { ...checked, coverage: { ...checked.coverage, stamped: { status: 'through', through: 9 } } } }]
    show()
    await screen.findByRole('button', { name: 'Stamp now' })
    expect(screen.queryByText(LABEL)).toBeNull()
  })

  it('takes the chassis’s word on the last run only as it gives it', () => {
    for (const lastChecked of [{ checked: true }, { checked: false, reason: REWRITTEN }]) {
      expect(isAuditStamping({ ...set.stamping, lastChecked }), JSON.stringify(lastChecked)).toBe(true)
    }
    for (const lastChecked of [{ checked: true, reason: REWRITTEN }, { checked: false }, { checked: false, reason: '' }, { checked: 'yes' }, true]) {
      expect(isAuditStamping({ ...set.stamping, lastChecked }), JSON.stringify(lastChecked)).toBe(false)
    }
  })

  it('says settings Desk could not read, passes nothing of them, and offers their removal', async () => {
    const problem = 'Desk could not read the stamping settings it keeps for this desk, so it uses none of them now: settings.json is not a settings file Desk wrote.'
    records = [{ ...none, stamping: { state: 'unread', problem, removeToken } }]
    show()
    expect((await screen.findByText(problem)).getAttribute('role')).toBe('alert')
    expect(region().queryByRole('button', { name: 'Stamp now' })).toBeNull()
    expect(region().getByRole('button', { name: 'Remove the authority' })).toBeTruthy()
  })

  it('removes the authority on the confirmation, keeping the stamps, and checks the decision record again', async () => {
    records = [set, none]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove the authority' }))
    const dialog = await screen.findByRole('dialog', { name: 'Remove this desk’s time-stamping authority?' })
    expect(within(dialog).getByText('The stamps already in the trail’s stamps file are kept as they are.')).toBeTruthy()
    expect(posts).toEqual([])
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove the authority' }))
    expect(await screen.findByText('The time-stamping authority is removed. Desk stamps nothing more; the stamps already in the trail are kept.')).toBeTruthy()
    expect(posts).toEqual([{ url: '/api/audit/stamping/remove', body: { token: removeToken } }])
    expect(checks).toBe(2)
  })

  it('drops what the last action answered when the owner checks again', async () => {
    records = [set, none, none]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove the authority' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove the authority' }))
    const removed = 'The time-stamping authority is removed. Desk stamps nothing more; the stamps already in the trail are kept.'
    expect(await screen.findByText(removed)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(checks).toBe(3))
    await waitFor(() => expect(screen.queryByText(removed)).toBeNull())
  })
})

describe('the stamping client', () => {
  it('reads the decision record’s word on stamping, and refuses what is not one', () => {
    for (const value of [set.stamping, none.stamping, { state: 'unread', problem: 'Why.' }, { state: 'unavailable', problem: 'Why.' }, { ...set.stamping, pending: undefined, pendingLines: 2 }] as AuditStamping[]) {
      expect(isAuditStamping(value), JSON.stringify(value)).toBe(true)
      expect(isAuditRecord({ ...set, stamping: value })).toBe(true)
    }
    for (const value of [{ state: 'set' }, { state: 'set', settings, removeToken: 'ab' }, { state: 'none', settings }, { state: 'none', passed: true },
      { state: 'unread' }, { state: 'unavailable', problem: 'Why.', removeToken }, { state: 'set', settings: { ...settings, roots: [] }, removeToken },
      { state: 'set', settings: { ...settings, intervalMinutes: 4 }, removeToken }, { state: 'set', settings: { ...settings, authority: 'ftp://x' }, removeToken },
      { ...set.stamping, pending: -1 }, { ...set.stamping, pendingLines: -1 }, { ...set.stamping, pending: 1, pendingLines: 2 },
      { ...set.stamping, last: { ...stamped, status: 'done' } }, 'stamping']) {
      expect(isAuditStamping(value), JSON.stringify(value)).toBe(false)
      expect(isAuditRecord({ ...set, stamping: value }), JSON.stringify(value)).toBe(false)
    }
    expect(isAuditRecord({ state: 'no-trail', stamping: none.stamping })).toBe(false)
  })

  it('reads each run with what it carries, and nothing else', () => {
    expect(isStampRun(stamped)).toBe(true)
    expect(isStampRun({ at: 1, status: 'already-stamped', trail, sequence: 2 })).toBe(true)
    expect(isStampRun({ at: 1, status: 'refused', diagnostics: [{ code: 'JPS-X', message: 'Why.' }] })).toBe(true)
    expect(isStampRun({ at: 1, status: 'problem', problem: 'Why.' })).toBe(true)
    expect(isStampRun({ ...stamped, digest: 'sha256:' + '0a'.repeat(32) })).toBe(true)
    for (const value of [{ ...stamped, existedBy: undefined }, { ...stamped, sequence: 0 }, { ...stamped, trail: 'x' }, { at: 1, status: 'already-stamped', trail, sequence: 2, policy: '1.2' },
      { ...stamped, digest: 'sha256:x' }, { at: 1, status: 'problem', problem: 'Why.', digest: 'sha256:' + '0a'.repeat(32) },
      { at: 1, status: 'refused', diagnostics: [] }, { at: 1, status: 'problem' }, { at: 1, status: 'refused', diagnostics: [{ code: 'JPS-X', message: 'Why.' }], trail }, { status: 'problem', problem: 'Why.' }]) {
      expect(isStampRun(value), JSON.stringify(value)).toBe(false)
    }
  })

  it('reads the stamps of a report as the runtime gives them', () => {
    expect(isAuditReport(checked)).toBe(true)
    expect(isAuditReport({ ...checked, trail: undefined })).toBe(true)
    expect(isAuditReport({ ...checked, trail: 'x' })).toBe(false)
    for (const stamps of [{ ...checked.stamps, lag: undefined }, { ...checked.stamps, trusted: -1 }, { ...checked.stamps, lag: { ...checked.stamps!.lag, maxSequence: undefined } },
      { ...checked.stamps, coveredBy: '' }, { ...checked.stamps, lag: { ...checked.stamps!.lag, atAfterStamp: 'no' } }]) {
      expect(isAuditReport({ ...checked, stamps }), JSON.stringify(stamps)).toBe(false)
    }
  })

  it('posts Stamp now as an empty JSON request, and reads its run', async () => {
    expect(await stampNow()).toEqual({ ...stamped, requested: true })
    expect(vi.mocked(deskFetch)).toHaveBeenCalledWith('/api/audit/stamping/stamp', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
    answers['/api/audit/stamping/stamp'] = () => json(200, { run: { status: 'stamped' } })
    await expect(stampNow()).rejects.toThrow('Desk could not read what the stamp run answered. The decision record, checked again, shows the stamps the runtime accepts.')
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/stamping.go'), 'utf8')
    expect(STAMPING_REASONS).toHaveLength(35)
    for (const reason of STAMPING_REASONS) {
      for (const part of reason.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, reason).toBe(part)
      }
    }
  })
})

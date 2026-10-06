/**
 * The hand-over in the decision-record panel (ADR-0010, section 2): the
 * holders and what Desk recorded of each, in each state the chassis answers;
 * ADR's sentence beside them; adding a holder; a download saved exactly as
 * served, under the name it was served with, and the owner's confirmation,
 * after which the holders and the decision record are read again; a stale
 * confirmation; nothing new; and a refusal that is not JSON. The client's
 * checks of what the chassis answers, and that each of Desk's own sentences
 * about the hand-over is one the chassis says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { formatDate } from '../i18n'
import { followsTheProject } from '../mcp/projectChange'
import { testQueryClient } from '../testing/harness'
import { downloadCheckpoints, HANDOVER_REASONS, HOLDERS_KEY, isAuditRecord, isHolders, type AuditRecord, type AuditReport, type Holder, type Holders } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const trail = '9a5ef41d74e7d7e003c8a34cff056351'
const moved = 'c962ef5fa560f62c67bd4c1c29e011e3'
const digest = 'sha256:' + 'ab'.repeat(32)
const fileDigest = 'sha256:' + 'cd'.repeat(32)
const ADR_SENTENCE = "This is Desk's own record. You keep it, and you can change it, so it proves nothing to a holder or to anyone else. Only the holder's own copy counts."
const HELD_STATEMENT = 'Desk ran this on your machine, over your trail, with keys and checkpoints you keep. It shows what a holder would see. It is not evidence to anyone who does not trust you. A holder runs the same command on a copy, with what it holds.'
const STALE = 'What you downloaded is not what the trail gives now. Download it again and hand over that file.'
const report: AuditReport = { status: 'valid', lines: 5, bytes: 10, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 5, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked' }, signedRecords: 0, unsignedRecords: 0,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 5, stamped: { status: 'not-checked' } },
  segments: [{ firstLine: 1, lastLine: 5 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: [], doesNotEstablish: [] }
const auditor: Holder = { id: 'a1b2c3d4e5f60718', label: 'Auditor', channel: 'e-mail to records@example.com', addedAt: 1791201600, trails: {} }
const handed: Holder = { ...auditor, trails: { [trail]: { through: 3, confirmedAt: 1791205200, digest, unwitnessed: 2 } } }
/** Bytes a re-encoding would change: CRLF, a byte that is not UTF-8, and no final newline. */
const served = new Uint8Array([0x7b, 0x7d, 0x0d, 0x0a, 0xff, 0xfe, 0x7b, 0x22, 0x26, 0x22, 0x7d])
const name = `checkpoints-${trail}-1-3.jsonl`

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const checkpoints = (headers: Record<string, string> = {}) => new Response(served, { status: 200, headers: {
  'Content-Type': 'application/jsonl', 'Content-Disposition': `attachment; filename="${name}"`, 'Desk-Checkpoints-Trail': trail,
  'Desk-Checkpoints-From': '0', 'Desk-Checkpoints-Through': '3', 'Desk-Checkpoints-Digest': fileDigest, 'Desk-Checkpoints-More': 'false', ...headers } })

let record: AuditRecord
let holders: Holders[]
let download: () => Response
let confirm: () => Response
let add: () => Response
let asked: { url: string; method: string; body?: unknown }[]
let saved: { blob: Blob; name: string }[]
beforeEach(() => {
  asked = []
  saved = []
  record = { state: 'report', runtime: '0.27.1', report }
  holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 } }]
  download = () => checkpoints()
  confirm = () => json(200, { ...auditor, trails: { [trail]: { through: 3, confirmedAt: 1791205200, digest: fileDigest, unwitnessed: 0 } } })
  add = () => json(201, { id: '0f1e2d3c4b5a6978', label: 'Regulator', channel: 'portal', addedAt: 1791201600, trails: {} })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    const method = init?.method ?? 'GET'
    asked.push({ url: String(url), method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (String(url) === '/api/audit/verify') return json(200, record)
    if (String(url) === '/api/audit/holders' && method === 'GET') return json(200, holders.length > 1 ? holders.shift()! : holders[0]!)
    if (String(url) === '/api/audit/holders' && method === 'POST') return add()
    if (String(url).startsWith('/api/audit/checkpoints?')) return download()
    if (String(url).endsWith('/confirm') && method === 'POST') return confirm()
    return json(404, { error: 'not here' })
  })
  let pending: Blob | undefined
  URL.createObjectURL = (blob: Blob) => { pending = blob; return 'blob:checkpoints' }
  URL.revokeObjectURL = () => undefined
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    if (pending) saved.push({ blob: pending, name: this.download })
  })
})
const createObjectURL = URL.createObjectURL, revokeObjectURL = URL.revokeObjectURL
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.restoreAllMocks(); URL.createObjectURL = createObjectURL; URL.revokeObjectURL = revokeObjectURL })

function show(client: QueryClient = testQueryClient()) {
  render(<QueryClientProvider client={client}><DecisionRecord /></QueryClientProvider>)
  return client
}
const section = () => within(screen.getByRole('region', { name: 'Hand-over' }))
/** The section, once the panel has answered and shows it. */
const opened = async () => within(await screen.findByRole('region', { name: 'Hand-over' }))
const count = (url: string, method = 'GET') => asked.filter(call => call.url === url && call.method === method).length

describe('the hand-over section', () => {
  it('lists each holder with what Desk recorded, and says beside the list what that record is', async () => {
    const elsewhere: Holder = { ...auditor, id: '0f1e2d3c4b5a6978', label: 'Counterparty', channel: 'ticket', trails: { [moved]: { through: 9, confirmedAt: 1791201000, digest } }, otherTrail: true }
    const fresh: Holder = { ...auditor, id: '1122334455667788', label: 'Regulator', channel: 'portal' }
    holders = [{ holders: [handed, elsewhere, fresh], trail: { identity: trail, sequence: 5 } }]
    show()
    const items = await screen.findAllByRole('listitem', { name: /^(Auditor|Counterparty|Regulator)$/ })
    expect(section().getByText(ADR_SENTENCE)).toBeTruthy()
    const [first, second, third] = items.map(item => within(item))
    expect(first!.getByText('Auditor')).toBeTruthy()
    expect(first!.getByText('e-mail to records@example.com')).toBeTruthy()
    expect(first!.getByText(`Handed over through record 3 on ${formatDate(1791205200 * 1000, { dateStyle: 'medium', timeStyle: 'short' })}`)).toBeTruthy()
    expect(first!.getByLabelText('SHA-256 of what was handed over').textContent).toBe(digest)
    expect(first!.getByText('Records since: 2')).toBeTruthy()
    expect(second!.getByText('The trail was moved aside since: this holder starts at 0 for the new trail')).toBeTruthy()
    expect(second!.queryByText(/^Handed over/)).toBeNull()
    expect(third!.getByText('Nothing handed over yet')).toBeTruthy()
    expect(section().getAllByRole('button', { name: 'Download checkpoints' })).toHaveLength(3)
    // Never a claim of Desk's own: the runtime's sentences are the only ones.
    expect(screen.getByRole('region', { name: 'Hand-over' }).textContent).not.toMatch(/verified|witnessed|proof|evidence/i)
  })

  it('says when there is no holder yet', async () => {
    holders = [{ holders: [], trail: { identity: trail, sequence: 3 } }]
    show()
    expect(await screen.findByText('No holder yet.')).toBeTruthy()
    expect(section().queryByRole('button', { name: 'Download checkpoints' })).toBeNull()
  })

  it('offers nothing to download where the trail has no chained record, and shows what was recorded', async () => {
    holders = [{ holders: [handed], trail: null }]
    show()
    expect(await screen.findByText('The trail has no chained record yet, so there is nothing to hand over.')).toBeTruthy()
    expect(section().getByText(/^Handed over through record 3 on /)).toBeTruthy()
    expect(section().queryByText(/^Records since/)).toBeNull()
    expect(section().queryByRole('button', { name: 'Download checkpoints' })).toBeNull()
  })

  it('says the runtime’s refusal to give a checkpoint, in its words', async () => {
    const said = 'The trail fails 1 check(s), the first incomplete-last-line at line 4, so no checkpoint is given for it; jpack audit verify lists them.'
    holders = [{ holders: [auditor], trail: null, diagnostics: [{ code: 'JPS-AUDIT-CHECKPOINT-REFUSED', message: said }] }]
    show()
    expect(await screen.findByText('The runtime gives no checkpoint of this trail now, so nothing can be handed over.')).toBeTruthy()
    const list = section().getByRole('list', { name: 'What the runtime said' })
    expect(list.textContent).toBe('JPS-AUDIT-CHECKPOINT-REFUSED ' + said)
    expect(list.querySelector('li')!.getAttribute('lang')).toBe('en')
    expect(section().queryByRole('button', { name: 'Download checkpoints' })).toBeNull()
  })

  it('is shown beside the runtime’s refusal to check the trail too', async () => {
    record = { state: 'unverified', runtime: '0.27.1', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'None yet.' }] }
    show()
    expect(await (await opened()).findByText('Nothing handed over yet')).toBeTruthy()
  })

  it('runs only when shown and when asked, and follows no change to the project', async () => {
    const client = show()
    await (await opened()).findByText('Nothing handed over yet')
    expect(count('/api/audit/holders')).toBe(1)
    expect(followsTheProject(client.getQueryCache().find({ queryKey: HOLDERS_KEY })!)).toBe(false)
  })

  it('adds a holder by the owner’s words, and lists it', async () => {
    holders = [{ holders: [], trail: { identity: trail, sequence: 3 } }, { holders: [{ id: '0f1e2d3c4b5a6978', label: 'Regulator', channel: 'portal', addedAt: 1791201600, trails: {} }], trail: { identity: trail, sequence: 3 } }]
    show()
    await screen.findByText('No holder yet.')
    const form = within(screen.getByRole('form', { name: 'Add holder' }))
    const button = form.getByRole('button', { name: 'Add holder' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect((form.getByLabelText('Label') as HTMLInputElement).placeholder).toBe('Counterparty: procurement desk')
    expect((form.getByLabelText('Channel') as HTMLInputElement).placeholder).toBe('e-mail to records@…')
    fireEvent.change(form.getByLabelText('Label'), { target: { value: 'Regulator' } })
    fireEvent.change(form.getByLabelText('Channel'), { target: { value: 'portal' } })
    fireEvent.click(button)
    expect(await screen.findByRole('listitem', { name: 'Regulator' })).toBeTruthy()
    expect(asked.filter(call => call.method === 'POST')).toEqual([{ url: '/api/audit/holders', method: 'POST', body: { label: 'Regulator', channel: 'portal' } }])
    expect((form.getByLabelText('Label') as HTMLInputElement).value).toBe('')
  })

  it('says why a holder was refused', async () => {
    const refused = "A holder's label is 1 to 120 characters and its channel 1 to 200, each with no control characters."
    add = () => json(400, { error: refused, code: 'bad-request' })
    show()
    await (await opened()).findByText('Nothing handed over yet')
    const form = within(screen.getByRole('form', { name: 'Add holder' }))
    fireEvent.change(form.getByLabelText('Label'), { target: { value: 'x'.repeat(121) } })
    fireEvent.change(form.getByLabelText('Channel'), { target: { value: 'portal' } })
    fireEvent.click(form.getByRole('button', { name: 'Add holder' }))
    expect((await form.findByRole('alert')).textContent).toBe(refused)
    expect(count('/api/audit/holders')).toBe(1)
  })
})

describe('handing checkpoints over', () => {
  it('saves exactly the bytes served, under their name, and records them on the owner’s confirmation, then checks the trail again', async () => {
    holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 } }, { holders: [{ ...auditor, trails: { [trail]: { through: 3, confirmedAt: 1791205200, digest: fileDigest, unwitnessed: 0 } } }], trail: { identity: trail, sequence: 3 } }]
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect(await (await opened()).findByText(`Saved ${name}: the checkpoints after record 0, through record 3.`)).toBeTruthy()
    expect(asked.some(call => call.url === '/api/audit/checkpoints?holder=a1b2c3d4e5f60718')).toBe(true)
    expect(saved).toHaveLength(1)
    expect(saved[0]!.name).toBe(name)
    expect(new Uint8Array(await saved[0]!.blob.arrayBuffer())).toEqual(served)
    expect(section().getByLabelText('SHA-256 of the file').textContent).toBe(fileDigest)
    expect(section().getByText('Confirm: the file went to Auditor')).toBeTruthy()
    expect(section().queryByText(/^More records remain/)).toBeNull()
    // A download moves nothing: nothing was confirmed, and nothing checked again.
    expect(asked.filter(call => call.url.endsWith('/confirm'))).toEqual([])
    expect(count('/api/audit/verify')).toBe(1)
    fireEvent.click(section().getByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Recorded: the file went to Auditor, through record 3.')).toBeTruthy()
    expect(asked.filter(call => call.url.endsWith('/confirm'))).toEqual([{ url: '/api/audit/holders/a1b2c3d4e5f60718/confirm', method: 'POST', body: { trail, from: 0, through: 3, digest: fileDigest } }])
    await waitFor(() => expect(count('/api/audit/verify')).toBe(2))
    expect(await (await opened()).findByText(/^Handed over through record 3 on /)).toBeTruthy()
    expect(count('/api/audit/holders')).toBeGreaterThanOrEqual(2)
    expect(section().queryByRole('button', { name: 'Confirm' })).toBeNull()
    expect(screen.getByText('Recorded: the file went to Auditor, through record 3.')).toBeTruthy()
  })

  it('drops a download waiting for its confirmation when the panel is opened again', async () => {
    const client = testQueryClient()
    const view = (visible: boolean) => <QueryClientProvider client={client}><DecisionRecord visible={visible} /></QueryClientProvider>
    const { rerender } = render(view(true))
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect(await (await opened()).findByRole('button', { name: 'Confirm' })).toBeTruthy()
    rerender(view(false))
    rerender(view(true))
    await waitFor(() => expect(count('/api/audit/verify')).toBe(2))
    expect(await (await opened()).findByText('Nothing handed over yet')).toBeTruthy()
    expect(section().queryByRole('button', { name: 'Confirm' })).toBeNull()
  })

  it('says more remain where the answer says so', async () => {
    download = () => checkpoints({ 'Desk-Checkpoints-More': 'true' })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect(await (await opened()).findByText('More records remain: download again once this is confirmed.')).toBeTruthy()
  })

  it('says a stale confirmation, records nothing, and drops the confirmation', async () => {
    confirm = () => json(409, { reason: 'stale', code: 'stale', error: STALE })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Confirm' }))
    expect((await (await opened()).findByText(STALE)).getAttribute('role')).toBe('alert')
    expect(section().queryByRole('button', { name: 'Confirm' })).toBeNull()
    expect(section().queryByText(/^Recorded/)).toBeNull()
    expect(count('/api/audit/verify')).toBe(1)
  })

  it('says when there is nothing new, and saves nothing', async () => {
    download = () => new Response(null, { status: 204, headers: { 'Desk-Checkpoints-Trail': trail } })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect((await (await opened()).findByText('Nothing new to hand over to this holder.')).getAttribute('role')).toBe('status')
    expect(saved).toHaveLength(0)
    expect(section().queryByRole('button', { name: 'Confirm' })).toBeNull()
  })

  it('says a refusal that is not JSON as one, and saves nothing', async () => {
    download = () => new Response('upstream broke', { status: 500, headers: { 'Content-Type': 'text/plain' } })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect((await (await opened()).findByRole('alert')).textContent).toBe('The checkpoints could not be downloaded. Please try again.')
    expect(saved).toHaveLength(0)
    expect(section().queryByRole('button', { name: 'Confirm' })).toBeNull()
  })

  it('says the chassis’s refusal in its words', async () => {
    const said = 'The runtime gives no checkpoint of this trail now: The trail fails 1 check(s), the first incomplete-last-line at line 4, so no checkpoint is given for it; jpack audit verify lists them.'
    download = () => json(409, { error: said, code: 'bad-request' })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    expect((await (await opened()).findByRole('alert')).textContent).toBe(said)
  })

  it('keeps the confirmation where it could not be recorded for another reason', async () => {
    confirm = () => new Response('busy', { status: 500 })
    show()
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Download checkpoints' }))
    fireEvent.click(await (await opened()).findByRole('button', { name: 'Confirm' }))
    expect((await (await opened()).findByRole('alert')).textContent).toBe('The hand-over could not be recorded. Please try again.')
    expect(section().getByRole('button', { name: 'Confirm' })).toBeTruthy()
    expect(count('/api/audit/verify')).toBe(1)
  })
})

describe('the decision record with what was handed over', () => {
  it('says what Desk ran it with when it passed checkpoints it handed over', async () => {
    record = { state: 'report', runtime: '0.27.1', report, expected: 1 }
    show()
    expect(await screen.findByText(HELD_STATEMENT)).toBeTruthy()
  })

  it('names the holders whose checkpoints it could not read', async () => {
    record = { state: 'report', runtime: '0.27.1', report, expected: 1, expectUnread: ['Auditor', 'Regulator'] }
    show()
    expect(await screen.findByText('Desk could not read the checkpoints it keeps as handed over to Auditor, Regulator, so the check ran without them.')).toBeTruthy()
  })
})

describe('the hand-over client', () => {
  it('reads the holders, and refuses what is not them', () => {
    expect(isHolders({ holders: [handed, auditor], trail: { identity: trail, sequence: 5 } })).toBe(true)
    expect(isHolders({ holders: [], trail: null, diagnostics: [{ code: 'JPS-AUDIT-CHECKPOINT-REFUSED', message: 'Refused.' }] })).toBe(true)
    for (const value of [{ holders: [] }, { holders: [{ ...auditor, id: 'A1B2C3D4E5F60718' }], trail: null }, { holders: [{ ...auditor, label: '' }], trail: null },
      { holders: [{ ...auditor, trails: { 'not-a-trail': handed.trails[trail] } }], trail: null }, { holders: [{ ...auditor, trails: { [trail]: { ...handed.trails[trail], digest: 'sha256:x' } } }], trail: null },
      { holders: [{ ...auditor, trails: { [trail]: { ...handed.trails[trail], through: 0 } } }], trail: null }, { holders: [], trail: { identity: trail, sequence: 0 } },
      { holders: [], trail: { identity: trail, sequence: 3 }, diagnostics: [{ code: 'X', message: 'Y' }] }, { holders: [], trail: null, diagnostics: [] }]) {
      expect(isHolders(value), JSON.stringify(value)).toBe(false)
    }
    expect(isAuditRecord({ state: 'report', report, expected: 2, expectUnread: ['Auditor'] })).toBe(true)
    expect(isAuditRecord({ state: 'report', report, expected: -1 })).toBe(false)
    expect(isAuditRecord({ state: 'report', report, expectUnread: [''] })).toBe(false)
  })

  it('refuses a download whose headers do not name what was served', async () => {
    for (const headers of [{ 'Desk-Checkpoints-Digest': 'sha256:x' }, { 'Desk-Checkpoints-Trail': 'x' }, { 'Desk-Checkpoints-Through': '0' },
      { 'Desk-Checkpoints-More': 'maybe' }, { 'Content-Disposition': 'attachment; filename="evaluations.jsonl"' },
      { 'Content-Disposition': `attachment; filename="checkpoints-${trail}-2-3.jsonl"` }] as Record<string, string>[]) {
      download = () => checkpoints(headers)
      await expect(downloadCheckpoints(auditor.id), JSON.stringify(headers)).rejects.toThrow('The checkpoints could not be downloaded. Please try again.')
    }
    download = () => new Response(null, { status: 204 })
    expect(await downloadCheckpoints(auditor.id)).toBeNull()
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/handover.go'), 'utf8')
    expect(HANDOVER_REASONS).toHaveLength(6)
    for (const reason of HANDOVER_REASONS) {
      for (const part of reason.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, reason).toBe(part)
      }
    }
    expect(source.includes(STALE)).toBe(true)
  })
})

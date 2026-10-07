/**
 * Rotating a desk's signing key from the decision-record panel (ADR-0010,
 * section 1, "Rotating it"): the action, and the confirmation's exact words;
 * only the confirmation sends the panel's token; a rotation's answer, kept
 * across the check the panel runs after it; a refusal, in Desk's words; a
 * rotation that did not finish; and why none is offered. The client's checks
 * of what the chassis answers, and that each of Desk's own sentences about
 * rotation is one the chassis says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, isAuditRotation, ROTATION_REASONS, rotateSigningKey, StaleRotation, type AuditKeys, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

// Two keys runtime 0.26.0 generated, and the keyIds it printed for them.
const deskKey = { publicKey: '882a7f2be72a4b0c0a03b590300c72e8ed3fab24355a6950f9e6399814c67350', keyId: '4ba1de706a3baa4d8f5456340604190a', at: 0 }
const nextKey = { publicKey: '272ad95977cf9721551ead07d2c9563cb7403b3688a8c23508b6a02ba4302623', keyId: '1cba17c1fa03b21777f6bf729e3f1a2b', at: 7 }
const token = 'ab'.repeat(32)
const report: AuditReport = { status: 'valid', lines: 7, bytes: 2856, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 7, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'through', through: 7 }, signedRecords: 7, unsignedRecords: 0,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 7, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } },
  segments: [{ firstLine: 1, lastLine: 7 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: [], doesNotEstablish: [] }
const kept: AuditKeys = { state: 'kept', public: [deskKey] }
type Reported = Extract<AuditRecord, { state: 'report' }>
const offered: Reported = { state: 'report', runtime: '0.27.1', report, keys: kept, signing: { state: 'no-key' }, rotation: { state: 'available', token } }
const rotated = { state: 'rotated', at: 7, from: deskKey, next: nextKey }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let records: AuditRecord[]
let rotation: () => Response
let checks: number
let posts: { body: unknown; headers: HeadersInit | undefined }[]
beforeEach(() => {
  checks = 0
  posts = []
  records = [offered]
  rotation = () => json(200, rotated)
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (String(url) === '/api/audit/verify') {
      checks++
      return json(200, records.length > 1 ? records.shift()! : records[0]!)
    }
    if (String(url) === '/api/audit/key/rotate' && init?.method === 'POST') {
      posts.push({ body: JSON.parse(String(init.body)), headers: init.headers })
      return rotation()
    }
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  return render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}
const region = () => within(screen.getByRole('region', { name: 'Rotating the key' }))

const CONFIRMATION = [
  'The current key stops signing. Records written after the rotation are signed with the new key.',
  'A record written while the rotation is in progress may be unsigned.',
  'A rotation revokes nothing: whoever holds the old key can still sign as it. Give each holder the new public key, and tell them about the old one if you no longer trust it: only a holder’s own jpack audit verify --revoked refuses what it signs.',
  'The old key’s file loses its name, but its bytes may remain on the disk.',
  'A lost key cannot be rotated away from: a rotation needs the key in force.'
]

describe('rotating the signing key', () => {
  it('offers the action, and says in its confirmation what a rotation does and does not do', async () => {
    show()
    const button = await screen.findByRole('button', { name: 'Rotate signing key' })
    expect(region().getByText('Rotation hands this desk’s signing over to a new key. It is never done on a schedule: only when you ask.')).toBeTruthy()
    fireEvent.click(button)
    const dialog = await screen.findByRole('dialog', { name: 'Rotate this desk’s signing key?' })
    expect(within(dialog).getByText('Desk has the runtime make a new key beside the current one and hand signing over to it, with jpack audit key rotate.')).toBeTruthy()
    expect(within(within(dialog).getByRole('list', { name: 'What a rotation does and does not do' })).getAllByRole('listitem').map(item => item.textContent)).toEqual(CONFIRMATION)
    // Opening it sends nothing; cancelling sends nothing either.
    expect(posts).toEqual([])
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(posts).toEqual([])
  })

  it('sends the panel’s token on confirmation, says what was rotated, and checks the trail again', async () => {
    records = [offered, { ...offered, keys: { state: 'kept', public: [deskKey, nextKey] }, rotation: { state: 'unavailable', reason: "This desk's key took over after record 7, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first." } }]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate signing key' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Rotate the key' }))
    expect(await screen.findByText('The key was rotated: key 2 signs the records after record 7, and key 1 signs nothing more. Hand the new public key to each holder.')).toBeTruthy()
    expect(posts).toEqual([{ body: { token }, headers: { 'Content-Type': 'application/json' } }])
    expect(checks).toBe(2)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Rotate signing key' })).toBeNull()
    expect(region().getByText("This desk's key took over after record 7, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first.")).toBeTruthy()
    expect([...screen.getByRole('region', { name: 'Signing key' }).querySelectorAll('pre')].map(block => block.getAttribute('aria-label')))
      .toEqual([`Public key 1, keyId ${deskKey.keyId}, signing from the first record`, `Public key 2, keyId ${nextKey.keyId}, signing the records after record 7`])
  })

  it('says a rotation the runtime wrote and Desk did not finish as not finished, never as rotated, and checks the trail again', async () => {
    const unfinished = 'The rotation of this desk\'s key did not finish: the runtime wrote it, and Desk could not finish it: the list of public keys could not be written with the next key: the list of public keys changed after it was read. Records written until it is finished may be unsigned. Desk finishes it when it next starts, and the decision record says a rotation did not finish.'
    const reason = 'The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.'
    rotation = () => json(500, { error: unfinished, code: 'internal' })
    records = [offered, { ...offered, keys: { state: 'unread', problem: 'Desk\'s list of this desk\'s public keys does not agree with the key rotations in the trail\'s signature sidecar, so it passed no key.' }, rotation: { state: 'unfinished', reason } }]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate signing key' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rotate the key' }))
    expect((await screen.findByText(unfinished)).getAttribute('role')).toBe('alert')
    expect(await screen.findByText('A rotation of this desk’s signing key did not finish. ' + reason)).toBeTruthy()
    expect(checks).toBe(2)
    expect(screen.queryByText(/^The key was rotated/)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Rotate signing key' })).toBeNull()
    expect(screen.getByRole('region', { name: 'Signing key' }).querySelectorAll('pre')).toHaveLength(0)
  })

  it('reads the keys afresh when the owner checks again, and drops what the last rotation answered', async () => {
    const after = { ...offered, keys: { state: 'kept', public: [deskKey, nextKey] }, rotation: { state: 'unavailable', reason: 'Why not now.' } } satisfies Reported
    records = [offered, after]
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate signing key' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rotate the key' }))
    expect(await screen.findByText(/^The key was rotated/)).toBeTruthy()
    expect(checks).toBe(2)
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(checks).toBe(3))
    await waitFor(() => expect(screen.queryByText(/^The key was rotated/)).toBeNull())
    expect(await screen.findByText('Why not now.')).toBeTruthy()
  })

  it('reads the keys afresh when the panel is opened again, and drops what the last rotation answered', async () => {
    records = [offered, { ...offered, rotation: { state: 'unavailable', reason: 'Why not now.' } }, { ...offered, rotation: { state: 'unavailable', reason: 'Read again.' } }]
    const client = testQueryClient()
    const view = (visible: boolean) => <QueryClientProvider client={client}><DecisionRecord visible={visible} /></QueryClientProvider>
    const { rerender } = render(view(true))
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate signing key' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rotate the key' }))
    expect(await screen.findByText(/^The key was rotated/)).toBeTruthy()
    expect(await screen.findByText('Why not now.')).toBeTruthy()
    rerender(view(false))
    rerender(view(true))
    expect(await screen.findByText('Read again.')).toBeTruthy()
    expect(checks).toBe(3)
    expect(screen.queryByText(/^The key was rotated/)).toBeNull()
  })

  it('shows a refusal in Desk’s words, and checks the trail again', async () => {
    const refused = 'The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names.'
    rotation = () => json(409, { error: refused, code: 'bad-request' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate signing key' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rotate the key' }))
    expect((await screen.findByText(refused)).getAttribute('role')).toBe('alert')
    expect(checks).toBe(2)
    expect(screen.queryByText(/^The key was rotated/)).toBeNull()
  })

  it('says a rotation did not finish, and why, and offers none', async () => {
    const reason = 'Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: the trail\'s signature sidecar could not be read: a runtime held the trail\'s lock for too long.'
    records = [{ ...offered, rotation: { state: 'unfinished', reason } }]
    show()
    const alert = await screen.findByText(/^A rotation of this desk’s signing key did not finish\./)
    expect(alert.textContent).toBe('A rotation of this desk’s signing key did not finish. ' + reason)
    expect(alert.getAttribute('role')).toBe('alert')
    expect(screen.queryByRole('button', { name: 'Rotate signing key' })).toBeNull()
  })

  it('says why none is offered, and offers none', async () => {
    for (const reason of ['Desk keeps no signing key for the project it was started on, so it has none to rotate.', 'Desk keeps no signing key for this desk, so it has none to rotate.',
      'JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project\'s records with the key it names, not with the key Desk keeps, so Desk rotates no key here.']) {
      records = [{ ...offered, rotation: { state: 'unavailable', reason } }]
      show()
      expect(await screen.findByText(reason)).toBeTruthy()
      expect(screen.queryByRole('button', { name: 'Rotate signing key' })).toBeNull()
      cleanup()
    }
  })
})

describe('the rotation client', () => {
  it('reads the panel’s word on rotation, and refuses what is not one', () => {
    for (const value of [{ state: 'available', token }, { state: 'unavailable', reason: 'Why.' }, { state: 'unfinished', reason: 'Why.' }]) {
      expect(isAuditRotation(value), JSON.stringify(value)).toBe(true)
      expect(isAuditRecord({ ...offered, rotation: value }), JSON.stringify(value)).toBe(true)
    }
    for (const value of [{ state: 'available' }, { state: 'available', token: token.slice(1) }, { state: 'available', token: token.toUpperCase() },
      { state: 'available', token, reason: 'Why.' }, { state: 'unavailable' }, { state: 'unavailable', reason: '' }, { state: 'unfinished', reason: 'Why.', token },
      { state: 'rotated' }, 'available']) {
      expect(isAuditRotation(value), JSON.stringify(value)).toBe(false)
      expect(isAuditRecord({ ...offered, rotation: value }), JSON.stringify(value)).toBe(false)
    }
  })

  it('posts the token, reads a rotation, and refuses an answer that is not one', async () => {
    expect(await rotateSigningKey(token)).toEqual(rotated)
    expect(vi.mocked(deskFetch)).toHaveBeenCalledWith('/api/audit/key/rotate', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
    for (const body of [{ ...rotated, state: 'done' }, { ...rotated, at: 0 }, { ...rotated, next: { ...nextKey, at: 6 } }, { ...rotated, next: deskKey }, { ...rotated, from: undefined }]) {
      rotation = () => json(200, body)
      await expect(rotateSigningKey(token), JSON.stringify(body)).rejects.toThrow('The key could not be rotated. Check the decision record again.')
    }
    rotation = () => json(409, { error: 'This desk\'s keys changed after the decision record showed them, so nothing was rotated. Check the decision record again.', code: 'stale' })
    await expect(rotateSigningKey(token)).rejects.toBeInstanceOf(StaleRotation)
    rotation = () => new Response('not json', { status: 500 })
    await expect(rotateSigningKey(token)).rejects.toThrow('The key could not be rotated. Check the decision record again.')
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    // Each sentence's words between its placeholders, as the chassis's Go
    // source spells them.
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/rotation.go'), 'utf8')
    expect(ROTATION_REASONS).toHaveLength(12)
    for (const reason of ROTATION_REASONS) {
      for (const part of reason.split(/\{\{\w+\}\}/)) {
        expect(source.includes(part) ? part : `missing: ${part}`, reason).toBe(part)
      }
    }
  })
})

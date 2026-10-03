/**
 * The upgrade offer on the page: what it lists, that it writes nothing until
 * the owner confirms, that it sends back the token of exactly the offer it
 * showed and the owner's choice about requireComparableFacts, and where it
 * appears.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../../files/client'
import { McpContext } from '../../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../../testing/harness'
import { PacksPane } from '../PacksPane'
import type { Review } from '../review/client'
import { ReviewProvider } from '../review/ReviewContext'
import { confirmUpgrade, readUpgrade, StaleUpgrade, type Upgrade } from './client'
import { dismissedKey, ProjectGates, UpgradeNote } from './UpgradeNote'
import { UpgradeView } from './UpgradeView'

vi.mock(import('../../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))
// The listing's own request goes through the module's own `deskFetch`, which the
// mock above does not reach; the project's identity is all the note reads of it.
vi.mock(import('../../files/queries'), async original => ({ ...(await original()), useFileListing: (() => ({ data: { root: '/projects/existing', files: [] } })) as never }))

const token = 'a'.repeat(64)
const declinedToken = 'b'.repeat(64)
const before = '{\n  "configVersion": "3",\n  "packs": {\n    "alpha": {"path": "packs/a.json"}\n  }\n}\n'
const after = '{\n  "configVersion": "5",\n  "requireReviewed": true,\n  "requireComparableFacts": true,\n  "audit": {\n    "dir": ".desk-private/audit"\n  },\n  "packs": {\n    "alpha": {"path": "packs/a.json"}\n  }\n}\n'
const alpha = '{"id":"alpha","title":"Alpha"}'
const text = (value: string) => ({ state: 'text' as const, digest: 'sha256:' + value })
const review: Review = { status: 'error', locked: false, findings: [], diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock.' }],
  contents: Object.fromEntries([after, alpha].map(value => ['sha256:' + value, value])), files: [
    { kind: 'config', path: 'jpack.json', lock: 'none', now: text(after) },
    { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'none', now: text(alpha) }
  ] }
const offer: Upgrade = { state: 'offer', runtime: '0.25.0', reads: ['1', '2', '3', '4', '5'], gated: false, comparableFacts: 'off', requireComparableFacts: true,
  from: '3', to: '5', changes: ['configVersion', 'requireReviewed', 'requireComparableFacts', 'audit'], configBefore: before, configAfter: after,
  gitignore: 'add', audit: { state: 'create', dir: '.desk-private/audit' }, locked: false, review, token }
const declined: Upgrade = { ...offer, requireComparableFacts: false, changes: ['configVersion', 'requireReviewed', 'audit'], configAfter: after.replace('  "requireComparableFacts": true,\n', ''), token: declinedToken }
const gatedOn: Upgrade = { state: 'unchanged', gated: true, comparableFacts: 'on', requireComparableFacts: false, changes: [], locked: true }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let offers: Record<'with' | 'without', Upgrade[]>
let reads: string[] = []
let posted: unknown[] = []
let confirmAnswer: () => Response
beforeEach(() => {
  offers = { with: [offer], without: [declined] }
  reads = []
  posted = []
  localStorage.clear()
  confirmAnswer = () => json(200, { files: 2, configVersion: '5', requireComparableFacts: true, gitignore: 'add', audit: 'create', copies: 'stored' })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    const path = String(url)
    if (path === '/api/upgrade' && init?.method === 'POST') {
      posted.push({ type: (init.headers as Record<string, string>)['Content-Type'], body: JSON.parse(String(init.body)) })
      return confirmAnswer()
    }
    if (path.startsWith('/api/upgrade')) {
      reads.push(path)
      const list = path.endsWith('requireComparableFacts=false') ? offers.without : offers.with
      return json(200, list.length > 1 ? list.shift() : list[0])
    }
    if (path === '/api/review') return json(200, review)
    return json(404, { error: 'not here' })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show(node: React.ReactNode) {
  render(<QueryClientProvider client={testQueryClient()}><MemoryRouter>{node}</MemoryRouter></QueryClientProvider>)
}

describe('the upgrade client', () => {
  it('refuses an answer that is not an upgrade', async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { state: 'offer' }))
    await expect(readUpgrade(true)).rejects.toThrow('The upgrade could not be loaded')
  })
  it('asks for the offer without requireComparableFacts only when declined', async () => {
    await readUpgrade(true)
    await readUpgrade(false)
    expect(reads).toEqual(['/api/upgrade', '/api/upgrade?requireComparableFacts=false'])
  })
  it('sends the token and the choice as JSON, and tells a stale confirmation apart', async () => {
    confirmAnswer = () => json(409, { code: 'stale', error: 'The project changed after you reviewed the upgrade.' })
    await expect(confirmUpgrade(token, false)).rejects.toBeInstanceOf(StaleUpgrade)
    expect(posted).toEqual([{ type: 'application/json', body: { token, requireComparableFacts: false } }])
    confirmAnswer = () => json(500, { code: 'internal', error: 'The runtime did not lock the project.' })
    await expect(confirmUpgrade(token, true)).rejects.toThrow('The runtime did not lock the project.')
  })
})

describe('the upgrade page', () => {
  it('lists each change, what it writes and why', async () => {
    show(<UpgradeView />)
    const config = await screen.findByRole('listitem', { name: 'jpack.json' })
    expect(config.textContent).toContain('configVersion moves from 3 to 5')
    expect(config.textContent).toContain('requireReviewed: a deciding run must apply exactly the packs you last locked.')
    expect(config.textContent).toContain('audit: each completed deciding run is recorded in .desk-private/audit')
    expect(config.textContent).toContain('Every other member, and the order of the members, stay as they are.')
    expect(config.textContent).toContain(after)
    expect(screen.getByRole('listitem', { name: '.gitignore' }).textContent).toContain('Desk adds the line .desk-private/ at the end')
    const lock = screen.getByRole('listitem', { name: 'The first review and lock' })
    expect(lock.textContent).toContain('The lock covers 2 files')
    expect(lock.textContent).toContain('There is no reviewed-set lock.')
    expect(within(lock).getByText('packs/a.json')).toBeTruthy()
    expect(lock.textContent).toContain(alpha)
    expect(within(lock).queryByText('This project already keeps a lock')).toBeNull()
    const facts = screen.getByRole('listitem', { name: 'requireComparableFacts' })
    expect(within(facts).getByRole('checkbox', { name: 'Also refuse a fact of a type no comparison can match' })).toHaveProperty('checked', true)
    expect(screen.getByText('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')).toBeTruthy()
  })

  it('writes nothing until the owner confirms, then sends back the token of what it showed', async () => {
    // Packs' review is read again afterwards: the project has a new lock.
    show(<ReviewProvider><UpgradeView /></ReviewProvider>)
    const button = await screen.findByRole('button', { name: 'Turn the gates on and lock 2 files' })
    const reviews = () => vi.mocked(deskFetch).mock.calls.filter(([url]) => url === '/api/review').length
    await waitFor(() => expect(reviews()).toBe(1))
    expect(posted).toEqual([])
    fireEvent.click(button)
    expect((await screen.findByRole('status')).textContent).toContain('The gates are on. jpack.json is at configVersion 5, and these 2 files are this project’s reviewed set.')
    expect(posted).toEqual([{ type: 'application/json', body: { token, requireComparableFacts: true } }])
    await waitFor(() => expect(reviews()).toBe(2))
  })

  it('takes the rest without requireComparableFacts, confirming the offer made without it', async () => {
    show(<UpgradeView />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Also refuse a fact of a type no comparison can match' }))
    await waitFor(() => expect(reads).toContain('/api/upgrade?requireComparableFacts=false'))
    await waitFor(() => expect(screen.getByRole('listitem', { name: 'jpack.json' }).textContent).not.toContain('requireComparableFacts'))
    fireEvent.click(screen.getByRole('button', { name: 'Turn the gates on and lock 2 files' }))
    await waitFor(() => expect(posted).toEqual([{ type: 'application/json', body: { token: declinedToken, requireComparableFacts: false } }]))
  })

  it('tells a project that keeps a lock what the new configuration does to it, in the runtime’s own finding too', async () => {
    offers.with = [{ ...offer, locked: true, review: { ...review, status: 'invalid', locked: true, diagnostics: [], findings: [{ name: 'config-drift', path: 'jpack.json', detail: 'The configuration’s own bytes differ from the reviewed set.' }] } }]
    show(<UpgradeView />)
    const warning = await screen.findByRole('region', { name: 'This project already keeps a lock' })
    expect(warning.textContent).toContain('To the runtime the new jpack.json is config-drift')
    expect(warning.textContent).toContain('a CI step that runs packs verify fails until the new lock is committed')
    expect(screen.getByRole('list', { name: 'What the runtime said' }).textContent).toBe('The project file changed; every decision waits for a lock jpack.json The configuration’s own bytes differ from the reviewed set.')
  })

  it('shows the step again when the project changed after the offer', async () => {
    offers.with = [offer, { ...offer, token: 'c'.repeat(64) }]
    confirmAnswer = () => json(409, { code: 'stale', error: 'stale' })
    show(<UpgradeView />)
    fireEvent.click(await screen.findByRole('button', { name: 'Turn the gates on and lock 2 files' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The project changed after you reviewed the upgrade, so nothing was written. Review it again.')
    await waitFor(() => expect(reads.filter(path => path === '/api/upgrade')).toHaveLength(2))
  })

  it('says what it does to .gitignore, and what it cannot offer', async () => {
    for (const [gitignore, says] of [['create', 'Desk creates one holding the line .desk-private/'], ['ignored', '.gitignore already ignores .desk-private/'], ['outside', 'This project is not in a Git work tree']] as const) {
      offers.with = [{ ...offer, gitignore }]
      show(<UpgradeView />)
      expect((await screen.findByRole('listitem', { name: '.gitignore' })).textContent).toContain(says)
      cleanup()
    }
    offers.with = [{ ...offer, comparableFacts: 'unavailable', requireComparableFacts: false, runtime: '0.24.0' }]
    show(<UpgradeView />)
    expect((await screen.findByRole('listitem', { name: 'requireComparableFacts' })).textContent).toContain('requireComparableFacts needs runtime 0.25.0 or later. The runtime this Desk runs (jpack 0.24.0)')
    expect(screen.queryByRole('checkbox')).toBeNull()
  })

  it('offers no confirmation without a token, where nothing can be offered, or where there is nothing to change', async () => {
    offers.with = [{ ...offer, token: undefined }]
    show(<UpgradeView />)
    await screen.findByRole('listitem', { name: 'jpack.json' })
    expect(screen.queryByRole('button', { name: /Turn the gates on/ })).toBeNull()
    cleanup()
    offers.with = [{ state: 'unavailable', reason: 'The runtime reads configuration versions 1, 2, 3.', gated: false, requireComparableFacts: false, changes: [], locked: false }]
    show(<UpgradeView />)
    expect((await screen.findByRole('alert')).textContent).toContain('The runtime reads configuration versions 1, 2, 3.')
    expect(screen.queryByRole('button', { name: /Turn the gates on/ })).toBeNull()
    cleanup()
    offers.with = [gatedOn]
    show(<UpgradeView />)
    expect((await screen.findByText(/This project’s gates are on/)).getAttribute('role')).toBe('status')
    expect(screen.queryByRole('button', { name: /Turn the gates on/ })).toBeNull()
  })
})

describe('where the offer appears', () => {
  it('is a note on Packs until the owner dismisses it, and the dismissal is kept', async () => {
    show(<UpgradeNote />)
    const note = await screen.findByRole('complementary', { name: 'Turn on this project’s gates' })
    expect(within(note).getByRole('link', { name: 'See what changes' }).getAttribute('href')).toBe('/packs/_upgrade')
    fireEvent.click(within(note).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('complementary')).toBeNull()
    expect(localStorage.getItem(dismissedKey('/projects/existing'))).toBe('1')
    cleanup()
    reads = []
    show(<UpgradeNote />)
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(screen.queryByRole('complementary')).toBeNull()
    expect(reads).toEqual([])
  })

  it('is not shown where the gates are on', async () => {
    offers.with = [{ ...gatedOn, state: 'offer', comparableFacts: 'off', token }]
    show(<UpgradeNote />)
    await waitFor(() => expect(reads).toEqual(['/api/upgrade']))
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('is on the Packs collection', async () => {
    const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [{ id: 'alpha' }] }) }) })
    show(<McpContext.Provider value={connected({ client: stub.client })}><ReviewProvider><PacksPane /></ReviewProvider></McpContext.Provider>)
    expect(await screen.findByRole('complementary', { name: 'Turn on this project’s gates' })).toBeTruthy()
  })

  it('stays available in Admin → Project', async () => {
    show(<ProjectGates />)
    expect((await screen.findByRole('link', { name: 'Review the upgrade' })).getAttribute('href')).toBe('/packs/_upgrade')
    cleanup()
    offers.with = [{ ...gatedOn, comparableFacts: 'off' }]
    show(<ProjectGates />)
    expect((await screen.findByText(/requireComparableFacts is off/)).textContent).toContain('This project’s gates are on')
    expect(screen.getByRole('link', { name: 'Review turning requireComparableFacts on' }).getAttribute('href')).toBe('/packs/_upgrade')
    cleanup()
    offers.with = [{ state: 'unavailable', reason: 'JPACK_CONFIG names another project.', gated: false, requireComparableFacts: false, changes: [], locked: false }]
    show(<ProjectGates />)
    expect((await screen.findByText(/Desk offers no upgrade here/)).textContent).toContain('JPACK_CONFIG names another project.')
    expect(screen.queryByRole('link')).toBeNull()
  })
})

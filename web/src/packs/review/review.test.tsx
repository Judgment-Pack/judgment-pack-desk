import {Tooltip} from 'radix-ui'
import { formatJson } from '../../files/formatJson'
const displayJSON = (raw: string) => { const result = formatJson(raw); return result.ok ? result.content.trimEnd() : raw }
/**
 * Review and lock on the page: what it reads, what it shows, and that it
 * locks only on the owner's confirmation, sending back the token of exactly
 * the reading it showed.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../../files/client'
import { McpContext } from '../../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../../testing/harness'
import { PacksPane } from '../PacksPane'
import { confirmLock, readReview, StaleReview, type Review } from './client'
import { ReviewAndLockView } from './ReviewAndLockView'
import { ReviewProvider } from './ReviewContext'

vi.mock(import('../../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const token = 'f'.repeat(64)
const config = '{"configVersion":"5","packs":{"alpha":{"path":"packs/a.json"},"beta":{"path":"packs/b.json"}}}'
const before = '{"id":"alpha","title":"Before"}'
const after = '{"id":"alpha","title":"After"}'
const beta = '{"id":"beta","title":"Beta"}'
/** A side showing text, whose text the review carries in its contents. */
const text = (value: string) => ({ state: 'text' as const, digest: 'sha256:' + value })
const contents = Object.fromEntries([config, before, after, beta].map(value => ['sha256:' + value, value]))
const differing: Review = { status: 'invalid', locked: true, diagnostics: [], token, contents, findings: [
  { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json', detail: 'The pack document’s bytes differ.' },
  { name: 'document-drift', kind: 'pack', id: 'beta', path: 'packs/b.json' }
], files: [
  { kind: 'config', path: 'jpack.json', lock: 'same', now: text(config) },
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'other', earlier: text(before), now: text(after) },
  { kind: 'pack', id: 'beta', path: 'packs/b.json', lock: 'other', earlier: { state: 'no-copy' }, now: text(beta) }
] }
const first: Review = { status: 'error', locked: false, findings: [], token, contents, diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock.' }], files: [
  { kind: 'config', path: 'jpack.json', lock: 'none', now: text(config) },
  { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'none', now: text(after) },
  { kind: 'pack', id: 'beta', path: 'packs/b.json', lock: 'none', now: text(beta) }
] }

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let reviews: Review[] = []
let posted: unknown[] = []
let lockAnswer: () => Response
beforeEach(() => {
  posted = []
  lockAnswer = () => json(200, { files: 3, copies: 'stored' })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    if (url === '/api/review/lock') {
      posted.push({ method: init?.method, type: (init?.headers as Record<string, string>)['Content-Type'], body: JSON.parse(String(init?.body)) })
      return lockAnswer()
    }
    return json(200, reviews.length > 1 ? reviews.shift() : reviews[0])
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

function show() {
  render(<QueryClientProvider client={testQueryClient()}><Tooltip.Provider><MemoryRouter><ReviewProvider><ReviewAndLockView /></ReviewProvider></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
}
const file = (path: string) => document.querySelector(`[data-file="${path}"]`) as HTMLElement

describe('the review client', () => {
  it('refuses an answer that is not a review', async () => {
    vi.mocked(deskFetch).mockResolvedValueOnce(json(200, { status: 'valid' }))
    await expect(readReview()).rejects.toThrow('The review could not be loaded')
  })
  it('sends the review’s token as JSON, and tells a stale confirmation apart', async () => {
    lockAnswer = () => json(409, { code: 'stale', error: 'The project changed after you reviewed it.' })
    await expect(confirmLock(token)).rejects.toBeInstanceOf(StaleReview)
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { token } }])
    lockAnswer = () => json(500, { code: 'internal', error: 'The runtime did not lock the project.' })
    await expect(confirmLock(token)).rejects.toThrow('The runtime did not lock the project.')
  })
})

describe('the review page', () => {
  it('shows every file, each finding in plain words, and a diff only where the desk kept the locked bytes', async () => {
    reviews = [differing]
    show()
    expect(await screen.findAllByText('Changed since the last lock')).toHaveLength(2)
    expect(screen.getByText('Locking records that you confirmed these exact files as this project’s reviewed set. It is not a second person’s approval, and it records no name.')).toBeTruthy()
    expect(screen.getByText(/A change to jpack.json holds every pack/)).toBeTruthy()
    expect(within(file('jpack.json')).getByText('Matches the last lock')).toBeTruthy()
    expect(within(file('packs/a.json')).getByText('The pack document’s bytes differ.')).toBeTruthy()
    // Alpha's locked bytes are kept, so its changes are drawn; beta's are not.
    expect(within(file('packs/a.json')).getByRole('region', { name: 'Pack changes' })).toBeTruthy()
    expect(within(file('packs/b.json')).queryByRole('region', { name: 'Pack changes' })).toBeNull()
    expect(within(file('packs/b.json')).getByText('Desk has no earlier copy of the locked version to compare with.')).toBeTruthy()
    expect(file('packs/b.json').textContent).toContain(displayJSON(beta))
  })

  it('locks nothing until the owner confirms, then sends back the token of what it showed', async () => {
    reviews = [differing]
    show()
    const button = await screen.findByRole('button', { name: 'Confirm and lock 2 differences' })
    expect(screen.getByText('This lock covers 3 files: jpack.json and every pack and graph it declares.')).toBeTruthy()
    expect(posted).toEqual([])
    fireEvent.click(button)
    expect((await screen.findByRole('status')).textContent).toContain('Locked. These 3 files are now this project’s reviewed set.')
    expect(posted).toEqual([{ method: 'POST', type: 'application/json', body: { token } }])
  })

  it('shows the step again when the project changed after the review', async () => {
    reviews = [differing, { ...differing, findings: differing.findings.slice(0, 1) }]
    lockAnswer = () => json(409, { code: 'stale', error: 'stale' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm and lock 2 differences' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The project changed after you reviewed it, so nothing was locked. Review the changes again.')
    await screen.findByRole('button', { name: 'Confirm and lock 1 difference' })
  })

  it('reads a first lock as a full review: every file, open', async () => {
    reviews = [first]
    show()
    await screen.findByText('This project has no reviewed-set lock yet, so every file below is new to it. Read each one before you lock.')
    expect(screen.getByText('There is no reviewed-set lock.')).toBeTruthy()
    for (const [path, text] of [['jpack.json', config], ['packs/a.json', after], ['packs/b.json', beta]] as const) {
      expect(within(file(path)).getByText('Not in a lock yet')).toBeTruthy()
      expect(file(path).querySelector('details')?.open).toBe(true)
      expect(file(path).textContent).toContain(displayJSON(text))
    }
    expect(screen.getByRole('button', { name: 'Confirm and lock 3 files' })).toBeTruthy()
  })

  it('offers no lock when every file matches, when a file cannot be shown, or without a token', async () => {
    reviews = [{ ...first, status: 'valid', locked: true, diagnostics: [], files: first.files.map(item => ({ ...item, lock: 'same' as const })) }]
    show()
    await screen.findByText('Every file matches the reviewed set. There is nothing to lock.')
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
    cleanup()
    // The desk gives no token for a reading it cannot show; the page offers
    // no lock for one even if a token came.
    reviews = [{ ...first, blocked: 'packs/b.json is larger than 1048576 bytes, which is more than Desk shows', files: [first.files[0]!, first.files[1]!, { ...first.files[2]!, now: { state: 'not-shown' } }] }]
    show()
    expect((await screen.findByRole('alert')).textContent).toContain('Desk cannot lock this project here until it can show you every file a lock would cover.')
    expect(screen.getByText('The current file is too large to show here.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Confirm and lock/ })).toBeNull()
  })
})

describe('the collection', () => {
  it('shows each pack’s finding without putting project lock management in the collection', async () => {
    reviews = [differing]
    const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [{ id: 'alpha' }, { id: 'beta' }, { id: 'gamma' }] }) }) })
    render(<QueryClientProvider client={testQueryClient()}><MemoryRouter><McpContext.Provider value={connected({ client: stub.client })}><ReviewProvider><PacksPane /></ReviewProvider></McpContext.Provider></MemoryRouter></QueryClientProvider>)
    const alpha = await screen.findByRole('link', { name: /alpha/ })
    await waitFor(() => expect(alpha.textContent).toContain('Changed since the last lock'))
    expect(screen.getByRole('link', { name: /gamma/ }).textContent).not.toContain('Changed since the last lock')
    expect(screen.queryByRole('button', { name: 'Collection options' })).toBeNull()
    expect(screen.queryByRole('link', { name: 'Review and lock' })).toBeNull()
  })
})

const retainedReview={reference:{sha256:'a'.repeat(64),bytes:100},review:{version:1 as const,target:'urn:judgmentpack:project-review:pack:alpha',basePublication:'b'.repeat(64),storageRevision:'c'.repeat(64),candidate:{sha256:'d'.repeat(64),bytes:100},proposedVersion:'1.0.1',impact:{classification:'patch' as const,rationale:'Corrected a compatible conversion'},dependencies:[],preview:{}}}
const reviewedVersion={key:'pack:alpha',path:'packs/a.json',artifactId:'urn:alpha',version:'1.0.1',baseVersion:'1.0.0',required:true,review:retainedReview}
it('restores a retained compatibility judgment, but only publishes after explicit confirmation',async()=>{
 reviews=[{...differing,artifactReviews:[reviewedVersion]}]
 show()
 const button=await screen.findByRole('button',{name:'Confirm and lock 2 differences'}) as HTMLButtonElement
 expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe(retainedReview.review.impact.rationale)
 expect(button.disabled).toBe(false);expect(posted).toHaveLength(0)
 fireEvent.click(button)
 await waitFor(()=>expect(posted).toHaveLength(1))
 expect((posted[0] as any).body).toEqual({token,reviews:{'pack:alpha':retainedReview.reference}})
})
it('invalidates compatibility findings on edit and checkpoints the exact replacement judgment before lock',async()=>{
 reviews=[{...differing,artifactReviews:[reviewedVersion]}]
 const checks:any[]=[]
 const original=vi.mocked(deskFetch).getMockImplementation()!
 vi.mocked(deskFetch).mockImplementation(async(url,init)=>{
  if(url==='/api/review/artifacts'){
   const body=JSON.parse(String(init?.body));checks.push(body)
   return json(200,{items:[{...reviewedVersion,review:{...retainedReview,reference:{sha256:'e'.repeat(64),bytes:110},review:{...retainedReview.review,impact:body.impacts['pack:alpha']}}}]})
  }
  return original(url,init)
 })
 show()
 const button=await screen.findByRole('button',{name:'Confirm and lock 2 differences'}) as HTMLButtonElement
 fireEvent.change(screen.getByLabelText('Reason for this version'),{target:{value:'Reviewed with the downstream owner'}})
 expect(button.disabled).toBe(true);fireEvent.click(button);expect(posted).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Check versions'}))
 await waitFor(()=>expect(button.disabled).toBe(false))
 expect(checks).toEqual([{token,impacts:{'pack:alpha':{classification:'patch',rationale:'Reviewed with the downstream owner'}}}])
 expect(posted).toHaveLength(0)
 fireEvent.click(button)
 await waitFor(()=>expect((posted[0] as any).body.reviews['pack:alpha'].sha256).toBe('e'.repeat(64)))
})
it('blocks publication when compatibility remains unreviewed',async()=>{
 reviews=[{...differing,artifactReviews:[{...reviewedVersion,review:undefined}]}]
 show()
 const button=await screen.findByRole('button',{name:'Confirm and lock 2 differences'}) as HTMLButtonElement
 expect(button.disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Check versions'}) as HTMLButtonElement).disabled).toBe(true)
 expect(posted).toHaveLength(0)
})

it('protects unsaved compatibility edits when navigating away',async()=>{
 const {createMemoryRouter,RouterProvider}=await import('react-router-dom')
 const {UnsavedChangesProvider}=await import('../../shell/UnsavedChanges')
 reviews=[{...differing,artifactReviews:[reviewedVersion]}]
 const router=createMemoryRouter([{path:'/',element:<UnsavedChangesProvider><ReviewProvider><ReviewAndLockView/></ReviewProvider></UnsavedChangesProvider>},{path:'/library',element:<p>Library destination</p>}])
 render(<QueryClientProvider client={testQueryClient()}><Tooltip.Provider><RouterProvider router={router}/></Tooltip.Provider></QueryClientProvider>)
 fireEvent.change(await screen.findByLabelText('Reason for this version'),{target:{value:'An unfinished compatibility judgment'}})
 fireEvent.click(screen.getByRole('link',{name:'Back to packs'}))
 await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(router.state.location.pathname).toBe('/')
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}))
 expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe('An unfinished compatibility judgment')
})

it('offers explicit receipt recovery when every file already matches, without requesting another lock',async()=>{
 reviews=[{...first,status:'valid',locked:true,diagnostics:[],publicationPending:true,files:first.files.map(item=>({...item,lock:'same' as const}))}]
 lockAnswer=()=>json(200,{files:3,copies:'stored',reconciled:true})
 show()
 await screen.findByText('Publication saved; history confirmation is pending.')
 expect(posted).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Confirm publication history'}))
 await screen.findByText('Publication history confirmed.')
 expect(posted).toEqual([{method:'POST',type:'application/json',body:{token,reconcile:true}}])
})

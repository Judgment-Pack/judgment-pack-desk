import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { candidateHistory, DefinitionHistory } from './DefinitionHistory'
import type { Candidate } from '../research/run'

vi.mock('../chat/ChatProvider', () => ({useChats: () => ({packDrafts:[],error:''})}))
vi.mock('../files/queries', () => ({useFileListing: () => ({data:{root:'project'}})}))
afterEach(() => {cleanup(); vi.unstubAllGlobals()})
const raw='{"recordVersion":"1","pack":{"id":"urn:one","version":"0.1.0"},"at":"2026-10-10T12:00:00Z","disposition":{"kind":"outcome","outcomeId":"approved"},"inputs":{"facts":{"n":9007199254740993}}}'
const client = () => new QueryClient({defaultOptions:{queries:{retry:false}}})
it('loads records only for the exact definition and never evaluates on tab navigation', async () => {
  const fetcher=vi.fn(async(url:string) => new Response(JSON.stringify(url.startsWith('/api/definitions/history') ? {state:'records',records:[{raw,digest:'sha256:exact'}],unread:0} : url.startsWith('/api/briefs') ? {content:{subjects:{}}} : {content:{version:1,suites:{}}})))
  vi.stubGlobal('fetch',fetcher)
  render(<MemoryRouter><QueryClientProvider client={client()}><DefinitionHistory kind="pack" owner="one" documentId="urn:one"/></QueryClientProvider></MemoryRouter>)
  fireEvent.click(await screen.findByRole('button',{name:'Decision recorded'}))
  expect(await screen.findByRole('heading',{name:'Decision recorded'})).toBeTruthy()
  expect(screen.getByText(/Signatures, checkpoints and timestamps have not been checked/)).toBeTruthy()
  fireEvent.click(screen.getByText('Technical details'))
  fireEvent.click(screen.getByText('Inputs as evaluated'))
  expect(screen.getByRole('region',{name:'Recorded inputs'}).textContent).toContain('9007199254740993')
  expect(fetcher.mock.calls.some(([url])=>url==='/api/definitions/history?kind=pack&id=urn%3Aone')).toBe(true)
  expect(fetcher.mock.calls.every(([url])=>['/api/definitions/history?','/api/briefs?','/api/pack-tests'].some(prefix=>url.startsWith(prefix)))).toBe(true)
  fireEvent.click(screen.getByRole('button',{name:'Back to history'}))
  await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('searchbox',{name:'Search history'})))
})
it('retains revisions and checks without inventing a change timestamp or approval', () => {
  const candidate:Candidate={revision:2,document:{},text:'{}',digest:'a',producedBy:'conversation',previousCheck:{valid:true,documentDigest:'a',cases:[],diagnostics:[],record:{version:1,at:'2026-10-10T12:00:00Z',documentDigest:'a',runtime:'runtime',context:'context',inputs:[],statements:[],validation:{content:[]},digest:'b'}}}
  const entries=candidateHistory([candidate])
  expect(entries).toHaveLength(2)
  expect(entries[0]?.at).toBeUndefined()
  expect(entries[1]?.at).toBe('2026-10-10T12:00:00Z')
  expect(entries[1]?.kind).toBe('checks')
  expect(entries[1]?.note).toContain('do not approve the pack')
})
it('keeps unsaved graph history local and reports a session observation as non-durable', async () => {
  const fetcher=vi.fn()
  vi.stubGlobal('fetch',fetcher)
  render(<MemoryRouter><QueryClientProvider client={client()}><DefinitionHistory kind="graph" draft owner="" observation={{status:'passed'}}/></QueryClientProvider></MemoryRouter>)
  fireEvent.click(screen.getByRole('button',{name:'Graph tests observed'}))
  expect(screen.getByText(/not a durable decision record/)).toBeTruthy()
  expect(fetcher).not.toHaveBeenCalled()
})

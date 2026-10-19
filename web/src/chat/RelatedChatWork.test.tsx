import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { Tooltip } from 'radix-ui'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { relatedDialects, relatedGraphs, RelatedChatWork } from './RelatedChatWork'
import { RelatedWork, RelatedWorkEntry } from './RelatedWork'
import { dialectAPI } from '../dialects/client'
import { IconHistory } from '../shell/icons'
import type { Chat } from './store'
const fake = vi.hoisted(() => ({ chats: [] as Chat[], drafts: [] as Chat[], packDrafts: [] as any[] }))
vi.mock('./ChatProvider', () => ({ useChats: () => fake }))
vi.mock('../dialects/client', async original => ({ ...await original<typeof import('../dialects/client')>(), dialectAPI: vi.fn() }))
const id = 'prep_' + 'a'.repeat(32), second = 'prep_' + 'b'.repeat(32)
const draft = { draftId: 'draft-1', name: 'Companies', savedInputId: id, target: 'companies' }
const action = { id: 'action-1', inputId: id, name: 'Companies', kind: 'update-recipe', recipeVersion: 3, applied: { at: '2026-10-13' } }
let cache: QueryClient
beforeEach(() => {
  cache = new QueryClient({defaultOptions:{queries:{retry:false}}})
  fake.chats = [{ id: 'chat-1', inputDrafts: [draft], inputActions: [action] } as any]
  fake.drafts = []; fake.packDrafts = []
  vi.mocked(dialectAPI).mockReset().mockResolvedValue({ document: { id, name: 'Companies', recipeVersion: 4, records: [] } } as any)
})
afterEach(() => { cleanup(); cache.clear() })
function show(children: React.ReactNode) {
  return render(<QueryClientProvider client={cache}><Tooltip.Provider><MemoryRouter>{children}</MemoryRouter></Tooltip.Provider></QueryClientProvider>)
}
it('groups creation and updates by saved identity, keeping same-name inputs distinct', () => {
  const rows = relatedDialects({input:{id},inputDrafts:[draft,{...draft,draftId:'draft-2',savedInputId:second}] as any,inputActions:[action] as any})
  expect(rows.map(r=>r.id)).toEqual([id,second]); expect(rows[0].drafts).toHaveLength(1);expect(rows[0].actions).toHaveLength(1)
})
it('offers one Input destination preserving chat, with collapsed grouped history', async () => {
  show(<RelatedChatWork chatId="chat-1"/>)
  const links = await screen.findAllByRole('link',{name:'Open dialect'})
  expect(links).toHaveLength(1);expect(links[0].getAttribute('href')).toBe('/dialects/'+id+'?tab=recipe&chat=chat-1')
  expect(await screen.findByText('Recipe version 4',{exact:false})).toBeTruthy()
  expect(screen.getByText('History').closest('details')?.open).toBe(false)
  fireEvent.click(screen.getByText('History'));expect(screen.getAllByText('Recipe saved')).toHaveLength(2)
  expect(screen.getByText('Proposed recipe update')).toBeTruthy()
  expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method||method==='GET')).toBe(true)
})
it('marks current work open without adding another navigation link', async () => {
  show(<RelatedChatWork chatId="chat-1" current={{kind:'input',id,title:'Companies',open:true,text:'Current selected records'}}/>)
  expect(await screen.findByText('Open in workspace')).toBeTruthy();expect(screen.queryByRole('link',{name:'Open dialect'})).toBeNull()
})
it('uses the same presentation for Jobs without recipe-specific assumptions', () => {
  show(<RelatedWork><RelatedWorkEntry title="Weekly qualification" icon={<IconHistory/>} description="Job · Scheduled" href="/jobs/job-1" action="Open job"/></RelatedWork>)
  expect(screen.getByRole('region',{name:'Related work'})).toBeTruthy()
  expect(screen.getByRole('link',{name:'Open job'}).getAttribute('href')).toBe('/jobs/job-1')
})
it('includes Library packs and graph drafts in the shared view', () => {
  fake.chats=[{id:'chat-1',pack:{id:'qualification',path:'pack.json',digest:'a'},graphDrafts:[{draftId:'graph-1',id:'qualification-flow',saved:false}]} as any]
  show(<RelatedChatWork chatId="chat-1"/>)
  expect(screen.getByRole('link',{name:'Open pack'}).getAttribute('href')).toBe('/packs/qualification?chat=chat-1')
  expect(screen.getByRole('link',{name:'Review graph'}).getAttribute('href')).toBe('/graphs?chat=chat-1&draft=graph-1')
})

it('keeps an older unsaved creation proposal collapsed after a later one is saved', async () => {
  fake.chats=[{...fake.chats[0],inputDrafts:[{...draft,draftId:'old-unsaved',savedInputId:undefined},draft]} as any]
  const {container}=show(<RelatedChatWork chatId="chat-1"/>)
  await screen.findByRole('link',{name:'Open dialect'})
  expect(screen.getByRole('button',{name:'Review recipe'}).closest('details')?.open).toBe(false)
  expect(screen.queryByText('Needs review')).toBeNull()
  expect(screen.getByText('Earlier proposals').closest('details')?.open).toBe(false)
  fireEvent.click(screen.getByText('Earlier proposals'))
  expect(screen.getByRole('button',{name:'Review recipe'})).toBeTruthy()
  expect(screen.getByText('Earlier proposal · Not saved',{exact:false})).toBeTruthy()
  expect(container.querySelector('[data-artifact-compact]')).toBeNull()
})
it('shows a new recipe title once with its guarded review action', () => {
  fake.chats=[{id:'chat-1',inputDrafts:[{...draft,savedInputId:undefined}]} as any]
  const {container}=show(<RelatedChatWork chatId="chat-1" disabled/>)
  expect(screen.getAllByText('Companies',{exact:true})).toHaveLength(1)
  expect(screen.getByRole('button',{name:'Review recipe'})).toHaveProperty('disabled',true)
  expect(screen.getByText('New dialect',{exact:false})).toBeTruthy()
  expect(container.querySelector('[data-artifact-compact]')).toBeNull()
})
it('shows a pending update as a flat action under its saved dialect', async () => {
  fake.chats=[{...fake.chats[0],inputActions:[{...action,applied:undefined}]} as any]
  const {container}=show(<RelatedChatWork chatId="chat-1" disabled/>)
  await screen.findByRole('link',{name:'Open dialect'})
  expect(screen.getByRole('button',{name:'Review changes'})).toHaveProperty('disabled',true)
  expect(screen.getAllByText('Companies',{exact:true})).toHaveLength(1)
  expect(container.querySelector('[data-artifact-compact]')).toBeNull()
})
it('does not offer Open graph for an unsaved graph workspace', () => {
  const proposal={draftId:'draft-graph',id:'flow',path:'flow.graph.json',saved:false}
  const chat={id:'chat-1',graph:{id:'flow',path:proposal.path,workspace:proposal.draftId},graphDrafts:[proposal]} as any
  fake.chats=[chat]
  expect(relatedGraphs(chat)).toHaveLength(1)
  show(<RelatedChatWork chatId="chat-1"/>)
  expect(screen.getAllByText('flow',{exact:true})).toHaveLength(1)
  expect(screen.queryByRole('link',{name:'Open graph'})).toBeNull()
  expect(screen.getByRole('link',{name:'Review graph'}).getAttribute('href')).toContain('draft=draft-graph')
})
it('groups a saved graph and pending update with older revisions collapsed', () => {
  const graph={id:'flow',path:'flow.graph.json',workspace:'flow'}
  fake.chats=[{id:'chat-1',graph,graphDrafts:[{...graph,draftId:'first',saved:true},{...graph,draftId:'second',baseSha256:'a'.repeat(64)}]} as any]
  show(<RelatedChatWork chatId="chat-1"/>)
  expect(screen.getAllByText('flow',{exact:true})).toHaveLength(1)
  expect(screen.getByRole('link',{name:'Open graph'})).toBeTruthy()
  expect(screen.getByRole('link',{name:'Review graph'}).getAttribute('href')).toContain('draft=second')
  expect(screen.getByText('History').closest('details')?.open).toBe(false)
})
it('does not mislabel a pack with another item’s context title', () => {
  fake.chats=[{id:'chat-1',pack:{id:'saved-pack',path:'pack.json',digest:'a'}} as any]
  show(<RelatedChatWork chatId="chat-1" current={{kind:'pack',id:'another-pack',title:'Another pack',open:true}}/>)
  expect(screen.getByText('saved-pack')).toBeTruthy()
  expect(screen.queryByText('Another pack')).toBeNull()
  expect(screen.getByRole('link',{name:'Open pack'})).toBeTruthy()
})
it('shows an empty state for an Dialects list chat without related entities', () => {
  fake.chats=[{id:'chat-1',input:{id:'list'}} as any]
  show(<RelatedChatWork chatId="chat-1" current={{kind:'input',id:'list',title:'Dialects'}}/>)
  const reader=screen.getByRole('region',{name:'Related work'})
  expect(within(reader).queryByRole('link')).toBeNull()
  expect(within(reader).getByText('Work you create or review in this chat will appear here.')).toBeTruthy()
})

it('keeps the saved pack destination available while its original draft is open', () => {
  fake.chats=[{id:'chat-1',draftId:'pack-draft'} as any]
  fake.packDrafts=[{id:'pack-draft',title:'Qualified account',finalized:{id:'qualified-account',path:'pack.json',digest:'a'}}]
  show(<RelatedChatWork chatId="chat-1" current={{kind:'pack',id:'pack-draft',title:'Qualified account',open:true}}/>)
  expect(screen.getByRole('link',{name:'Open pack'}).getAttribute('href')).toBe('/packs/qualified-account?chat=chat-1')
})

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createRef, useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { ChatToolbar, ChatHistoryList } from './ChatHistory'
import type { Chat } from './store'

const fixture = vi.hoisted(() => ({chats: [] as Chat[]}))
vi.mock('./ChatProvider', () => ({ useChats: () => ({ store: { canCreate: true }, bindings: new Map(), chats: fixture.chats, ready: true }) }))
afterEach(cleanup)
const chat = { id: 'chat', composer: '' } as Chat
function toolbar(onSources?: (opener: HTMLButtonElement | null) => void) {
  return <ChatToolbar chat={chat} history={false} historyRef={createRef()} onHistory={() => {}} onBack={() => {}} onNew={() => {}} onSources={onSources}/>
}

it('hands source reading a durable opener after the menu closes, without reclaiming reader focus', async () => {
  const reader = document.createElement('section')
  reader.tabIndex = -1
  document.body.append(reader)
  try {
    const read = vi.fn(() => reader.focus())
    render(toolbar(read))
    const trigger = screen.getByRole('button', { name: 'Chat options' })
    fireEvent.keyDown(trigger, { key: 'Enter' })
    const item = await screen.findByRole('menuitem', { name: 'Chat sources' })
    expect(read).not.toHaveBeenCalled()
    fireEvent.click(item)
    await waitFor(() => expect(read).toHaveBeenCalledExactlyOnceWith(trigger))
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(reader)
    expect(trigger.isConnected).toBe(true)
  } finally { reader.remove() }
})

it('dismisses options without opening sources and returns focus to its trigger', async () => {
  const read = vi.fn()
  render(toolbar(read))
  const trigger = screen.getByRole('button', { name: 'Chat options' })
  fireEvent.keyDown(trigger, { key: 'Enter' })
  fireEvent.keyDown(await screen.findByRole('menuitem', { name: 'Chat sources' }), { key: 'Escape' })
  await waitFor(() => expect(document.activeElement).toBe(trigger))
  expect(read).not.toHaveBeenCalled()
})

it('omits an empty options menu when the conversation has no sources', () => {
  render(toolbar())
  expect(screen.queryByRole('button', { name: 'Chat options' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Chat history' })).toBeTruthy()
})

function Location() { const at = useLocation(); return <output aria-label="Location">{at.pathname + at.search}</output> }
it('shows only this graph’s history by default and stays on Tests when switching sessions', () => {
  const base = {composer: '', pinned: false, archived: false, updatedAt: '2026-10-10T12:00:00Z'}
  fixture.chats = [
    {...base, id: 'graph-a', title: 'Graph chat', graph: {id: 'flow', workspace: 'flow', path: ''}},
    {...base, id: 'other', title: 'Other graph', graph: {id: 'other', workspace: 'other', path: ''}},
    {...base, id: 'pack', title: 'Pack chat', pack: {id: 'flow', path: '', digest: ''}}
  ] as Chat[]
  render(<MemoryRouter initialEntries={['/graphs/flow?view=tests&chat=old']}><ChatHistoryList scope={{kind: 'graph', id: 'flow'}}/><Location/></MemoryRouter>)
  expect(screen.getByRole('button', {name: /^Graph chat/})).toBeTruthy()
  expect(screen.queryByRole('button', {name: /^Other graph/})).toBeNull()
  expect(screen.queryByRole('button', {name: /^Pack chat/})).toBeNull()
  fireEvent.click(screen.getByRole('button', {name: /^Graph chat/}))
  expect(screen.getByLabelText('Location').textContent).toBe('/graphs/flow?view=tests&chat=graph-a')
  fireEvent.click(screen.getByRole('checkbox', {name: 'All chats'}))
  expect(screen.getByRole('button', {name: /^Other graph/})).toBeTruthy()
  expect(screen.getByRole('button', {name: /^Pack chat/})).toBeTruthy()
})


it('offers New chat only inside history and runs it after history closes', async () => {
  const onNew = vi.fn()
  fixture.chats = []
  function Toolbar() {
    const [history, setHistory] = useState(false)
    return <ChatToolbar chat={{...chat, composer:'Existing question'}} history={history} historyRef={createRef()} onHistory={() => setHistory(true)} onBack={() => setHistory(false)} onNew={onNew}/>
  }
  render(<MemoryRouter><Toolbar/></MemoryRouter>)
  expect(screen.queryByRole('button', {name:'New chat'})).toBeNull()
  fireEvent.click(screen.getByRole('button', {name:'Chat history'}))
  fireEvent.click(await screen.findByRole('button', {name:'New chat'}))
  await waitFor(() => expect(onNew).toHaveBeenCalledOnce())
  expect(screen.queryByRole('dialog', {name:'Chat history'})).toBeNull()
})

it('isolates per-input history and preserves the current Dialects tab when navigating sessions',()=>{
 const id='prep_'+'a'.repeat(32),base={composer:'',pinned:false,archived:false,updatedAt:'2026-10-10T12:00:00Z'}
 fixture.chats=[{...base,id:'input-chat',title:'This input',input:{id}},{...base,id:'list-chat',title:'General Dialects',input:{id:'list'}},{...base,id:'other',title:'Another input',input:{id:'prep_'+'b'.repeat(32)}}] as Chat[]
 render(<MemoryRouter initialEntries={['/dialects/'+id+'?tab=recipe&chat=old']}><ChatHistoryList scope={{kind:'input',id}}/><Location/></MemoryRouter>)
 expect(screen.queryByRole('button',{name:/^General Dialects/})).toBeNull()
 expect(screen.queryByRole('button',{name:/^Another input/})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:/^This input/}))
 expect(screen.getByLabelText('Location').textContent).toBe('/dialects/'+id+'?tab=recipe&chat=input-chat')
})

it('finds linked main chats in Dialects history and keeps each recipe tab when switching',()=>{
 const id='prep_'+'a'.repeat(32),other='prep_'+'b'.repeat(32),base={composer:'',pinned:false,archived:false,updatedAt:'2026-10-10T12:00:00Z'}
 fixture.chats=[{...base,id:'source',title:'Original interview',inputDrafts:[{savedInputId:id},{savedInputId:other}]},{...base,id:'recovered',title:'Saved origin only'},{...base,id:'unrelated',title:'Unrelated chat'}] as Chat[]
 render(<MemoryRouter initialEntries={['/dialects/'+id+'?tab=recipe']}><ChatHistoryList scope={{kind:'input',id,originChatId:'recovered'}}/><Location/></MemoryRouter>)
 expect(screen.queryByRole('button',{name:/^Unrelated chat/})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:/^Original interview/}))
 expect(screen.getByLabelText('Location').textContent).toBe('/dialects/'+id+'?tab=recipe&chat=source')
 fireEvent.click(screen.getByRole('button',{name:/^Saved origin only/}))
 expect(screen.getByLabelText('Location').textContent).toBe('/dialects/'+id+'?tab=recipe&chat=recovered')
})
it('lists Dialects-related conversations in the collection history without duplicating shared conversations',()=>{
 const id='prep_'+'a'.repeat(32),base={composer:'',pinned:false,archived:false,updatedAt:'2026-10-10T12:00:00Z'}
 fixture.chats=[{...base,id:'source',title:'Original interview',inputDrafts:[{savedInputId:id},{savedInputId:id}]},{...base,id:'scoped',title:'Recipe conversation',input:{id}},{...base,id:'unrelated',title:'Unrelated chat'}] as Chat[]
 render(<MemoryRouter initialEntries={['/dialects']}><ChatHistoryList scope={{kind:'input',id:'list'}}/><Location/></MemoryRouter>)
 expect(screen.getAllByRole('button',{name:/^Original interview/})).toHaveLength(1)
 expect(screen.getByRole('button',{name:/^Recipe conversation/})).toBeTruthy()
 expect(screen.queryByRole('button',{name:/^Unrelated chat/})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:/^Original interview/}))
 expect(screen.getByLabelText('Location').textContent).toBe('/dialects?chat=source')
})

for (const compact of [false, true]) it(`animates only working history entries (${compact ? 'popover' : 'page'})`, () => {
 const base = {composer:'A question',pinned:false,archived:false,updatedAt:'2026-10-13T12:00:00Z'}
 fixture.chats = [{...base,id:'working',title:'Active conversation',checkpoint:{state:{status:'running',turns:[],candidates:[]}}}, {...base,id:'done',title:'Finished conversation',checkpoint:{state:{status:'complete',turns:[],candidates:[]}}}] as unknown as Chat[]
 const {rerender} = render(<MemoryRouter><ChatHistoryList compact={compact}/></MemoryRouter>)
 const active = screen.getByRole('button',{name:/^Active conversation/})
 expect(active.querySelector('[data-running]')?.textContent).toBe('Working')
 expect(active.querySelector('[data-spinner]')).toBeTruthy()
 expect(screen.getByRole('button',{name:/^Finished conversation/}).querySelector('[data-spinner]')).toBeNull()
 fixture.chats = fixture.chats.map(chat => chat.id === 'working' ? {...chat,checkpoint:{...chat.checkpoint!,state:{...chat.checkpoint!.state,status:'stopped'}}} : chat)
 rerender(<MemoryRouter><ChatHistoryList compact={compact}/></MemoryRouter>)
 expect(screen.getByRole('button',{name:/^Active conversation/}).textContent).toContain('Interrupted')
 expect(screen.queryByText('Working')).toBeNull()
 expect(document.querySelector('[data-spinner]')).toBeNull()
})

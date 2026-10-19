import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { BufferIdentity } from '../packs/edit/useDocumentBuffer'
import { PackAssistant } from './PackAssistant'

const fake = vi.hoisted(() => ({ snapshot: {} as any, editing: {} as any, open: false }))
vi.mock('./usePackProposalSave',()=>({usePackProposalSave:()=>({save:vi.fn(),saving:false,error:'',reason:''})}))
vi.mock('../files/queries', () => ({useFileContent:()=>({data:undefined})}))
vi.mock('../packs/test-workspace/store', () => ({ useTestStorage: () => ({suite: {cases: [], runs: []}, query: {isSuccess: true}}) }))
vi.mock('./ChatProvider', () => ({ useChats: () => fake.snapshot }))
vi.mock('../shell/InspectorSlot', () => ({ useInspectorControls: () => ({ open: fake.open }) }))
vi.mock('../packs/edit/editingContext', () => ({ useEditing: () => fake.editing }))
vi.mock('./ChatPanel', () => ({ ChatPanel: ({ context, proposalActions }: any) => <><button onClick={context?.beforeSend}>Send request</button>{proposalActions}</> }))
afterEach(cleanup)
const original = '{"id":"original"}'
const base = { path: 'packs/p.json', generation: 1, revision: 1 } as BufferIdentity
const chat = { id: 'chat-one', pack: { id: 'p' }, updatedAt: '2026-01-01' }
const store = { activate: vi.fn() }
function snapshot(revision: number, status = 'ready') {
  return { state: { status, restored: false, candidates: [{ revision, document: { id: 'proposed' }, digest: `digest-${revision}` }] } }
}
function view(identity = base, draft = original) {
  return <MemoryRouter><PackAssistant packId="p" path={base.path} editing draft={draft} identity={identity} busy={() => ''} diagnostics={undefined} /></MemoryRouter>
}
beforeEach(() => {
  fake.open = false
  fake.snapshot = { store, ready: true, chats: [chat], drafts: [], bindings: new Map([['chat-one', snapshot(1)]]) }
  fake.editing = { editing: true, pending: new Set(), write: vi.fn() }
})
it('does not offer an older candidate as the answer to a new clarification', () => {
  const rendered = render(view())
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  fake.snapshot.bindings = new Map([['chat-one', snapshot(1, 'needs-input')]])
  rendered.rerender(view())
  expect(screen.queryByText('Review proposed changes')).toBeNull()
})
it('offers only a new candidate and refuses it when the buffer revision changes', () => {
  const rendered = render(view())
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  fake.snapshot.bindings = new Map([['chat-one', snapshot(2)]])
  rendered.rerender(view())
  fireEvent.click(screen.getByText('Review proposed changes'))
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(false)
  // Bytes can return to their original value after an intervening edit; the
  // editor's revision is still a new baseline and must reject the old answer.
  rendered.rerender(view({ ...base, revision: 3 }))
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(true)
  expect(fake.editing.write).not.toHaveBeenCalled()
})
it('carries a view-mode proposal into Edit only when path and bytes still match', () => {
  fake.editing.editing = false
  const props = { packId: 'p', path: 'packs/p.json', draft: original, busy: () => '', diagnostics: undefined }
  const rendered = render(<MemoryRouter><PackAssistant {...props} editing={false} /></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  fake.snapshot.bindings = new Map([['chat-one', snapshot(2)]])
  rendered.rerender(<MemoryRouter><PackAssistant {...props} editing={false} /></MemoryRouter>)
  fireEvent.click(screen.getByText('Review proposed changes'))
  expect(screen.queryByRole('button', { name: 'Apply to draft' })).toBeNull()
  fake.editing.editing = true
  rendered.rerender(<MemoryRouter><PackAssistant {...props} editing identity={{ ...base, generation: 4 }} /></MemoryRouter>)
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(false)
  // A clarification keeps this unaccepted proposal available.
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(false)
  rendered.rerender(<MemoryRouter><PackAssistant {...props} editing identity={{ ...base, generation: 4, revision: 2 }} /></MemoryRouter>)
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(true)
})

it('opening Assistant preserves the builder tab and selected item', async () => {
 fake.open=true;fake.snapshot.chats=[]
 fake.snapshot.store={canCreate:true,activate:vi.fn(),startChat:vi.fn(()=>{fake.snapshot.chats=[chat];return chat})}
 function Address(){const location=useLocation();return <output aria-label="Address">{location.pathname+location.search}</output>}
 render(<MemoryRouter initialEntries={['/packs/p?edit=1&builder=build&editItem=%2Frules%2F0']}><Address/><PackAssistant packId="p" path={base.path} editing draft={original} identity={base} busy={()=>''} diagnostics={undefined}/></MemoryRouter>)
 await waitFor(()=>expect(screen.getByLabelText('Address').textContent).toBe('/packs/p?edit=1&builder=build&editItem=%2Frules%2F0&chat=chat-one'))
})

it('restores a pending saved-pack proposal after remount and hides it after Save', async()=>{
 const bytes='{"id":"p","version":"0.1.0"}',proposal={id:'p',version:'0.1.1'},updated=JSON.stringify(proposal)
 const linked={...chat,pack:{id:'p',path:base.path},createdCandidateDigest:'created'}
 const state={status:'complete',restored:true,turns:[],candidates:[{revision:3,document:proposal,text:updated,digest:'new',packReview:{basis:{packId:'p',path:base.path,text:bytes}}}]}
 fake.snapshot.chats=[linked];fake.snapshot.bindings=new Map([['chat-one',{state}]])
 const props={packId:'p',path:base.path,draft:bytes,busy:()=>'',diagnostics:undefined,onEdit:vi.fn()}
 const first=render(<MemoryRouter><PackAssistant {...props} editing={false}/></MemoryRouter>)
 await waitFor(()=>expect(screen.getByRole('button',{name:'Save proposal'})).toBeTruthy())
 expect(screen.getByText('Saved version 0.1.0 · Changes not saved')).toBeTruthy()
 first.unmount()
 const second=render(<MemoryRouter><PackAssistant {...props} editing identity={base}/></MemoryRouter>)
 await waitFor(()=>expect(screen.getByText('Review proposed changes')).toBeTruthy())
 fireEvent.click(screen.getByText('Review proposed changes'))
 expect(screen.getByRole('button',{name:'Apply to draft'}).hasAttribute('disabled')).toBe(false)
 second.rerender(<MemoryRouter><PackAssistant {...props} editing={false} draft={updated}/></MemoryRouter>)
 expect(screen.queryByLabelText('Proposed pack update')).toBeNull()
})
it('a recovered proposal cannot overwrite a changed saved pack',async()=>{
 fake.snapshot.chats=[{...chat,pack:{id:'p',path:base.path},createdCandidateDigest:'created'}]
 fake.snapshot.bindings=new Map([['chat-one',{state:{status:'complete',restored:true,turns:[],candidates:[{revision:3,document:{id:'proposed'},text:'{"id":"proposed"}',digest:'new',packReview:{basis:{packId:'p',path:base.path,text:original}}}]}}]])
 render(<MemoryRouter><PackAssistant packId="p" path={base.path} editing draft='{"id":"changed"}' identity={base} busy={()=>''} diagnostics={undefined}/></MemoryRouter>)
 await waitFor(()=>expect(screen.getByText('Review proposed changes')).toBeTruthy())
 fireEvent.click(screen.getByText('Review proposed changes'))
 expect(screen.getByRole('button',{name:'Apply to draft'}).hasAttribute('disabled')).toBe(true)
})

import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {createMemoryRouter,RouterProvider} from 'react-router-dom'
import {useSyncExternalStore} from 'react'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {ChatStore,type Chat,type ChatPersistence} from '../chat/store'
import {memoryDraftPersistence} from '../testing/draftPersistence'
import {INITIAL_STATE} from '../research/run'
import {PolicyInterpretationAction,PolicyInterpretationProposals} from './PolicyInterpretation'
import type {PolicyRecoveryInfo} from './client'
import type {PolicyAuthoringSource} from './authoringSource'
const fixture=vi.hoisted(()=>({store:null as ChatStore|null}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:fixture.store,...useSyncExternalStore(fixture.store!.subscribe,fixture.store!.getSnapshot)})}))
const policyId='policy_'+'a'.repeat(48),revisionId='revision_'+'b'.repeat(48),publicationId='publication_'+'c'.repeat(48)
const source:PolicyAuthoringSource={version:1,policyId,revisionId,publicationId,candidateDigest:'d'.repeat(64),content:{sha256:'d'.repeat(64),bytes:8},title:'Old publication',artifactVersion:'1.0.0',narrative:'Old text'}
let store:ChatStore
async function setup(chats:Chat[]=[],view=<PolicyInterpretationAction selection={{policyId,publicationId}}/>,recovery?:PolicyRecoveryInfo){
 const io:ChatPersistence={read:async()=>({project:'/test',sha256:'digest',content:{version:1,chats}}),write:vi.fn(async content=>({project:'/test',sha256:'digest',content}))}
 store=new ChatStore('/test',io,memoryDraftPersistence('/test'));fixture.store=store;await store.load()
 const fetch=vi.fn(async(input:RequestInfo|URL)=>Response.json(String(input).endsWith('/source-status')?{recovery}:source));vi.stubGlobal('fetch',fetch)
 const router=createMemoryRouter([{path:'/policies',element:<UnsavedChangesProvider>{view}</UnsavedChangesProvider>},{path:'/chats/:id',element:<p>Ordinary authoring destination</p>}],{initialEntries:['/policies']})
 render(<RouterProvider router={router}/>);return {router,fetch,io}
}
afterEach(()=>{cleanup();store?.dispose();vi.unstubAllGlobals();sessionStorage.clear()})
it('reviews the exact selected publication then opens an unsent ordinary composer without any model request',async()=>{
 const {fetch,router}=await setup()
 fireEvent.click(screen.getByRole('button',{name:'Design pack'}))
 const dialog=await screen.findByRole('dialog',{name:'Design pack from Policy'})
 expect(within(dialog).getByText('Old publication')).toBeTruthy();expect(within(dialog).getByText(publicationId)).toBeTruthy()
 expect(store.getSnapshot().chats).toEqual([])
 fireEvent.click(within(dialog).getByRole('button',{name:'Open authoring chat'}))
 await screen.findByText('Ordinary authoring destination')
 const chat=store.getSnapshot().chats[0]!
 expect(router.state.location.pathname).toBe('/chats/'+chat.id);expect(chat.composer).toContain('Old publication');expect(chat.policySources).toEqual([source]);expect(chat.checkpoint).toBeUndefined()
 expect(fetch).toHaveBeenCalledTimes(3);const exactReads=fetch.mock.calls.filter(([url])=>String(url).includes('/authoring-source?'));expect(exactReads).toHaveLength(2);expect(exactReads.every(([url])=>String(url).includes('publicationId='+publicationId))).toBe(true);expect(fetch.mock.calls.some(([url])=>String(url).endsWith('/source-status'))).toBe(true)
})
it.each(['pack','graph'] as const)('Plan blocks %s handoffs and Work opens the retained proposal through the same source review',async artifactKind=>{
 const chat:Chat={id:'policy-chat',title:'Policy',pinned:false,archived:false,updatedAt:'2026-10-18T00:00:00Z',composer:'',model:'',mode:'draft',view:'chat',policy:{id:policyId},interaction:'plan',policyInterpretations:[{version:1,artifactKind,id:'proposal',policyId,revisionId,publicationId,createdAt:'2026-10-18T00:00:00Z'}],checkpoint:{sources:[],state:{...INITIAL_STATE,status:'complete',phase:'idle'}}}
 const {fetch}=await setup([chat],<PolicyInterpretationProposals chatId={chat.id}/>)
 expect((screen.getByRole('button',{name:artifactKind==='graph'?'Design graph':'Design pack'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:artifactKind==='graph'?'Design graph':'Design pack'}));expect(fetch).not.toHaveBeenCalled()
 act(()=>store.update(chat.id,{interaction:'work'}))
 fireEvent.click(screen.getByRole('button',{name:artifactKind==='graph'?'Design graph':'Design pack'}))
 const dialog=await screen.findByRole('dialog',{name:artifactKind==='graph'?'Design graph from Policy':'Design pack from Policy'})
 fireEvent.click(within(dialog).getByRole('button',{name:'Open authoring chat'}))
 await waitFor(()=>expect(store.getSnapshot().chats).toHaveLength(2))
 const parent=store.getSnapshot().chats.find(c=>c.id===chat.id)!,opened=store.getSnapshot().chats.find(c=>c.id!==chat.id)!
 expect(parent.policyInterpretations?.[0]?.openedChatId).toBe(opened.id);expect(opened.policySources).toEqual([source]);expect(opened.checkpoint).toBeUndefined()
})

it('reviews a selected publication for graph authoring and keeps the composer unsent',async()=>{
 const {fetch,router}=await setup([],<PolicyInterpretationAction artifactKind="graph" selection={{policyId,publicationId}}/>)
 fireEvent.click(screen.getByRole('button',{name:'Design graph'}))
 const dialog=await screen.findByRole('dialog',{name:'Design graph from Policy'})
 expect(within(dialog).getByText(publicationId)).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('button',{name:'Open authoring chat'}))
 await screen.findByText('Ordinary authoring destination')
 const chat=store.getSnapshot().chats[0]!
 expect(router.state.location.pathname).toBe('/chats/'+chat.id);expect(chat.policySources).toEqual([source]);expect(chat.policyInterpretationKind).toBe('graph')
 expect(chat.composer).toContain('Design a graph interpretation');expect(chat.checkpoint).toBeUndefined()
 expect(fetch).toHaveBeenCalledTimes(3);expect(fetch.mock.calls.filter(([url])=>String(url).includes('/authoring-source?'))).toHaveLength(2)
})

it.each(['pack','graph'] as const)('marks restored %s source review as imported history without opening or sending a conversation',async artifactKind=>{
 const recovery:PolicyRecoveryInfo={snapshotId:'restored-one',restoredAt:'2026-10-19T12:00:00Z',currentnessUnknown:true,freshReviewRequired:true,sourceConsentRequired:true}
 const app=await setup([],<PolicyInterpretationAction artifactKind={artifactKind} selection={{policyId,publicationId}}/>,recovery)
 fireEvent.click(screen.getByRole('button',{name:artifactKind==='graph'?'Design graph':'Design pack'}));const dialog=await screen.findByRole('dialog')
 expect(within(dialog).getByText('Restored Policy history · Currentness unknown')).toBeTruthy();expect(within(dialog).getByText(/historical records, not authenticated current approval/)).toBeTruthy()
 expect(within(dialog).getByText(publicationId)).toBeTruthy();expect(store.getSnapshot().chats).toEqual([]);expect(app.fetch.mock.calls.every(([url])=>String(url).includes('/authoring-source?')||String(url).endsWith('/source-status'))).toBe(true)
})

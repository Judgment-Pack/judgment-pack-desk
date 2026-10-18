import sharedSource from '../../../internal/desk/testdata/policy-authoring-source.json'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatStore,type ChatPersistence,type Chat} from '../chat/store'
import {memoryDraftPersistence} from '../testing/draftPersistence'
import {INITIAL_STATE} from '../research/run'
import {policyChatTools} from './chatTools'
import {planningSession} from '../assistant/planning/policy'
import {preparePolicyInterpretation,openPolicyInterpretation,validPolicyInterpretationProposal,type PolicyInterpretationProposal} from './interpretation'
import {policySourceDocuments,validPolicySources,type PolicyAuthoringSource} from './authoringSource'
import {sourceHandoff} from '../packs/drafts/sourceHandoff'
import {usePublishPolicyEditing} from './chatEditing'
import {renderHook,cleanup} from '@testing-library/react'

const policyId='policy_'+'a'.repeat(48),revisionId='revision_'+'b'.repeat(48),publicationId='publication_'+'c'.repeat(48)
const candidateBytes=new TextEncoder().encode(JSON.stringify({schemaVersion:1,title:'Original Policy',narrative:'Exact old policy narrative.',artifactVersion:'1.0.0'})).length
const source:PolicyAuthoringSource={version:1,policyId,revisionId,publicationId,candidateDigest:'d'.repeat(64),content:{sha256:'d'.repeat(64),bytes:candidateBytes},title:'Original Policy',artifactVersion:'1.0.0',narrative:'Exact old policy narrative.'}
const stores:ChatStore[]=[]
afterEach(()=>{cleanup();stores.forEach(s=>s.dispose());stores.length=0;sessionStorage.clear();vi.unstubAllGlobals()})
function io(initial:Chat[]=[]):ChatPersistence{let content={version:1 as const,chats:initial};return {read:async()=>({project:'/test',sha256:'digest',content:structuredClone(content)}),write:vi.fn(async value=>{content=structuredClone(value);return {project:'/test',sha256:'digest',content:structuredClone(content)}})}}
async function setup(history=io(),drafts=memoryDraftPersistence('/test')){const store=new ChatStore('/test',history,drafts);stores.push(store);await store.load();return {store,history,drafts}}
function endpoint(){const fetch=vi.fn(async(input:RequestInfo|URL)=>{const url=String(input);if(url.includes('/authoring-source?publicationId='+publicationId))return Response.json(source);if(url.includes('/authoring-source?revisionId='+revisionId)){const {publicationId:_pub,...saved}=source;return Response.json(saved)}throw Error('Unexpected endpoint '+url)});vi.stubGlobal('fetch',fetch);return fetch}
it('prepares the selected old publication without reading the mutable head and opens an ordinary unsent chat',async()=>{
 const fetch=endpoint(),{store,history}=await setup(),unrelated=store.startChat();store.update(unrelated.id,{composer:'Keep my unrelated question'})
 const selected=await preparePolicyInterpretation({policyId,publicationId}),chat=openPolicyInterpretation(store,selected)
 expect(selected.content.bytes).toBeGreaterThan(new TextEncoder().encode(selected.narrative).length)
 expect(fetch).toHaveBeenCalledTimes(1);expect(String(fetch.mock.calls[0]![0])).toContain('publicationId='+publicationId)
 expect(chat).toMatchObject({mode:'draft',policySources:[source]});expect(chat.policy).toBeUndefined();expect(chat.pack).toBeUndefined();expect(chat.checkpoint).toBeUndefined();expect(chat.composer).toContain('Original Policy')
 expect(store.getSnapshot().drafts.find(c=>c.id===unrelated.id)?.composer).toBe('Keep my unrelated question')
 expect(store.getSnapshot().packDrafts).toEqual([])
 await store.checkpoint(new AbortController().signal)
 const reloaded=(await setup(history)).store.getSnapshot().chats.find(c=>c.id===chat.id)!
 expect(reloaded.policySources).toEqual([source]);expect(reloaded.composer).toBe(chat.composer)
})
it('uses identical preparation for a reviewed Policy-chat handoff and repeated acceptance preserves the opened composer',async()=>{
 endpoint();const {store}=await setup(),proposals:PolicyInterpretationProposal[]=[]
 const tools=policyChatTools({id:policyId,request:()=>undefined,afterTurnId:()=> 'user-turn',retain:async()=>{},retainInterpretation:async p=>{proposals.push(p)}})
 const tool=tools.find(t=>t.name==='propose_policy_pack_interpretation')!
 expect(tool.effect).toBe('proposal')
 const session=planningSession({context:{phase:'plan'},prompt:'Plan',tools:[],hostTools:tools,callTool:vi.fn()} as never)
 expect((session as {hostTools:{name:string}[]}).hostTools.map(t=>t.name)).toEqual(['read_policy'])
 await tool.execute({publicationId},new AbortController().signal)
 expect(proposals[0]).toMatchObject({policyId,revisionId,publicationId,afterTurnId:'user-turn'})
 expect(store.getSnapshot().chats).toEqual([])
 const selected=await preparePolicyInterpretation({policyId,publicationId:proposals[0]!.publicationId!}),manual=await preparePolicyInterpretation({policyId,publicationId})
 expect(selected).toEqual(manual)
 const opened=openPolicyInterpretation(store,selected);store.update(opened.id,{composer:'My reviewed request'})
 expect(openPolicyInterpretation(store,selected,opened.id).composer).toBe('My reviewed request');expect(store.getSnapshot().chats).toHaveLength(1)
 store.remove(opened.id);expect(()=>openPolicyInterpretation(store,selected,opened.id)).toThrow('deleted')
})
it('refuses substituted source identity and blocks dirty, pending or conflicting Policy editors before source preparation',async()=>{
 const fetch=endpoint(),guard=renderHook(()=>usePublishPolicyEditing(policyId,true))
 await expect(preparePolicyInterpretation({policyId,publicationId})).rejects.toThrow('Save or discard');expect(fetch).not.toHaveBeenCalled()
 guard.unmount();vi.stubGlobal('fetch',vi.fn(async()=>Response.json({...source,publicationId:'publication_'+'e'.repeat(48)})))
 await expect(preparePolicyInterpretation({policyId,publicationId})).rejects.toThrow()
 expect(validPolicySources([source,source])).toBe(false);expect(validPolicySources([{...source,narrative:'x'.repeat(131073)}])).toBe(false)
})
it('retains exact private source through draft migration, chat deletion, reload, reopen and finalization without project handoff text',async()=>{
 const original:Chat={id:'original-chat',title:'Interpretation',pinned:false,archived:false,updatedAt:'2026-10-18T00:00:00Z',composer:'',model:'',mode:'draft',view:'draft',policySources:[source],checkpoint:{sources:[],state:{...INITIAL_STATE,status:'ready',phase:'review',turns:[{id:'user',role:'user',kind:'message',text:'PRIVATE TRANSCRIPT',at:'2026-10-18T00:00:00Z'}],candidates:[{text:'{"title":"Draft"}',document:{title:'Draft'},digest:'candidate',revision:1,producedBy:'conversation'}]}}}
 const {store,history,drafts}=await setup(io([original]));const draft=store.getSnapshot().packDrafts[0]!
 expect(draft.policySources).toEqual([source]);expect(draft.sourceFiles).toEqual([])
 expect(JSON.stringify(sourceHandoff(draft))).not.toContain(source.narrative);expect(JSON.stringify(sourceHandoff(draft))).not.toContain('policySources')
 store.remove(original.id);await store.flush()
 const reloaded=(await setup(history,drafts)).store,next=reloaded.startChat(undefined,'draft',true,draft.id)
 expect(next.policySources).toEqual([source]);expect(next.checkpoint?.state.turns).toEqual([])
 reloaded.finalizeDraft(draft.id,{id:'saved-pack',path:'packs/saved.pack.json',digest:'saved'});await reloaded.flush()
 expect((await setup(history,drafts)).store.getSnapshot().packDrafts[0]).toMatchObject({policySources:[source],finalized:{id:'saved-pack'}})
 const documents=policySourceDocuments([source]);expect(documents[0]?.value).toEqual(source)
 expect(policySourceDocuments()).toEqual([])
})

it('accepts the server-authored shared candidate envelope with independent narrative and candidate byte identities',async()=>{
 expect(sharedSource.content.bytes).toBe(121)
 expect(new TextEncoder().encode(sharedSource.narrative).length).toBe(39)
 expect(validPolicySources([sharedSource])).toBe(true)
 vi.stubGlobal('fetch',vi.fn(async()=>Response.json(sharedSource)))
 expect(await preparePolicyInterpretation({policyId:sharedSource.policyId,revisionId:sharedSource.revisionId})).toEqual(sharedSource)
})

it('round-trips reviewed handoff selection and opened-chat identity without adopting pack scope in Policy chat',async()=>{
 const parent:Chat={id:'policy-chat',title:'Policy',pinned:false,archived:false,updatedAt:'2026-10-18T00:00:00Z',composer:'',model:'',mode:'draft',view:'chat',policy:{id:policyId},policyInterpretations:[{version:1,id:'handoff',policyId,revisionId,publicationId,createdAt:'2026-10-18T00:00:00Z'}]}
 const {store,history}=await setup(io([parent])),opened=openPolicyInterpretation(store,source)
 store.update(parent.id,{policyInterpretations:parent.policyInterpretations!.map(p=>({...p,openedChatId:opened.id}))})
 await store.checkpoint(new AbortController().signal)
 const reloaded=(await setup(history)).store,held=reloaded.getSnapshot().chats.find(c=>c.id===parent.id)!
 expect(held.policyInterpretations).toMatchObject([{policyId,revisionId,publicationId,openedChatId:opened.id}])
 expect(held.policy).toEqual({id:policyId});expect(held.pack).toBeUndefined();expect(held.draftId).toBeUndefined()
 reloaded.update(opened.id,{composer:'Preserved after reload'})
 expect(openPolicyInterpretation(reloaded,source,held.policyInterpretations![0]!.openedChatId).composer).toBe('Preserved after reload')
 expect(reloaded.getSnapshot().chats).toHaveLength(2)
})

it('matches the persisted handoff schema for timestamp grammar, UTF-8 limits, exact keys and optional null refusals',()=>{
 const proposal:PolicyInterpretationProposal={version:1,id:'handoff-1',policyId,revisionId,createdAt:'2026-10-18T00:00:00Z'}
 for(const createdAt of ['2026-10-18T00:00:00Z','2026-10-18T00:00:00.123456789Z','2024-02-29T23:59:59.1+05:30','0000-02-29T00:00:00-23:59'])expect(validPolicyInterpretationProposal({...proposal,createdAt})).toBe(true)
 for(const createdAt of ['2026-10-18','October 18, 2026','2026-10-18 00:00:00Z','2026-10-18T0:00:00Z','2026-10-18T00:00:00.1234567890Z','2026-10-18T00:00:00,1Z','2026-02-29T00:00:00Z','2026-04-31T00:00:00Z','2026-10-18T24:00:00Z','2026-10-18T00:60:00Z','2026-10-18T00:00:60Z','2026-10-18T00:00:00+24:00','2026-10-18T00:00:00+00:60'])expect(validPolicyInterpretationProposal({...proposal,createdAt})).toBe(false)
 expect(validPolicyInterpretationProposal({...proposal,id:'x'.repeat(80),openedChatId:'X'.repeat(80),afterTurnId:'é'.repeat(128)})).toBe(true)
 expect(validPolicyInterpretationProposal({...proposal,afterTurnId:''})).toBe(true)
 for(const patch of [{artifactKind:'job'},{artifactKind:null},{ArtifactKind:'graph'},{id:'x'.repeat(81)},{id:'has_underscore'},{openedChatId:'x'.repeat(81)},{openedChatId:''},{openedChatId:'has_underscore'},{afterTurnId:'é'.repeat(129)},{afterTurnId:3},{publicationId:null},{afterTurnId:null},{openedChatId:null},{PublicationId:publicationId},{AfterTurnId:'turn'},{OpenedChatId:'chat'}])expect(validPolicyInterpretationProposal({...proposal,...patch})).toBe(false)
})

it('keeps graph handoffs distinct from historical pack handoffs and never dispatches on acceptance',async()=>{
 endpoint();const {store}=await setup(),proposals:PolicyInterpretationProposal[]=[]
 const tools=policyChatTools({id:policyId,request:()=>undefined,afterTurnId:()=> 'user-turn',retain:async()=>{},retainInterpretation:async p=>{proposals.push(p)}})
 const graph=tools.find(t=>t.name==='propose_policy_graph_interpretation')!
 const plan=planningSession({context:{phase:'plan'},prompt:'Plan',tools:[],hostTools:tools,callTool:vi.fn()} as never)
 expect((plan as {hostTools:{name:string}[]}).hostTools.map(t=>t.name)).toEqual(['read_policy'])
 await graph.execute({publicationId},new AbortController().signal)
 expect(proposals[0]).toMatchObject({artifactKind:'graph',publicationId,revisionId})
 expect(store.getSnapshot().chats).toEqual([])
 const chat=openPolicyInterpretation(store,source,undefined,'graph')
 expect(chat.policyInterpretationKind).toBe('graph');expect(chat.composer).toContain('Design a graph interpretation')
 expect(chat.checkpoint).toBeUndefined();expect(chat.pack).toBeUndefined();expect(chat.policy).toBeUndefined()
 expect(()=>openPolicyInterpretation(store,source,chat.id,'pack')).toThrow()
 expect(validPolicyInterpretationProposal({...proposals[0],artifactKind:'pack'})).toBe(true)
 const historical={...proposals[0]};delete historical.artifactKind;expect(validPolicyInterpretationProposal(historical)).toBe(true)
})

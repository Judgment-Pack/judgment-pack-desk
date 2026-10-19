import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider,useConfirmDeskExit} from '../shell/UnsavedChanges'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import type {Chat,ChatStore} from '../chat/store'
import {PolicyCoverage} from './PolicyCoverage'
import {coverageKey} from './coverageClient'
import {createPolicyPendingJournal} from './pendingOperation'
import type {PolicyWrite} from './client'
import type {CoverageCheck,CoverageOutcome,CoverageReply} from './coverage'
import {coverageCandidateId,coverageOptions,coverageOutcome,coveragePack,coveragePolicy,coveragePolicyId,coverageRecordId,coverageReply,coverageSummary} from '../testing/policyCoverage'

const context=vi.hoisted(()=>({store:null as ChatStore|null,reveal:vi.fn()}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:context.store,ready:!!context.store})}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorControls:()=>({reveal:context.reveal})}))
afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear();context.store=null;context.reveal.mockReset()})
function mount({existing=coverageReply(),mutate,recovered,checking,editing=false,unpublished=false}:{existing?:CoverageReply;mutate?:(operation:PolicyWrite)=>Promise<CoverageOutcome>;recovered?:CoverageOutcome;checking?:Partial<CoverageCheck>;editing?:boolean;unpublished?:boolean}={}){
 let current=existing,receipt=recovered,readFailure=false
 const writes:PolicyWrite[]=[],requests:{path:string;method:string}[]=[],onPendingChange=vi.fn(),onEditingChange=vi.fn(),exited=vi.fn(),cache=testQueryClient()
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const path=String(input),method=init?.method??'GET';requests.push({path,method})
  const base='/api/policies/'+coveragePolicyId
  if(path.startsWith('/api/policy-operations/'))return receipt?Response.json({...receipt,delivery:'recovered'}):Response.json({code:'not-found',outcome:'not-committed'},{status:404})
  if(path===base+'/coverage-options')return Response.json(unpublished?{...coverageOptions,policyPublications:[]}:coverageOptions)
  if(path===base+'/coverage'){
   if(method==='POST'){
    const operation:PolicyWrite={path:'/'+coveragePolicyId+'/coverage',method:'POST',body:JSON.parse(String(init?.body))};writes.push(operation)
    const result=mutate?await mutate(operation):coverageOutcome(operation,current);current=result.coverage;return Response.json(result)
   }
   if(readFailure)throw Error('refresh unavailable')
   return Response.json({items:unpublished?[]:[coverageSummary(current)]})
  }
  if(path===base+'/coverage/resolve')return Response.json({policyRevision:coveragePolicy.revision,source:current.document.candidate.source,pack:current.document.candidate.pack,inventory:current.inventory})
  if(path.endsWith('/check'))return Response.json({coverageRevision:current.revision,candidateDigest:current.document.candidate.digest,policyRevision:coveragePolicy.revision,availability:'available',report:{status:'references-valid'},...checking})
  if(path===base+'/coverage/'+coverageRecordId||path===base+'/coverage/'+coverageRecordId+'/candidates/'+coverageCandidateId){if(readFailure)throw Error('refresh unavailable');return Response.json(current)}
  throw Error('Unexpected fixture request '+path)
 }))
 const view=render(<QueryClientProvider client={cache}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider><PolicyCoverage policy={unpublished?{...coveragePolicy,document:{...coveragePolicy.document,publications:[]}}:coveragePolicy} active editing={editing} disabled={false} onPendingChange={onPendingChange} onEditingChange={onEditingChange}/><ExitFixture onExit={exited}/></DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)
 return {view,cache,writes,requests,onPendingChange,onEditingChange,exited,setRecovery:(result:CoverageOutcome)=>{receipt=result;current=result.coverage},setReadFailure:(value:boolean)=>{readFailure=value}}
}
function ExitFixture({onExit}:{onExit:(leave:boolean)=>void}){const leave=useConfirmDeskExit();return <button onClick={()=>void leave().then(onExit)}>Leave Desk fixture</button>}
async function openRecord(){fireEvent.click(await screen.findByRole('button',{name:'Open coverage record'}));await screen.findByRole('textbox',{name:'Manual handoff'});await waitFor(()=>expect(screen.getByRole('button',{name:'Check declaration'})).toHaveProperty('disabled',false))}
async function choose(name:string,option:RegExp){fireEvent.keyDown(screen.getByRole('combobox',{name}),{key:'Enter'});fireEvent.click(await screen.findByRole('option',{name:option}))}
const base='/api/policies/'+coveragePolicyId

it('keeps unpublished coverage quiet and does not start checks or writes on mount',async()=>{
 const app=mount({unpublished:true})
 expect(await screen.findByText('Publish a Policy version to review coverage.')).toBeTruthy()
 expect(screen.queryByRole('button',{name:'Save coverage draft'})).toBeNull()
 expect(app.requests.every(request=>request.method==='GET')).toBe(true);expect(app.writes).toEqual([])
})
it('loads a saved exact candidate, checks only explicitly and keeps older Runtime drafts usable',async()=>{
 const app=mount({checking:{availability:'unsupported',report:undefined,reason:'profile-not-supported'}});await openRecord()
 expect(screen.getByRole('textbox',{name:'Manual handoff'})).toHaveProperty('value','Administrator checks the exact published narrative.')
 expect(app.requests.some(request=>request.path.endsWith('/check'))).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Check declaration'}))
 expect(await screen.findByText('Coverage checking unsupported')).toBeTruthy()
 expect(screen.getByRole('button',{name:'Record coverage review'})).toHaveProperty('disabled',true)
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'New explicit manual process'}})
 expect(screen.getByRole('button',{name:'Save coverage draft'})).toHaveProperty('disabled',false)
 await act(()=>app.cache.invalidateQueries({queryKey:coverageKey(coveragePolicyId),exact:true}))
 expect(app.requests.filter(request=>request.path.endsWith('/check'))).toHaveLength(1);expect(app.writes).toHaveLength(0)
})
it('resolves exact publication inventory explicitly and saves typed declarations with its own frozen CAS',async()=>{
 const app=mount();await openRecord()
 fireEvent.click(screen.getByRole('button',{name:'Inspect selected publications'}))
 expect(await screen.findByText('Selected publication details')).toBeTruthy()
 await choose('Declared handling',/^Implemented in pack$/)
 fireEvent.click(screen.getByRole('checkbox',{name:'Rule: eligible'}));fireEvent.click(screen.getByRole('checkbox',{name:'Fallback'}))
 fireEvent.change(screen.getByRole('textbox',{name:'Implementation basis'}),{target:{value:'Reviewed the complete narrative and these exact references.'}})
 fireEvent.change(screen.getByRole('textbox',{name:'Coverage review comment'}),{target:{value:'Keep this unsaved review comment for the next step.'}})
 fireEvent.click(screen.getByRole('button',{name:'Save coverage draft'}))
 await screen.findByText('Coverage draft saved')
 expect(app.writes).toHaveLength(1)
 expect(app.writes[0]?.body).toMatchObject({action:'save',coverageId:coverageRecordId,baseRevision:coverageReply().revision,policyPublicationId:coverageOptions.policyPublications[0]!.publicationId,pack:coveragePack,scope:{kind:'whole-narrative'},statement:{status:'implemented',consumerElements:[{kind:'rule',id:'eligible'},{kind:'fallback'}],basis:'Reviewed the complete narrative and these exact references.'}})
 expect(app.requests.filter(request=>request.path===base+'/coverage/resolve')).toHaveLength(1)
 expect(screen.getByRole('button',{name:'Record coverage review'})).toHaveProperty('disabled',true)
 expect(screen.getByRole('textbox',{name:'Coverage review comment'})).toHaveProperty('value','Keep this unsaved review comment for the next step.')
 const journal=await createPolicyPendingJournal('coverage',coveragePolicyId);expect(journal.read()).toBeUndefined()
})
it('records administrator review separately and a declaration change invalidates the check',async()=>{
 const app=mount();await openRecord()
 fireEvent.click(screen.getByRole('button',{name:'Check declaration'}));await screen.findByText('Declared references are structurally valid')
 expect(screen.getByRole('button',{name:'Record coverage review'})).toHaveProperty('disabled',false)
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'Changed declaration'}})
 expect(screen.queryByText('Declared references are structurally valid')).toBeNull();expect(screen.getByRole('button',{name:'Record coverage review'})).toHaveProperty('disabled',true)
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'Administrator checks the exact published narrative.'}})
 fireEvent.click(screen.getByRole('button',{name:'Check declaration'}));await screen.findByText('Declared references are structurally valid')
 fireEvent.change(screen.getByRole('textbox',{name:'Coverage review comment'}),{target:{value:'Reviewed exact publications locally.'}})
 fireEvent.click(screen.getByRole('button',{name:'Record coverage review'}));await screen.findByText('Coverage review recorded')
 expect(app.writes[0]?.body).toMatchObject({action:'review',baseRevision:coverageReply().revision,candidateDigest:coverageReply().document.candidate.digest,policyRevision:coveragePolicy.revision,decision:'approve',comment:'Reviewed exact publications locally.'})
})
it('preserves edits on background revision changes and retains cached records on refresh failure',async()=>{
 const app=mount();await openRecord();const textbox=screen.getByRole('textbox',{name:'Manual handoff'})
 fireEvent.change(textbox,{target:{value:'My local coverage change'}})
 await waitFor(()=>expect(app.cache.getQueryState([...coverageKey(coveragePolicyId),coverageRecordId])?.fetchStatus).toBe('idle'))
 await act(()=>app.cache.setQueryData([...coverageKey(coveragePolicyId),coverageRecordId],{...coverageReply(),revision:'0'.repeat(64)}))
 expect(textbox).toHaveProperty('value','My local coverage change');await waitFor(()=>expect(screen.getByRole('button',{name:'Save coverage draft'})).toHaveProperty('disabled',true))
 expect(screen.getByText(/The coverage record changed. Your draft is preserved/)).toBeTruthy()
 app.setReadFailure(true);await act(()=>app.cache.invalidateQueries({queryKey:coverageKey(coveragePolicyId),exact:true}))
 expect(screen.getByRole('table',{name:'Saved coverage records'})).toBeTruthy();expect(textbox).toHaveProperty('value','My local coverage change');expect(app.writes).toHaveLength(0)
})
it('blocks checks when a coverage refresh observes a newer Policy without changing the saved declaration',async()=>{
 const app=mount();await openRecord()
 await waitFor(()=>expect(app.cache.getQueryState([...coverageKey(coveragePolicyId),coverageRecordId])?.fetchStatus).toBe('idle'))
 await act(()=>app.cache.setQueryData([...coverageKey(coveragePolicyId),coverageRecordId],{...coverageReply(),policyRevision:'0'.repeat(64)}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'Check declaration'})).toHaveProperty('disabled',true))
 expect(screen.getByText('The Policy changed. Resolve the Policy editor and reload this coverage record before checking or reviewing.')).toBeTruthy()
 expect(screen.getByRole('textbox',{name:'Manual handoff'})).toHaveProperty('value','Administrator checks the exact published narrative.')
 expect(app.requests.some(request=>request.path.endsWith('/check'))).toBe(false)
})
it('refuses a substituted check and keeps missing Pack publication reviews historical',async()=>{
 const existing=coverageReply();existing.availability.packPublication='missing';existing.recovery={snapshotId:'restore-one',restoredAt:'2026-10-18T12:00:00Z',currentnessUnknown:true,freshReviewRequired:true,sourceConsentRequired:false}
 const app=mount({existing,checking:{candidateDigest:'0'.repeat(64)}});await openRecord()
 expect(screen.getByText(/Imported coverage reviews are historical assertions/)).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Check declaration'}));await screen.findByRole('alert')
 expect(screen.getByRole('button',{name:'Record coverage review'})).toHaveProperty('disabled',true);expect(app.writes).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Request coverage changes'}));await screen.findByText('Coverage review recorded')
 expect(app.writes[0]?.body.decision).toBe('request-changes')
})
it('journals uncertain delivery and recovers after remount using only the read receipt route',async()=>{
 const first=mount({mutate:async()=>{throw Error('lost reply')}});await openRecord()
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'Saved with interrupted delivery'}})
 fireEvent.click(screen.getByRole('button',{name:'Save coverage draft'}));await screen.findByRole('button',{name:'Check operation result'})
 expect(first.writes).toHaveLength(1);const original=first.writes[0]!,journal=await createPolicyPendingJournal('coverage',coveragePolicyId);expect(journal.read()?.body.operationId).toBe(original.body.operationId)
 expect(screen.getByRole('button',{name:'Open coverage record'})).toHaveProperty('disabled',true)
 const recovered=coverageOutcome(original);cleanup()
 const second=mount({existing:recovered.coverage,recovered});fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}))
 await waitFor(()=>expect(journal.read()).toBeUndefined())
 expect(screen.getByRole('textbox',{name:'Manual handoff'})).toHaveProperty('value','Saved with interrupted delivery')
 expect(second.writes).toHaveLength(0);expect(second.requests.some(request=>request.path.startsWith('/api/policy-operations/'))).toBe(true)
})
it('retains a pending operation when a mismatched receipt claims completion',async()=>{
 const app=mount({mutate:async operation=>{const result=coverageOutcome(operation);result.receipt.coverageCandidateId='coverage_candidate_'+'0'.repeat(48);return result}});await openRecord()
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'My exact candidate'}});fireEvent.click(screen.getByRole('button',{name:'Save coverage draft'}))
 await screen.findByRole('button',{name:'Check operation result'});const journal=await createPolicyPendingJournal('coverage',coveragePolicyId)
 expect(journal.read()?.body.operationId).toBe(app.writes[0]?.body.operationId);expect(screen.getByRole('textbox',{name:'Manual handoff'})).toHaveProperty('value','My exact candidate')
})
it('protects a dirty coverage buffer when switching records or starting another draft',async()=>{
 mount();await openRecord();fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'Keep this local declaration'}})
 fireEvent.click(screen.getByRole('button',{name:'New coverage draft'}))
 const dialog=await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(within(dialog).getByText('Discard unsaved coverage changes?')).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('button',{name:'Keep editing'}))
 expect(screen.getByRole('textbox',{name:'Manual handoff'})).toHaveProperty('value','Keep this local declaration')
})
it('does not allow Save draft and leave to discard an uncertain failed coverage save',async()=>{
 const app=mount({mutate:async()=>{throw Error('delivery interrupted')}});await openRecord()
 fireEvent.change(screen.getByRole('textbox',{name:'Manual handoff'}),{target:{value:'Retain this failed draft'}})
 fireEvent.click(screen.getByRole('button',{name:'Leave Desk fixture'}))
 const dialog=await screen.findByRole('dialog',{name:'Unsaved changes'})
 fireEvent.click(within(dialog).getByRole('button',{name:'Save draft and leave'}))
 expect(await within(dialog).findByText('The coverage draft could not be saved completely. Your changes are still here; finish recovery before leaving.')).toBeTruthy()
 expect(app.exited).not.toHaveBeenCalled();expect(screen.getByDisplayValue('Retain this failed draft')).toBeTruthy()
 const journal=await createPolicyPendingJournal('coverage',coveragePolicyId);expect(journal.read()).toBeTruthy()
})
it('does not offer Save draft and leave for an incomplete declaration',async()=>{
 mount();await screen.findByRole('combobox',{name:'Policy publication'})
 await choose('Policy publication',/Eligibility policy/)
 fireEvent.click(screen.getByRole('button',{name:'Leave Desk fixture'}))
 const dialog=await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(within(dialog).queryByRole('button',{name:'Save draft and leave'})).toBeNull()
 expect(within(dialog).getByRole('button',{name:'Keep editing'})).toBeTruthy()
})
it('confirms append and checkpoints the same linked Plan chat without sending',async()=>{
 const chat={id:'policy-chat',policy:{id:coveragePolicyId},composer:'Existing composed question',updatedAt:'2026-10-18T12:00:00Z',interaction:'plan',attachments:[{name:'kept.pdf'}],checkpoint:{state:{turns:[{id:'turn-one',text:'Earlier message'}]}}} as unknown as Chat
 const checkpoint=vi.fn().mockResolvedValue(undefined),startChat=vi.fn(),update=vi.fn((_id:string,patch:Partial<Chat>)=>Object.assign(chat,patch))
 context.store={getSnapshot:()=>({ready:true,chats:[chat],drafts:[]}),canCreate:true,activate:vi.fn(),update,checkpoint,startChat} as unknown as ChatStore
 mount();await openRecord();fireEvent.click(screen.getByRole('button',{name:'Discuss review'}))
 const dialog=await screen.findByRole('dialog',{name:'Append coverage request?'});expect(chat.composer).toBe('Existing composed question')
 fireEvent.click(within(dialog).getByRole('button',{name:'Cancel'}));expect(update).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Discuss review'}));fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button',{name:'Append request'}))
 await waitFor(()=>expect(checkpoint).toHaveBeenCalledTimes(1))
 expect(chat.composer).toContain(coverageCandidateId);expect(chat.composer).toContain('Existing composed question\n\n');expect(chat.interaction).toBe('plan')
 expect(chat.attachments).toEqual([{name:'kept.pdf'}]);expect(chat.checkpoint?.state.turns).toHaveLength(1);expect(startChat).not.toHaveBeenCalled();expect(context.reveal).toHaveBeenCalledTimes(1)
})

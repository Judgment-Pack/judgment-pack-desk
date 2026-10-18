import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider,useQuery} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import {PolicySourceStatus,sourceAuthorityResultMatches} from './PolicySourceStatus'
import {policyQuery,PolicyRequestError,type PolicyReply,type PolicyWrite,type SourceStatusReply,type SourceAuthorityOutcome,type ExternalPolicyVersion,type SourceAuthorityEvent} from './client'
import {createPolicyPendingJournal} from './pendingOperation'
import sourceWire from '../../../internal/desk/testdata/policy-source-observation.json'

const at='2026-10-19T12:00:00Z',actor={kind:'local',subject:'administrator'},source={provider:'google-drive',accountId:'account-A',resourceId:'file-A'}
const policy:PolicyReply={revision:'policy-one',document:{id:'policy-one',managementMode:'desk-managed',createdAt:at,updatedAt:at,candidate:{title:'Working policy',narrative:'Preserve the saved working draft',artifactVersion:'2.0.0',digest:'candidate-one',revisionId:'candidate-one',content:{sha256:'a'.repeat(64),bytes:20}},workflow:{independentReviewRequired:false},workflowDigest:'workflow-one',reviewState:'unreviewed',revisions:[],reviews:[],publications:[]}}
const observation={...sourceWire,id:'observation-one',bindingId:'binding-one',source,name:'policy.txt',retainedAt:at,semanticDigest:'b'.repeat(64),original:{sha256:sourceWire.content.bytesDigest.slice(7),bytes:sourceWire.content.byteLength},document:{sha256:sourceWire.extraction.content.bytesDigest.slice(7),bytes:sourceWire.extraction.content.byteLength}} as ExternalPolicyVersion['observation']
const initial:SourceStatusReply={revision:'authority-none',policyRevision:policy.revision,sourceRevision:'tracking-one',candidateDigest:policy.document.candidate.digest,candidateRevisionId:policy.document.candidate.revisionId,ownership:{mode:'desk-managed',inherited:true},binding:{version:1,operationId:'bind-one',bindingId:'binding-one',source,createdAt:at,state:'active',coverage:'snapshot-only',lifecycle:{kind:'not-exposed'}},capability:{kind:'not-exposed',mappingSupported:false},availability:{state:'present',usable:true,gap:false,lastSuccess:at},currentPreview:{eligible:false,reason:'configuration-required'},configurations:[],externalVersions:[],events:[],actions:{configure:{allowed:true},setOwnership:{allowed:true},applyExternal:{allowed:false,reason:'configuration-required'},importDraft:{allowed:false,reason:'configuration-required'}}}
const configured=(status:'draft'|'published'='published'):SourceStatusReply=>({...initial,currentConfig:{id:'config-one',bindingId:'binding-one',source,capability:'not-exposed',fallbackStatus:status,at,actor},currentPreview:{eligible:true,status,provenance:'admin-configured',configurationId:'config-one',observationId:observation.id,bindingId:observation.bindingId,sourceRevision:observation.sourceRevision,source,content:observation.content},actions:{...initial.actions,applyExternal:{allowed:status==='published'},importDraft:{allowed:true}}})
const version:ExternalPolicyVersion={id:'external-one',eventId:'event-one',configurationId:'config-one',observation,status:'published',provenance:'admin-configured',at,actor}
function outcome(operation:PolicyWrite,before:SourceStatusReply):SourceAuthorityOutcome{
 const action=operation.body.action as SourceAuthorityEvent['kind'],event:SourceAuthorityEvent={id:'event-'+action,sequence:before.events.length+1,kind:action,at,actor,rationale:String(operation.body.rationale),policySequence:1}
 const snapshot=structuredClone(before);snapshot.revision='authority-'+action
 if(action==='configure'){
  const configuration={id:'config-two',bindingId:String(operation.body.bindingId),source,capability:'not-exposed' as const,fallbackStatus:operation.body.fallbackStatus as 'draft'|'published',at,actor}
  event.configuration=configuration;snapshot.currentConfig=configuration;snapshot.configurations.push(configuration)
 }else if(action==='set-ownership'){
  event.managementMode=operation.body.managementMode as 'desk-managed'|'externally-managed';snapshot.ownership={mode:event.managementMode,inherited:false,eventId:event.id,at,actor}
 }else if(action==='apply-external'){
  event.externalVersion={...version,eventId:event.id};snapshot.lastApplied=event.externalVersion;snapshot.externalVersions.push(event.externalVersion)
 }else{event.importedDraft={configurationId:'config-one',observation,candidateRevisionId:'imported-candidate',candidateDigest:'imported-digest',workflowDigest:'workflow-one'}}
 snapshot.events.push(event)
 return {outcomeKind:'source-authority',delivery:'committed',policyId:policy.document.id,policyRevision:snapshot.policyRevision,sourceStatus:snapshot,receipt:{operationId:String(operation.body.operationId),kind:'authority-'+action,policyId:policy.document.id,authorityRevision:snapshot.revision,authorityEventId:event.id,...(event.configuration?{configurationId:event.configuration.id}:{}),...(event.externalVersion||event.importedDraft?{configurationId:'config-one',observationId:observation.id}:{}),...(event.externalVersion?{externalVersionId:event.externalVersion.id}:{})}}
}
function DraftProbe(){const query=useQuery(policyQuery(policy.document.id));return query.error?<p>Working draft unavailable</p>:null}
function mount({read=initial,mutate,editing=false,draftUnavailable=false,document=sourceWire.document}:{read?:SourceStatusReply;mutate?:(operation:PolicyWrite)=>Promise<SourceAuthorityOutcome>;editing?:boolean;draftUnavailable?:boolean;document?:typeof sourceWire.document}={}){
 let current=structuredClone(read),recovery:SourceAuthorityOutcome|undefined
 const writes:PolicyWrite[]=[],requests:{url:string;method:string}[]=[],cache=testQueryClient(),onPendingChange=vi.fn()
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input);requests.push({url,method:init?.method??'GET'})
  if(url.startsWith('/api/policy-operations/'))return recovery?Response.json({...recovery,delivery:'recovered'}):Response.json({code:'not-found'},{status:404})
  if(url==='/api/policies/policy-one/source-status'){
   if(init?.method==='POST'){
    const operation:PolicyWrite={path:'/policy-one/source-status',method:'POST',body:JSON.parse(String(init.body))};writes.push(operation)
    const result=mutate?await mutate(operation):outcome(operation,current);current=result.sourceStatus;return Response.json(result)
   }
   return Response.json(current)
  }
  if(url.endsWith('/original'))return new Response(new Uint8Array([65,66,67]))
  if(url.startsWith('/api/policies/policy-one/source-sync/observations/'))return Response.json({document,observation})
  if(url.startsWith('/api/policies/policy-one/source-status/versions/'))return Response.json({version:current.externalVersions.find(item=>item.id===url.split('/').at(-1))??version,configuration:configured().currentConfig,document})
  if(url==='/api/policies/policy-one'){if(draftUnavailable)throw Error('unrelated draft read unavailable');return Response.json(policy)}
  if(url==='/api/policies')return Response.json({items:[]})
  throw Error('Unexpected source-status test request '+url)
 }))
 render(<QueryClientProvider client={cache}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider>{draftUnavailable&&<DraftProbe/>}<PolicySourceStatus policy={policy} active editing={editing} disabled={false} onPendingChange={onPendingChange}/></DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)
 return {cache,writes,requests,onPendingChange,setRecovery:(value:SourceAuthorityOutcome)=>{recovery=value;current=value.sourceStatus}}
}
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();sessionStorage.clear()})
async function review(name:string){fireEvent.click(await screen.findByRole('button',{name}));return screen.findByRole('dialog',{name})}
function confirm(dialog:HTMLElement){fireEvent.change(within(dialog).getByRole('textbox',{name:'Decision rationale'}),{target:{value:'Explicit administrator review'}});fireEvent.click(within(dialog).getByRole('button',{name:'Confirm source decision'}))}

it('shows inherited ownership and default Draft honestly without applying configuration or calling a command',async()=>{
 const app=mount();await screen.findByText('Inherited Desk-managed default. Review a selection to record it explicitly.')
 expect(screen.getByText('Default Draft; no fallback configuration has been reviewed.')).toBeTruthy()
 expect(screen.getByText('No external version retained.')).toBeTruthy();expect(app.writes).toEqual([])
 expect(app.requests.every(item=>item.method==='GET')).toBe(true)
 const dialog=await review('Review ownership');confirm(dialog)
 await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0]!.body).toMatchObject({action:'set-ownership',managementMode:'desk-managed',baseRevision:policy.revision,baseSourceRevision:initial.sourceRevision,baseAuthorityRevision:initial.revision,rationale:'Explicit administrator review'})
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Check operation result'})).toBeNull())
})
it('reviews Published fallback as an administrator assertion and never automatically retains or changes ownership',async()=>{
 const app=mount();const dialog=await review('Review fallback configuration')
 fireEvent.keyDown(within(dialog).getByRole('combobox',{name:'Fallback status'}),{key:'Enter'});fireEvent.click(await screen.findByRole('option',{name:/^Published$/}))
 expect(within(dialog).getByText('Published will be an administrator-configured assertion. This source provides no approval or sign-off.')).toBeTruthy()
 confirm(dialog);await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0]!.body).toMatchObject({action:'configure',bindingId:'binding-one',fallbackStatus:'published'})
 await screen.findByText('Source decision recorded.');expect(screen.getByText('No external version retained.')).toBeTruthy()
 expect(app.writes.map(operation=>operation.body.action)).toEqual(['configure'])
})
it('freezes review identity and refuses confirmation after source/configuration drift',async()=>{
 const app=mount({read:configured()}),dialog=await review('Review retaining external version')
 await waitFor(()=>expect(app.cache.getQueryState(['policies','policy-one','source-status'])?.fetchStatus).toBe('idle'))
 await act(()=>app.cache.setQueryData(['policies','policy-one','source-status'],{...configured(),revision:'new-authority',sourceRevision:'new-tracking',currentPreview:{...configured().currentPreview,observationId:'new-observation'}}))
 fireEvent.click(within(dialog).getByText('Technical details'))
 expect(within(dialog).getByText('observation-one')).toBeTruthy();expect(within(dialog).queryByText('new-observation')).toBeNull()
 expect((await within(dialog).findByRole('alert')).textContent).toContain('changed')
 confirm(dialog);expect(app.writes).toEqual([])
})
it('retains exact last-reviewed status through unavailable source and unknown capable-provider evaluations',async()=>{
 const saved={...configured(),lastApplied:version,externalVersions:[version],availability:{state:'revoked',usable:false,gap:true},currentPreview:{eligible:false,reason:'source-revoked'},actions:{...initial.actions,configure:{allowed:false},applyExternal:{allowed:false},importDraft:{allowed:false}}}
 const app=mount({read:saved});await screen.findByText('Published · Administrator configured')
 expect(screen.getByText('Source access revoked')).toBeTruthy();expect(screen.getByRole('button',{name:'Inspect retained external version'})).toBeTruthy()
 await act(()=>app.cache.setQueryData(['policies','policy-one','source-status'],{...saved,currentPreview:{eligible:false,reason:'unknown-source-status',rawStatus:'awaiting-counsel'}}))
 expect(await screen.findByText('The source status is not mapped. Review it before continuing.')).toBeTruthy()
 expect(screen.getByText('Published · Administrator configured')).toBeTruthy();expect(app.writes).toEqual([])
})
it('renders genuine future source-provided retained evidence separately from administrator fallback',async()=>{
 const provided={...version,provenance:'source-provided' as const,rawStatus:'released-by-owner',evidence:{sha256:'e'.repeat(64),bytes:42}}
 mount({read:{...configured(),lastApplied:provided,externalVersions:[provided]}})
 await screen.findByText('Published · Source provided')
 expect(screen.getByText('These source records do not establish verified external approval, governing adoption or permission to execute.')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Inspect retained external version'}));const detail=await screen.findByRole('dialog',{name:'Retained external version'})
 expect(await within(detail).findByText('released-by-owner')).toBeTruthy();expect(within(detail).getByText('e'.repeat(64))).toBeTruthy()
})
it('recovers a lost reply after remount through reads without repeating the configuration command',async()=>{
 const first=mount({mutate:async()=>{throw Error('lost response')}});confirm(await review('Review fallback configuration'))
 await screen.findByRole('button',{name:'Check operation result'});expect(first.writes).toHaveLength(1)
 const journal=await createPolicyPendingJournal('source-status','policy-one');expect(journal.read()?.body.action).toBe('configure')
 cleanup();const second=mount();second.setRecovery(outcome(first.writes[0]!,initial))
 fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}));await waitFor(()=>expect(journal.read()).toBeUndefined())
 expect(second.writes).toEqual([]);expect(second.requests.some(item=>item.url.startsWith('/api/policy-operations/')&&item.method==='GET')).toBe(true)
})
it('keeps wrong-family recovery unresolved and cannot clear a pending source-status operation',async()=>{
 const first=mount({mutate:async()=>{throw Error('lost response')}});confirm(await review('Review ownership'));await screen.findByRole('button',{name:'Check operation result'})
 cleanup();const second=mount();second.setRecovery({...outcome(first.writes[0]!,initial),outcomeKind:'lifecycle'} as unknown as SourceAuthorityOutcome)
 fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}));await screen.findByText('The operation response was interrupted. Recover its exact result before making another change.')
 expect((await createPolicyPendingJournal('source-status','policy-one')).read()).toBeDefined();expect(second.writes).toEqual([])
})
it('acknowledges a source decision without requiring an unrelated working-draft read',async()=>{
 const app=mount({draftUnavailable:true});await screen.findByText('Working draft unavailable');confirm(await review('Review ownership'))
 await screen.findByText('Source decision recorded.');await waitFor(async()=>expect((await createPolicyPendingJournal('source-status','policy-one')).read()).toBeUndefined())
 expect(app.writes).toHaveLength(1);expect(screen.getByText('Working draft unavailable')).toBeTruthy()
})
it('allows explicit Published source import as a fresh Draft with reviewed editable text',async()=>{
 const app=mount({read:configured('published')}),dialog=await review('Review import as Draft')
 expect(within(dialog).getByRole('textbox',{name:'Imported draft text'})).not.toHaveProperty('value',policy.document.candidate.narrative)
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Draft artifact version'}),{target:{value:'3.0.0'}})
 confirm(dialog);await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0]!.body).toMatchObject({action:'import-draft',observationId:'observation-one',configurationId:'config-one',draft:{title:'Working policy',artifactVersion:'3.0.0'}})
 expect(policy.document.candidate.narrative).toBe('Preserve the saved working draft')
})
it('keeps source decisions disabled while the working draft has unsaved edits',async()=>{
 mount({read:configured(),editing:true});const button=await screen.findByRole('button',{name:'Review import as Draft'})
 expect((button as HTMLButtonElement).disabled).toBe(true);expect((screen.getByRole('button',{name:'Review ownership'}) as HTMLButtonElement).disabled).toBe(true)
})
it('requires explicit partial-page acknowledgement before importing readable extraction',async()=>{
 const partial=structuredClone(sourceWire.document);partial.content.truncated=true;partial.processing.status='partial'
 const app=mount({read:configured(),document:partial}),dialog=await review('Review import as Draft')
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Decision rationale'}),{target:{value:'Read and reviewed available pages'}})
 const submit=within(dialog).getByRole('button',{name:'Confirm source decision'})
 expect((submit as HTMLButtonElement).disabled).toBe(true);fireEvent.click(submit);expect(app.writes).toEqual([])
 expect(within(dialog).getByRole('button',{name:'Download original'})).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('checkbox',{name:'Use readable pages despite missing content.'}));fireEvent.click(submit)
 await waitFor(()=>expect(app.writes).toHaveLength(1));expect(app.writes[0]!.body.action).toBe('import-draft')
})
it('downloads the exact retained observation original from external version inspection',async()=>{
 const NativeURL=URL
 vi.stubGlobal('URL',class extends NativeURL{static createObjectURL=vi.fn(()=> 'blob:retained-fixture');static revokeObjectURL=vi.fn()})
 const click=vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(()=>{})
 const app=mount({read:{...configured(),lastApplied:version,externalVersions:[version]}})
 fireEvent.click(await screen.findByRole('button',{name:'Inspect retained external version'}));const dialog=await screen.findByRole('dialog',{name:'Retained external version'})
 fireEvent.click(await within(dialog).findByRole('button',{name:'Download original'}));await waitFor(()=>expect(click).toHaveBeenCalledOnce())
 expect(app.requests).toContainEqual({url:'/api/policies/policy-one/source-sync/observations/observation-one/original',method:'GET'});expect(app.writes).toEqual([])
})
it('rejects receipt target/configuration mismatches before acknowledging delivery',()=>{
 const operation:PolicyWrite={path:'/policy-one/source-status',method:'POST',body:{operationId:'operation-one',action:'apply-external',observationId:'observation-one',configurationId:'config-one'}}
 const result=outcome(operation,configured());expect(sourceAuthorityResultMatches(result,operation,'policy-one')).toBe(true)
 for(const invalid of [{...result,policyId:'other-policy'},{...result,receipt:{...result.receipt,configurationId:'other-config'}},{...result,receipt:{...result.receipt,operationId:'other-operation'}}])expect(sourceAuthorityResultMatches(invalid,operation,'policy-one')).toBe(false)
 expect(new PolicyRequestError('external-ownership').message).toContain('does not allow this action')
})

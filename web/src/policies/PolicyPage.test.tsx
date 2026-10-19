import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {createMemoryRouter,RouterProvider} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import {PolicyPage} from './PolicyPage'
import {policyLibraryItems,folderAssignmentKey,deletableLibraryDraft} from '../library/items'
import {type PolicyReply,type PolicyMutationReply,type PolicyWrite,type PolicySourceSync,type PolicySourceObservation,type SourceStatusReply,type SourceAuthorityOutcome,writePolicy} from './client'
import sourceWire from '../../../internal/desk/testdata/policy-source-observation.json'
import {createPolicyPendingJournal} from './pendingOperation'

vi.mock('./PolicyAssistant',()=>({PolicyAssistant:()=>null}))

const at='2026-10-17T12:00:00Z',actor={kind:'local',subject:'local-admin'}
function policy(revision='head-one',narrative='Expense policy',reviewState:PolicyReply['document']['reviewState']='unreviewed'):PolicyReply{
 const candidate={title:'Expenses',narrative,artifactVersion:'1.0.0',digest:'digest-'+revision,revisionId:'candidate-'+revision,content:{sha256:'content-'+revision,bytes:100}}
 const workflow={independentReviewRequired:false}
 return {revision,document:{id:'policy-one',managementMode:'desk-managed',createdAt:at,updatedAt:at,candidate,workflow,workflowDigest:'workflow-one',reviewState,revisions:[{id:candidate.revisionId,sequence:1,digest:candidate.digest,title:candidate.title,artifactVersion:candidate.artifactVersion,content:candidate.content,workflow,workflowDigest:'workflow-one',at,actor}],reviews:[],publications:[]}}
}
const result=(reply:PolicyReply,delivery:'committed'|'recovered'='committed'):PolicyMutationReply=>({delivery,receipt:{operationId:'fixture'},policy:reply})
function mount({path='/policies/policy-one',read=policy(),sync={revision:'source-none',supported:false,pending:false},mutate=async()=>result(policy('head-two')),authorityMutate}:{path?:string;read?:PolicyReply;sync?:PolicySourceSync;mutate?:(request:PolicyWrite)=>Promise<PolicyMutationReply>;authorityMutate?:(request:PolicyWrite)=>Promise<SourceAuthorityOutcome>}={}){
 let current=read,recovery:PolicyMutationReply|undefined,recoveryMissing=false,readFailure=false
 let authority:SourceStatusReply|undefined
 const writes:PolicyWrite[]=[],requests:string[]=[]
 const fetch=vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input);requests.push(url)
  if(url.startsWith('/api/policy-operations/'))return recoveryMissing||!recovery?Response.json({code:'not-found',error:'internal secret'},{status:404}):Response.json({...recovery,delivery:'recovered'})
  if(url==='/api/policies/policy-one'&&(!init?.method||init.method==='GET')){
   if(readFailure)throw new Error('fixture read failed')
   return Response.json(current)
  }
  if(url==='/api/policies/policy-one/source-sync'&&(!init?.method||init.method==='GET'))return Response.json(sync)
  if(url==='/api/policies/policy-one/source-status'){
   if(init?.method==='POST'&&authorityMutate){const operation:PolicyWrite={path:'/policy-one/source-status',method:'POST',body:JSON.parse(String(init.body))};writes.push(operation);const outcome=await authorityMutate(operation);authority=outcome.sourceStatus;return Response.json(outcome)}
   if(!init?.method||init.method==='GET')return Response.json(authority??{revision:'authority-none',policyRevision:current.revision,sourceRevision:sync.revision,candidateDigest:current.document.candidate.digest,candidateRevisionId:current.document.candidate.revisionId,ownership:{mode:current.document.managementMode,inherited:true},capability:{kind:'unavailable',mappingSupported:false},availability:{state:'unbound',usable:false,gap:false},currentPreview:{eligible:false,reason:'source-unbound'},configurations:[],externalVersions:[],events:[],actions:{configure:{allowed:false},setOwnership:{allowed:true},applyExternal:{allowed:false},importDraft:{allowed:false}}})
  }
  if(url.startsWith('/api/policies/policy-one/source-sync/observations/'))return Response.json({document:sourceWire.document})
  if(url==='/api/policies/policy-one/source')return new Response(new TextEncoder().encode('# Original\r\nCafé 中文\r\n'))
  if(url.startsWith('/api/policies/policy-one/revisions/'))return Response.json({candidate:{...policy().document.candidate,narrative:'Immutable original text'},workflow:current.document.workflow,workflowDigest:'workflow-one',at,actor})
  if(url==='/api/policies'){
   if(!init?.method||init.method==='GET')return Response.json({items:[]})
  }
  if(init?.method==='POST'||init?.method==='PUT'){
   const request={path:url.replace('/api/policies',''),method:init.method,body:JSON.parse(String(init.body))} as PolicyWrite
   writes.push(request)
   const reply=await mutate(request);current=reply.policy;return Response.json(reply)
  }
  throw new Error('Unexpected policy fixture request: '+url)
 })
 vi.stubGlobal('fetch',fetch)
 const cache=testQueryClient()
 // Match actual parameter routes so detail pages exercise frozen query bases.
 const routed=createMemoryRouter([{element:<UnsavedChangesProvider><DeskNoticesProvider><PolicyRoutes/></DeskNoticesProvider></UnsavedChangesProvider>,children:[{path:'/policies/new',element:<PolicyPage/>},{path:'/policies/:policyId',element:<PolicyPage/>},{path:'/library',element:<p>Library destination</p>}]}],{initialEntries:[path]})
 render(<QueryClientProvider client={cache}><RouterProvider router={routed}/></QueryClientProvider>)
 return {cache,router:routed,writes,requests,setCurrent:(next:PolicyReply)=>{current=next},setRecovery:(next?:PolicyMutationReply)=>{recovery=next},setRecoveryMissing:(value:boolean)=>{recoveryMissing=value},setReadFailure:(value:boolean)=>{readFailure=value}}
}
import {Outlet} from 'react-router-dom'
function PolicyRoutes(){return <Outlet/>}
afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear()})
const edit=(text:string)=>fireEvent.change(screen.getByRole('textbox',{name:'Policy text'}),{target:{value:text}})
const clickSave=()=>fireEvent.click(screen.getByRole('button',{name:'Save draft'}))
const sourceTab=()=>fireEvent.mouseDown(screen.getByRole('tab',{name:'Source'}),{button:0,ctrlKey:false})

it('marks restored approval history as imported assertions and permits a fresh candidate review',async()=>{
 const saved=policy('restored-head','Retained text','unreviewed');saved.currentCandidatePublished=false;saved.recovery={snapshotId:'restore-one',restoredAt:at,currentnessUnknown:true,freshReviewRequired:true,sourceConsentRequired:false}
 mount({read:saved});await screen.findByRole('textbox',{name:'Policy text'})
 expect(screen.getByText('Restored Policy history · Currentness unknown')).toBeTruthy();expect(screen.getByText(/historical records, not authenticated current approval/)).toBeTruthy()
 await waitFor(()=>expect(screen.getByRole('button',{name:'Approve candidate'})).toHaveProperty('disabled',false));expect(screen.getByRole('button',{name:'Publish version'})).toHaveProperty('disabled',true)
 expect(screen.getByText('Current candidate unpublished')).toBeTruthy()
})
it('offers fresh consent while held source provider controls stay unavailable and retained text can be reviewed locally',async()=>{
 const recovery={snapshotId:'restore-one',restoredAt:at,currentnessUnknown:true as const,freshReviewRequired:true,sourceConsentRequired:true}
 const observation={...sourceWire,id:'old-observation',name:'imported.txt',retainedAt:at,semanticDigest:'semantic'} as unknown as PolicySourceObservation
 const sync:PolicySourceSync={revision:'restored-source',supported:true,pending:true,recovery,state:{version:1,generation:1,state:'authorization-ended',frequencyMinutes:15,gap:true,errorCode:'restored-source-consent-required',grant:{actor,at,resourceId:sourceWire.source.resourceId},binding:{bindingId:sourceWire.bindingId,source:sourceWire.source},observations:[observation],resolutions:[]}}
 const app=mount({read:{...policy(),recovery},sync});await screen.findByRole('textbox',{name:'Policy text'});sourceTab()
 expect(await screen.findByText('Historical source authorization · Fresh consent required')).toBeTruthy();await waitFor(()=>expect(screen.getByRole('button',{name:'Choose Google Drive source'})).toHaveProperty('disabled',false))
 for(const name of ['Check now','Pause checks','Resume checks','Revoke source access'])expect(screen.queryByRole('button',{name})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Review keeping current candidate'}));expect(await screen.findByText(/This reviews an imported historical source snapshot locally/)).toBeTruthy();expect(app.writes).toEqual([])
})

it('shows unsupported automatic checks honestly for an older installed Gateway',async()=>{
 mount();await screen.findByRole('textbox',{name:'Policy text'});sourceTab()
 expect(await screen.findByText('The installed Gateway does not support automatic policy source checks.')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Choose Google Drive source'}) as HTMLButtonElement).disabled).toBe(true)
})
it('preserves the frozen editor while source freshness changes and blocks publication on unresolved updates',async()=>{
 const app=mount({read:policy('head-one','Saved candidate','approved')}),sourceKey=['policies','policy-one','source-sync']
 await screen.findByRole('textbox',{name:'Policy text'})
 // Settle the initial read before injecting a refresh; its response must not
 // overwrite the simulated update while query observers are still scheduling.
 await waitFor(()=>{
  expect(app.cache.getQueryState(sourceKey)?.fetchStatus).toBe('idle')
  expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(false)
 })
 edit('My unsaved local candidate')
 await act(()=>app.cache.setQueryData(sourceKey,{revision:'observed-two',supported:true,pending:true}))
 expect(await screen.findByText('Resolve the latest source update before publication.')).toBeTruthy()
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('My unsaved local candidate')
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 // Remove the independent dirty-editor gate: the unresolved source alone
 // still blocks publication, and resolving that source re-enables it.
 edit('Saved candidate')
 expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 await act(()=>app.cache.setQueryData(sourceKey,{revision:'observed-three',supported:true,pending:false}))
 await waitFor(()=>expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(false))
 expect(app.writes).toHaveLength(0)
})
it('journals uncertain source commands before dispatch and recovers after remount without replay',async()=>{
 const sync:PolicySourceSync={revision:'source-active',supported:true,pending:true,state:{version:1,generation:1,state:'active',frequencyMinutes:15,gap:false,grant:{actor,at,resourceId:'file-A'},binding:{bindingId:'binding-A',source:{provider:'google-drive',accountId:'account-A',resourceId:'file-A'}},observations:[],resolutions:[]}}
 const first=mount({sync,mutate:async()=>{throw new Error('lost reply')}});await screen.findByRole('textbox',{name:'Policy text'});sourceTab();fireEvent.click(await screen.findByRole('button',{name:'Pause checks'}));await screen.findByRole('button',{name:'Check operation result'})
 expect(first.writes).toHaveLength(1)
 const journal=await createPolicyPendingJournal('source-sync','policy-one');expect(journal.read()!.body.action).toBe('pause')
 cleanup()
 const second=mount({sync});second.setRecovery({...result(policy()),receipt:{operationId:first.writes[0]!.body.operationId,kind:'source-pause',policyId:'policy-one'},sourceSync:sync});await screen.findByRole('textbox',{name:'Policy text'});sourceTab();fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}));await waitFor(()=>expect(journal.read()).toBeUndefined())
 expect(second.writes).toHaveLength(0);expect(second.requests.some(path=>path.startsWith('/api/policy-operations/'))).toBe(true)
})
it('freezes the reviewed source and candidate when a newer source arrives while confirmation is open',async()=>{
 const observation={...sourceWire,id:sourceWire.observationId,retainedAt:at,semanticDigest:'semantic-a',name:'policy.txt',original:{sha256:sourceWire.content.bytesDigest.slice(7),bytes:sourceWire.content.byteLength},document:{sha256:sourceWire.extraction.content.bytesDigest.slice(7),bytes:sourceWire.extraction.content.byteLength}} as PolicySourceObservation
 const sync:PolicySourceSync={revision:'source-a',supported:true,pending:true,state:{version:1,generation:1,state:'active',frequencyMinutes:15,gap:false,grant:{actor,at,resourceId:sourceWire.source.resourceId},binding:{bindingId:sourceWire.bindingId,source:sourceWire.source},observations:[observation],resolutions:[]}}
 const app=mount({sync});await screen.findByRole('textbox',{name:'Policy text'});sourceTab();fireEvent.click(await screen.findByRole('button',{name:'Review keeping current candidate'}))
 const dialog=screen.getByRole('dialog',{name:'Keep the current candidate?'})
 expect(within(dialog).getByText('7')).toBeTruthy()
 const newer={...observation,id:'observation-b',sourceRevision:'8'}
 await act(()=>app.cache.setQueryData(['policies','policy-one','source-sync'],{...sync,revision:'source-b',state:{...sync.state!,observations:[observation,newer]}}))
 expect(within(dialog).getByText('7')).toBeTruthy();expect(within(dialog).queryByText('8')).toBeNull()
 await waitFor(()=>expect(within(dialog).getByRole('alert').textContent).toContain('The policy or source changed.'))
 expect((within(dialog).getByRole('button',{name:'Keep current candidate'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(within(dialog).getByRole('button',{name:'Keep current candidate'}));expect(app.writes).toHaveLength(0)
})
it('creates an independent policy, opens its stable page, and reloads the exact saved text',async()=>{
 const saved=policy('created','Pasted policy text'),app=mount({path:'/policies/new',mutate:async()=>result(saved)})
 expect((screen.getByRole('textbox',{name:'Review comment'}) as HTMLTextAreaElement).disabled).toBe(true)
 fireEvent.change(screen.getByRole('textbox',{name:'Title'}),{target:{value:'Expenses'}})
 edit('Pasted policy text');clickSave()
 await waitFor(()=>expect(app.router.state.location.pathname).toBe('/policies/policy-one'))
 expect((await screen.findByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Pasted policy text')
 expect((screen.getByRole('textbox',{name:'Review comment'}) as HTMLTextAreaElement).disabled).toBe(false)
 expect(app.writes).toHaveLength(1);expect(app.writes[0].body).toMatchObject({title:'Expenses',narrative:'Pasted policy text',workflow:{independentReviewRequired:false}})
 expect(app.writes[0].body).not.toHaveProperty('chatId')
 await act(()=>app.router.navigate('/library'))
 await act(()=>app.router.navigate('/policies/policy-one'))
 expect((await screen.findByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Pasted policy text')
})
it('never overwrites a local buffer on refresh and requires explicit reconciliation before saving',async()=>{
 const app=mount();await screen.findByRole('textbox',{name:'Policy text'});edit('My local text')
 const remote=policy('remote','Other editor text');app.setCurrent(remote)
 await act(()=>app.cache.invalidateQueries({queryKey:['policies','policy-one']}))
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('My local text')
 expect(await screen.findByText('Other editor text')).toBeTruthy();expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Keep my text on latest revision'}));clickSave()
 await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0].body).toMatchObject({baseRevision:'remote',narrative:'My local text'})
})
it('preserves edits after a stale write and prevents a fresh save until comparison',async()=>{
 const app=mount({mutate:async()=>{throw new Error('Unused')}})
 await screen.findByRole('textbox',{name:'Policy text'});edit('Local after conflict')
 app.setCurrent(policy('remote','Remote after conflict'))
 vi.stubGlobal('fetch',async(_input:RequestInfo|URL,init?:RequestInit)=>init?.method==='PUT'?Response.json({code:'revision-conflict',error:'private backend prose'},{status:409}):Response.json(policy('remote','Remote after conflict')))
 clickSave();await screen.findByText('Saved policy changed')
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Local after conflict')
 expect(screen.queryByText('private backend prose')).toBeNull()
 expect((screen.getByRole('button',{name:'Save draft'}) as HTMLButtonElement).disabled).toBe(true)
})
it('recovers a lost response through read-only receipt lookup and refetches the head before new actions',async()=>{
 const app=mount({mutate:async()=>{throw new Error('Lost response')}})
 await screen.findByRole('textbox',{name:'Policy text'});edit('Uncertain local text');clickSave()
 await screen.findByRole('button',{name:'Check operation result'})
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Check operation result'}))
 await screen.findByText('No completed operation was found yet. Its result is still unresolved. Check again before continuing.')
 expect(app.writes).toHaveLength(1)
 app.setRecovery(result(policy('committed','Uncertain local text')));app.setCurrent(policy('newer','Newer server text'))
 fireEvent.click(screen.getByRole('button',{name:'Check operation result'}))
 await screen.findByText('The operation completed, then the saved policy changed. Your local text is preserved. Review it before saving again.')
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Uncertain local text')
 expect(app.writes).toHaveLength(1)
 expect(app.requests.filter(path=>path.startsWith('/api/policy-operations/'))).toHaveLength(2)
 fireEvent.click(screen.getByRole('button',{name:'Keep my text on latest revision'}));clickSave();await waitFor(()=>expect(app.writes).toHaveLength(2))
 expect(app.writes[1].body.baseRevision).toBe('newer')
})
it('holds a completed mutation when the current-head read fails and retries only the read',async()=>{
 const app=mount();await screen.findByRole('textbox',{name:'Policy text'});edit('Changed text');app.setReadFailure(true);clickSave()
 await screen.findByRole('button',{name:'Reload current policy'})
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
 app.setReadFailure(false);fireEvent.click(screen.getByRole('button',{name:'Reload current policy'}))
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Reload current policy'})).toBeNull())
 expect(app.writes).toHaveLength(1)
})
it('keeps review separate from publication and binds each to the exact saved candidate and workflow',async()=>{
 const reviewed=policy('reviewed','Expense policy','approved');reviewed.document.currentReviewId='review-one'
 const published=structuredClone(reviewed);published.revision='published';published.document.publications=[{id:'pub-one',sequence:3,candidateRevisionId:published.document.candidate.revisionId,artifactVersion:'1.0.0',candidateDigest:published.document.candidate.digest,workflowDigest:'workflow-one',reviewId:'review-one',content:published.document.candidate.content,at,actor}]
 const app=mount({mutate:async request=>result(request.path.endsWith('/publish')?published:reviewed)})
 await screen.findByRole('textbox',{name:'Policy text'})
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Approve candidate'}));await screen.findAllByText('Approved',{selector:'p'})
 expect(app.writes[0].body).toMatchObject({decision:'approve',baseRevision:'head-one',candidateDigest:'digest-head-one',workflowDigest:'workflow-one'})
 expect(app.writes).toHaveLength(1)
 fireEvent.click(screen.getByRole('button',{name:'Publish version'}))
 const dialog=await screen.findByRole('dialog',{name:'Publish policy version?'})
 expect(within(dialog).getByText('digest-reviewed')).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('button',{name:'Publish version'}))
 await screen.findByText('Current candidate published')
 expect(app.writes[1].body).toMatchObject({baseRevision:'reviewed',candidateDigest:'digest-reviewed',workflowDigest:'workflow-one',reviewId:'review-one'})
 edit('New candidate text')
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
})
it('protects failed saves through shared exit confirmation and displays immutable actor and revision history',async()=>{
 const app=mount({mutate:async()=>{throw new Error('Fixture network failure')}})
 await screen.findByRole('textbox',{name:'Policy text'});edit('Still unsaved');clickSave()
 await screen.findByRole('button',{name:'Check operation result'})
 const closing=new Event('beforeunload',{cancelable:true});window.dispatchEvent(closing);expect(closing.defaultPrevented).toBe(true)
 await act(()=>app.router.navigate('/library'))
 expect(await screen.findByRole('dialog',{name:'Unsaved changes'})).toBeTruthy()
 expect(app.router.state.location.pathname).toBe('/policies/policy-one')
 expect(screen.queryByRole('button',{name:'Save draft and leave'})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}))
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Still unsaved')
 expect(app.writes).toHaveLength(1)
 fireEvent.mouseDown(screen.getByRole('tab',{name:'History'}),{button:0,ctrlKey:false})
 fireEvent.click(screen.getByRole('button',{name:'Inspect Draft saved'}))
 await screen.findByText('Immutable original text')
 expect(screen.getAllByText(/local-admin/).length).toBeGreaterThan(0)
})
it('does not expose raw server prose or mislabel a new candidate as an existing publication',async()=>{
 vi.stubGlobal('fetch',async()=>Response.json({code:'version-conflict',error:'secret raw provider response https://internal'},{status:409}))
 await expect(writePolicy({path:'/one/publish',method:'POST',body:{operationId:'fixture-operation'}})).rejects.toThrow('Publish with a version greater than the latest publication.')
 const item=policyLibraryItems([{id:'one',title:'Expense policy',artifactVersion:'1.0.0',candidateDigest:'new',currentCandidatePublished:false,updatedAt:at,reviewState:'unreviewed',publicationCount:1,latestPublication:{id:'pub',sequence:3,artifactVersion:'1.0.0',candidateDigest:'old',candidateRevisionId:'old',workflowDigest:'w',reviewId:'r',content:{sha256:'old',bytes:1},at,actor}}])[0]
 expect(item.status).toBe('draft');expect(folderAssignmentKey(item)).toBeUndefined();expect(deletableLibraryDraft(item)).toBe(false)
})

it('saves unfinished drafts and permits request-changes while independent approval remains unavailable',async()=>{
 const initial=policy();initial.document.candidate.narrative='';initial.document.candidate.title='';initial.document.candidate.artifactVersion='unfinished';initial.document.workflow.independentReviewRequired=true
 const saved=structuredClone(initial);saved.revision='unfinished-saved';saved.document.candidate.title='Work in progress'
 const app=mount({read:initial,mutate:async()=>result(saved)})
 await screen.findByRole('textbox',{name:'Title'})
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Request changes'}) as HTMLButtonElement).disabled).toBe(false)
 fireEvent.change(screen.getByRole('textbox',{name:'Title'}),{target:{value:'Work in progress'}})
 clickSave();await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0].body).toMatchObject({title:'Work in progress',narrative:'',artifactVersion:'unfinished',workflow:{independentReviewRequired:true}})
 await waitFor(()=>expect((screen.getByRole('button',{name:'Request changes'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(screen.getByRole('button',{name:'Request changes'}))
 await waitFor(()=>expect(app.writes).toHaveLength(2))
 expect(app.writes[1].body).toMatchObject({decision:'request-changes',baseRevision:'unfinished-saved'})
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
})

it('preserves an unsent review comment across draft saves, then sends and clears it only on explicit review',async()=>{
 const saved=policy('saved','Edited policy text'),reviewed=structuredClone(saved)
 reviewed.revision='reviewed';reviewed.document.reviewState='approved';reviewed.document.currentReviewId='review-one'
 const app=mount({mutate:async request=>result(request.path.endsWith('/review')?reviewed:saved)})
 const comment=await screen.findByRole('textbox',{name:'Review comment'})
 fireEvent.change(comment,{target:{value:'Retain this review context until my decision.'}})
 edit('Edited policy text');clickSave()
 await waitFor(()=>expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(false))
 expect((comment as HTMLTextAreaElement).value).toBe('Retain this review context until my decision.')
 expect(app.writes[0].body).not.toHaveProperty('comment')
 fireEvent.click(screen.getByRole('button',{name:'Approve candidate'}))
 await waitFor(()=>expect((comment as HTMLTextAreaElement).value).toBe(''))
 expect(app.writes).toHaveLength(2)
 expect(app.writes[1].body).toMatchObject({decision:'approve',baseRevision:'saved',comment:'Retain this review context until my decision.'})
})
it('preserves an unsent review comment when publishing an already approved candidate',async()=>{
 const reviewed=policy('reviewed','Expense policy','approved');reviewed.document.currentReviewId='review-one'
 const published=structuredClone(reviewed);published.revision='published';published.document.publications=[{id:'pub-one',sequence:3,candidateRevisionId:published.document.candidate.revisionId,artifactVersion:'1.0.0',candidateDigest:published.document.candidate.digest,workflowDigest:'workflow-one',reviewId:'review-one',content:published.document.candidate.content,at,actor}]
 const app=mount({read:reviewed,mutate:async()=>result(published)})
 const comment=await screen.findByRole('textbox',{name:'Review comment'})
 fireEvent.change(comment,{target:{value:'Unsent note for another review.'}})
 fireEvent.click(screen.getByRole('button',{name:'Publish version'}))
 const dialog=await screen.findByRole('dialog',{name:'Publish policy version?'})
 fireEvent.click(within(dialog).getByRole('button',{name:'Publish version'}))
 await screen.findByText('Current candidate published')
 expect((comment as HTMLTextAreaElement).value).toBe('Unsent note for another review.')
 expect(app.writes).toHaveLength(1)
 expect(app.writes[0].body).not.toHaveProperty('comment')
})
it('imports editable text while retaining original bytes across save and refresh',async()=>{
 const raw='# Original\r\nCafé 中文\r\n',saved=policy('imported','Edited interpretation')
 saved.document.candidate.source={name:'Original.md',mediaType:'text/markdown',origin:'user-upload',content:{sha256:'source-digest',bytes:new TextEncoder().encode(raw).length}}
 const app=mount({path:'/policies/new',mutate:async()=>result(saved)})
 expect(screen.getByText(/UTF-8 Markdown/)).toBeTruthy()
 fireEvent.change(screen.getByLabelText('Import file',{selector:'input'}),{target:{files:[new File([raw],'Original.md',{type:'text/markdown'})]}})
 await screen.findByText(/Selected original:/)
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe(raw.replace(/\r\n/g,'\n'))
 expect(app.writes).toHaveLength(0)
 edit('Edited interpretation');clickSave()
 await waitFor(()=>expect(app.router.state.location.pathname).toBe('/policies/policy-one'))
 expect(app.writes[0].body.narrative).toBe('Edited interpretation')
 const original=app.writes[0].body.import as {name:string;mediaType:string;data:string}
 expect(original).toMatchObject({name:'Original.md',mediaType:'text/markdown'})
 expect(Array.from(atob(original.data),character=>character.charCodeAt(0))).toEqual(Array.from(new TextEncoder().encode(raw)))
 await act(()=>app.router.navigate('/library'));await act(()=>app.router.navigate('/policies/policy-one'))
 expect(await screen.findByText('source-digest')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Read original'}))
 await waitFor(()=>expect(screen.getByText(/Café 中文/,{selector:'pre'})).toBeTruthy())
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Edited interpretation')
 edit('Later edit');clickSave();await waitFor(()=>expect(app.writes).toHaveLength(2))
 expect(app.writes[1].body).not.toHaveProperty('import')
})
it('retains selected original and draft after failed save and protects navigation',async()=>{
 const app=mount({path:'/policies/new',mutate:async()=>{throw new Error('lost acknowledgement')}})
 fireEvent.change(screen.getByLabelText('Import file',{selector:'input'}),{target:{files:[new File(['Original text'],'policy.txt',{type:'text/plain'})]}})
 await screen.findByText(/Selected original:/)
 edit('Local interpretation');clickSave()
 await screen.findByRole('button',{name:'Check operation result'})
 expect(screen.getByText(/Selected original:/).textContent).toContain('policy.txt')
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Local interpretation')
 await act(()=>app.router.navigate('/library'))
 await screen.findByRole('dialog',{name:'Unsaved changes'})
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}))
 expect(screen.getByText(/Selected original:/).textContent).toContain('policy.txt')
 expect(app.writes).toHaveLength(1)
})
it('rejects invalid local uploads without sending or changing the draft',async()=>{
 const app=mount({path:'/policies/new'})
 fireEvent.change(screen.getByLabelText('Import file',{selector:'input'}),{target:{files:[new File([new Uint8Array([0xff])],'bad.md',{type:'text/markdown'})]}})
 expect(await screen.findByRole('alert')).toHaveProperty('textContent','Import a UTF-8 .txt or .md file up to 128 KiB with a simple filename.')
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('')
 expect(app.writes).toHaveLength(0)
})
it('blocks Design pack while the editor is dirty or its frozen saved revision conflicts with a refresh',async()=>{
 const app=mount();await screen.findByRole('textbox',{name:'Policy text'})
 expect((screen.getByRole('button',{name:'Design pack'}) as HTMLButtonElement).disabled).toBe(false)
 edit('Unsaved interpretation source')
 expect((screen.getByRole('button',{name:'Design pack'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Design pack'}));expect(app.requests.some(url=>url.includes('/authoring-source'))).toBe(false)
 edit('Expense policy')
 expect((screen.getByRole('button',{name:'Design pack'}) as HTMLButtonElement).disabled).toBe(false)
 await act(()=>app.cache.setQueryData(['policies','policy-one'],policy('remote-head','New saved text')))
 await screen.findByText('Saved policy changed')
 expect((screen.getByRole('button',{name:'Design pack'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('textbox',{name:'Policy text'}) as HTMLTextAreaElement).value).toBe('Expense policy')
})

function ownershipOutcome(operation:PolicyWrite,head:PolicyReply):SourceAuthorityOutcome{
 const event={id:'ownership-event',sequence:1,kind:'set-ownership' as const,at,actor,rationale:String(operation.body.rationale),policySequence:1,managementMode:'externally-managed' as const}
 const sourceStatus:SourceStatusReply={revision:'authority-reviewed',policyRevision:head.revision,sourceRevision:'source-none',candidateDigest:head.document.candidate.digest,candidateRevisionId:head.document.candidate.revisionId,ownership:{mode:'externally-managed',inherited:false,eventId:event.id,at,actor},capability:{kind:'unavailable',mappingSupported:false},availability:{state:'unbound',usable:false,gap:false},currentPreview:{eligible:false,reason:'source-unbound'},configurations:[],externalVersions:[],events:[event],actions:{configure:{allowed:false},setOwnership:{allowed:true},applyExternal:{allowed:false},importDraft:{allowed:false}}}
 return {outcomeKind:'source-authority',delivery:'committed',policyId:'policy-one',policyRevision:head.revision,sourceStatus,receipt:{operationId:String(operation.body.operationId),kind:'authority-set-ownership',policyId:'policy-one',authorityRevision:sourceStatus.revision,authorityEventId:event.id}}
}
async function setExternalOwnership(){
 sourceTab();fireEvent.click(await screen.findByRole('button',{name:'Review ownership'}));const dialog=await screen.findByRole('dialog',{name:'Review ownership'})
 fireEvent.keyDown(within(dialog).getByRole('combobox',{name:'Ownership'}),{key:'Enter'});fireEvent.click(await screen.findByRole('option',{name:/^Externally managed$/}))
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Decision rationale'}),{target:{value:'External system owns source publication'}});fireEvent.click(within(dialog).getByRole('button',{name:'Confirm source decision'}))
}
it('opens only the exact completed ownership head after explicit review, preserving the clean working text',async()=>{
 const external=policy('ownership-head','Expense policy');external.document.managementMode='externally-managed'
 const app=mount({authorityMutate:async operation=>{app.setCurrent(external);return ownershipOutcome(operation,external)}})
 await screen.findByRole('textbox',{name:'Policy text'});await setExternalOwnership();await screen.findByText('Source decision recorded.')
 await waitFor(()=>expect((screen.getByRole('button',{name:'Review ownership'}) as HTMLButtonElement).disabled).toBe(false))
 expect(screen.queryByRole('button',{name:'Compare latest saved draft'})).toBeNull()
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Draft'}),{button:0,ctrlKey:false})
 expect(screen.getByRole('textbox',{name:'Policy text'})).toHaveProperty('value','Expense policy')
 expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 expect(app.writes).toHaveLength(1)
})
it('acknowledges ownership independently of a failed optional draft refresh and offers direct comparison',async()=>{
 const external=policy('ownership-head');external.document.managementMode='externally-managed'
 const app=mount({authorityMutate:async operation=>{app.setCurrent(external);app.setReadFailure(true);return ownershipOutcome(operation,external)}})
 await screen.findByRole('textbox',{name:'Policy text'});await setExternalOwnership()
 const compare=await screen.findByRole('button',{name:'Compare latest saved draft'})
 expect((await createPolicyPendingJournal('source-status','policy-one')).read()).toBeUndefined()
 expect(screen.queryByRole('button',{name:'Check operation result'})).toBeNull();expect(app.writes).toHaveLength(1)
 app.setReadFailure(false);fireEvent.click(compare)
 await screen.findByRole('button',{name:'Load latest saved draft'})
 expect(screen.getByRole('textbox',{name:'Policy text'})).toHaveProperty('value','Expense policy')
})
it('preserves the editor if a later saved head differs from the source action receipt',async()=>{
 const receiptHead=policy('ownership-head');receiptHead.document.managementMode='externally-managed'
 const later=policy('later-head','Later editor text');later.document.managementMode='externally-managed'
 const app=mount({authorityMutate:async operation=>{app.setCurrent(later);return ownershipOutcome(operation,receiptHead)}})
 await screen.findByRole('textbox',{name:'Policy text'});await setExternalOwnership();await screen.findByRole('button',{name:'Compare latest saved draft'})
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Draft'}),{button:0,ctrlKey:false})
 expect(screen.getByRole('textbox',{name:'Policy text'})).toHaveProperty('value','Expense policy')
 expect(await screen.findByText('Later editor text')).toBeTruthy();expect(app.writes).toHaveLength(1)
})

it('uses canonical current publication eligibility instead of matching a historical pre-ownership publication',async()=>{
 const saved=policy('after-roundtrip','Expense policy');saved.currentCandidatePublished=false
 saved.sourceAuthority={revision:'authority-return',ownership:{mode:'desk-managed',inherited:false},externalVersionCount:0}
 saved.document.publications=[{id:'old-publication',sequence:3,candidateRevisionId:saved.document.candidate.revisionId,artifactVersion:'1.0.0',candidateDigest:saved.document.candidate.digest,workflowDigest:saved.document.workflowDigest,reviewId:'old-review',content:saved.document.candidate.content,at,actor}]
 mount({read:saved});await screen.findByRole('textbox',{name:'Policy text'})
 expect(screen.getByText('Current candidate unpublished')).toBeTruthy();expect(screen.queryByText('Current candidate published')).toBeNull()
 expect(screen.getByText('This policy version is already published. Save a new artifact version before publishing again.')).toBeTruthy()
})
it('labels earlier local publication and review as history under external ownership',async()=>{
 const saved=policy('external','Expense policy','approved');saved.document.managementMode='externally-managed';saved.currentCandidatePublished=false
 saved.document.publications=[{id:'old-publication',sequence:3,candidateRevisionId:saved.document.candidate.revisionId,artifactVersion:'1.0.0',candidateDigest:saved.document.candidate.digest,workflowDigest:saved.document.workflowDigest,reviewId:'old-review',content:saved.document.candidate.content,at,actor}]
 mount({read:saved});await screen.findByRole('textbox',{name:'Policy text'})
 expect(screen.getByText('Historical local publications are retained.')).toBeTruthy();expect(screen.getByText('Local reviews remain history under external ownership.')).toBeTruthy()
 expect(screen.queryByText('Current candidate published')).toBeNull();expect((screen.getByRole('button',{name:'Approve candidate'}) as HTMLButtonElement).disabled).toBe(true)
})

import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import {UnsavedChangesProvider,useRegisteredChanges} from '../shell/UnsavedChanges'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {VerifiedSessionProvider} from '../auth/VerifiedSession'
import {PolicyRecoverySettings} from './PolicyRecoverySettings'
import {POLICY_RECOVERY_KEY,POLICY_RECOVERY_INCLUDED,POLICY_RECOVERY_EXCLUDED,policyRecoveryQuery,type PolicyRecoveryStatus,type PolicyRecoveryPreview,type PolicyRestoreRequest,type PolicyRestoreOutcome} from './policyRecovery'
import {createPolicyRecoveryJournal} from './policyRecoveryJournal'

const file=new File(['zip-fixture'],'policy-history.zip',{type:'application/zip'}),projectId='host-project-one',at='2026-10-19T12:00:00Z'
const counts={policies:2,revisions:4,reviews:3,publications:1,operations:8,sourceObservations:2,externalVersions:1,blobs:9,blobBytes:4000}
const status:PolicyRecoveryStatus={projectId,projectPath:'/project/current',revision:'ledger-one',available:true,canInspect:true,canRestore:true,counts,limits:{archiveBytes:536870912+2097152,expandedBytes:536870912,entries:8192,manifestBytes:1048576},scope:'project-policies',included:POLICY_RECOVERY_INCLUDED,excluded:POLICY_RECOVERY_EXCLUDED}
const preview:PolicyRecoveryPreview={projectId,projectPath:status.projectPath,archiveDigest:'archive-one',archiveBytes:file.size,ledgerDigest:'ledger-archived',schemaVersion:2,archiveVersion:1,capturedAt:at,counts,scope:status.scope,included:status.included,excluded:status.excluded,destination:'/private/new-project',destinationRevision:'parent-one',authority:{currentnessUnknown:true,freshReviewRequired:true,sourceConsentRequired:true},restartRequired:true}
const completed=(request:PolicyRestoreRequest):PolicyRestoreOutcome=>({delivery:'committed',outcomeKind:'policy-recovery',projectId,receipt:{...request,kind:'policy-recovery-restore',requestDigest:'d'.repeat(64),snapshotId:'e'.repeat(64),at,actor:{kind:'local',subject:'current administrator'}},restartRequired:true,authority:preview.authority})
function OtherWork({held}:{held:boolean}){useRegisteredChanges(held,'Other editor',{kind:'autosave',saveFailed:true});return null}
function mount({read=status,restore,recovery,held=false}:{read?:PolicyRecoveryStatus;restore?:(settings:PolicyRestoreRequest)=>Promise<Response>;recovery?:()=>Promise<Response>;held?:boolean}={}){
 const requests:{url:string;init?:RequestInit;settings?:Record<string,unknown>}[]=[],cache=testQueryClient()
 let current=read,inspectReply=preview,failStatus=false
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input),settings=init?.body instanceof FormData?JSON.parse(String(init.body.get('settings'))):undefined;requests.push({url,init,settings})
  if(url==='/api/policy-recovery'){if(failStatus)throw Error('Refresh unavailable');return Response.json(current)}
  if(url==='/api/policy-recovery/archive')return new Response('exact archive',{headers:{'X-Policy-Archive-Digest':'download-digest'}})
  if(url==='/api/policy-recovery/inspect')return Response.json(inspectReply)
  if(url==='/api/policy-recovery/restore')return restore?restore(settings):Response.json(completed(settings))
  if(url.startsWith('/api/policy-recovery/operations/'))return recovery?recovery():Response.json({code:'not-found',outcome:'not-committed'},{status:404})
  throw Error('Unexpected recovery request: '+url)
 }))
 const frame=(other:boolean)=><QueryClientProvider client={cache}><VerifiedSessionProvider value={{subject:'current administrator',issuer:null}}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider><OtherWork held={other}/><PolicyRecoverySettings/></DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></VerifiedSessionProvider></QueryClientProvider>
 const view=render(frame(held))
 return {requests,cache,view,frame,setStatus:(value:PolicyRecoveryStatus)=>{current=value},setPreview:(value:PolicyRecoveryPreview)=>{inspectReply=value},failStatus:()=>{failStatus=true}}
}
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();sessionStorage.clear()})
it('never polls archive validation and disables hidden-panel verification',()=>{
 expect(policyRecoveryQuery().refetchInterval).toBe(false);expect(policyRecoveryQuery(false).enabled).toBe(false)
})
async function openRestore(){const button=await screen.findByRole('button',{name:'Restore Policy archive…'});await waitFor(()=>expect(button).toHaveProperty('disabled',false));fireEvent.click(button);return screen.findByRole('dialog',{name:'Inspect Policy recovery archive'})}
async function inspect(){const dialog=await openRestore();fireEvent.change(within(dialog).getByLabelText('Policy recovery archive'),{target:{files:[file]}});fireEvent.change(within(dialog).getByLabelText('New project destination'),{target:{value:'/private/new-project'}});fireEvent.click(within(dialog).getByRole('button',{name:'Inspect archive'}));return screen.findByRole('dialog',{name:'Review Policy recovery'})}
function confirm(dialog:HTMLElement){fireEvent.change(within(dialog).getByLabelText('Type Yes to confirm.'),{target:{value:'Yes'}});fireEvent.click(within(dialog).getByRole('button',{name:'Restore into new project'}))}

it('keeps Policy-only scope separate and performs only reads until an explicit action',async()=>{
 const app=mount();await screen.findByText('/project/current')
 expect(screen.getByText(/Excludes chats, Pack and Graph drafts/)).toBeTruthy();expect(screen.getByText(/Archives are unencrypted/)).toBeTruthy()
 expect(app.requests.every(r=>!r.init?.method||r.init.method==='GET')).toBe(true)
})
it('allows independent recovery when the current Policy export and counts are unavailable',async()=>{
 mount({read:{...status,available:false,counts:undefined,revision:undefined,reason:'archive-integrity'}})
 expect(await screen.findByRole('button',{name:'Download Policy archive…'})).toHaveProperty('disabled',true)
 expect(screen.getByText(/A valid archive can still be inspected/)).toBeTruthy();expect(screen.queryByText('0 Policies')).toBeNull();await openRestore()
})
it('reviews download scope and freezes the export revision before its read-only request',async()=>{
 const click=vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(()=>{}),NativeURL=URL
 vi.stubGlobal('URL',class extends NativeURL{static createObjectURL=()=> 'blob:archive';static revokeObjectURL=vi.fn()})
 const app=mount();const opener=await screen.findByRole('button',{name:'Download Policy archive…'});await waitFor(()=>expect(opener).toHaveProperty('disabled',false));fireEvent.click(opener)
 const dialog=await screen.findByRole('dialog',{name:'Review Policy archive download'});expect(app.requests.filter(r=>r.url.endsWith('/archive'))).toHaveLength(0)
 fireEvent.click(within(dialog).getByRole('button',{name:'Download Policy archive'}));await screen.findByText('Policy archive download started.')
 expect(click).toHaveBeenCalledOnce();expect(app.requests.find(r=>r.url.endsWith('/archive'))?.init?.headers).toMatchObject({'If-Match':'ledger-one',Authorization:expect.stringMatching(/^Bearer /)})
 expect(app.requests.every(r=>!r.init?.method||r.init.method==='GET')).toBe(true)
})
it('inspects without restoring and freezes exact archive/destination identity for explicit confirmation',async()=>{
 const app=mount(),dialog=await inspect();expect(app.requests.filter(r=>r.url.endsWith('/restore'))).toHaveLength(0)
 const call=app.requests.find(r=>r.url.endsWith('/inspect'))!;expect(call.settings).toEqual({destination:'/private/new-project'});expect([...(call.init!.body as FormData).keys()]).toEqual(['settings','archive'])
 expect(within(dialog).getByText('/private/new-project')).toBeTruthy();expect(within(dialog).getByRole('button',{name:'Restore into new project'})).toHaveProperty('disabled',true)
 expect(within(dialog).getByText('archive-one').closest('details')).toHaveProperty('open',false)
 // A running source poll changes export revision, not this independent restore.
 await act(()=>app.cache.setQueryData(POLICY_RECOVERY_KEY,{...status,revision:'later-poll'}))
 confirm(dialog);await screen.findByText('Policy recovery completed in a new inactive project folder.')
 const restore=app.requests.find(r=>r.url.endsWith('/restore'))!;expect(restore.settings).toEqual({operationId:expect.any(String),projectId,archiveDigest:'archive-one',archiveBytes:file.size,ledgerDigest:'ledger-archived',destination:'/private/new-project',destinationRevision:'parent-one'})
 expect(screen.getByText(/Your current project remains open/)).toBeTruthy();expect(window.location.pathname).not.toBe('/private/new-project')
 expect(await (await createPolicyRecoveryJournal(projectId)).read()).toBeUndefined()
})
it('blocks another pending editor during an open review without counting its own file and confirmation',async()=>{
 const app=mount(),dialog=await inspect();fireEvent.change(within(dialog).getByLabelText('Type Yes to confirm.'),{target:{value:'Yes'}})
 expect(within(dialog).getByRole('button',{name:'Restore into new project'})).toHaveProperty('disabled',false)
 app.view.rerender(app.frame(true));expect(within(dialog).getByRole('button',{name:'Restore into new project'})).toHaveProperty('disabled',true)
 fireEvent.click(within(dialog).getByRole('button',{name:'Restore into new project'}));expect(app.requests.filter(r=>r.url.endsWith('/restore'))).toHaveLength(0)
})
it('preserves file/path and requires reinspection after a proven noncommitted destination refusal',async()=>{
 const app=mount({restore:async()=>Response.json({code:'destination-conflict',error:'private diagnostic',outcome:'not-committed'},{status:409})});confirm(await inspect())
 await screen.findByText('The destination is no longer available or its parent changed. Choose a new destination and inspect again.')
 expect(screen.getByText('Selected archive: policy-history.zip')).toBeTruthy();expect(screen.getByLabelText('New project destination')).toHaveProperty('value','/private/new-project');expect(screen.queryByText('private diagnostic')).toBeNull()
 expect((await createPolicyRecoveryJournal(projectId)).read()).toBeUndefined();expect(app.requests.filter(r=>r.url.endsWith('/restore'))).toHaveLength(1)
})
it('recovers a lost restore reply after remount using only the exact receipt GET without file reupload',async()=>{
 const first=mount({restore:async()=>{throw Error('lost response')}});confirm(await inspect());await screen.findAllByRole('button',{name:'Check restore result'})
 const sent=first.requests.find(r=>r.url.endsWith('/restore'))!.settings as unknown as PolicyRestoreRequest,journal=await createPolicyRecoveryJournal(projectId);expect(journal.read()).toEqual(sent)
 cleanup();const second=mount({recovery:async()=>Response.json({...completed(sent),delivery:'recovered'})});fireEvent.click(await screen.findByRole('button',{name:'Check restore result'}))
 await screen.findByText('Policy recovery completed in a new inactive project folder.');expect(journal.read()).toBeUndefined()
 expect(second.requests.every(r=>!r.init?.method||r.init.method==='GET')).toBe(true);expect(second.requests.some(r=>r.url.endsWith('/operations/'+sent.operationId))).toBe(true)
})
it.each(['wrong-family','wrong-destination','not-found','unknown-error','wrong-actor','malformed-actor','malformed-time','malformed-snapshot','malformed-request-digest'] as const)('keeps %s recovery unresolved and never repeats restore',async kind=>{
 const first=mount({restore:async()=>{throw Error('lost')}});confirm(await inspect());await screen.findAllByRole('button',{name:'Check restore result'});const sent=first.requests.find(r=>r.url.endsWith('/restore'))!.settings as unknown as PolicyRestoreRequest
 cleanup();const second=mount({recovery:async()=>{if(kind==='not-found')return Response.json({code:'not-found'},{status:404});if(kind==='unknown-error')return Response.json({code:'storage-unavailable',outcome:'unknown'},{status:503});const value=completed(sent);if(kind==='wrong-family')return Response.json({...value,outcomeKind:'lifecycle'});const patches={ 'wrong-destination':{destination:'/other'},'wrong-actor':{actor:{kind:'local',subject:'another administrator'}},'malformed-actor':{actor:{kind:'oidc',subject:123,issuer:''}},'malformed-time':{at:'2026-02-30T12:00:00Z'},'malformed-snapshot':{snapshotId:'not-a-digest'},'malformed-request-digest':{requestDigest:'A'.repeat(64)}};return Response.json({...value,receipt:{...value.receipt,...patches[kind]}})}})
 fireEvent.click(await screen.findByRole('button',{name:'Check restore result'}));await screen.findByRole('alert');expect((await createPolicyRecoveryJournal(projectId)).read()).toEqual(sent);expect(second.requests.every(r=>!r.init?.method||r.init.method==='GET')).toBe(true)
})
it('refuses to dispatch restore if its metadata journal cannot be saved',async()=>{
 const app=mount(),dialog=await inspect();vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw Error('Storage denied')});confirm(dialog)
 await screen.findByRole('alert');expect(app.requests.filter(r=>r.url.endsWith('/restore'))).toHaveLength(0)
})
it('preserves a confirmed outcome when clearing the browser journal fails',async()=>{
 const app=mount(),dialog=await inspect();vi.spyOn(Storage.prototype,'removeItem').mockImplementation(()=>{throw Error('Storage denied')});confirm(dialog)
 await screen.findByText('Policy recovery completed in a new inactive project folder.');expect(await screen.findByRole('alert')).toHaveProperty('textContent','Restore was confirmed, but this browser could not clear its recovery record. Checking remains read-only; do not repeat restore.')
 expect(screen.getByText(/The restore is confirmed. This browser still holds/)).toBeTruthy();expect(app.requests.filter(r=>r.url.endsWith('/restore'))).toHaveLength(1)
})
it('does not reuse a pending journal in another project or current session, or store archive bytes/tokens',async()=>{
 giveThisPageASessionForTesting('session-one');const first=await createPolicyRecoveryJournal(projectId),request={operationId:'operation-one',projectId,archiveDigest:'digest',archiveBytes:20,ledgerDigest:'ledger',destination:'/new',destinationRevision:'parent'};first.store(request)
 expect((await createPolicyRecoveryJournal('another-project')).read()).toBeUndefined();giveThisPageASessionForTesting('session-two');expect((await createPolicyRecoveryJournal(projectId)).read()).toBeUndefined()
 const serialized=Object.keys(sessionStorage).map(key=>key+sessionStorage.getItem(key)).join('');expect(serialized).not.toContain('session-one');expect(serialized).not.toContain('zip-fixture');expect(serialized).not.toContain('Bearer')
})

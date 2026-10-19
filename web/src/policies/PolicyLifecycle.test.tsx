import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider,useQuery} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import {PolicyLifecycle} from './PolicyLifecycle'
import {policyQuery,type PolicyReply,type PolicyLifecycle as Lifecycle,type PolicyLifecycleOutcome,type PolicyWrite} from './client'
import {createPolicyPendingJournal} from './pendingOperation'

const at='2030-01-01T12:00:00Z',actor={kind:'local',subject:'administrator'}
const candidate={title:'Policy',narrative:'Retained policy',artifactVersion:'1.0.0',digest:'candidate-one',revisionId:'revision-one',content:{sha256:'candidate-one',bytes:100}},workflow={independentReviewRequired:false}
const publication={id:'publication-one',sequence:3,candidateRevisionId:candidate.revisionId,at,actor,artifactVersion:'1.0.0',candidateDigest:candidate.digest,workflowDigest:'workflow-one',reviewId:'review-one',content:candidate.content}
const policy:PolicyReply={revision:'policy-revision',document:{id:'policy-one',managementMode:'desk-managed',createdAt:at,updatedAt:at,candidate,workflow,workflowDigest:'workflow-one',reviewState:'approved',currentReviewId:'review-one',revisions:[],reviews:[],publications:[publication]}}
const initial:Lifecycle={revision:'lifecycle-none',policyRevision:policy.revision,observedAt:at,releases:[{publicationId:publication.id,status:'published'}],events:[]}
function outcome(operation:PolicyWrite,state:Lifecycle=initial):PolicyLifecycleOutcome{
 const event={id:'lifecycle-one',sequence:1,action:operation.body.action as 'activate'|'withdraw',publicationId:publication.id,candidateDigest:candidate.digest,at,actor,provenance:'admin-configured' as const,...(operation.body.action==='activate'?{effectiveAt:at}:{rationale:String(operation.body.rationale)})}
 const lifecycle={...state,revision:'lifecycle-one',events:[event],releases:[{publicationId:publication.id,status:operation.body.action==='activate'?'effective' as const:'withdrawn' as const,activationId:operation.body.action==='activate'?event.id:undefined}]}
 return {outcomeKind:'lifecycle',delivery:'committed',policyId:policy.document.id,policyRevision:policy.revision,lifecycle,receipt:{operationId:operation.body.operationId,kind:'lifecycle-'+operation.body.action,policyId:policy.document.id,publicationId:publication.id,lifecycleRevision:lifecycle.revision,lifecycleEventId:event.id}}
}
function DraftProbe(){const query=useQuery(policyQuery(policy.document.id));return query.error?<p>Draft unavailable</p>:null}
function mount({editing=false,mutate,existing=initial,draftUnavailable=false,external=false}:{editing?:boolean;mutate?:(operation:PolicyWrite)=>Promise<PolicyLifecycleOutcome>;existing?:Lifecycle;draftUnavailable?:boolean;external?:boolean}={}){
 let current=existing,recovery:PolicyLifecycleOutcome|undefined
 const writes:PolicyWrite[]=[],requests:string[]=[],cache=testQueryClient(),onPendingChange=vi.fn()
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input);requests.push(url)
  if(url.startsWith('/api/policy-operations/'))return recovery?Response.json({...recovery,delivery:'recovered'}):Response.json({code:'not-found'},{status:404})
  if(url==='/api/policies/policy-one/lifecycle'){
   if(init?.method==='POST'){
    const operation:PolicyWrite={path:'/policy-one/lifecycle',method:'POST',body:JSON.parse(String(init.body))};writes.push(operation)
    const result=mutate?await mutate(operation):outcome(operation);current=result.lifecycle!;return Response.json(result)
   }
   return Response.json(current)
  }
  if(url==='/api/policies/policy-one'){if(draftUnavailable)throw new Error('unrelated draft content unavailable');return Response.json(policy)}
  if(url==='/api/policies')return Response.json({items:[]})
  throw new Error('unexpected fixture request')
 }))
 const view=render(<QueryClientProvider client={cache}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider>{draftUnavailable&&<DraftProbe/>}<PolicyLifecycle policy={external?{...policy,document:{...policy.document,managementMode:'externally-managed'}}:policy} active editing={editing} disabled={false} onPendingChange={onPendingChange}/></DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)
 return {writes,requests,cache,view,onPendingChange,setRecovery:(value:PolicyLifecycleOutcome)=>{recovery=value;current=value.lifecycle}}
}
afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear()})

it('uses canonical activation eligibility for restored publications and preserves historical withdrawal',async()=>{
 const app=mount({existing:{...initial,recovery:{snapshotId:'restored-one',restoredAt:at,currentnessUnknown:true,freshReviewRequired:false,sourceConsentRequired:false},releases:[{...initial.releases[0]!,activationAllowed:false}]}})
 expect(await screen.findByRole('button',{name:'Activate release'})).toHaveProperty('disabled',true);expect(screen.getByRole('button',{name:'Withdraw release'})).toHaveProperty('disabled',false)
 expect(screen.getByText(/Imported lifecycle schedules are historical assertions/)).toBeTruthy();expect(app.writes).toEqual([])
 await act(()=>app.cache.setQueryData(['policies','policy-one','lifecycle'],{...initial,recovery:{snapshotId:'restored-one',restoredAt:at,currentnessUnknown:true,freshReviewRequired:false,sourceConsentRequired:false},releases:[{...initial.releases[0]!,activationAllowed:true}]}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'Activate release'})).toHaveProperty('disabled',false))
})

it('keeps a legacy publication inactive until explicit reviewed activation',async()=>{
 const app=mount()
 expect(await screen.findByText('No activation recorded')).toBeTruthy()
 expect(app.writes).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Activate release'}))
 const dialog=screen.getByRole('dialog',{name:'Activate this release?'})
 expect(within(dialog).getByText(candidate.digest)).toBeTruthy()
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Effective time (UTC)'}),{target:{value:'2030-01-01T13:00:00Z'}})
 fireEvent.click(within(dialog).getByRole('button',{name:'Activate release'}))
 await waitFor(()=>expect(app.writes).toHaveLength(1))
 expect(app.writes[0]!.body).toMatchObject({baseRevision:policy.revision,baseLifecycleRevision:initial.revision,publicationId:publication.id,effectiveAt:'2030-01-01T13:00:00Z'})
 await screen.findByText(/^Activation recorded/)
 expect(app.requests.filter(path=>path==='/api/policies/policy-one/lifecycle').length).toBeGreaterThan(1)
})
it('requires a withdrawal rationale and never offers withdrawal reversal',async()=>{
 const app=mount();fireEvent.click(await screen.findByRole('button',{name:'Withdraw release'}))
 const dialog=screen.getByRole('dialog',{name:'Withdraw this release?'}),confirm=within(dialog).getByRole('button',{name:'Withdraw release'})
 expect((confirm as HTMLButtonElement).disabled).toBe(true)
 fireEvent.change(within(dialog).getByRole('textbox',{name:'Withdrawal rationale'}),{target:{value:'Retire the exact release'}})
 fireEvent.click(confirm)
 await waitFor(()=>expect(app.writes).toHaveLength(1))
 await screen.findByText('Withdrawn')
 expect(app.writes[0]!.body.rationale).toBe('Retire the exact release')
 expect(screen.queryByRole('button',{name:'Activate release'})).toBeNull()
})
it('freezes the reviewed lifecycle and refuses stale confirmation',async()=>{
 const app=mount();fireEvent.click(await screen.findByRole('button',{name:'Activate release'}))
 const dialog=screen.getByRole('dialog',{name:'Activate this release?'})
 await waitFor(()=>expect(app.cache.getQueryState(['policies','policy-one','lifecycle'])?.fetchStatus).toBe('idle'))
 await act(()=>app.cache.setQueryData(['policies','policy-one','lifecycle'],{...initial,revision:'new-lifecycle'}))
 await waitFor(()=>expect(within(dialog).getByRole('alert').textContent).toContain('The policy or lifecycle changed.'))
 fireEvent.click(within(dialog).getByRole('button',{name:'Activate release'}))
 expect(app.writes).toHaveLength(0)
})
it('journals an uncertain activation and recovers after remount without another write',async()=>{
 const first=mount({mutate:async()=>{throw new Error('lost reply')}})
 fireEvent.click(await screen.findByRole('button',{name:'Activate release'}))
 fireEvent.click(within(screen.getByRole('dialog')).getByRole('button',{name:'Activate release'}))
 await screen.findByRole('button',{name:'Check operation result'})
 expect(first.writes).toHaveLength(1)
 const journal=await createPolicyPendingJournal('lifecycle','policy-one'),pending=sessionStorage.getItem(journal.key)
 expect(JSON.parse(pending!).body.publicationId).toBe(publication.id)
 cleanup()
 const second=mount();second.setRecovery(outcome(first.writes[0]!))
 fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}))
 await waitFor(()=>expect(sessionStorage.getItem(journal.key)).toBeNull())
 expect(second.writes).toHaveLength(0)
 expect(second.requests.some(path=>path.startsWith('/api/policy-operations/'))).toBe(true)
})
it('uses the shared dirty-editor guard for lifecycle decisions',async()=>{
 const app=mount({editing:true})
 const activate=await screen.findByRole('button',{name:'Activate release'})
 expect((activate as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Withdraw release'}) as HTMLButtonElement).disabled).toBe(true)
 expect(app.writes).toHaveLength(0)
})
it('blocks local activation under external ownership while preserving restrictive historical withdrawal',async()=>{
 const app=mount({external:true});const activate=await screen.findByRole('button',{name:'Activate release'})
 expect((activate as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Withdraw release'}) as HTMLButtonElement).disabled).toBe(false)
 fireEvent.click(activate);expect(app.writes).toEqual([])
})
it('keeps a missing recovery unresolved and never dispatches a replacement operation',async()=>{
 mount({mutate:async()=>{throw new Error('lost reply')}})
 fireEvent.click(await screen.findByRole('button',{name:'Activate release'}))
 fireEvent.click(within(screen.getByRole('dialog')).getByRole('button',{name:'Activate release'}))
 await screen.findByRole('button',{name:'Check operation result'})
 cleanup()
 const second=mount()
 fireEvent.click(await screen.findByRole('button',{name:'Check operation result'}))
 await screen.findByText('No completed operation was found yet. Its result is still unresolved. Check again before continuing.')
 expect((await createPolicyPendingJournal('lifecycle','policy-one')).read()).toBeDefined()
 expect(second.writes).toHaveLength(0)
 expect((screen.getByRole('button',{name:'Activate release'}) as HTMLButtonElement).disabled).toBe(true)
})
it('acknowledges a lifecycle commit while unrelated draft content remains unavailable',async()=>{
 const app=mount({draftUnavailable:true})
 await screen.findByText('Draft unavailable')
 fireEvent.click(await screen.findByRole('button',{name:'Activate release'}))
 fireEvent.click(within(screen.getByRole('dialog')).getByRole('button',{name:'Activate release'}))
 await screen.findByText('Effective')
 const journal=await createPolicyPendingJournal('lifecycle','policy-one')
 await waitFor(()=>expect(journal.read()).toBeUndefined())
 expect(screen.getByText('Draft unavailable')).toBeTruthy()
 expect(app.writes).toHaveLength(1)
})

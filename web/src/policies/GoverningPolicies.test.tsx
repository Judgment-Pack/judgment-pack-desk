import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {Link,MemoryRouter,RouterProvider,createMemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {DeskNoticesProvider} from '../shell/DeskNotices'
import {adoptionFixtures,adoptionId,adoptionOutcome,adoptionRaw,adoptionSummary} from '../testing/policyAdoption'
import type {AdoptionCommand,AdoptionOptions,AdoptionOutcome,AdoptionReply} from './adoption'
import {ADOPTIONS_KEY} from './adoptionClient'
import {createAdoptionPendingJournal} from './adoptionPending'
import {GoverningPolicies} from './GoverningPolicies'

afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear()})
async function mount({mutate,existing=false,initial,recovered,optionsWait,optionsFailure=false,disabled=false,content=adoptionRaw,navigation=false}:{mutate?:(command:AdoptionCommand,reply:AdoptionReply)=>Promise<AdoptionOutcome|Response>;existing?:boolean;initial?:AdoptionReply;recovered?:AdoptionOutcome;optionsWait?:Promise<void>;optionsFailure?:boolean;disabled?:boolean;content?:string;navigation?:boolean}={}){
 const fixture=await adoptionFixtures(),cache=testQueryClient(),writes:AdoptionCommand[]=[],requests:{path:string;method:string}[]=[]
 let current=initial??fixture.reply,receipt=recovered,options:AdoptionOptions=fixture.options,failed=false,hasRecord=existing
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const path=String(input),method=init?.method??'GET';requests.push({path,method})
  if(path.startsWith('/api/policy-adoptions/options?')){await optionsWait;if(optionsFailure||failed)throw Error('options unavailable');return Response.json({...options,packPublications:options.packPublications.map(pack=>({...pack,alias:'Eligibility'}))})}
  if(path.startsWith('/api/policy-operations/'))return receipt?Response.json({...receipt,delivery:'recovered'}):Response.json({code:'not-found',outcome:'not-committed'},{status:404})
  if(path==='/api/policy-adoptions'){
   if(method==='POST'){const command=JSON.parse(String(init?.body)) as AdoptionCommand;writes.push(command);const result=mutate?await mutate(command,current):adoptionOutcome(command,current);if(result instanceof Response)return result;current=result.adoption;receipt=result;hasRecord=true;return Response.json(result)}
   if(failed)throw Error('history unavailable');return Response.json({items:hasRecord?[adoptionSummary(current)]:[]})
  }
  if(path==='/api/policy-adoptions/'+adoptionId){if(failed)throw Error('record unavailable');return Response.json(current)}
  throw Error('Unexpected adoption fixture '+path)
 }))
 const tree=(nextContent:string=content,nextDisabled=disabled)=>{const child=<UnsavedChangesProvider><DeskNoticesProvider><GoverningPolicies owner="Eligibility" content={nextContent} disabled={nextDisabled}/>{navigation&&<Link to="?view=overview">Overview fixture</Link>}</DeskNoticesProvider></UnsavedChangesProvider>;return <QueryClientProvider client={cache}>{navigation?<RouterProvider router={createMemoryRouter([{path:'/packs/Eligibility',element:child}],{initialEntries:['/packs/Eligibility?view=review']})}/>:<MemoryRouter>{child}</MemoryRouter>}</QueryClientProvider>}
 const view=render(tree())
 return {...fixture,view,cache,writes,requests,rerender:(nextContent:string,nextDisabled=disabled)=>view.rerender(tree(nextContent,nextDisabled)),setCurrent:(reply:AdoptionReply)=>{current=reply},setOptions:(next:AdoptionOptions)=>{options=next},setFailed:(value:boolean)=>{failed=value},setRecovery:(result:AdoptionOutcome)=>{receipt=result;current=result.adoption;hasRecord=true}}
}
async function selectPack(){const select=await screen.findByRole('combobox',{name:'Governing Pack publication'});fireEvent.keyDown(select,{key:'Enter'});fireEvent.click(await screen.findByRole('option',{name:/1\.0\.0/}));await screen.findByRole('checkbox',{name:/Eligibility policy/})}
async function selectPolicies(){await selectPack();fireEvent.click(screen.getByRole('checkbox',{name:/Eligibility policy/}));fireEvent.click(screen.getByRole('checkbox',{name:/Reporting policy/}));await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare adoption candidate'})).toHaveProperty('disabled',false))}
async function openHistory(){fireEvent.click(screen.getByText('Governing-policy history',{exact:true}));fireEvent.click(await screen.findByRole('button',{name:'Open adoption'}));await screen.findByRole('textbox',{name:'Adoption review comment'});await waitFor(()=>expect(screen.getByRole('button',{name:'Adopt complete policy set'})).toHaveProperty('disabled',false))}

it('prepares all exact selected dependencies and requires a separate explicit whole-set adoption',async()=>{
 const app=await mount();await selectPolicies();expect(app.requests.every(row=>row.method==='GET')).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Prepare adoption candidate'}));await screen.findByText('Adoption candidate prepared. Review the complete saved dependency set before adopting it.')
 expect(app.writes).toHaveLength(1);expect(app.writes[0]).toMatchObject({action:'prepare',pack:app.pack,coverages:app.choices.map(({policyId,coverageId,revision,candidateId,reviewId})=>({policyId,coverageId,revision,candidateId,reviewId}))});expect((app.writes[0] as any).pack.alias).toBeUndefined()
 expect(screen.getByText('Exact retained adoption basis')).toBeTruthy();expect(screen.getByText(/It does not yet protect runs/)).toBeTruthy()
 fireEvent.change(screen.getByRole('textbox',{name:'Adoption review comment'}),{target:{value:'Reviewed every retained dependency together.'}})
 fireEvent.click(screen.getByRole('button',{name:'Adopt complete policy set'}));await screen.findByText('Governing-policy adoption recorded. Run protection is not connected yet.')
 expect(app.writes).toHaveLength(2);expect(app.writes[1]).toMatchObject({action:'review',adoptionId,baseRevision:app.reply.revision,candidateDigest:app.reply.document.candidate.digest,basisDigest:app.reply.document.candidate.basisDigest,decision:'approve'});expect(app.writes[1]).not.toHaveProperty('coverages');expect(app.writes[1]).not.toHaveProperty('pack')
 expect((await createAdoptionPendingJournal('Eligibility')).read()).toBeUndefined()
})
it('preserves selected snapshots when publication choices change and never polls preparation',async()=>{
 const app=await mount();await selectPolicies();app.setOptions({...app.options,coverages:app.choices.map(row=>({...row,revision:'0'.repeat(64)}))})
 await act(()=>app.cache.invalidateQueries({queryKey:[...ADOPTIONS_KEY,'options','Eligibility']}))
 expect(await screen.findByText(/Selected coverage changed or is no longer available/)).toBeTruthy();expect(screen.getByRole('button',{name:'Prepare adoption candidate'})).toHaveProperty('disabled',true)
 fireEvent.click(screen.getByText('Selected complete dependency set'));expect(screen.getAllByText(/"revision": "3333/).length).toBeGreaterThan(0);expect(app.writes).toHaveLength(0)
})
it('keeps frozen basis and review comment on changed CAS and on failed cached refresh',async()=>{
 const app=await mount({existing:true});await openHistory();fireEvent.change(screen.getByRole('textbox',{name:'Adoption review comment'}),{target:{value:'Keep my exact review comment'}})
 await act(()=>app.cache.setQueryData([...ADOPTIONS_KEY,adoptionId],{...app.reply,revision:'0'.repeat(64)}))
 expect(await screen.findByText(/The saved adoption changed/)).toBeTruthy();expect(screen.getByRole('textbox',{name:'Adoption review comment'})).toHaveProperty('value','Keep my exact review comment');expect(screen.getByRole('button',{name:'Adopt complete policy set'})).toHaveProperty('disabled',true)
 app.setFailed(true);await act(()=>app.cache.invalidateQueries({queryKey:ADOPTIONS_KEY}));expect(screen.getByRole('table',{name:'Saved governing-policy adoptions'})).toBeTruthy();expect(app.writes).toHaveLength(0)
})
it('disables approval when the complete live basis becomes stale without rewriting retained history',async()=>{
 const app=await mount({existing:true});await openHistory();await act(()=>app.cache.setQueryData([...ADOPTIONS_KEY,adoptionId],{...app.reply,readiness:{status:'stale'}}))
 expect(await screen.findByText(/The complete saved adoption basis is stale or unavailable/)).toBeTruthy();expect(screen.getByRole('button',{name:'Adopt complete policy set'})).toHaveProperty('disabled',true);expect(screen.getByRole('button',{name:'Request adoption changes'})).toHaveProperty('disabled',false)
 fireEvent.click(screen.getByRole('button',{name:'Request adoption changes'}));await screen.findByText('Adoption changes requested.');expect(app.writes[0]).toMatchObject({decision:'request-changes'})
})
it('shows unsupported Runtime honestly and preserves readable saved history',async()=>{
 const app=await mount({existing:true,mutate:async()=>Response.json({code:'unsupported-contract',outcome:'not-committed'},{status:422})});await selectPolicies();fireEvent.click(screen.getByRole('button',{name:'Prepare adoption candidate'}))
 expect(await screen.findByText('Adoption capability unavailable')).toBeTruthy();expect(screen.getByRole('checkbox',{name:/Eligibility policy/})).toHaveProperty('checked',true);expect((await createAdoptionPendingJournal('Eligibility')).read()).toBeUndefined();fireEvent.click(screen.getByText('Governing-policy history'));expect(await screen.findByRole('button',{name:'Open adoption'})).toBeTruthy();expect(app.writes).toHaveLength(1)
})
it('recovers lost preparation after reload and a Pack content change using only a read receipt',async()=>{
 const first=await mount({mutate:async()=>{throw Error('lost response')}});await selectPolicies();fireEvent.click(screen.getByRole('button',{name:'Prepare adoption candidate'}));await screen.findByRole('button',{name:'Check adoption operation result'})
 const command=first.writes[0]!,receipt=adoptionOutcome(command,first.reply),journal=await createAdoptionPendingJournal('Eligibility');expect(journal.read()?.command.operationId).toBe(command.operationId);cleanup()
 const second=await mount({content:adoptionRaw+' ',recovered:receipt});fireEvent.click(await screen.findByRole('button',{name:'Check adoption operation result'}));await screen.findByText(/This adoption belongs to an earlier Pack publication/)
 expect(screen.getByRole('button',{name:'Adopt complete policy set'})).toHaveProperty('disabled',true);expect(screen.getByRole('button',{name:'Request adoption changes'})).toHaveProperty('disabled',true);expect(second.writes).toHaveLength(0);expect(journal.read()).toBeUndefined();expect(second.requests.some(row=>row.path.startsWith('/api/policy-operations/'))).toBe(true)
})
it('keeps recovery pending when a response omits one chosen dependency',async()=>{
 const app=await mount({mutate:async(command,reply)=>{const result=adoptionOutcome(command,reply);result.adoption.document.candidate.dependencies.pop();return result}});await selectPolicies();fireEvent.click(screen.getByRole('button',{name:'Prepare adoption candidate'}));await screen.findByRole('button',{name:'Check adoption operation result'})
 expect((await createAdoptionPendingJournal('Eligibility')).read()).toBeTruthy();expect(screen.getByRole('button',{name:'Prepare adoption candidate'})).toHaveProperty('disabled',true);expect(app.writes).toHaveLength(1)
})
it('recovers a lost adoption review after reload without sending another review',async()=>{
 const first=await mount({existing:true,mutate:async()=>{throw Error('review response interrupted')}});await openHistory();fireEvent.change(screen.getByRole('textbox',{name:'Adoption review comment'}),{target:{value:'Reviewed the exact whole set.'}});fireEvent.click(screen.getByRole('button',{name:'Adopt complete policy set'}));await screen.findByRole('button',{name:'Check adoption operation result'})
 const command=first.writes[0]!,receipt=adoptionOutcome(command,first.reply);cleanup()
 const second=await mount({existing:true,initial:receipt.adoption,recovered:receipt});expect(second.writes).toHaveLength(0);fireEvent.click(await screen.findByRole('button',{name:'Check adoption operation result'}));await screen.findByText('Governing-policy adoption recorded. Run protection is not connected yet.');expect(second.writes).toHaveLength(0);fireEvent.click(screen.getByText('Recorded adoption decisions'));expect(screen.getByText(/Reviewed the exact whole set/)).toBeTruthy()
})
it('distinguishes initial loading, unavailable choices and empty publication results',async()=>{
 let release!:()=>void;const waiting=new Promise<void>(resolve=>{release=resolve});await mount({optionsWait:waiting,optionsFailure:true});expect(screen.getByText('Loading governing-policy choices…')).toBeTruthy();expect(screen.queryByText(/No retained publication matches/)).toBeNull();await act(async()=>release());expect(await screen.findByText('Governing-policy choices unavailable')).toBeTruthy();expect(screen.queryByText('Loading governing-policy choices…')).toBeNull();expect(screen.queryByText(/No retained publication matches/)).toBeNull()
})
it('refuses another publication substituted during an exact historical read',async()=>{
 const app=await mount({existing:true});await screen.findByRole('combobox',{name:'Governing Pack publication'});fireEvent.click(screen.getByText('Governing-policy history'));await screen.findByRole('button',{name:'Open adoption'})
 app.setCurrent({...app.reply,document:{...app.reply.document,candidate:{...app.reply.document.candidate,pack:{...app.pack,publicationId:'0'.repeat(64)}}}});fireEvent.click(screen.getByRole('button',{name:'Open adoption'}));expect(await screen.findByRole('alert')).toBeTruthy();expect(screen.queryByRole('button',{name:'Adopt complete policy set'})).toBeNull();expect(app.writes).toHaveLength(0)
})
it('blocks manual mutations while the parent Pack has unsaved work',async()=>{
 const app=await mount({disabled:true});await screen.findByRole('combobox',{name:'Governing Pack publication'});expect(screen.getByRole('combobox',{name:'Governing Pack publication'})).toHaveProperty('disabled',true);expect(screen.getByText(/Save or resolve the Pack editor/)).toBeTruthy();expect(app.writes).toHaveLength(0)
})
it('protects unsaved selection when opening saved history',async()=>{
 await mount({existing:true});await selectPolicies();fireEvent.click(screen.getByText('Governing-policy history'));fireEvent.click(await screen.findByRole('button',{name:'Open adoption'}));const dialog=await screen.findByRole('dialog',{name:'Unsaved changes'});fireEvent.click(within(dialog).getByRole('button',{name:'Keep editing'}));expect(screen.getByRole('checkbox',{name:/Eligibility policy/})).toHaveProperty('checked',true)
})
it('guards query-only tab navigation that would unmount an unsaved adoption selection',async()=>{
 await mount({navigation:true});await selectPolicies();fireEvent.click(screen.getByRole('link',{name:'Overview fixture'}));const dialog=await screen.findByRole('dialog',{name:'Unsaved changes'});expect(within(dialog).getByText(/Leaving will discard unsaved governing-policy selections/)).toBeTruthy();fireEvent.click(within(dialog).getByRole('button',{name:'Keep editing'}));expect(screen.getByRole('checkbox',{name:/Reporting policy/})).toHaveProperty('checked',true)
})
it('keeps a malformed pending journal visible, preserved and closed to new writes',async()=>{
 const journal=await createAdoptionPendingJournal('Eligibility'),raw='{"command":{"action":"review","operationId":"incomplete"}}';sessionStorage.setItem(journal.key,raw)
 const app=await mount();expect(await screen.findByRole('button',{name:'Retry pending adoption recovery'})).toBeTruthy();expect(screen.getByRole('combobox',{name:'Governing Pack publication'})).toHaveProperty('disabled',true);expect(sessionStorage.getItem(journal.key)).toBe(raw);expect(app.writes).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Retry pending adoption recovery'}));await screen.findByRole('button',{name:'Retry pending adoption recovery'});expect(sessionStorage.getItem(journal.key)).toBe(raw);expect(app.requests.every(row=>row.method==='GET')).toBe(true)
})
it('does not dispatch a write when its operation cannot be persisted',async()=>{
 const app=await mount();await selectPolicies();const original=Storage.prototype.setItem,storage=vi.spyOn(Storage.prototype,'setItem').mockImplementation(function(this:Storage,key,value){if(key.startsWith('jpack.adoption-pending.'))throw Error('storage unavailable');return original.call(this,key,value)})
 fireEvent.click(screen.getByRole('button',{name:'Prepare adoption candidate'}));expect(await screen.findByRole('button',{name:'Retry pending adoption recovery'})).toBeTruthy();expect(app.writes).toHaveLength(0);expect(screen.getByRole('button',{name:'Prepare adoption candidate'})).toHaveProperty('disabled',true);storage.mockRestore();fireEvent.click(screen.getByRole('button',{name:'Retry pending adoption recovery'}));await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare adoption candidate'})).toHaveProperty('disabled',false))
})
it('retains readable earlier adoption history after the saved Pack bytes change without a pending operation',async()=>{
 const app=await mount({existing:true,content:adoptionRaw+' '});await screen.findByRole('combobox',{name:'Governing Pack publication'});fireEvent.click(screen.getByText('Governing-policy history'));expect(await screen.findByText('Earlier Pack publication')).toBeTruthy();fireEvent.click(screen.getByRole('button',{name:'Open adoption'}));await screen.findByText(/This adoption belongs to an earlier Pack publication/)
 expect(screen.getByRole('button',{name:'Adopt complete policy set'})).toHaveProperty('disabled',true);expect(screen.getByRole('button',{name:'Request adoption changes'})).toHaveProperty('disabled',true);fireEvent.click(screen.getByText('Exact retained adoption basis'));expect(screen.getByText(app.pack.content.sha256)).toBeTruthy();expect(app.writes).toHaveLength(0)
})

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DefinitionBrief } from './DefinitionBrief'
import type { Reply, Snapshot } from './model'
import type { RunSnapshot } from '../assistant/serverRuns/types'
import { deskFetch } from '../files/client'
const fixture = vi.hoisted(() => ({generate:vi.fn(), reference:vi.fn(), commit:vi.fn(), events:vi.fn(), action:vi.fn(), ready:true}))
vi.mock('../packs/test-workspace/store', () => ({useTestStorage: () => ({query: {data: {sha256:'tests'}}})}))
vi.mock('../files/queries', () => ({useFileListing: () => ({data:{root:'project'}})}))
vi.mock('../config/DeskConfigProvider', () => ({useEffectiveConfig: () => ({config:{research:{}}})}))
vi.mock('../assistant/useAssistantSlot', () => ({useAssistantSlot: () => ({connectionId:'ai-brief',connectionRevision:'connection-revision',endpoint:{model:'fixture',models:['fixture']},engine:'vercel',thinking:'off'}),assistantReady: () => fixture.ready}))
vi.mock('../assistant/serverRuns/client',()=>({serverRunClient:async()=>({briefReference:fixture.reference,start:fixture.generate,commitBrief:fixture.commit,briefReceipt:async()=>structuredClone(reply),events:fixture.events,action:fixture.action,read:async(r:RunSnapshot)=>r})}))
vi.mock('../files/client', async original => ({...await original(),deskFetch:vi.fn()}))
const text={context:'Retained context',findings:'Recorded findings',uncertainty:'Open questions',nextAction:'Review the inputs'}
const content='{"title":"One","version":"1"}'
let snapshot:Snapshot,reply:Reply,commands:string[],refuse=false
beforeEach(() => {
 fixture.ready=true;fixture.generate.mockReset();fixture.reference.mockReset();fixture.commit.mockReset();fixture.events.mockReset();fixture.action.mockReset();commands=[];refuse=false
 fixture.generate.mockImplementation(async()=>run())
 fixture.reference.mockImplementation(async()=>({start:{admissionId:'brief-one',purpose:'brief',ownerId:'pack:one',revision:'a'.repeat(64),requestId:'one'}}))
 fixture.commit.mockImplementation(async()=>{reply.content.subjects['pack:one']={revisions:[{id:'one',revision:1,at:'2026-10-10T12:00:00Z',snapshot,model:'fixture',method:'ai-summary-v1',text}]};return structuredClone(reply)})
 snapshot={kind:'pack',title:'One',pack:content,record:{definition:{scope:'pack',id:'one',sha256:'digest'},tests:{cases:[]}}}
 reply={project:'project',content:{version:1,subjects:{}},snapshot}
 vi.mocked(deskFetch).mockImplementation(async (_path,init) => {
  if(init?.method==='POST') {
   const command=JSON.parse(init.body as string);commands.push(command.action)
   if(refuse) return new Response('{"error":"The saved dialects changed"}',{status:409})
   if(command.action==='begin') reply.content.subjects[command.subject]={revisions:reply.content.subjects[command.subject]?.revisions??[],pending:{token:'one',expires:new Date(Date.now()+60000).toISOString(),snapshot:command.snapshot,model:command.model}}
   if(command.action==='complete') reply.content.subjects[command.subject]={revisions:[{id:'one',revision:1,at:'2026-10-10T12:00:00Z',snapshot:reply.content.subjects[command.subject]!.pending!.snapshot,model:'fixture',method:'ai-summary-v1',text:command.text}]}
  }
  return new Response(JSON.stringify(reply),{status:200})
 })
})
afterEach(cleanup)
function run(state:RunSnapshot['state']='complete'):RunSnapshot{return {admissionId:'brief-one',id:'arun_'+ '1'.repeat(32),owner:{kind:'brief',id:'pack:one',revision:'a'.repeat(64)},requestId:'one',state,revision:1,operations:[],events:[],cursor:'1:0'}}
function draw(contentText=content,blocked?:string) {
 const cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 return render(<QueryClientProvider client={cache}><DefinitionBrief kind="pack" owner="one" title="One" content={contentText} blocked={blocked}/></QueryClientProvider>)
}
it('generates only on request, persists a frozen revision, and reopens without another model call',async()=>{
 let view=draw();await screen.findByRole('button',{name:'Generate brief'})
 expect(fixture.generate).not.toHaveBeenCalled();expect(commands).toEqual([])
 fireEvent.click(screen.getByRole('button',{name:'Generate brief'}));await screen.findByText('Retained context')
 expect(commands).toEqual(['begin']);expect(fixture.generate).toHaveBeenCalledOnce();expect(fixture.commit).toHaveBeenCalledOnce()
 expect(fixture.generate.mock.calls[0][2]).toEqual({connectionId:'ai-brief',connectionRevision:'connection-revision'})
 expect(screen.queryByText('Actual')).toBeNull()
 view.unmount();view=draw();await screen.findByText('Retained context')
 expect(fixture.generate).toHaveBeenCalledOnce();view.unmount()
})
it('shows outdated inputs and keeps the previous revision readable',async()=>{
 reply.content.subjects['pack:one']={revisions:[{id:'old',revision:1,at:'2026-10-09T12:00:00Z',snapshot:{...snapshot,pack:'{"version":"0"}'},text,model:'fixture',method:'ai-summary-v1'}]}
 draw();await screen.findByText('Retained context');expect(screen.getByText(/Dialects have changed/)).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Brief actions'}),{key:'Enter'})
 expect((await screen.findAllByRole('menuitem')).map(e=>e.textContent)).toEqual(['Regenerate brief','Revision history','Print / Save PDF'])
 expect(fixture.generate).not.toHaveBeenCalled()
})
it('refuses unsaved definitions and does not invoke a model when reservation is stale',async()=>{
 const view=draw(content+' ', 'Save changes first');await screen.findByRole('button',{name:'Generate brief'})
 expect(screen.getByRole('button',{name:'Generate brief'})).toHaveProperty('disabled',true)
 view.unmount();refuse=true;draw();await screen.findByRole('button',{name:'Generate brief'})
 fireEvent.click(screen.getByRole('button',{name:'Generate brief'}));await screen.findByText(/The saved dialects changed/)
 expect(fixture.generate).not.toHaveBeenCalled();expect(commands).toEqual(['begin'])
})
it('keeps saved revisions available after source removal without enabling regeneration',async()=>{
 reply.content.subjects['pack:one']={revisions:[{id:'old',revision:1,at:'2026-10-09T12:00:00Z',snapshot,text,model:'fixture',method:'ai-summary-v1'}]}
 reply.snapshot=undefined;reply.snapshotIssue='Definition removed'
 draw();await screen.findByText('Retained context');expect(screen.getByText('Definition removed')).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Brief actions'}),{key:'Enter'})
 await waitFor(()=>expect(screen.getByRole('menuitem',{name:'Regenerate brief'}).getAttribute('data-disabled')).not.toBeNull())
 expect(fixture.generate).not.toHaveBeenCalled()
})

it('lets the reader review the next model selection before regenerating a saved brief',async()=>{
 draw();fireEvent.click(await screen.findByRole('button',{name:'Generate brief'}))
 await screen.findByText('Retained context')
 fireEvent.keyDown(screen.getByRole('button',{name:'Brief actions'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Regenerate brief'}))
 const dialog=await screen.findByRole('dialog',{name:'Regenerate brief'})
 expect(within(dialog).getByRole('combobox',{name:'Brief model'}).textContent).toContain('fixture')
 expect(fixture.generate).toHaveBeenCalledOnce()
 fireEvent.click(within(dialog).getByRole('button',{name:'Regenerate brief'}))
 await waitFor(()=>expect(fixture.generate).toHaveBeenCalledTimes(2))
})

it('reconciles a lost start acknowledgement without issuing generation twice',async()=>{
 fixture.generate.mockRejectedValueOnce(Error('lost start acknowledgement'))
 fixture.reference.mockResolvedValueOnce({start:{admissionId:'brief-one',purpose:'brief',ownerId:'pack:one',revision:'a'.repeat(64),requestId:'one'}}).mockResolvedValue({start:{admissionId:'brief-one',purpose:'brief',ownerId:'pack:one',revision:'a'.repeat(64),requestId:'one'},run:run()})
 draw();fireEvent.click(await screen.findByRole('button',{name:'Generate brief'}));await screen.findByText('Retained context')
 expect(fixture.generate).toHaveBeenCalledOnce();expect(fixture.commit).toHaveBeenCalledOnce();expect(commands).toEqual(['begin'])
})

it('reconciles a lost save acknowledgement by the exact reservation token',async()=>{
 const commit=fixture.commit.getMockImplementation()!
 fixture.commit.mockImplementationOnce(async(...args)=>{await commit(...args);throw Error('lost save acknowledgement')})
 draw();fireEvent.click(await screen.findByRole('button',{name:'Generate brief'}));await screen.findByText('Retained context')
 expect(fixture.generate).toHaveBeenCalledOnce();expect(fixture.commit).toHaveBeenCalledOnce();expect(reply.content.subjects['pack:one'].revisions).toHaveLength(1)
})

it('reattaches to interrupted output without replay or commit and stops only its reservation',async()=>{
 reply.content.subjects['pack:one']={revisions:[],pending:{token:'one',expires:new Date(Date.now()+60000).toISOString(),snapshot,model:'fixture'}}
 fixture.reference.mockResolvedValue({start:{admissionId:'brief-one',purpose:'brief',ownerId:'pack:one',revision:'a'.repeat(64),requestId:'one'},run:run('interrupted')})
 draw();await screen.findByRole('button',{name:'Stop'})
 expect(fixture.generate).not.toHaveBeenCalled();expect(fixture.commit).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Stop'}));await waitFor(()=>expect(commands).toContain('cancel'))
 expect(fixture.action).not.toHaveBeenCalled();expect(fixture.generate).not.toHaveBeenCalled()
})

it('unsubscribes on navigation and recovers a completed server run on return',async()=>{
 let detached=false
 fixture.generate.mockResolvedValue(run('running'))
 fixture.events.mockImplementation(async function*(_observations,signal:AbortSignal){await new Promise<void>(resolve=>signal.addEventListener('abort',()=>{detached=true;resolve()},{once:true}));if(false)yield undefined})
 let view=draw();fireEvent.click(await screen.findByRole('button',{name:'Generate brief'}));await screen.findByRole('button',{name:'Stop'})
 view.unmount();await waitFor(()=>expect(detached).toBe(true));expect(fixture.action).not.toHaveBeenCalled();expect(commands).toEqual(['begin'])
 fixture.reference.mockResolvedValue({start:{admissionId:'brief-one',purpose:'brief',ownerId:'pack:one',revision:'a'.repeat(64),requestId:'one'},run:run()})
 view=draw();await screen.findByText('Retained context')
 expect(fixture.generate).toHaveBeenCalledOnce();expect(fixture.commit).toHaveBeenCalledOnce();view.unmount()
})

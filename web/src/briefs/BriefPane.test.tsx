import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DefinitionBrief } from './DefinitionBrief'
import type { Reply, Snapshot } from './model'
import { deskFetch } from '../files/client'
const fixture = vi.hoisted(() => ({generate:vi.fn(), ready:true}))
vi.mock('../packs/test-workspace/store', () => ({useTestStorage: () => ({query: {data: {sha256:'tests'}}})}))
vi.mock('../files/queries', () => ({useFileListing: () => ({data:{root:'project'}})}))
vi.mock('../config/DeskConfigProvider', () => ({useEffectiveConfig: () => ({config:{research:{}}})}))
vi.mock('../assistant/useAssistantSlot', () => ({useAssistantSlot: () => ({endpoint:{model:'fixture',models:['fixture']},engine:'vercel',thinking:'off'}),assistantReady: () => fixture.ready}))
vi.mock('../assistant/useAssistantRun', () => ({useAssistantRun: () => ({})}))
vi.mock('../assistant/useProposalGenerator', () => ({useProposalGenerator: () => fixture.generate}))
vi.mock('../files/client', async original => ({...await original(),deskFetch:vi.fn()}))
const text={context:'Retained context',findings:'Recorded findings',uncertainty:'Open questions',nextAction:'Review the inputs'}
const content='{"title":"One","version":"1"}'
let snapshot:Snapshot,reply:Reply,commands:string[],refuse=false
beforeEach(() => {
 fixture.ready=true;fixture.generate.mockReset().mockResolvedValue({document:text});commands=[];refuse=false
 snapshot={kind:'pack',title:'One',pack:content,record:{definition:{scope:'pack',id:'one',sha256:'digest'},tests:{cases:[]}}}
 reply={project:'project',content:{version:1,subjects:{}},snapshot}
 vi.mocked(deskFetch).mockImplementation(async (_path,init) => {
  if(init?.method==='POST') {
   const command=JSON.parse(init.body as string);commands.push(command.action)
   if(refuse) return new Response('{"error":"The saved inputs changed"}',{status:409})
   if(command.action==='begin') reply.content.subjects[command.subject]={revisions:reply.content.subjects[command.subject]?.revisions??[],pending:{token:'one',expires:new Date(Date.now()+60000).toISOString(),snapshot:command.snapshot,model:command.model}}
   if(command.action==='complete') reply.content.subjects[command.subject]={revisions:[{id:'one',revision:1,at:'2026-10-10T12:00:00Z',snapshot:reply.content.subjects[command.subject]!.pending!.snapshot,model:'fixture',method:'ai-summary-v1',text:command.text}]}
  }
  return new Response(JSON.stringify(reply),{status:200})
 })
})
afterEach(cleanup)
function draw(contentText=content,blocked?:string) {
 const cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 return render(<QueryClientProvider client={cache}><DefinitionBrief kind="pack" owner="one" title="One" content={contentText} blocked={blocked}/></QueryClientProvider>)
}
it('generates only on request, persists a frozen revision, and reopens without another model call',async()=>{
 let view=draw();await screen.findByRole('button',{name:'Generate brief'})
 expect(fixture.generate).not.toHaveBeenCalled();expect(commands).toEqual([])
 fireEvent.click(screen.getByRole('button',{name:'Generate brief'}));await screen.findByText('Retained context')
 expect(commands).toEqual(['begin','complete']);expect(fixture.generate).toHaveBeenCalledOnce()
 expect(screen.queryByText('Actual')).toBeNull()
 view.unmount();view=draw();await screen.findByText('Retained context')
 expect(fixture.generate).toHaveBeenCalledOnce();view.unmount()
})
it('shows outdated inputs and keeps the previous revision readable',async()=>{
 reply.content.subjects['pack:one']={revisions:[{id:'old',revision:1,at:'2026-10-09T12:00:00Z',snapshot:{...snapshot,pack:'{"version":"0"}'},text,model:'fixture',method:'ai-summary-v1'}]}
 draw();await screen.findByText('Retained context');expect(screen.getByText(/Inputs have changed/)).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('button',{name:'Brief actions'}),{key:'Enter'})
 expect((await screen.findAllByRole('menuitem')).map(e=>e.textContent)).toEqual(['Regenerate brief','Revision history','Print / Save PDF'])
 expect(fixture.generate).not.toHaveBeenCalled()
})
it('refuses unsaved definitions and does not invoke a model when reservation is stale',async()=>{
 const view=draw(content+' ', 'Save changes first');await screen.findByRole('button',{name:'Generate brief'})
 expect(screen.getByRole('button',{name:'Generate brief'})).toHaveProperty('disabled',true)
 view.unmount();refuse=true;draw();await screen.findByRole('button',{name:'Generate brief'})
 fireEvent.click(screen.getByRole('button',{name:'Generate brief'}));await screen.findByText(/The saved inputs changed/)
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

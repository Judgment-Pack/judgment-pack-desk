import {readFileSync} from 'node:fs'
import {join} from 'node:path'
import {afterEach,expect,it,vi} from 'vitest'
import {act,cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {memoryRequest as transport} from '@judgment-pack/harness/memory/client'
import {MemorySettings} from './MemorySettings'
import {MEMORY_KEY,readMemoryReply,type MemoryReply} from './client'
import {decodeSnapshot} from '../serverRuns/decode'
vi.mock('@judgment-pack/harness/memory/client',async original=>({...await original<typeof import('@judgment-pack/harness/memory/client')>(),memoryRequest:vi.fn()}))
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.resetAllMocks()})
const outputs=process.env.JPS_MEMORY_OUTPUTS
for(const native of [false,true])for(const phase of ['work','plan'])it.runIf(outputs)(`consumes actual ${native?'native':'API'} ${phase} proposals through the default review UI`,async()=>{
 const observation=JSON.parse(readFileSync(join(outputs!,`${phase}-${native}-true.json`),'utf8'))
 const run=decodeSnapshot(observation.run,{owner:observation.run.owner,requestId:observation.run.requestId}),before=await readMemoryReply(observation.Before),after=await readMemoryReply(observation.After)
 expect(run.checkpoint?.memories?.proposed).toHaveLength(1)
 expect(before.content.entries).toHaveLength(0)
 expect(before.proposals?.[0].memory).toEqual(run.checkpoint?.memories?.proposed[0])
 const proposal=before.proposals![0],receipt=after.content.reviews![0]
 vi.spyOn(crypto,'randomUUID').mockReturnValue(receipt.id as ReturnType<typeof crypto.randomUUID>)
 vi.mocked(transport).mockResolvedValueOnce(before).mockResolvedValue(after)
 const cache=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={cache}><MemorySettings/></QueryClientProvider>)
 await screen.findByText('Keep outreach concise.')
 expect(vi.mocked(transport).mock.calls.filter(call=>call[0])).toHaveLength(0)
 if(phase==='plan'){
  fireEvent.click(screen.getByRole('button',{name:'Edit'}))
  const dialog=screen.getByRole('dialog',{name:'Edit memory'})
  fireEvent.change(within(dialog).getByRole('textbox',{name:'Memory'}),{target:{value:'Keep outreach evidence explicit.'}})
  act(()=>cache.setQueryData(MEMORY_KEY,{...before,sha256:'b'.repeat(64),content:{...before.content,settings:{...before.content.settings,use:true}},proposals:[{...proposal,runRevision:proposal.runRevision+1}]}))
  await waitFor(()=>expect((screen.getByLabelText('Use saved memory') as HTMLInputElement).checked).toBe(true))
  fireEvent.click(within(dialog).getByRole('button',{name:'Remember'}))
 }else fireEvent.click(screen.getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(transport).toHaveBeenCalledWith({action:'review',review:{id:receipt.id,runId:proposal.runId,runRevision:proposal.runRevision,proposalId:proposal.memory.id,proposalDigest:proposal.memory.digest,decision:phase==='plan'?'edit':'accept',...(phase==='plan'?{text:'Keep outreach evidence explicit.'}:{})}},before.sha256))
 await waitFor(()=>expect(cache.getQueryData<MemoryReply>(MEMORY_KEY)?.content.reviews).toHaveLength(1))
 expect(after.content.entries[0]!.text).toBe(phase==='plan'?'Keep outreach evidence explicit.':'Keep outreach concise.')
 expect(after.content.entries[0]!.state).toBe('active')
})
it('an uncertain human write refetches observations and never repeats the mutation',async()=>{
 const doc={version:1 as const,settings:{use:false,suggest:true},entries:[]},proposal={runId:'arun_'+'a'.repeat(32),runRevision:7,chatId:'chat-one',turnId:'message-one',reviewable:true,memory:{id:'proposal-one',kind:'preference',text:'Keep outreach concise.',source:{run:'agent-run-one',proposedBy:'model'},scope:'project',digest:'sha256:3704e6fbc67976084bc7708f3c428132f879adfa86917c6451fe000c7b83298a',createdAt:'2026-10-24T10:00:00Z',state:'proposed'}}
 const before={sha256:'a'.repeat(64),content:doc,proposals:[proposal]}
 vi.mocked(transport).mockImplementation(async action=>{if(action)throw Error('uncertain fixture');return before as never})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><MemorySettings/></QueryClientProvider>)
 await screen.findByText(proposal.memory.text)
 fireEvent.click(screen.getByRole('button',{name:'Remember'}))
 await waitFor(()=>expect(transport).toHaveBeenCalledTimes(3))
 expect(vi.mocked(transport).mock.calls.filter(call=>call[0])).toHaveLength(1)
})

// The Desk's case of the harness's execution tests: the chat checkpoint is the Desk's.
// The rest of the file moved with the harness to @judgment-pack/harness (execution/execution.test.ts).
import {describe,it,expect,vi} from 'vitest'
import {contextualSession} from '@judgment-pack/harness/context/session'
import {normalize} from '@judgment-pack/harness/thinking'
import type {AssistantSession} from '@judgment-pack/harness/engine'
import {extendExecution,ExecutionPaused} from '@judgment-pack/harness/execution/state'
import {checkpoint,decodeCheckpoint} from '../../chat/checkpoint'
import {INITIAL_STATE} from '../../research/run'
const base=():AssistantSession=>({prompt:'Fix the missing-size exception and verify unchanged expectations',context:{currentRequest:{message:'Fix it',input:'current pack and test facts'}},allowConversation:true,interactive:true,testPrompt:'',tools:[],hostTools:[],callTool:vi.fn(async()=>({content:[]})),model:{family:'gemini',model:'fixture',call:vi.fn(async()=>new Response('{}'))},thinking:normalize('off','gemini'),signal:new AbortController().signal})
describe('shared execution continuity',()=>{
 it('keeps progress and exact typed operation results across JSON checkpoint and explicit extension',async()=>{
  const input=base(),ready=await contextualSession(input),task=ready.context.execution
  const update=ready.session.values.hostTools.find(t=>t.name==='update_plan')!
  await update.execute({explanation:'Exception fixed in a proposal; tests remain',plan:[{step:'Inspect exception',status:'completed'},{step:'Verify proposed change',status:'in_progress'}]},input.signal)
  const ref=await ready.context.ledger.retain({id:'test-facts',kind:'tool',label:'Exact facts',value:{zero:0,flag:false,nothing:null,decimal:'24'}})
  task.observe({type:'tool_call',callId:'check',name:'experimental_evaluate',args:{facts:{zero:0,flag:false,nothing:null}}})
  task.observe({type:'tool_result',callId:'check',name:'experimental_evaluate',isError:false,text:'verified',structured:{result:'set-aside'}})
  for(let i=0;i<20;i++)await ready.session.values.model.call('generate',{body:'{}'})
  await expect(ready.session.values.model.call('generate',{body:'{}'})).rejects.toBeInstanceOf(ExecutionPaused)
  expect(input.model.call).toHaveBeenCalledTimes(20)
  const saved=decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint({...INITIAL_STATE,execution:task.snapshot('paused')},[])))).state.execution!
  expect(saved.operations[0]).toMatchObject({args:{facts:{zero:0,flag:false,nothing:null}},result:{structuredContent:{result:'set-aside'}}})
  const resumed=await contextualSession({...input,context:{...input.context,execution:extendExecution(saved)}})
  expect(resumed.context.execution.state).toMatchObject({used:20,limit:30,plan:saved.plan})
  expect(resumed.context.ledger.read({id:ref.id,path:'/flag'})).toMatchObject({value:false})
  // Since v0.1.0 the execution snapshot travels in the request's state, not in the prompt (agent#9).
  expect(resumed.context.execution.requestState()).toContain('Verify proposed change')
  expect(resumed.context.execution.requestState()).toContain('retainedOperations')
  expect(resumed.session.prompt).not.toContain('retainedOperations')
  expect(input.callTool).not.toHaveBeenCalled()
 })
})

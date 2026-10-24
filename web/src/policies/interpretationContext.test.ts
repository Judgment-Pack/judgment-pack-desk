import {expect,it,vi} from 'vitest'
import {contextualSession,SessionContext} from '@judgment-pack/harness/context/session'
import {vercel} from '@judgment-pack/harness/vercel'
import {runCodex} from '@judgment-pack/harness/codex'
import {normalize} from '@judgment-pack/harness/thinking'
import type {AssistantEvent,AssistantSession,AgentAssistantSession} from '@judgment-pack/harness/engine'
import {policySourceDocuments,POLICY_SOURCE_INSTRUCTIONS,type PolicyAuthoringSource} from './authoringSource'
import {failureError} from '@judgment-pack/harness/failures'
const source:PolicyAuthoringSource={version:1,policyId:'policy_'+'a'.repeat(48),revisionId:'revision_'+'b'.repeat(48),candidateDigest:'c'.repeat(64),content:{sha256:'c'.repeat(64),bytes:131072},title:'Old source',artifactVersion:'1.0.0',narrative:'Exact source text.\n'.repeat(7000).padEnd(131072,' ')}
function base():AssistantSession{return {prompt:'Design a graph using declared packs and the selected exact Policy.\n'+POLICY_SOURCE_INSTRUCTIONS,context:{documents:policySourceDocuments([source]),configuration:{version:1,models:{fixture:{contextWindowTokens:16384,outputTokens:4096}}}},testPrompt:'',tools:[],hostTools:[],callTool:async()=>({}),model:{family:'gemini',model:'fixture',call:async()=>new Response()},thinking:normalize('off','gemini'),signal:new AbortController().signal,allowConversation:true,interactive:true,critic:'off'}}
it('retains one exact source entry through repeated initialization and projects bounded reads without copying it into the prompt',async()=>{
 const session=base(),context=new SessionContext(session)
 await context.initialize(session.context!.documents);await context.initialize(session.context!.documents)
 expect(context.ledger.list().items).toHaveLength(1)
 const entry=context.ledger.list().items[0]!
 expect(context.ledger.read({id:entry.contextId,path:'/revisionId'})).toMatchObject({text:source.revisionId})
 expect(context.ledger.read({id:entry.contextId,path:'/narrative',query:'Exact source text.'})).toMatchObject({text:expect.stringContaining('Exact source text.')})
 const projected=await context.result('read_context',{structuredContent:{source}})
 expect(JSON.stringify(projected).length).toBeLessThan(source.narrative.length)
 expect(session.prompt).not.toContain(source.narrative)
})
it('blocks retained derivative source access and provider dispatch after authorization changes',async()=>{
 const session=base();let allowed=true
 session.context!.authorize=async()=>{if(!allowed)throw failureError('permission','desk','not-started')}
 const call=vi.fn(async()=>new Response());session.model.call=call
 const {session:managed}=await contextualSession(session)
 const listed=await managed.values.hostTools.find(t=>t.name==='list_context')!.execute({},session.signal)
 expect(JSON.stringify(listed)).toContain(source.revisionId)
 allowed=false
 await expect(managed.values.hostTools.find(t=>t.name==='read_context')!.execute({id:'any'},session.signal)).rejects.toThrow()
 await expect(managed.values.model.call('/v1/chat/completions',{method:'POST',headers:{},body:'{}'})).rejects.toThrow()
 expect(call).not.toHaveBeenCalled()
})
it.each(['gemini','anthropic','openai-compatible'] as const)('makes exact derivative source available through shared context tools for %s',async family=>{
 const session=base();session.model.family=family;let calls=0;session.context!.authorize=vi.fn(async()=>{})
 session.model.call=async()=>{
  calls++;const name='list_context',args={},text='The selected source needs interpretation review.'
  const payload=family==='gemini'?{candidates:[{content:{role:'model',parts:calls===1?[{functionCall:{name,args}}]:[{text}]},finishReason:'STOP'}]}:family==='anthropic'?{id:'message_'+calls,type:'message',role:'assistant',model:'fixture',content:calls===1?[{type:'tool_use',id:'tool',name,input:args}]:[{type:'text',text}],stop_reason:calls===1?'tool_use':'end_turn',usage:{input_tokens:1,output_tokens:1}}:{id:'completion_'+calls,object:'chat.completion',created:0,model:'fixture',choices:[{index:0,message:{role:'assistant',content:calls===1?null:text,...(calls===1?{tool_calls:[{id:'tool',type:'function',function:{name,arguments:'{}'}}]}:{})},finish_reason:calls===1?'tool_calls':'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}
  return Response.json(payload)
 }
 const events:AssistantEvent[]=[];for await(const event of vercel.start(session))events.push(event)
 expect(events.filter(e=>e.type==='error')).toEqual([])
 expect(events.some(e=>e.type==='tool_result'&&JSON.stringify(e).includes(source.revisionId))).toBe(true)
 expect(session.context!.authorize).toHaveBeenCalled()
 expect(calls).toBe(2)
})
it('shares retained-source authorization with the subscription adapter',async()=>{
 const {model:_model,thinking:_thinking,...session}=base();let allowed=true
 session.context!.authorize=async()=>{if(!allowed)throw failureError('permission','desk','not-started')}
 const agent:AgentAssistantSession={...session,agent:{model:'fixture',run:async(request,callbacks)=>{
  const list=request.tools.find(t=>t.description.startsWith('list_context:'))!
  const result=await callbacks.tool({id:'list',name:list.name,arguments:{}},callbacks.signal)
  expect(result.text).toContain(source.revisionId)
  allowed=false
  const blocked=await callbacks.tool({id:'list-again',name:list.name,arguments:{}},callbacks.signal)
  expect(blocked.isError).toBe(true)
 }}}
 const events:AssistantEvent[]=[];for await(const event of runCodex(agent))events.push(event)
 expect(events.some(e=>e.type==='tool_result'&&e.isError&&JSON.stringify(e).includes('permission'))).toBe(true)
})

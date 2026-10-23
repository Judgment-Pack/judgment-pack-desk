import {expect,it,vi} from 'vitest'
import {vercel} from '@judgment-pack/harness/vercel'
import {runCodex} from '@judgment-pack/harness/codex'
import {normalize} from '@judgment-pack/harness/thinking'
import {contextualSession,SessionContext} from '@judgment-pack/harness/context/session'
import {failureError} from '@judgment-pack/harness/failures'
import type {AssistantEvent,AssistantSession,AgentAssistantSession} from '@judgment-pack/harness/engine'
import {policyChatTools} from './chatTools'
import {readPolicy,type PolicyReply} from './client'
import type {PolicyProposal} from './chat'
import {resolvePolicyComparison,type PolicyComparison,type PolicyComparisonRequest} from './comparison'
import {planningSession} from '@judgment-pack/harness/planning/policy'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),readPolicy:vi.fn()}))
vi.mock('./comparison',async original=>({...await original<typeof import('./comparison')>(),resolvePolicyComparison:vi.fn()}))
const id='policy_'+'a'.repeat(48),revision='b'.repeat(64)
const candidate={title:'Retention policy',narrative:('Exact policy clause.\n'.repeat(6000)+'CLAUSE_END').padEnd(131072,' '),artifactVersion:'1.0.0'}
const saved={revision,document:{id,candidate}} as PolicyReply
function base():AssistantSession{return {prompt:'Change the title to Requested title.\n'+JSON.stringify({id,revision,candidate}),context:{currentRequest:{message:'Change the title to Requested title.',input:JSON.stringify({id,revision,candidate})},configuration:{version:1,models:{fixture:{contextWindowTokens:16384,outputTokens:4096}}}},testPrompt:'',tools:[],hostTools:[],callTool:async()=>({}),model:{family:'gemini',model:'fixture',call:async()=>new Response()},thinking:normalize('off','gemini'),signal:new AbortController().signal,allowConversation:true,interactive:true,adversarialReview:false}}
const retainTools=(retained:PolicyProposal[])=>policyChatTools({id,request:()=>({revision,candidate}),afterTurnId:()=> 'latest-user',retain:async p=>{const index=retained.findIndex(row=>row.id===p.id);if(index<0)retained.push(structuredClone(p));else retained[index]=structuredClone(p)}})
it('retains a near-limit Policy narrative and request revision through shared prompt and tool-result projection',async()=>{
 const session=base(),context=new SessionContext(session),prompt=await context.prompt(session.prompt,'Host instructions',[],session.context!.currentRequest)
 expect(prompt.length).toBeLessThan(session.prompt.length)
 expect(context.ledger.list().items.length).toBeGreaterThan(0)
 const result=await context.result('read_policy',{structuredContent:saved})
 const reference=result.structuredContent as {contextId:string}
 expect(reference.contextId).toBeDefined()
 expect(context.ledger.read({id:reference.contextId,path:'/structuredContent/revision'})).toMatchObject({text:revision})
 expect(context.ledger.read({id:reference.contextId,path:'/structuredContent/document/candidate/title'})).toMatchObject({text:candidate.title})
 expect(saved.document.candidate.narrative).toBe(candidate.narrative)
 expect(new TextEncoder().encode(candidate.narrative).length).toBeLessThanOrEqual(131072)
})
it.each(['openai-compatible','anthropic','gemini'] as const)('%s uses the existing managed provider path for large Policy context and a retained draft proposal',async family=>{
 vi.mocked(readPolicy).mockResolvedValue(saved)
 const retained:PolicyProposal[]=[],session=base();session.model.family=family;session.thinking=normalize('off',family);session.hostTools=retainTools(retained)
 let calls=0,callbackError:unknown
 session.model.call=async(_suffix,request)=>{try{
  calls++;const wire=JSON.stringify(JSON.parse(request.body));expect(calls).toBeLessThanOrEqual(3)
  if(calls===2){expect(wire).toContain('result-read_policy-');expect(wire).toContain('read_context')}
  const name=calls===1?'read_policy':'propose_policy_update',args=calls===1?{}:{revision,title:'Requested title'},text='The draft update is proposed for review. No review or publication occurred.'
  let answer:unknown
  if(family==='gemini')answer={candidates:[{content:{role:'model',parts:calls===3?[{text}]:[{functionCall:{name,args}}]},finishReason:'STOP'}]}
  else if(family==='anthropic')answer={id:'message_'+calls,type:'message',role:'assistant',model:'fixture',content:calls===3?[{type:'text',text}]:[{type:'tool_use',id:'tool_'+calls,name,input:args}],stop_reason:calls===3?'end_turn':'tool_use',usage:{input_tokens:1,output_tokens:1}}
  else answer={id:'completion_'+calls,object:'chat.completion',created:0,model:'fixture',choices:[{index:0,message:{role:'assistant',content:calls===3?text:null,...(calls===3?{}:{tool_calls:[{id:'tool_'+calls,type:'function',function:{name,arguments:JSON.stringify(args)}}]})},finish_reason:calls===3?'stop':'tool_calls'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}
  return new Response(JSON.stringify(answer),{headers:{'Content-Type':'application/json'}})
 }catch(error){callbackError=error;throw error}}
 const events:AssistantEvent[]=[];for await(const event of vercel.start(session))events.push(event)
 if(callbackError)throw callbackError
 expect(events.filter(event=>event.type==='error'||event.type==='budget_paused')).toEqual([])
 expect(events.filter(event=>event.type==='guardrail'&&event.action==='refused')).toEqual([])
 expect(retained).toHaveLength(1);expect(retained[0]).toMatchObject({revision,before:candidate,candidate:{...candidate,title:'Requested title'},check:{status:'passed'},afterTurnId:'latest-user'})
 expect(events.filter(event=>event.type==='tool_result').every(event=>!event.isError)).toBe(true)
 expect(calls).toBe(3)
})
it('uses the same Policy tools and context projection through the subscription adapter',async()=>{
 vi.mocked(readPolicy).mockResolvedValue(saved)
 const retained:PolicyProposal[]=[],api=base();const {model:_model,thinking:_thinking,...common}=api
 const session:AgentAssistantSession={...common,hostTools:retainTools(retained),agent:{model:'fixture',run:async(request,callbacks)=>{
  expect(request.prompt.length).toBeLessThan(api.prompt.length)
  const read=request.tools.find(tool=>tool.description.startsWith('read_policy:'))!,propose=request.tools.find(tool=>tool.description.startsWith('propose_policy_update:'))!
  const answer=await callbacks.tool({id:'read',name:read.name,arguments:{}},callbacks.signal)
  // The reference to the retained answer names the ledger's reader, read_window, since v0.1.0 (agent#1).
  expect(answer.isError).toBe(false);expect(answer.text).toContain('read_window')
  const proposal=await callbacks.tool({id:'propose',name:propose.name,arguments:{revision,title:'Requested title'}},callbacks.signal)
  expect(proposal.isError).toBe(false)
  await callbacks.event({type:'message',id:'answer',text:'The Policy draft update is proposed for review.',phase:'final'})
 }}}
 const events:AssistantEvent[]=[];for await(const event of runCodex(session))events.push(event)
 expect(events.filter(event=>event.type==='error'||event.type==='guardrail'&&event.action==='refused')).toEqual([]);expect(retained[0]).toMatchObject({revision,before:candidate,candidate:{...candidate,title:'Requested title'},check:{status:'passed'}})
})
it('checks current access before retained-context retrieval and each provider dispatch',async()=>{
 let allowed=true,authorizations=0,reads=0,calls=0
 const session=base();session.prompt='Explain this Policy';session.context!.authorize=async()=>{authorizations++;if(!allowed)throw failureError('permission','desk','not-started')}
 session.hostTools=[{name:'read_policy',description:'Read current Policy',effect:'read',inputSchema:{type:'object',properties:{}},execute:async()=>{reads++;return {structuredContent:saved}}}]
 session.model.call=async()=>{calls++;return new Response()}
 const managed=await contextualSession(session)
 await managed.session.hostTools.find(tool=>tool.name==='read_policy')!.execute({},session.signal)
 allowed=false
 await expect(managed.session.hostTools.find(tool=>tool.name==='list_context')!.execute({},session.signal)).rejects.toThrow()
 await expect(managed.session.model.call('/v1/chat/completions',{method:'POST',headers:{},body:'{}'})).rejects.toThrow()
 expect(reads).toBe(1);expect(calls).toBe(0);expect(authorizations).toBe(4)
})
it('refuses pack or graph proposal envelopes in a Policy-only conversation without adopting another artifact scope',async()=>{
 const session=base();session.prompt='Create a pack from this Policy';session.context={...session.context,artifactProposals:false};session.model.call=async()=>new Response(JSON.stringify({candidates:[{content:{role:'model',parts:[{text:'```json\n'+JSON.stringify({proposal:{kind:'create',document:{id:'urn:pack',version:'1.0.0'},unknowns:[]}})+'\n```'}]},finishReason:'STOP'}]}),{headers:{'Content-Type':'application/json'}})
 const events:AssistantEvent[]=[];for await(const event of vercel.start(session))events.push(event)
 expect(events.some(event=>event.type==='proposal')).toBe(false)
 expect(events).toContainEqual(expect.objectContaining({type:'guardrail',tool:'proposal',action:'refused'}))
})

const comparisonRequest:PolicyComparisonRequest={before:{kind:'publication',publicationId:'publication_'+'1'.repeat(48)},after:{kind:'revision',revisionId:'revision_'+'2'.repeat(48)}}
function comparisonResult():PolicyComparison {
 const side=(selector:PolicyComparisonRequest['before'])=>({selector,identity:{revisionId:'revision_'+'2'.repeat(48),candidateDigest:'3'.repeat(64),content:{sha256:'3'.repeat(64),bytes:131200}},text:candidate.narrative,metadata:{kind:'narrative' as const,title:'Retention policy',completeness:'complete' as const}})
 return {policyId:id,before:side(comparisonRequest.before),after:side(comparisonRequest.after),differences:[],limitations:['informational-only']}
}
const contextReference=(text:string)=>text.match(/[a-f0-9-]{36}:result-read_policy_comparison-[a-f0-9]+:[a-f0-9]+/)?.[0]
it.each(['openai-compatible','anthropic','gemini'] as const)('%s projects large exact comparisons through shared Plan context and read_context',async family=>{
 vi.mocked(resolvePolicyComparison).mockResolvedValue(comparisonResult())
 const input=base();input.prompt='Explain the selected exact comparison.';input.context={...input.context,currentRequest:{message:input.prompt,input:''},phase:'plan'};input.model.family=family;input.thinking=normalize('off',family);input.hostTools=retainTools([])
 const session=planningSession(input);let calls=0,callbackError:unknown
 session.model.call=async(_path,request)=>{try{
  calls++;const wire=JSON.stringify(JSON.parse(request.body));expect(calls).toBeLessThanOrEqual(3)
  expect(wire).not.toContain('propose_policy_update');expect(wire).toContain('read_policy_comparison')
  if(calls===2){expect(wire.length).toBeLessThan(candidate.narrative.length);expect(contextReference(wire)).toBeDefined()}
  if(calls===3)expect(wire).toContain('Exact policy clause.')
  const name=calls===1?'read_policy_comparison':'read_context',args=calls===1?comparisonRequest:{id:contextReference(wire),path:'/structuredContent/before/text',limit:1024},text='The selected retained comparison is informational only.'
  let answer:unknown
  if(family==='gemini')answer={candidates:[{content:{role:'model',parts:calls===3?[{text}]:[{functionCall:{name,args}}]},finishReason:'STOP'}]}
  else if(family==='anthropic')answer={id:'message_'+calls,type:'message',role:'assistant',model:'fixture',content:calls===3?[{type:'text',text}]:[{type:'tool_use',id:'tool_'+calls,name,input:args}],stop_reason:calls===3?'end_turn':'tool_use',usage:{input_tokens:1,output_tokens:1}}
  else answer={id:'completion_'+calls,object:'chat.completion',created:0,model:'fixture',choices:[{index:0,message:{role:'assistant',content:calls===3?text:null,...(calls===3?{}:{tool_calls:[{id:'tool_'+calls,type:'function',function:{name,arguments:JSON.stringify(args)}}]})},finish_reason:calls===3?'stop':'tool_calls'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}
  return new Response(JSON.stringify(answer),{headers:{'Content-Type':'application/json'}})
 }catch(error){callbackError=error;throw error}}
 const events:AssistantEvent[]=[];for await(const event of vercel.start(session))events.push(event)
 if(callbackError)throw callbackError
 expect(events.filter(e=>e.type==='error'||e.type==='budget_paused')).toEqual([]);expect(events.filter(e=>e.type==='tool_result').every(e=>!e.isError)).toBe(true);expect(calls).toBe(3)
 expect(resolvePolicyComparison).toHaveBeenLastCalledWith(id,comparisonRequest,expect.any(AbortSignal))
})
it('subscription Plan compares and retrieves the same bounded retained context without proposal capabilities',async()=>{
 vi.mocked(resolvePolicyComparison).mockResolvedValue(comparisonResult())
 const api=base(),{model:_model,thinking:_thinking,...common}=api
 const session:AgentAssistantSession={...common,prompt:'Explain these retained changes',context:{...common.context,currentRequest:{message:'Explain changes',input:''},phase:'plan'},hostTools:retainTools([]),agent:{model:'fixture',run:async(request,callbacks)=>{
  expect(request.tools.some(tool=>tool.description.startsWith('propose_policy_update:'))).toBe(false)
  const compare=request.tools.find(tool=>tool.description.startsWith('read_policy_comparison:'))!,read=request.tools.find(tool=>tool.description.startsWith('read_context:'))!
  const answer=await callbacks.tool({id:'compare',name:compare.name,arguments:comparisonRequest},callbacks.signal)
  expect(answer.isError).toBe(false);expect(answer.text.length).toBeLessThan(candidate.narrative.length)
  const reference=contextReference(answer.text);expect(reference).toBeDefined()
  const excerpt=await callbacks.tool({id:'excerpt',name:read.name,arguments:{id:reference,path:'/structuredContent/before/text',limit:1024}},callbacks.signal)
  expect(excerpt.isError).toBe(false);expect(excerpt.text).toContain('Exact policy clause.');expect(excerpt.text.length).toBeLessThan(4096)
  await callbacks.event({type:'message',id:'answer',text:'Retained changes explained with limitations.',phase:'final'})
 }}}
 const events:AssistantEvent[]=[];for await(const event of runCodex(planningSession(session)))events.push(event)
 expect(events.filter(e=>e.type==='error')).toEqual([])
})
it('comparison tool refuses malformed/cross-Policy inputs and declares fresh read-only reference behavior',async()=>{
 vi.mocked(resolvePolicyComparison).mockClear();const tools=retainTools([]),tool=tools.find(t=>t.name==='read_policy_comparison')!
 expect(tool).toMatchObject({effect:'read',delegation:'read',contextPolicy:{reference:true}});expect(tool.contextPolicy?.immutable).toBeUndefined()
 for(const args of [{...comparisonRequest,policyId:'policy_'+'0'.repeat(48)},{...comparisonRequest,url:'/api/policies/other'}, {...comparisonRequest,before:{kind:'revision',revisionId:'latest'}}])await expect(tool.execute(args,new AbortController().signal)).rejects.toThrow()
 expect(resolvePolicyComparison).not.toHaveBeenCalled();expect(policyChatTools({request:()=>undefined,afterTurnId:()=>undefined,retain:async()=>{}})).toEqual([])
})

it('checks current access before retained comparison retrieval and later provider dispatch',async()=>{
 vi.mocked(resolvePolicyComparison).mockClear().mockResolvedValue(comparisonResult())
 let allowed=true,calls=0
 const session=base();session.prompt='Explain changes';session.hostTools=retainTools([]);session.context!.authorize=async()=>{if(!allowed)throw failureError('permission','desk','not-started')};session.model.call=async()=>{calls++;return new Response()}
 const managed=await contextualSession(session),answer=await managed.session.hostTools.find(t=>t.name==='read_policy_comparison')!.execute(comparisonRequest,session.signal)
 const projected=await managed.context.result('read_policy_comparison',answer as import('@judgment-pack/harness/engine').McpToolResult),contextId=(projected.structuredContent as {contextId:string}).contextId
 expect(contextId).toBeDefined();allowed=false
 await expect(managed.session.hostTools.find(t=>t.name==='read_context')!.execute({id:contextId,path:'/structuredContent/before/text',limit:1024},session.signal)).rejects.toThrow()
 await expect(managed.session.model.call('/v1/chat/completions',{method:'POST',headers:{},body:'{}'})).rejects.toThrow()
 expect(calls).toBe(0);expect(resolvePolicyComparison).toHaveBeenCalledTimes(1)
})

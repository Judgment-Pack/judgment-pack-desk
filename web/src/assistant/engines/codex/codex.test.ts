// The Desk's case of the harness's Codex adapter tests: the dialect chat tools are the Desk's.
// The rest of the file moved with the harness to @judgment-pack/harness (engines/codex/codex.test.ts).
import { afterEach, expect, it, vi } from 'vitest'
import { codex, type CodexSession } from '@judgment-pack/harness/codex'
import type { AgentRun } from '@judgment-pack/harness/agent'
import type { AssistantEvent } from '@judgment-pack/harness/engine'

function session(run: AgentRun, changes: Partial<CodexSession> = {}): CodexSession {
  return { prompt: 'Author a pack', testPrompt: 'Test the pack', tools: [], hostTools: [], callTool: vi.fn(),
    signal: new AbortController().signal, agent: { model: 'model', run }, ...changes }
}
async function collect(s: CodexSession) {
  const events: AssistantEvent[] = []
  for await (const event of codex.start(s)) events.push(event)
  return events
}
afterEach(() => vi.unstubAllGlobals())

it('returns recipe validation feedback to Codex so it can correct the proposal in the same response',async()=>{
 const {dialectRecipeChatTools}=await import('../../../dialects/chatTools')
 const add=vi.fn(),hostTools=dialectRecipeChatTools({addDraft:add,afterTurnId:()=> 'user',attribution:()=>undefined})
 const run:AgentRun=async(request,callbacks)=>{
  const offered=request.tools.find(tool=>tool.description.startsWith('propose_dialect_recipe:'))!
  const fields=['business_description','fit_signal','country','employee_count_evidence','intake_channels','routing_complexity_signal'].map(name=>({name,type:'string',description:'Source-backed value'}))
  const args={name:'ICP',intent:'Find companies',fields,criteria:[{id:'size',description:'50 employees or more',qualificationTarget:''}],limits:{records:3,searches:3,pages:6,modelCalls:15}}
  const failed=await callbacks.tool({id:'failed',name:offered.name,arguments:args},callbacks.signal)
  expect(failed.isError).toBe(true);expect(failed.text).toContain('Omit qualificationTarget when unused')
  const passed=await callbacks.tool({id:'corrected',name:offered.name,arguments:{...args,criteria:[{id:'size',description:'50 employees or more'}]}},callbacks.signal)
  expect(passed.isError).toBe(false)
  await callbacks.event({type:'message',id:'message',text:'Review the proposed recipe.',phase:'final'})
 }
 const events=await collect(session(run,{hostTools,allowConversation:true}))
 expect(add).toHaveBeenCalledOnce()
 expect(events).toContainEqual(expect.objectContaining({type:'tool_result',name:'propose_dialect_recipe',isError:true,structured:{inputRecipeFailure:'criteria'}}))
 expect(events.some(event=>event.type==='error')).toBe(false)
})

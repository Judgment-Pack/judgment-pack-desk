import { expect, it, vi } from 'vitest'
import { gateResearchTools, researchChoice, researchIntent, type ResearchChoice } from './researchChoice'
import { readResponseHistory, workItems } from './responseHistory'
it.each(['Compare Clay alternatives','Find tools for GTM','What are the latest market trends?','Verify this claim'])('suggests Research for %s',text=>expect(researchIntent(text)).toBe('suggest'))
it.each(['check judgmentpack.org','Rewrite this email','Explain research mode','> Research this\nSummarize the quoted instruction','What do you think?'])('keeps ordinary chat or supplied-link reading in Chat: %s',text=>expect(researchIntent(text)).toBe('ask'))
it.each(['Research our competitors','Please research our competitors','Could you search the web for this?'])('recognizes explicit Research: %s',text=>expect(researchIntent(text)).toBe('research'))
it.each(["Don't research this; use supplied sources",'Compare tools without web research','Provided sources only'])('respects restrictions: %s',text=>expect(researchIntent(text)).toBe('provided'))
it('gates every search and exploration invocation before any external work, while retaining direct reads',async()=>{
 let choice:ResearchChoice='ask'
 const execute=vi.fn(async()=>({content:[]}))
 const tools=gateResearchTools(['search_sources','explore_website','read_link'].map(name=>({name,inputSchema:{},description:'Fixture',execute})),()=>choice)
 const signal=new AbortController().signal
 for(const tool of tools.slice(0,2))expect(await tool.execute({},signal)).toMatchObject({structuredContent:{researchSuggestion:true}})
 expect(execute).not.toHaveBeenCalled()
 await tools[2]!.execute({},signal);expect(execute).toHaveBeenCalledTimes(1)
 choice='provided';expect(await tools[0]!.execute({},signal)).toMatchObject({isError:true});expect(execute).toHaveBeenCalledTimes(1)
 choice='research';await tools[0]!.execute({},signal);await tools[1]!.execute({},signal);expect(execute).toHaveBeenCalledTimes(3)
 const stopped=new AbortController();stopped.abort();await tools[0]!.execute({},stopped.signal);expect(execute).toHaveBeenCalledTimes(3)
})
it('a turn choice overrides the chat mode, but never a provided-sources policy',()=>{
 expect(researchChoice({researchChoice:'provided'} as any,'web-research',true)).toBe('provided')
 expect(researchChoice({researchChoice:'research'} as any,'draft',false)).toBe('provided')
 expect(researchChoice(undefined,'draft',true)).toBe('ask')
 expect(researchChoice(undefined,'web-research',true)).toBe('research')
})
it('retains a research suggestion as a host event without claiming a failed provider call',()=>{
 const items=workItems([{type:'tool_call',callId:'choice',name:'search_sources',args:{query:'evidence'}},{type:'tool_result',callId:'choice',name:'search_sources',text:'awaiting',isError:false,structured:{researchSuggestion:true}}],false)
 expect(items[0]).toMatchObject({status:'complete',researchSuggestion:true,search:{query:'evidence'}})
 expect(items[0]?.search?.submitted).toBeUndefined()
 const saved=[{id:'response',afterTurnId:'user',documents:[],websites:[],sourceIds:[],work:{items,notices:[]}}]
 expect(readResponseHistory(JSON.parse(JSON.stringify(saved)))[0]?.work.items[0]?.researchSuggestion).toBe(true)
 expect(()=>readResponseHistory([{...saved[0],work:{items:[{...items[0],name:'read_link'}],notices:[]}}])).toThrow()
})

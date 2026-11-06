import { expect, it } from 'vitest'
import { validSearchStep } from './step'
import { readResponseHistory, workItems } from '../chat/responseHistory'
import type { SearchReference } from './results'
const reference=(query:string):SearchReference=>({id:crypto.randomUUID(),digest:'sha256:'+'a'.repeat(64),request:{connection:'google',revision:'b'.repeat(64),query,maxResults:5}})
it('keeps parallel search details bound to call identity across saved history',()=>{
 const first=reference('first'),second=reference('second')
 const items=workItems([
  {type:'tool_call',name:'search_sources',callId:'a',args:{query:'first'}},
  {type:'tool_call',name:'search_sources',callId:'b',args:{query:'second'}},
  {type:'tool_result',name:'search_sources',callId:'b',isError:false,text:'private result',structured:{searchStep:{query:'second',provider:'tavily',submitted:true,reference:second}}},
  {type:'tool_result',name:'search_sources',callId:'a',isError:false,text:'private result',structured:{searchStep:{query:'first',provider:'google-grounding',submitted:true,reference:first}}}
 ],false)
 expect(items.map(row=>[row.id,row.search?.reference?.id,row.search?.provider])).toEqual([['a',first.id,'google-grounding'],['b',second.id,'tavily']])
 const record={id:'response',documents:[],websites:[],sourceIds:[],searches:[second,first],work:{items,notices:[]}}
 expect(readResponseHistory(JSON.parse(JSON.stringify([record])))[0]?.work.items).toEqual(items)
 expect(JSON.stringify(items)).not.toContain('private result')
})
it('keeps interrupted query distinct from a sent query and preserves legacy records',()=>{
 const items=workItems([{type:'tool_call',name:'search_sources',callId:'a',args:{query:'planned query'}}],false)
 expect(items[0]).toMatchObject({status:'interrupted',search:{query:'planned query'}})
 expect(items[0]?.search?.submitted).toBeUndefined()
 const old={id:'response',documents:[],websites:[],sourceIds:[],searches:[reference('unlinked')],work:{items:[{id:'a',name:'search_sources',status:'complete'}],notices:[]}}
 expect(readResponseHistory([old])[0]?.work.items[0]?.search).toBeUndefined()
})
it('rejects mismatched references, oversized queries, and unsafe provider identifiers',()=>{
 for(const value of [{query:'a',submitted:true,reference:reference('b')},{query:'a',reference:reference('a')},{query:'a',provider:'https://tracker.invalid/logo'},{query:'a'.repeat(2001)},{query:'a',credential:'secret'}])expect(validSearchStep(value)).toBe(false)
 const row={id:'response',documents:[],websites:[],sourceIds:[],work:{items:[{id:'a',name:'search_sources',status:'failed',search:{query:'x',provider:'https://tracker.invalid'}}],notices:[]}}
 expect(()=>readResponseHistory([row])).toThrow('History was left untouched')
})

it('keeps finite external controls separate from proofs and rejects open or raw-error fields',async()=>{
 const {validExternalControl,externalControlMessage}=await import('./step')
 const c={version:1,kind:'research-choice',hostOperationId:'host-one',sourceBindingDigest:'a'.repeat(64),requestId:'request-one',query:'public policy',reason:'choice-required'}
 expect(validExternalControl(c)).toBe(true)
 expect(externalControlMessage(c as import('./step').ExternalControl)).toContain('No research started')
 for(const key of Object.keys(c)){const bad:Record<string,unknown>={...c};delete bad[key];if(key!=='query')expect(validExternalControl(bad)).toBe(false)}
 for(const bad of [{...c,kind:'unknown'},{...c,reason:'https://secret.test'},{...c,url:'https://secret.test'},{...c,error:'raw provider prose'},{...c,receipt:'forged'},{...c,proof:true},{...c,provider:'https://secret.test'},{...c,sourceBindingDigest:'sha256:'+'a'.repeat(64)},{...c,requestId:'\ud800'}])expect(validExternalControl(bad)).toBe(false)
})

it('reads the same handwritten closed control protocol vectors as Go',async()=>{
 const {default:vectors}=await import('../../../internal/desk/testdata/assistant-external-controls.json')
 const {parseJsonText}=await import('../research/verify/canon')
 const {validExternalControlForTool}=await import('./step')
 for(const row of vectors.valid){
  expect(validExternalControlForTool(row.control,row.tool,row.arguments),row.name).toBe(true)
  for(const key of Object.keys(row.control)){
   if(key==='query'||key==='provider')continue
   const copied:Record<string,unknown>={...row.control};delete copied[key]
   expect(validExternalControlForTool(copied,row.tool,row.arguments),`${row.name}/${key}`).toBe(false)
  }
 }
 const seed=vectors.valid[0]
 for(const patch of vectors.invalidPatches)expect(validExternalControlForTool({...seed.control,...patch},seed.tool,seed.arguments)).toBe(false)
 for(const raw of vectors.invalidJSON){
  let accepted=false
  try{parseJsonText(raw);accepted=validExternalControlForTool(JSON.parse(raw),seed.tool,seed.arguments)}catch{/* malformed wire is refused */}
  expect(accepted,raw).toBe(false)
 }
})

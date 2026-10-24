import { expect, it, vi } from 'vitest'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
import { INITIAL_STATE, type RunState } from './run'
import { checkpoint, decodeCheckpoint } from '../chat/checkpoint'
import { artifactCheckpoint, type PackDraft } from '../packs/drafts/model'
import { recoverDraft, emptySuite } from '../packs/test-workspace/model'
import { digestOf } from './checkCandidate'
import { runtimeCheckIdentity, runSnapshotCheck, verifyRetainedCheck } from './retainedCheck'
import type { CallTool, McpToolResult } from '@judgment-pack/harness/engine'
const tool = {name:'jpack',version:'0.29.0'}
const outcome = {kind:'outcome',outcomeId:'accept',reasons:[],handoff:{state:'none'}}
const document = JSON.stringify({specVersion:'0.2.0-draft',id:'https://example.org/check',version:'1.0.0',
  title:'Hours screening',decision:{intent:'test',question:'Accept?'},outcomes:[{id:'accept',label:'Accept'},{id:'reject',label:'Reject'}],
  rules:[{id:'hours',description:'At least one hour',when:{op:'fact',path:'/work/hours',operator:'greater-than-or-equal',value:'1'},outcome:'accept',onUnknown:'escalate'}],
  fallbackOutcome:'reject',escalation:{triggers:['unknown'],target:{kind:'human-role',name:'Reviewer'}}})
const row = {id:'yes',facts:{work:{hours:'2'}},expectedDisposition:outcome,expectationSource:'you-1',rationale:'The requirement says accept.'}
const time = '2026-10-10T12:00:00Z'
const reply = (value: unknown): McpToolResult => ({content:[{type:'text',text:JSON.stringify(value)}]})
function fixture(options: {version?:string; mismatch?:boolean; validation?:boolean}={}) {
  const ownTool = {...tool,version:options.version ?? tool.version}
  const call: CallTool = vi.fn(async(name,args) => {
    if(name==='describe_runtime')return reply({tool:ownTool,outputVersion:'2'})
    if(name==='validate')return reply({tool:ownTool,status:options.validation === false ? 'invalid' : 'valid',diagnostics:[]})
    if(name==='experimental_test_cases') {
      const matrix=JSON.parse(args.matrix as string),pass=!options.mismatch
      return reply({tool:ownTool,evaluatorSpecVersion:'0.2.0-draft',status:pass?'passed':'mismatch',summary:{total:matrix.cases.length,passed:pass?matrix.cases.length:0},
        packs:[{rows:matrix.cases.map((r:typeof row)=>({id:r.id,status:pass?'passed':'mismatch',expected:JSON.stringify(outcome),actual:JSON.stringify(pass?outcome:{...outcome,outcomeId:'reject'})}))}]})
    }
    if(name==='experimental_validate_expectations')return reply({specVersion:'0.2.0-draft',status:'valid',results:(args.expectations as unknown[] ?? []).map((_,index)=>({index,status:'valid',canonical:JSON.stringify(outcome)}))})
    throw Error('Unexpected call '+name)
  })
  return {call}
}
async function checked(options:Parameters<typeof fixture>[0]={},cases=[row]) {
 const h=fixture(options)
 const check=await runSnapshotCheck(document,cases,h.call,new AbortController().signal,()=>runtimeCheckIdentity(h.call),async()=>digestOf('context'),[['you-1','Always accept.']],time)
 const state:RunState={...INITIAL_STATE,turns:[{id:'statement-1',role:'user',kind:'brief',text:'Always accept.',statement:'Always accept.',at:time}],cases,
 candidates:[{text:document,document:JSON.parse(document),digest:await digestOf(document),revision:1,producedBy:'conversation',check}]}
 return {...h,check,saved:decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(state,[]))))}
}
it('round-trips completed check inputs and reports without evaluating again',async()=>{
 const h=await checked();vi.mocked(h.call).mockClear()
 const replay=await verifyRetainedCheck(h.saved.state.candidates[0]!.previousCheck!.record!)
 expect(replay.cases.map(r=>r.passed)).toEqual([true]);expect(h.call).not.toHaveBeenCalled()
})
it('retains only cited statement provenance when a draft has no conversation',async()=>{
 const h=await checked(),artifact=artifactCheckpoint(h.saved)
 expect(artifact.state.turns).toEqual([])
 expect(artifact.state.candidates[0]?.previousCheck?.record?.statements).toEqual([['you-1','Always accept.']])
})
it('does not promote a tampered or incomplete retained report',async()=>{
 const h=await checked(),record=structuredClone(h.check.record!)
 record.validation=reply({status:'valid',tool})
 await expect(verifyRetainedCheck(record)).rejects.toThrow('integrity')
})
it('preserves failed comparisons and empty suites as separate results after reload',async()=>{
 const failed=await checked({mismatch:true}),empty=await checked({},[])
 expect((await verifyRetainedCheck(failed.check.record!)).cases.map(r=>r.passed)).toEqual([false])
 expect((await verifyRetainedCheck(empty.check.record!)).cases).toEqual([])
})
it.each(['runtime','context'] as const)('refuses a snapshot whose %s changes during checks',async change=>{
 const h=fixture();let reads=0
 const changing=async()=>String(reads++)
 await expect(runSnapshotCheck(document,[row],h.call,new AbortController().signal,
 change==='runtime'?changing:()=>runtimeCheckIdentity(h.call),change==='context'?changing:async()=>digestOf('context'),[],time)).rejects.toThrow('changed during')
})
it('imports exact retained inputs and full Runtime report into Tests',async()=>{
 const h=await checked(),draft={id:'draft-test',generation:1,updatedAt:time,documents:[],checkpoint:h.saved} as unknown as PackDraft
 const suite=await recoverDraft(emptySuite(),draft)
 expect(suite.runs[0]?.cases[0]?.row.facts).toEqual(row.facts)
 expect(suite.runs[0]?.report?.tool).toEqual(tool)
 expect(suite.runs[0]?.at).toBe(time)
})
it('does not commit a stopped in-flight snapshot check',async()=>{
 const h=fixture(),abort=new AbortController()
 const call:CallTool=async(name,args)=>{const response=await h.call(name,args);if(name==='experimental_test_cases')abort.abort();return response}
 await expect(runSnapshotCheck(document,[row],call,abort.signal,()=>runtimeCheckIdentity(h.call),async()=>digestOf('context'),[],time)).rejects.toMatchObject({name:'AbortError'})
})
it.runIf(Boolean(process.env.JPACK_EXPECTATION_BINARY))('round-trips an actual Runtime snapshot report across independent runtime processes',async()=>{
  const connect=async()=>{
    const transport=new StdioClientTransport({command:process.env.JPACK_EXPECTATION_BINARY!,args:['mcp'],cwd:'/tmp',stderr:'pipe'})
    const client=new Client({name:'desk-retained-check-test',version:'1'},{capabilities:{}})
    await client.connect(transport)
    const call:CallTool=async(name,args)=>await client.callTool({name,arguments:args}) as McpToolResult
    return {client,call}
  }
  const first=await connect()
  let record
  try{record=(await runSnapshotCheck(document,[row],first.call,new AbortController().signal,()=>runtimeCheckIdentity(first.call),async()=>await digestOf('context'),[['you-1','Always accept.']],time)).record!}
  finally{await first.client.close()}
  const second=await connect()
  try{
    expect(await runtimeCheckIdentity(second.call)).toBe(record.runtime)
    const replay=await verifyRetainedCheck(JSON.parse(JSON.stringify(record)))
    expect(replay.valid,JSON.stringify(replay.diagnostics)).toBe(true);expect(replay.cases.map(r=>r.passed)).toEqual([true])
  }finally{await second.client.close()}
},20000)

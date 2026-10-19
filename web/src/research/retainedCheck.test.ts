import { afterEach, expect, it, vi } from 'vitest'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
import { AuthoringRun, INITIAL_STATE, canCreateDraft, type RunPorts, type RunState } from './run'
import { Ledger } from './ledger'
import {failure, failureMessage} from '@judgment-pack/harness/failures'
import type {ExecutionCheckpoint} from '@judgment-pack/harness/execution/state'
import { checkpoint, decodeCheckpoint } from '../chat/checkpoint'
import { artifactCheckpoint, type PackDraft } from '../packs/drafts/model'
import { recoverDraft, emptySuite } from '../packs/test-workspace/model'
import { digestOf } from './checkCandidate'
import { runtimeCheckIdentity, runSnapshotCheck, verifyRetainedCheck } from './retainedCheck'
import { draftCheckStatus } from './DraftCheckStatus'
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
  const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),snapshotChecks:true,runtimeIdentity:()=>runtimeCheckIdentity(call),callTool:call,
    turn:vi.fn(async()=>{throw Error('No model should run')}),researchTools:[],authorPrompt:'',maxRevisions:0,seconds:5,gateway:null,
    seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),now:()=>new Date(time)}
  return {ports,call,run:new AuthoringRun(ports)}
}
async function initial():Promise<RunState>{return {...INITIAL_STATE,status:'needs-input',phase:'review',
  turns:[{id:'statement-1',role:'user',kind:'brief',text:'Always accept.',statement:'Always accept.',at:time}],cases:[structuredClone(row)],
  candidates:[{text:document,document:JSON.parse(document),digest:await digestOf(document),revision:1,producedBy:'conversation'}]}}
async function checked(options:Parameters<typeof fixture>[0]={}){
  const h=fixture(options),state=await initial()
  await h.run.restore(state)
  // Exercise the public action, including admission, checkpointing and readiness.
  h.run.recheck()
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).not.toBe('running'))
  expect(h.run.getSnapshot().candidates[0]?.check?.record,h.run.getSnapshot().detail).toBeDefined()
  return h
}
const saved=(run:AuthoringRun)=>decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(run.getSnapshot(),[]))))
afterEach(()=>vi.restoreAllMocks())

it('restores completed checks and readiness without evaluating or calling the model again',async()=>{
  const first=await checked(),h=fixture()
  expect(first.run.getSnapshot().status,first.run.getSnapshot().detail).toBe('ready')
  await h.run.restore(saved(first.run).state)
  expect(h.run.getSnapshot()).toMatchObject({restored:false,status:'ready',checkRecovery:'current'})
  expect(canCreateDraft(h.run.getSnapshot(),'draft',h.run.basisNow())).toBe(true)
  expect(vi.mocked(h.call).mock.calls.map(c=>c[0])).toEqual(['describe_runtime','describe_runtime'])
  expect(h.ports.turn).not.toHaveBeenCalled();expect(h.ports.seal).not.toHaveBeenCalled()
  const again=fixture();await again.run.restore(saved(h.run).state)
  expect(again.run.getSnapshot().status).toBe('ready')
})
it('retains only cited statement provenance when a draft has no conversation',async()=>{
  const first=await checked(),artifact=artifactCheckpoint(saved(first.run)),h=fixture()
  expect(artifact.state.turns).toEqual([])
  expect(artifact.state.candidates[0]?.previousCheck?.record?.statements).toEqual([['you-1','Always accept.']])
  await h.run.restore(artifact.state)
  expect(h.run.getSnapshot().status).toBe('ready')
})
it.each(['facts','expectation','document','runtime','statement','pin'] as const)('withholds readiness when %s changes',async(change)=>{
  const first=await checked(),copy=saved(first.run),h=fixture(change==='runtime'?{version:'0.30.0'}:{})
  if(change==='facts')copy.state.cases[0]!.facts={changed:true}
  if(change==='expectation')copy.state.cases[0]!.expectedDisposition={...outcome,outcomeId:'reject'}
  if(change==='document')copy.state.candidates[0]!.text=document+' '
  if(change==='statement')copy.state.turns[0]!.statement='Reject instead.'
  if(change==='pin')h.ports.pin=()=>({url:'https://changed.invalid',authority:'other',signer:{algorithm:'ed25519',public:'changed'}})
  await h.run.restore(copy.state)
  expect(h.run.getSnapshot().restored).toBe(true)
  expect(canCreateDraft(h.run.getSnapshot(),'draft',h.run.basisNow())).toBe(false)
  expect(h.run.getSnapshot().checkRecovery).toBe('outdated')
  expect(h.run.getSnapshot().candidates[0]?.check).toBeUndefined()
  expect(h.run.getSnapshot().candidates[0]?.previousCheck).toBeDefined()
})
it('does not promote a tampered or incomplete retained report',async()=>{
  const first=await checked(),copy=saved(first.run),h=fixture()
  copy.state.candidates[0]!.previousCheck!.record!.validation=reply({status:'valid',tool})
  await h.run.restore(copy.state)
  expect(h.run.getSnapshot()).toMatchObject({restored:true,checkRecovery:'unavailable'})
})
it('preserves failed comparisons as failed after reload',async()=>{
  const first=await checked({mismatch:true}),h=fixture()
  await h.run.restore(saved(first.run).state)
  expect(h.run.getSnapshot()).toMatchObject({restored:false,status:'needs-input',checkRecovery:'current'})
  expect(draftCheckStatus(h.run.getSnapshot()).label).toBe('Checks failed')
})
it('restores validation without claiming that an empty suite passed',async()=>{
  const h=fixture(),state=await initial();state.cases=[]
  await h.run.restore(state);h.run.recheck()
  await vi.waitFor(()=>expect(h.run.getSnapshot().checkRun).toBe('complete'))
  const reopened=fixture();await reopened.run.restore(saved(h.run).state)
  expect(draftCheckStatus(reopened.run.getSnapshot()).label).toBe('Valid · no tests')
  expect(reopened.run.getSnapshot().status).toBe('needs-input')
})
it.each(['running','failed','stopped'] as const)('does not turn a %s attempt into a completed passing check',async(checkRun)=>{
  const first=await checked(),copy=saved(first.run);copy.state.checkRun=checkRun
  const h=fixture();await h.run.restore(copy.state)
  expect(h.run.getSnapshot()).toMatchObject({restored:true,checkRecovery:'unavailable'})
  expect(draftCheckStatus(h.run.getSnapshot()).label).toBe(checkRun==='failed'?'Check failed':'Check interrupted')
})
it('keeps legacy summaries historical until an explicit run',async()=>{
  const first=await checked(),copy=saved(first.run);delete copy.state.candidates[0]!.previousCheck!.record
  const h=fixture();await h.run.restore(copy.state)
  expect(h.run.getSnapshot().restored).toBe(true);expect(h.call).not.toHaveBeenCalled()
  h.run.recheck();await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('ready'))
})
it('imports the exact retained inputs and full runtime report into Tests',async()=>{
  const h=await checked(),copy=saved(h.run)
  const draft={id:'draft-test',generation:1,updatedAt:time,documents:[],checkpoint:copy} as unknown as PackDraft
  const suite=await recoverDraft(emptySuite(),draft)
  expect(suite.runs[0]?.cases[0]?.row.facts).toEqual(row.facts)
  expect(suite.runs[0]?.report?.tool).toEqual(tool)
  expect(suite.runs[0]?.at).toBe(new Date(time).toISOString())
})
it('does not commit a stopped in-flight snapshot check',async()=>{
  const h=fixture(),abort=new AbortController()
  const call:CallTool=async(name,args)=>{const response=await h.call(name,args);if(name==='experimental_test_cases')abort.abort();return response}
  await expect(runSnapshotCheck(document,[row],call,abort.signal,h.ports.runtimeIdentity!,async()=>await digestOf('context'),[],time)).rejects.toMatchObject({name:'AbortError'})
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

it('keeps inherited test statements separate from new chat messages across reloads',async()=>{
  const first=await checked(),artifact=artifactCheckpoint(saved(first.run)),h=fixture()
  h.ports.turn=async(_request,_signal,emit)=>{emit({type:'message',text:'Noted.'})}
  await h.run.restore(artifact.state)
  h.run.send('Reject instead.')
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).not.toBe('running'))
  const reopened=fixture();await reopened.run.restore(saved(h.run).state)
  expect(reopened.run.getSnapshot().status,reopened.run.getSnapshot().detail).toBe('ready')
  expect(reopened.run.getSnapshot().inheritedStatements).toEqual([['you-1','Always accept.']])
  reopened.run.recheck()
  await vi.waitFor(()=>expect(reopened.run.getSnapshot().status).toBe('ready'))
  expect(reopened.run.getSnapshot().candidates[0]?.check?.record?.statements).toEqual([['you-1','Always accept.']])
})
it('does not let a late restore replace a newer explicit failed attempt',async()=>{
  const first=await checked(),h=fixture(),calls=h.ports.runtimeIdentity!
  let release!:()=>void,started=false
  h.ports.runtimeIdentity=async()=>{if(!started){started=true;await new Promise<void>(resolve=>{release=resolve})}return calls()}
  const restore=h.run.restore(saved(first.run).state)
  await vi.waitFor(()=>expect(started).toBe(true))
  h.ports.callTool=async()=>{throw Error('Runtime unavailable')}
  h.run.recheck()
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).toBe('failed'))
  release();await restore
  expect(h.run.getSnapshot().checkRun).toBe('failed')
  expect(canCreateDraft(h.run.getSnapshot(),'draft',h.run.basisNow())).toBe(false)
})
it('leaves unfinished expectation review available without an endless checking status',async()=>{
  const h=fixture(),state=await initial()
  state.cases=[];state.expectationIssues=[{id:row.id,original:row,message:'Review this expectation'}]
  await h.run.restore(state);h.run.recheck()
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).not.toBe('running'))
  expect(h.run.getSnapshot().checkRun).toBe('failed')
  expect(draftCheckStatus(h.run.getSnapshot()).label).toBe('Check failed')
  expect(h.ports.turn).not.toHaveBeenCalled()
})

/** A completed draft and a later response have independent recovery owners. */
async function interruptedConversation(status: 'failed' | 'stopped' | 'budget' = 'failed') {
  const first = await checked(), state = saved(first.run).state
  state.candidates[0]!.responseId = 'earlier-response'
  state.turns.push({id:'follow-up',role:'user',kind:'message',text:'Explain the result.',at:time})
  state.responses = [{id:'latest-response',afterTurnId:'follow-up',documents:[],websites:[],sourceIds:[],work:{items:[],notices:[]}}]
  state.phase = 'conversation'; state.status = status
  state.failure = status === 'failed' ? failure('invalid-response','model') : undefined
  state.detail = state.failure ? failureMessage(state.failure) : 'Interrupted'
  state.execution = {version:1,id:'execution',requestKey:'a'.repeat(64),phase:'work',status:status === 'budget' ? 'paused' : 'stopped',
    unit:'model-requests',used:status === 'budget' ? 20 : 11,limit:20,grants:[],plan:[],explanation:'',operations:[],context:{scope:'execution',entries:[]}} satisfies ExecutionCheckpoint
  return decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(state,[])))).state
}
it.each(['current','outdated','unavailable'] as const)('keeps failed response recovery after background draft verification is %s',async(result)=>{
  const state=await interruptedConversation(),h=fixture(result === 'outdated' ? {version:'0.30.0'} : {})
  if(result === 'unavailable')h.ports.runtimeIdentity=async()=>{throw Error('Runtime unavailable')}
  await h.run.restore(state)
  const before=h.run.getSnapshot()
  expect(h.run.canRetryResponse).toBe(true)
  await h.run.refreshCheck()
  expect(h.run.getSnapshot()).toMatchObject({phase:'conversation',status:'failed',detail:before.detail,failure:before.failure,execution:before.execution,checkRecovery:result})
  expect(h.run.canRetryResponse).toBe(true)
  expect(canCreateDraft(h.run.getSnapshot(),'draft',h.run.basisNow())).toBe(result === 'current')
  // A second persisted reload must preserve the same response and budget.
  const again=fixture();await again.run.restore(saved(h.run).state);await again.run.refreshCheck()
  expect(again.run.canRetryResponse).toBe(true)
  expect(again.run.getSnapshot().execution).toEqual(before.execution)
  expect(h.ports.turn).not.toHaveBeenCalled();expect(again.ports.turn).not.toHaveBeenCalled()
})
it.each(['stopped','budget'] as const)('keeps %s conversation controls when the runtime reconnects',async(status)=>{
  const h=fixture();await h.run.restore(await interruptedConversation(status));const before=h.run.getSnapshot()
  await h.run.refreshCheck()
  expect(h.run.getSnapshot()).toMatchObject({phase:before.phase,status:before.status,detail:before.detail,execution:before.execution,checkRecovery:'current'})
  expect(h.run.canRetryResponse).toBe(status === 'stopped')
  expect(h.ports.turn).not.toHaveBeenCalled()
})
it('keeps an unanswered question after background draft verification',async()=>{
  const state=await interruptedConversation(),h=fixture()
  state.status='needs-input';state.failure=undefined
  state.turns.push({id:'question',role:'assistant',kind:'question',text:'Which option?',at:time})
  state.questions=[{id:'question',prompt:{title:'Choose',question:'Which option?',selection:'single',options:[],recommendedOptionId:null,recommendation:null},draft:{selected:[],text:''}}]
  await h.run.restore(state);const before=h.run.getSnapshot()
  await h.run.refreshCheck()
  expect(h.run.getSnapshot()).toMatchObject({status:'needs-input',detail:before.detail,questions:before.questions,checkRecovery:'current'})
  expect(h.ports.turn).not.toHaveBeenCalled()
})
it('keeps response recovery when a document basis change retraces a legacy draft',async()=>{
  const h=fixture();await h.run.restore(await interruptedConversation());await h.run.refreshCheck()
  // Old checkpoints may retain passing summaries without a complete record.
  const candidate=h.run.getSnapshot().candidates[0]!
  delete candidate.check!.record
  delete candidate.previousCheck!.record
  h.ports.documents=()=>[]
  h.ports.pin=()=>({url:'https://changed.invalid',authority:'other',signer:{algorithm:'ed25519',public:'changed'}})
  h.run.basisChanged()
  await vi.waitFor(()=>expect(h.run.getSnapshot().tracing).toBe(false))
  expect(h.run.getSnapshot()).toMatchObject({phase:'conversation',status:'failed'})
  expect(h.run.canRetryResponse).toBe(true)
  expect(canCreateDraft(h.run.getSnapshot(),'draft',h.run.basisNow())).toBe(true)
})

it('does not let delayed draft verification overwrite an explicit retry',async()=>{
  const state=await interruptedConversation(),h=fixture(),identity=h.ports.runtimeIdentity!
  let release!:()=>void,started=false
  h.ports.runtimeIdentity=async()=>{started=true;await new Promise<void>(resolve=>{release=resolve});return identity()}
  h.ports.turn=vi.fn(async(_request,_signal,emit)=>{emit({type:'message',text:'The explanation is complete.'})})
  await h.run.restore(state)
  const verification=h.run.refreshCheck()
  await vi.waitFor(()=>expect(started).toBe(true))
  h.run.retryResponse()
  await vi.waitFor(()=>expect(h.ports.turn).toHaveBeenCalledTimes(1))
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).not.toBe('running'))
  const after=h.run.getSnapshot();release();await verification
  expect(h.run.getSnapshot()).toMatchObject({phase:after.phase,status:after.status,detail:after.detail})
  expect(h.run.getSnapshot().turns.filter(t=>t.id==='follow-up')).toHaveLength(1)
  expect(h.run.getSnapshot().turns.at(-1)?.text).toBe('The explanation is complete.')
})
it('lets explicit draft checks take ownership after an interrupted response',async()=>{
  const h=fixture();await h.run.restore(await interruptedConversation())
  h.run.recheck()
  await vi.waitFor(()=>expect(h.run.getSnapshot().status).not.toBe('running'))
  expect(h.run.getSnapshot()).toMatchObject({phase:'review',status:'ready',checkRecovery:'current'})
  expect(h.run.canRetryResponse).toBe(false)
  expect(h.ports.turn).not.toHaveBeenCalled()
})

it('offers an explicit allowance after failure on the last request, including after reload',async()=>{
  const state=await interruptedConversation(),h=fixture()
  state.execution!.used=state.execution!.limit
  state.failure=failure('output-limit','model');state.detail=failureMessage(state.failure)
  await h.run.restore(state);await h.run.refreshCheck()
  expect(h.run.canRetryResponse).toBe(false);expect(h.run.canExtendResponse).toBe(true)
  const again=fixture();await again.run.restore(saved(h.run).state);await again.run.refreshCheck()
  expect(again.run.canExtendResponse).toBe(true)
  expect(again.ports.turn).not.toHaveBeenCalled()
  expect(again.run.getSnapshot().execution).toMatchObject({used:20,limit:20,grants:[]})
  expect(again.run.prepareBudgetContinuation()).toBe(true)
  expect(again.run.getSnapshot().execution).toMatchObject({used:20,limit:30,status:'running',grants:[{from:20,to:30}]})
  expect(again.ports.turn).not.toHaveBeenCalled()
  again.ports.turn=vi.fn(async(_request,_signal,emit)=>{emit({type:'message',text:'Completed using retained work.'})})
  again.run.continueExecution()
  await vi.waitFor(()=>expect(again.run.getSnapshot().status).not.toBe('running'))
  expect(again.ports.turn).toHaveBeenCalledOnce()
  expect(again.run.getSnapshot().turns.filter(t=>t.id==='follow-up')).toHaveLength(1)
})
it('does not extend a stopped, provider-managed, completed or still-allowed response',async()=>{
  for(const change of ['stopped','provider','complete','remaining'] as const){
    const state=await interruptedConversation(),h=fixture();state.execution!.used=20
    if(change==='stopped')state.status='stopped'
    if(change==='provider')Object.assign(state.execution!,{unit:'provider-managed',used:1,limit:1})
    if(change==='complete')state.execution!.status='complete'
    if(change==='remaining')state.execution!.used=19
    await h.run.restore(state)
    expect(h.run.canExtendResponse).toBe(false)
    expect(h.run.prepareBudgetContinuation()).toBe(false)
    expect(h.ports.turn).not.toHaveBeenCalled()
  }
})

it('retains repeated-work recovery after refresh without offering more allowance for the loop',async()=>{
 const state=await interruptedConversation(),h=fixture()
 state.failure=failure('repeated-work','desk');state.detail=failureMessage(state.failure)
 await h.run.restore(state);await h.run.refreshCheck()
 expect(h.run.canRetryResponse).toBe(true)
 const again=fixture();await again.run.restore(saved(h.run).state);await again.run.refreshCheck()
 expect(again.run.getSnapshot().failure?.code).toBe('repeated-work')
 expect(again.run.canRetryResponse).toBe(true)
 const exhausted=saved(again.run).state;exhausted.execution!.used=exhausted.execution!.limit
 const last=fixture();await last.run.restore(exhausted);await last.run.refreshCheck()
 expect(last.run.canRetryResponse).toBe(false);expect(last.run.canExtendResponse).toBe(false)
 expect(last.run.prepareBudgetContinuation()).toBe(false)
 expect(last.ports.turn).not.toHaveBeenCalled()
})

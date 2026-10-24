// Handwritten assertions over temporary actual host API/native observations.
import assert from 'node:assert/strict'
import {readFile,readdir} from 'node:fs/promises'
import {join} from 'node:path'
import {createHash} from 'node:crypto'
import {decodeSnapshot,parseRunJSON} from '../web/src/assistant/serverRuns/decode.ts'
import {readFrames,RunObservations} from '../web/src/assistant/serverRuns/stream.ts'
import {readCreationAction,withCreation} from '../web/src/assistant/serverRuns/creation.ts'
import {readinessKey} from '../web/src/research/readiness.ts'

const dir=process.argv[2]
const sha256=text=>createHash('sha256').update(text).digest('hex')
assert(dir,'actual creation observations required')
const files=(await readdir(dir)).filter(name=>name.endsWith('.json'))
assert(files.length>=13,'API/native and explicit retry observations are required')
const actions=new Set(),engines=new Set()
for(const file of files){
 const output=parseRunJSON(await readFile(join(dir,file),'utf8')),raw=output.run
 const binding={owner:raw.owner,requestId:raw.requestId},run=decodeSnapshot(raw,binding)
 const chat=output.chat.chats.find(chat=>chat.id===raw.owner.id)
 assert(chat);assert(run.chatProjection.acknowledged)
 const saved=chat.checkpoint,request=saved.state.turns.find(turn=>turn.id===run.requestId)
 const action=readCreationAction(request.creation);actions.add(action.kind)
 const engine=file.startsWith('native-')?'native':'api';engines.add(engine)
 const models=run.operations.filter(operation=>operation.kind==='model')
 if(['recover','check','retry-validation','approve-correction'].includes(action.kind))assert.equal(models.length,0)
 else assert.equal(models.length,1)
 if(engine==='native')assert.equal(run.checkpoint.unit,'provider-managed')
 const state={...saved.state,candidates:saved.state.candidates.map(candidate=>({...candidate,digest:sha256(candidate.text)}))}
 if(run.creation){
  const current=withCreation(state,run.creation,run.state==='complete',true)
  assert.equal(current.candidates.at(-1).text,run.creation.candidate)
  if(run.creation.action==='approve-correction'&&run.state==='complete'){
   assert.equal(current.cases.length,2);assert(current.candidates.at(-1).check.cases.every(row=>row.passed))
   assert.equal(current.readiness,readinessKey(current));assert.equal(current.tracedBasis,run.creation.sourceDigest)
   assert.deepEqual(current.cases[0].facts,{zero:0,flag:false,nil:null,numeric:'1'})
   assert.equal(current.cases[0].expectedHandoffTarget,null)
  }
  const historical=withCreation(state,run.creation,false,false)
  assert.equal(historical.readiness,'');assert(historical.restored)
  assert(historical.expectationIssues.every(issue=>!issue.proposal))
  assert.throws(()=>withCreation({...state,cases:[...state.cases,{...state.cases[0],id:'invented'}]},run.creation,true))
 }
 for(const patch of [{requestId:'foreign-request'},{owner:{...raw.owner,revision:'f'.repeat(64)}}])assert.throws(()=>decodeSnapshot(raw,{...binding,...patch}))
 const bytes=new Uint8Array(await readFile(join(dir,file.replace(/\.json$/,'.sse'))));let offset=0
 const body=new ReadableStream({pull(controller){if(offset===bytes.length){controller.close();return}const end=Math.min(offset+37,bytes.length);controller.enqueue(bytes.slice(offset,end));offset=end}})
 const observations=new RunObservations(binding,run)
 for await(const frame of readFrames(body))observations.accept(frame)
 assert.deepEqual(observations.snapshot,run)
}
assert.deepEqual([...engines].sort(),['api','native'])
assert.deepEqual([...actions].sort(),['approve-correction','check','establish-cases','propose-correction','recover','research-choice','retry-validation'])
console.log('Creation readers: actual API/native actions, exact source/case projection, typed facts and targets, historical refusal, full-suite readiness, one-use admission and UTF8 SSE passed.')

import {expect,it,vi} from 'vitest'
import {checkPackProposal,packCheckAllowsAuto,packReviewContext,legacyPackBasis,readPackReview,samePack} from './packProposal'
import {checkpoint,decodeCheckpoint} from './checkpoint'
import {AuthoringRun,INITIAL_STATE,type Candidate,type RunPorts} from '../research/run'
import {emptySuite,type TestCase} from '../packs/test-workspace/model'
import {Ledger} from '../research/ledger'
import {digestOf,jsonIdentity} from '../research/checkCandidate'
import {composeMessageInput} from './messageInput'
const text=JSON.stringify({id:'https://example.org/p',version:'0.1.1',sources:[]})
const basis={packId:'p',path:'packs/p.json',text:JSON.stringify({id:'https://example.org/p',version:'0.1.0'})}
const candidate:Candidate={revision:2,document:JSON.parse(text),text,digest:await digestOf(text),producedBy:'conversation',packReview:{basis}}
const test=(i:number):TestCase=>({id:'case-'+i,name:'Case '+i,revision:1,origin:'manual',sources:[],sourceMappings:{},rationale:'Reviewed policy',
 row:{id:'case-'+i,facts:{numeric:'24',zero:0,no:false,nothing:null},...(i===20?{expectedErrorClass:'input-invalid',expectedErrorPhase:'preflight'}:{expectedDisposition:{kind:'outcome',outcomeId:'yes',handoff:{state:'none'},reasons:[]}})}})
const suite={...emptySuite(),cases:Array.from({length:21},(_,i)=>test(i))}
const reply=(v:unknown)=>({content:[],structuredContent:v})
const call=()=>vi.fn(async(name:string,args:Record<string,unknown>)=>{
 if(name==='validate')return reply({status:'valid',diagnostics:[]})
 const cases=JSON.parse(args.matrix as string).cases
 return reply({status:'mismatch',summary:{total:cases.length,passed:cases.length-1},packs:[{rows:cases.map((c:TestCase,i:number)=>({id:c.id,status:i?'passed':'mismatch',expected:'expected',actual:'actual'}))}]})
})
it('checks all saved cases once, retaining typed inputs and expected errors',async()=>{
 const tool=call(),read=vi.fn(async()=>suite),snapshot=JSON.stringify(suite)
 const check=await checkPackProposal(candidate,read,tool,new AbortController().signal)
 expect(tool.mock.calls.map(c=>c[0])).toEqual(['validate','experimental_test_cases'])
 const sent=JSON.parse(tool.mock.calls[1]![1].matrix as string).cases
 expect(sent).toHaveLength(21);expect(sent[0].facts).toEqual(suite.cases[0]!.row.facts)
 expect(sent[20].expectedErrorClass).toBe('input-invalid')
 expect(check.report?.summary).toEqual({total:21,passed:20});expect(check.suiteKey).toBe(jsonIdentity(suite.cases))
 expect(JSON.stringify(suite)).toBe(snapshot)
})
it('refuses a partial report and changes to the suite during the check',async()=>{
 const bad=vi.fn(async(name:string)=>reply(name==='validate'?{status:'valid'}:{status:'passed',summary:{total:21,passed:21,mismatched:0},packs:[{rows:[]}]}))
 await expect(checkPackProposal(candidate,async()=>suite,bad,new AbortController().signal)).rejects.toThrow('incomplete')
 const read=vi.fn().mockResolvedValueOnce(suite).mockResolvedValueOnce({...suite,cases:suite.cases.slice(1)})
 await expect(checkPackProposal(candidate,read,call(),new AbortController().signal)).rejects.toThrow('suite changed')
})
it('retains proposal basis and checks through chat checkpoint decoding',async()=>{
 const check=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 const decoded=decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint({...INITIAL_STATE,candidates:[{...candidate,packReview:{basis,check}}]},[]))))
 expect(decoded.state.candidates[0]?.packReview).toEqual({basis,check})
 expect(readPackReview({basis:{...basis,text:'bad'}})).toBeUndefined()
 expect(samePack(text,JSON.stringify(JSON.parse(text),null,2))).toBe(true)
 expect(samePack(text,basis.text)).toBe(false)
})
it('recovers the prior comparison from retained host context, never from assistant prose',()=>{
 const input=composeMessageInput('go ahead','',undefined,basis.text).prompt
 const state={...INITIAL_STATE,turns:[{id:'u',role:'user' as const,kind:'message' as const,text:'go ahead',input,at:'2026-10-14T10:00:00Z'}]}
 expect(legacyPackBasis(state,{id:'p',path:basis.path})).toEqual(basis)
 expect(legacyPackBasis({...state,turns:[{...state.turns[0]!,role:'assistant'}]},{id:'p',path:basis.path})).toBeUndefined()
})
it('saved-pack checks bypass creation-era cases, persist after reload, and never invoke a model on recheck',async()=>{
 const check=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(),researchTools:[],authorPrompt:'',maxRevisions:0,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),
 savedPackReview:{target:vi.fn(async()=>basis),check:vi.fn(async()=>check)}}
 const run=new AuthoringRun(ports)
 await run.restore({...INITIAL_STATE,candidates:[candidate],cases:[{id:'old-draft',facts:{},expectedDisposition:{},expectationSource:'obsolete',rationale:'Obsolete draft'}]})
 expect(ports.savedPackReview!.check).not.toHaveBeenCalled();expect(run.getSnapshot().detail).toBe('')
 run.recheck();await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(ports.savedPackReview!.check).toHaveBeenCalledTimes(1);expect(ports.turn).not.toHaveBeenCalled();expect(ports.callTool).not.toHaveBeenCalled()
 expect(run.getSnapshot()).toMatchObject({status:'complete',detail:'',checkRun:'complete'})
 const next=new AuthoringRun(ports);await next.restore(decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(run.getSnapshot(),[])))).state)
 expect(next.getSnapshot().candidates[0]?.packReview?.check?.report?.summary.total).toBe(21)
 expect(ports.savedPackReview!.check).toHaveBeenCalledTimes(1)
})

it('new saved-pack replies retain their exact editor basis and check the saved suite without a draft reviewer',async()=>{
 const check=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(async(_request,_signal,emit)=>{emit({type:'proposal',document:JSON.parse(text),unknowns:[]});emit({type:'end'})}),researchTools:[],authorPrompt:'',maxRevisions:0,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:async()=>basis,check:vi.fn(async()=>check)}}
 const run=new AuthoringRun(ports),editor={...basis,text:JSON.stringify({...JSON.parse(basis.text),title:'Unsaved edit'})}
 run.start('Update this pack',[],'Update this pack',[],'Update this pack','provided',editor)
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(run.getSnapshot().status,run.getSnapshot().detail).toBe('complete')
 expect(run.getSnapshot().candidates.at(-1)?.packReview?.basis).toEqual(editor)
 expect(ports.turn).toHaveBeenCalledTimes(1);expect(ports.savedPackReview!.check).toHaveBeenCalledTimes(1)
 expect(ports.callTool).not.toHaveBeenCalled()
 expect(vi.mocked(ports.turn).mock.calls[0]![0].prompt).toContain('A proposal does not save')
})
it('cancellation during the pack read prevents dispatching the model',async()=>{
 let finish!:(value:typeof basis)=>void
 const target=new Promise<typeof basis>(resolve=>{finish=resolve})
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(),researchTools:[],authorPrompt:'',maxRevisions:0,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:()=>target,check:vi.fn()}}
 const run=new AuthoringRun(ports);run.start('Update this pack',[]);run.stop();finish(basis)
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(ports.turn).not.toHaveBeenCalled()
})

it('corrects failed saved-pack proposals within the original execution allowance, retaining expectations and basis',async()=>{
 const failed=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal),original=JSON.stringify(suite)
 const execution={version:1 as const,id:'execution',requestKey:'a'.repeat(64),phase:'work' as const,status:'complete' as const,unit:'model-requests' as const,used:7,limit:20,grants:[],plan:[],explanation:'',operations:[],context:{scope:'scope',entries:[]}}
 let turn=0
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(async(request,_signal,emit)=>{
   if(turn++){expect(request.context?.execution).toEqual(execution);expect(request.conversation).toBe(true);expect(request.prompt).toContain('HOST CHECK FINDINGS');expect(request.prompt).toContain('Preserve every saved case')}
   emit({type:'proposal',document:{...JSON.parse(text),version:turn===1?'0.1.1':'0.1.2'},unknowns:[]})
   emit({type:'execution_checkpoint',checkpoint:execution});emit({type:'end'})
 }),researchTools:[],authorPrompt:'',maxRevisions:2,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:async()=>basis,context:async()=>packReviewContext(basis,suite),check:vi.fn(async c=>({...failed,documentDigest:c.digest,...(turn>1?{report:{status:'passed',summary:{total:21,passed:21,mismatched:0},packs:[{...failed.report!.packs![0]!,status:'passed',summary:{total:21,passed:21,mismatched:0},rows:failed.report!.packs![0]!.rows!.map(row=>({...row,status:'passed'}))}]}}:{})}))}}
 const run=new AuthoringRun(ports);run.start('Fix the pack',[])
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(run.getSnapshot().status,run.getSnapshot().detail).toBe('complete')
 expect(ports.turn).toHaveBeenCalledTimes(2);expect(ports.savedPackReview!.check).toHaveBeenCalledTimes(2)
 expect(run.getSnapshot().candidates.at(-1)?.packReview?.basis).toEqual(basis)
 expect(JSON.stringify(suite)).toBe(original)
 expect(vi.mocked(ports.turn).mock.calls[0]![0].prompt).toContain('SAVED TESTS AND COVERAGE')
})
it('allows a policy explanation to stop correction without inventing a replacement',async()=>{
 const failed=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 let turns=0
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(async(_r,_s,emit)=>{if(turns++)emit({type:'message',text:'The saved expectation refers to an older policy. Please review the intended handoff.'});else emit({type:'proposal',document:JSON.parse(text),unknowns:[]});emit({type:'end'})}),researchTools:[],authorPrompt:'',maxRevisions:4,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:async()=>basis,check:vi.fn(async()=>failed)}}
 const run=new AuthoringRun(ports);run.start('Fix the pack',[])
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(run.getSnapshot().status,run.getSnapshot().detail).toBe('complete');expect(run.getSnapshot().candidates).toHaveLength(1)
 expect(ports.turn).toHaveBeenCalledTimes(2);expect(ports.savedPackReview!.check).toHaveBeenCalledOnce()
})
it('stops unchanged automatic corrections instead of looping through the allowance',async()=>{
 const failed=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(async(_r,_s,emit)=>{emit({type:'proposal',document:JSON.parse(text),unknowns:[]});emit({type:'end'})}),researchTools:[],authorPrompt:'',maxRevisions:4,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:async()=>basis,check:vi.fn(async()=>failed)}}
 const run=new AuthoringRun(ports);run.start('Fix the pack',[])
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(run.getSnapshot().status).toBe('stalled');expect(ports.turn).toHaveBeenCalledTimes(2);expect(ports.savedPackReview!.check).toHaveBeenCalledOnce()
})

it('requires complete assertions before Auto, treating coverage as an advisory, while ordinary review remains possible',async()=>{
 const c=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 expect(packCheckAllowsAuto(c)).toBe(false)
 const report={...c.report!,status:'passed' as const,summary:{total:21,passed:21,mismatched:0},packs:[{...c.report!.packs![0]!,rows:c.report!.packs![0]!.rows!.map(r=>({...r,status:'passed' as const})),coverage:[]}]}
 expect(packCheckAllowsAuto({...c,report})).toBe(true)
 expect(packCheckAllowsAuto({...c,totalCases:22,report})).toBe(false)
 expect(packCheckAllowsAuto({...c,report:{...report,packs:[{...report.packs[0]!,coverage:[{probe:'no-match',status:'missing',detail:'Not exercised'}]}]}})).toBe(true)
})

it('keeps an interactive policy question during correction and retains the failed check',async()=>{
 const failed=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 let turns=0
 const ports:RunPorts={mode:'draft',ledger:new Ledger('s'),callTool:vi.fn(),turn:vi.fn(async(_r,_s,emit)=>{
  if(turns++)emit({type:'user_question',id:'policy',question:{title:'Missing evidence',question:'Should missing size request a handoff?',selection:'single',options:[],recommendedOptionId:null,recommendation:null}})
  else emit({type:'proposal',document:JSON.parse(text),unknowns:[]})
  emit({type:'end'})
 }),researchTools:[],authorPrompt:'',maxRevisions:2,seconds:5,gateway:null,seal:vi.fn(),registry:vi.fn(),newSession:()=> 's',log:vi.fn(),savedPackReview:{target:async()=>basis,check:vi.fn(async()=>failed)}}
 const run=new AuthoringRun(ports);run.start('Fix this pack',[])
 await vi.waitFor(()=>expect(run.running).toBe(false))
 expect(run.getSnapshot()).toMatchObject({status:'needs-input',detail:'Waiting for your answer'})
 expect(run.getSnapshot().questions?.[0]).toMatchObject({id:'policy',prompt:{question:'Should missing size request a handoff?'}})
 expect(run.getSnapshot().candidates.at(-1)?.packReview?.check).toEqual(failed)
 expect(ports.turn).toHaveBeenCalledTimes(2)
})

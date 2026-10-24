import {expect,it,vi} from 'vitest'
import {checkPackProposal,packCheckAllowsAuto,legacyPackBasis,readPackReview,samePack} from './packProposal'
import {checkpoint,decodeCheckpoint} from './checkpoint'
import {INITIAL_STATE,type Candidate} from '../research/run'
import {emptySuite,type TestCase} from '../packs/test-workspace/model'
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
it('requires complete assertions before Auto, treating coverage as an advisory, while ordinary review remains possible',async()=>{
 const c=await checkPackProposal(candidate,async()=>suite,call(),new AbortController().signal)
 expect(packCheckAllowsAuto(c)).toBe(false)
 const report={...c.report!,status:'passed' as const,summary:{total:21,passed:21,mismatched:0},packs:[{...c.report!.packs![0]!,rows:c.report!.packs![0]!.rows!.map(r=>({...r,status:'passed' as const})),coverage:[]}]}
 expect(packCheckAllowsAuto({...c,report})).toBe(true)
 expect(packCheckAllowsAuto({...c,totalCases:22,report})).toBe(false)
 expect(packCheckAllowsAuto({...c,report:{...report,packs:[{...report.packs[0]!,coverage:[{probe:'no-match',status:'missing',detail:'Not exercised'}]}]}})).toBe(true)
})

import {expect,it} from 'vitest'
import {sha256} from '@judgment-pack/harness/records'
import {INITIAL_STATE} from '../../research/initialState'
import {readCreation,readCreationAction,withCreation} from './creation'
import {historicalDraftBinding} from '../../packs/drafts/historicalBinding'
import type {PackDraft} from '../../packs/drafts/model'
import {canCreateDraft,type RunState} from '../../research/run'
import {readinessKey} from '../../research/readiness'

const candidate='{ "title":"Exact", "zero":0, "flag":false, "nil":null }'
const disposition={kind:'outcome',outcomeId:'accept',reasons:[],handoff:{state:'none'}}
const row={id:'typed',facts:{zero:0,flag:false,nil:null,numeric:'1'},expectedDisposition:disposition,expectedHandoffTarget:null,expectationSource:'you-1',rationale:'The person requires accept.'}
const evidence=()=>({action:'check',candidateDigest:sha256(candidate),sourceDigest:'b'.repeat(64),generation:1,candidate,cases:JSON.stringify([row]),held:'[]',issues:'[]',citations:[],unknowns:[],check:{documentDigest:sha256(candidate),suiteKey:'current independent cases',at:'2026-10-23T00:00:00Z',valid:true,diagnostics:[],totalCases:1,report:{status:'passed',summary:{total:1,passed:1},packs:[{rows:[{id:row.id,status:'passed',expected:JSON.stringify(disposition),actual:JSON.stringify(disposition),actualHandoffTarget:'null'}]}]}}})
const state=():RunState=>({...INITIAL_STATE,cases:[row],candidates:[{text:candidate,digest:sha256(candidate),document:JSON.parse(candidate),revision:1,producedBy:'conversation' as const}]})

it('reads all seven closed actions and rejects invented operands or implicit research permission',()=>{
 const actions=[{kind:'recover',messageId:'assistant-one',messageDigest:'a'.repeat(64)},...['check','retry-validation','establish-cases'].map(kind=>({kind,candidateDigest:'a'.repeat(64)})),{kind:'propose-correction',candidateDigest:'a'.repeat(64),issueId:'typed'},{kind:'approve-correction',candidateDigest:'a'.repeat(64),issueId:'typed',token:'b'.repeat(64)},{kind:'research-choice',choice:'provided'}]
 for(const action of actions){expect(readCreationAction(action)).toEqual(action);expect(()=>readCreationAction({...action,approved:true})).toThrow()}
 for(const bad of [{kind:'check'},{kind:'approve-correction',candidateDigest:'a'.repeat(64),issueId:'typed'},{kind:'research-choice',choice:'auto'},{kind:'recover',messageId:'assistant-one',messageDigest:'not a digest'}])expect(()=>readCreationAction(bad)).toThrow()
})
it('requires exact current candidate and full ordered typed case evidence before projecting readiness',()=>{
 const current=withCreation(state(),readCreation(evidence()),true)
 expect(current.status).toBe('ready');expect(current.restored).toBe(false);expect(current.readiness).not.toBe('')
 expect(current.cases[0]?.facts).toEqual({zero:0,flag:false,nil:null,numeric:'1'})
 expect(current.candidates[0]?.check?.cases[0]?.expected).toEqual({disposition,handoffTarget:null})
 for(const changed of [{...state(),cases:[{...row,facts:{zero:'0'}}]},{...state(),cases:[{...row,expectedHandoffTarget:'other'}]},{...state(),candidates:[{...state().candidates[0]!,text:candidate+' '}]}])expect(()=>withCreation(changed,readCreation(evidence()),true)).toThrow()
 const prior=withCreation(state(),readCreation(evidence()),false)
 expect(prior.restored).toBe(true);expect(prior.readiness).toBe('');expect(prior.candidates[0]?.check).toBeUndefined();expect(prior.candidates[0]?.previousCheck?.valid).toBe(true)
})
it('rejects shrunken, duplicate, stale and malformed observation suites',()=>{
 for(const changed of [{...evidence(),candidateDigest:'f'.repeat(64)},{...evidence(),cases:JSON.stringify([row,row])},{...evidence(),check:{...evidence().check,totalCases:2}},{...evidence(),unknowns:[false]},{...evidence(),citations:[{traced:false}]},{...evidence(),proposal:{token:'a'.repeat(64),issueId:'absent',expectedDisposition:disposition,rationale:'No matching issue'}}])expect(()=>readCreation(changed)).toThrow()
 const held={...evidence(),check:undefined,cases:'[]',held:JSON.stringify([{...row,expectedDisposition:null}])}
 expect(readCreation(held).held).toBe(held.held)
 expect(()=>readCreation({...held,cases:JSON.stringify([row])})).toThrow()
})
it('constructs a historical draft display without restoring execution, current checks or correction tokens',()=>{
 const old={...state(),status:'ready' as const,readiness:'old-authority',execution:{grant:'old'},expectationIssues:[{id:'typed',original:row,message:'Old finding',proposal:{token:'historical',candidateDigest:sha256(candidate),expectedDisposition:disposition,rationale:'Old'}}]}
 old.candidates[0]={...old.candidates[0]!,check:{documentDigest:sha256(candidate),valid:true,diagnostics:[],cases:[]}}
 const binding=historicalDraftBinding({checkpoint:{state:old,sources:[]}} as unknown as PackDraft)
 expect(binding.run).toBeNull();expect(binding.state.execution).toBeUndefined();expect(binding.state.restored).toBe(true);expect(binding.state.readiness).toBe('');expect(binding.state.expectationIssues[0]?.proposal).toBeUndefined();expect(binding.state.candidates[0]?.check).toBeUndefined();expect(binding.state.candidates[0]?.previousCheck?.valid).toBe(true)
})

it('reads the shared original-position vectors without dropping explicit custody',async()=>{
 const {default:vectors}=await import('../../../../internal/desk/testdata/assistant-creation-positions.json')
 const original={...row,id:'invalid'},finding={status:'invalid',message:'Review original expectation.',admitted:true}
 for(const v of vectors){
  const issue=JSON.stringify({id:original.id,original,finding})
  const position='positionJSON' in v?v.positionJSON:undefined
  const issues='['+issue.slice(0,-1)+(position===undefined?'':',\"position\":'+position)+'}]'
  const read=()=>readCreation({...evidence(),check:undefined,issues})
  if(v.valid)expect(JSON.parse(read().issues)[0].position,v.name).toBe(position===undefined?undefined:JSON.parse(position))
  else expect(read,v.name).toThrow()
 }
})

it('admits a passing partial correction check while preserving remaining evidence and blocking readiness',()=>{
 const original={...row,id:'remaining'},finding={status:'invalid' as const,message:'Invalid expectation.',admitted:true}
 const unresolved={id:original.id,original,message:finding.message,finding,position:0}
 const resolved={id:'prior',original:{...row,id:'prior'},message:'Approved',resolved:{replacement:{...row,id:'prior'},approvedAt:'2026-10-24T00:00:00Z',rationale:'Explicit approval.'}}
 const partial={...evidence(),action:'approve-correction',issues:JSON.stringify([{id:original.id,original,finding,position:0}])}
 const saved={...state(),status:'ready' as const,readiness:'stale',expectationIssues:[unresolved,resolved]}
 const view=readCreation(partial),current=withCreation(saved,view,true)
 expect(current.candidates[0]?.check?.cases.every(c=>c.passed)).toBe(true)
 expect(current.status).toBe('needs-input');expect(current.readiness).toBe('')
 expect(canCreateDraft(current,'draft',current.tracedBasis)).toBe(false)
 expect(canCreateDraft({...current,status:'ready',readiness:readinessKey(current)},'draft',current.tracedBasis)).toBe(false)
 expect(current.expectationIssues.map(i=>i.id)).toEqual(['remaining','prior'])
 expect(current.cases).toBe(saved.cases)
 const historical=withCreation(saved,view,false,false)
 expect(historical.candidates[0]?.check).toBeUndefined();expect(historical.candidates[0]?.previousCheck?.valid).toBe(true)
 expect(historical.readiness).toBe('');expect(historical.status).toBe('needs-input')
 for(const changed of [{...partial,action:'check'},{...partial,check:{...partial.check,report:{...partial.check.report,packs:[{rows:[{...partial.check.report.packs[0]!.rows[0]!,id:'foreign'}]}]}}},{...partial,check:{...partial.check,valid:false}},{...partial,check:{...partial.check,totalCases:2}},{...partial,check:{...partial.check,report:{...partial.check.report,status:'mismatch',summary:{total:1,passed:0},packs:[{rows:[{...partial.check.report.packs[0]!.rows[0]!,status:'mismatch'}]}]}}}])expect(()=>readCreation(changed)).toThrow()
})

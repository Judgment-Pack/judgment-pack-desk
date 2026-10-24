import {expect,it} from 'vitest'
import {sha256} from '@judgment-pack/harness/records'
import {INITIAL_STATE} from '../../research/initialState'
import {readCreation,readCreationAction,withCreation} from './creation'
import {historicalDraftBinding} from '../../packs/drafts/historicalBinding'
import type {PackDraft} from '../../packs/drafts/model'
import type {RunState} from '../../research/run'

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

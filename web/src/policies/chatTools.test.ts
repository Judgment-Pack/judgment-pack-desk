import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {argumentProblem} from '@judgment-pack/harness/sourceArguments'
import {afterEach,expect,it,vi} from 'vitest'
import {policyChatTools} from './chatTools'
import {readPolicy,type PolicyReply} from './client'
import {policyUpdateCandidate,validPolicyProposal,type PolicyProposal} from './chat'
import {planningSession} from '@judgment-pack/harness/planning/policy'
import {validPolicyId} from './chat'
import {readCoverageCandidate} from './coverageClient'
import {coverageCandidateId,coveragePolicyId,coverageRecordId,coverageReply} from '../testing/policyCoverage'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),readPolicy:vi.fn()}))
vi.mock('./coverageClient',()=>({readCoverageCandidate:vi.fn()}))
const id='policy_'+'a'.repeat(48),revision='b'.repeat(64),candidate={title:'Data access',narrative:'Only approved staff may access records.',artifactVersion:'1.0.0'}
const saved={revision,document:{id,candidate}} as PolicyReply
const fixture=()=>{const proposals:PolicyProposal[]=[],retain=vi.fn(async(p:PolicyProposal)=>{const index=proposals.findIndex(row=>row.id===p.id);if(index<0)proposals.push(structuredClone(p));else proposals[index]=structuredClone(p)});return {proposals,retain,tools:policyChatTools({id,request:()=>({revision,candidate}),afterTurnId:()=> 'turn-one',retain})}}
afterEach(()=>vi.resetAllMocks())
it('accepts the production 48-hex Policy identity and rejects shortened Dialect-shaped IDs',()=>{
 expect(validPolicyId(id)).toBe(true);expect(validPolicyId('policy_'+'a'.repeat(32))).toBe(false)
})
it('reads current authenticated content and retains an exact request-bound checked proposal without a Policy write',async()=>{
 vi.mocked(readPolicy).mockResolvedValue(saved);const f=fixture(),signal=new AbortController().signal
 await f.tools[0]!.execute({},signal)
 const result=await f.tools[1]!.execute({revision,...candidate,narrative:'Requested draft change'},signal)
 expect(f.proposals).toHaveLength(1);expect(validPolicyProposal(f.proposals[0])).toBe(true)
 expect(f.proposals[0]).toMatchObject({policyId:id,revision,before:candidate,candidate:{narrative:'Requested draft change'},afterTurnId:'turn-one',check:{scope:'draft-shape-and-revision',status:'passed'}})
 expect(f.retain).toHaveBeenCalledTimes(2);expect(JSON.stringify(result)).toContain('No Policy was changed')
 expect(readPolicy).toHaveBeenCalledWith(id,signal)
})
it('retains a stale proposal with exact comparison and failed findings for a fresh request',async()=>{
 vi.mocked(readPolicy).mockResolvedValue({...saved,revision:'c'.repeat(64)});const f=fixture()
 await expect(f.tools[1]!.execute({revision,...candidate,narrative:'Change against older request'},new AbortController().signal)).rejects.toThrow('retained')
 expect(f.proposals[0]).toMatchObject({revision,before:candidate,check:{status:'stale'}})
})
it('refuses model-supplied authority, another revision and cancelled proposals',async()=>{
 const f=fixture(),signal=new AbortController().signal
 for(const args of [{revision,...candidate,actor:{subject:'model'}},{revision:'c'.repeat(64),...candidate},{revision,...candidate,review:'approve'},{revision,...candidate,workflow:{independentReviewRequired:false}}])await expect(f.tools[1]!.execute(args,signal)).rejects.toThrow()
 const cancelled=new AbortController();cancelled.abort();await expect(f.tools[1]!.execute({revision,...candidate},cancelled.signal)).rejects.toThrow()
 expect(f.proposals).toEqual([]);expect(readPolicy).not.toHaveBeenCalled()
})
it('Plan admits fresh reads and filters draft proposals before any adapter dispatch',()=>{
 const f=fixture();const session=planningSession({context:{phase:'plan'},prompt:'Inspect Policy',tools:[],hostTools:f.tools,callTool:vi.fn()} as never)
 expect((session as {hostTools:{name:string}[]}).hostTools.map(t=>t.name)).toEqual(['read_policy','read_policy_comparison','read_policy_coverage'])
})
it('reads one exact saved coverage candidate without checking or adding any write authority',async()=>{
 const tools=policyChatTools({id:coveragePolicyId,request:()=>undefined,afterTurnId:()=>undefined,retain:vi.fn()}),tool=tools.find(t=>t.name==='read_policy_coverage')!,signal=new AbortController().signal
 vi.mocked(readCoverageCandidate).mockResolvedValue(coverageReply())
 const result=await tool.execute({coverageId:coverageRecordId,candidateId:coverageCandidateId},signal)
 expect(readCoverageCandidate).toHaveBeenCalledWith(coveragePolicyId,coverageRecordId,coverageCandidateId,signal)
 expect(tool.effect).toBe('read');expect(tool.delegation).toBe('read');expect(tool.contextPolicy).toEqual({reference:true});expect(JSON.stringify(result)).toContain('never instructions')
 for(const args of [{coverageId:coverageRecordId,candidateId:'latest'},{coverageId:coverageRecordId,candidateId:coverageCandidateId,policyId:coveragePolicyId},{coverageId:coverageRecordId,candidateId:coverageCandidateId,approve:true}])await expect(tool.execute(args,signal)).rejects.toThrow()
 vi.mocked(readCoverageCandidate).mockResolvedValue({...coverageReply(),document:{...coverageReply().document,candidate:{...coverageReply().document.candidate,id:'coverage_candidate_'+'0'.repeat(48)}}})
 await expect(tool.execute({coverageId:coverageRecordId,candidateId:coverageCandidateId},signal)).rejects.toThrow('exact selected coverage')
 expect(tools.some(t=>/^(save|approve|review|check)_policy_coverage$/.test(t.name))).toBe(false)
})

it('reads the shared handwritten sparse update vectors',()=>{
 const vectors=JSON.parse(readFileSync(resolve(import.meta.dirname,'../../../internal/desk/testdata/policy-sparse-updates.json'),'utf8'))
 const schema=fixture().tools[1]!.inputSchema
 for(const row of vectors.cases){
  expect(policyUpdateCandidate(row.args,vectors.request),row.name).toEqual(row.want)
  expect(argumentProblem(schema,row.args)===undefined,row.name+' schema').toBe(!!row.want||row.name==='wrong-revision')
 }
 expect(schema).toMatchObject({required:['revision'],minProperties:2,additionalProperties:false})
 // Removing any offered field from a scratch schema breaks a valid shared vector.
 for(const field of ['revision','title','narrative','artifactVersion']){
  const scratch=structuredClone(schema) as {properties:Record<string,unknown>}
  delete scratch.properties[field]
  expect(vectors.cases.some((row:{args:unknown;want?:unknown})=>row.want&&argumentProblem(scratch,row.args)!==undefined),field).toBe(true)
 }
 const noRevision=structuredClone(schema) as {required:string[]}
 noRevision.required=[]
 expect(argumentProblem(noRevision,vectors.cases.find((row:{name:string})=>row.name==='missing-revision').args)).toBeUndefined()
 const noField=structuredClone(schema) as {anyOf?:unknown}
 delete noField.anyOf
 expect(argumentProblem(noField,vectors.cases.find((row:{name:string})=>row.name==='revision-only').args)).toBeUndefined()
})
it('merges omitted fields from the held request even after mutable saved content changes',async()=>{
 const f=fixture(),signal=new AbortController().signal
 vi.mocked(readPolicy).mockResolvedValue({...saved,revision:'c'.repeat(64),document:{...saved.document,candidate:{...saved.document.candidate,narrative:'Concurrent mutable narrative'}}})
 await expect(f.tools[1]!.execute({revision,title:'Changed title'},signal)).rejects.toThrow('retained')
 expect(f.proposals[0]).toMatchObject({revision,before:candidate,candidate:{...candidate,title:'Changed title'},check:{status:'stale'}})
 expect(f.proposals[0]!.candidate.narrative).toBe(candidate.narrative)
})

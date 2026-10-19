import {afterEach,expect,it,vi} from 'vitest'
import {adoptionFixtures,adoptionId,adoptionOutcome} from '../testing/policyAdoption'
import {adoptionSelection,type AdoptionCommand} from './adoption'
import {matchesAdoptionOutcome,readAdoption,recoverAdoptionOperation,validAdoptionReply,writeAdoption} from './adoptionClient'
import {createAdoptionPendingJournal} from './adoptionPending'

afterEach(()=>{vi.unstubAllGlobals();sessionStorage.clear()})
it('validates the retained closure and rejects duplicate, missing and malformed evidence',async()=>{
 const {reply}=await adoptionFixtures();expect(validAdoptionReply(reply)).toBe(true)
 const duplicate=structuredClone(reply);duplicate.document.candidate.dependencies.push(duplicate.document.candidate.dependencies[0]!);expect(validAdoptionReply(duplicate)).toBe(false)
 const malformed=structuredClone(reply);malformed.document.candidate.dependencies[0]!.coverageReview.candidateId='coverage_candidate_'+'0'.repeat(48);expect(validAdoptionReply(malformed)).toBe(false)
 const oversized=structuredClone(reply);oversized.document.candidate.materials=Array(17).fill(oversized.document.candidate.check);expect(validAdoptionReply(oversized)).toBe(false)
 const incomplete=structuredClone(reply);incomplete.document.candidate.dependencies=[];expect(validAdoptionReply(incomplete)).toBe(false)
 const invalidPublication=structuredClone(reply);invalidPublication.document.candidate.pack.publicationId='not-a-publication';expect(validAdoptionReply(invalidPublication)).toBe(false)
 const invalidEvidence=structuredClone(reply);delete invalidEvidence.document.candidate.dependencies[0]!.coverageReview.check;expect(validAdoptionReply(invalidEvidence)).toBe(false)
})
it('reads the exact requested resource and refuses wrong adoption and Pack publication identities',async()=>{
 const {reply,pack}=await adoptionFixtures(),fetch=vi.fn(async(_input:RequestInfo|URL,_init?:RequestInit)=>Response.json(reply));vi.stubGlobal('fetch',fetch)
 await expect(readAdoption(adoptionId,pack)).resolves.toEqual(reply)
 expect(fetch.mock.calls[0]?.[0]).toBe('/api/policy-adoptions/'+adoptionId)
 await expect(readAdoption('adoption_'+'0'.repeat(48),pack)).rejects.toMatchObject({code:'adoption-invalid-response'})
 await expect(readAdoption(adoptionId,{...pack,publicationId:'0'.repeat(64)})).rejects.toMatchObject({code:'adoption-invalid-response'})
})
it('matches the entire exact dependency set and rejects substituted operation receipts',async()=>{
 const {reply,pack,choices}=await adoptionFixtures(),command:AdoptionCommand={action:'prepare',operationId:crypto.randomUUID(),pack,coverages:choices.map(adoptionSelection)},pending={pack,command},result=adoptionOutcome(command,reply)
 expect(matchesAdoptionOutcome(result,pending)).toBe(true)
 const omitted=structuredClone(result);omitted.adoption.document.candidate.dependencies.pop();expect(matchesAdoptionOutcome(omitted,pending)).toBe(false)
 const substituted=structuredClone(result);substituted.receipt.adoptionCandidateId='adoption_candidate_'+'0'.repeat(48);expect(matchesAdoptionOutcome(substituted,pending)).toBe(false)
 const resource=structuredClone(result);resource.adoption.document.candidate.pack.publicationId='0'.repeat(64);expect(matchesAdoptionOutcome(resource,pending)).toBe(false)
})
it('journals by session, workspace and configured resource while retaining exact publication context',async()=>{
 const {reply,pack,choices}=await adoptionFixtures(),journal=await createAdoptionPendingJournal('Eligibility'),other=await createAdoptionPendingJournal('Other Pack')
 const pending={pack,command:{action:'prepare' as const,operationId:crypto.randomUUID(),pack,coverages:choices.map(adoptionSelection)}}
 journal.store(pending);expect((await createAdoptionPendingJournal('Eligibility')).read()).toEqual(pending);expect(other.read()).toBeUndefined();expect(journal.key).not.toContain('session-for-testing');expect(reply.document.candidate.pack).toEqual(journal.read()?.pack)
})
it('classifies lost write response as uncertain and uses a read-only recovery request',async()=>{
 const {pack,choices}=await adoptionFixtures(),command:AdoptionCommand={action:'prepare',operationId:crypto.randomUUID(),pack,coverages:choices.map(adoptionSelection)},fetch=vi.fn(async(_input:RequestInfo|URL,_init?:RequestInit)=>{throw Error('lost response')});vi.stubGlobal('fetch',fetch)
 await expect(writeAdoption(command)).rejects.toMatchObject({code:'delivery-unknown',uncertain:true});await expect(recoverAdoptionOperation(command.operationId)).rejects.toMatchObject({code:'unavailable',uncertain:false})
 expect(fetch).toHaveBeenCalledTimes(2);expect(fetch.mock.calls[0]?.[1]).toMatchObject({method:'POST'});expect(fetch.mock.calls[1]?.[0]).toBe('/api/policy-operations/'+command.operationId);expect(fetch.mock.calls[1]?.[1]).not.toHaveProperty('method','POST')
})

it('preserves selection and recovery state while an outdated Runtime requires the official draft contract',async()=>{
 const {pack,choices}=await adoptionFixtures(),command:AdoptionCommand={action:'prepare',operationId:crypto.randomUUID(),pack,coverages:choices.map(adoptionSelection)},journal=await createAdoptionPendingJournal('Official draft'),pending={pack,command}
 journal.store(pending)
 const fetch=vi.fn(async()=>Response.json({code:'runtime-unavailable',outcome:'not-committed'},{status:503,headers:{'X-Jpack-Desk-Refusal':'runtime-unavailable'}}));vi.stubGlobal('fetch',fetch)
 await expect(writeAdoption(command)).rejects.toMatchObject({code:'runtime-unavailable',uncertain:false,message:'Update the connected Runtime to support official Policy draft policy-v1.0.0-draft.1 (format 1.0.0). Your selection and saved governing-policy history remain available.'})
 expect(journal.read()).toEqual(pending);expect(fetch).toHaveBeenCalledTimes(1)
})

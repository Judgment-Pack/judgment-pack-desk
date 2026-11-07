import {validPlans,type ChatPlan} from '@judgment-pack/harness/planning/model'
import type {PlanResolver} from '@judgment-pack/harness/planning/references'
import {LegacyPlanReferenceUnresolved} from '@judgment-pack/harness/planning/references'
import {answer,deskFetch,readFile} from '../../files/client'
import {dialectAPI} from '../../dialects/client'

/** Desk's saved wire contract is closed, including the two reference variants.
 * The shared reader supplies semantic limits; this preserves exact custody. */
export function validDeskPlans(value:unknown):value is ChatPlan[] {
 if(!validPlans(value))return false
 return value.every(plan=>{
  const keys=Object.keys(plan).sort().join(',')
  if(keys!=='createdAt,id,objective,questions,references,revision,status,steps'&&keys!=='acceptedAt,createdAt,id,objective,questions,references,revision,status,steps')return false
  const seen=new Set<string>()
  return plan.references.every(ref=>{
   const current='digest' in ref,keys=Object.keys(ref).sort().join(','),key=current?'current\0'+ref.id:ref.kind+'\0'+ref.id
   if(keys!==(current?'digest,id,kind':'id,kind,revision')||seen.has(key)||typeof (current?ref.digest:ref.revision)!=='string')return false
   seen.add(key);return true
  })
 })
}

/** Historical Desk locators are explicitly bound to opaque handles for this review
 * only. This does not create an artifact registry or rewrite the saved pins. */
export function deskPlanResolver(plan:ChatPlan,saved?:{chat:string;revision:(signal:AbortSignal)=>Promise<string>}):PlanResolver {
 const bindings=plan.references.filter(ref=>'revision' in ref).map((ref,index)=>({ref,id:`desk-plan-legacy-${index+1}`}))
 return {
  resolve:async(reference,signal)=>{
   if(!saved||!plan.references.some(ref=>'digest' in ref&&ref.kind===reference.kind&&ref.id===reference.id))throw new LegacyPlanReferenceUnresolved()
   const revision=await saved.revision(signal)
   signal.throwIfAborted()
   const value=await answer<unknown>(await deskFetch(`/api/conversations/${encodeURIComponent(saved.chat)}/plans/${encodeURIComponent(plan.id)}/references/${encodeURIComponent(reference.id)}`,{signal,headers:{'If-Match':revision,'X-Plan-Revision':String(plan.revision)}}))
   if(!value||typeof value!=='object'||Array.isArray(value))throw new LegacyPlanReferenceUnresolved()
   const fields=value as Record<string,unknown>
   if(Object.keys(fields).sort().join(',')!=='digest,id,kind'||fields.kind!==reference.kind||fields.id!==reference.id||typeof fields.digest!=='string'||!/^[a-f0-9]{64}$/.test(fields.digest))throw new LegacyPlanReferenceUnresolved()
   return {kind:reference.kind,id:reference.id,digest:fields.digest}
  },
  legacy:async(reference,signal)=>{
   const binding=bindings.find(({ref})=>ref.kind===reference.kind&&ref.id===reference.id&&'revision' in reference&&ref.revision===reference.revision)
   if(!binding)throw new LegacyPlanReferenceUnresolved()
   const found=reference.kind==='file'?await readFile(reference.id,signal):await dialectAPI<{sha256:string}>('/'+encodeURIComponent(reference.id),'GET',undefined,undefined,signal)
   return {kind:reference.kind==='file'?'artifact':'dialect',id:binding.id,digest:found.sha256}
  }
 }
}

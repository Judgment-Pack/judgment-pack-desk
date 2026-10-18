import {checkArtifact} from '../assistant/execution/checkArtifact'
import type {HostTool} from '../assistant/engine'
import {toolInputFailure} from '../assistant/failures'
import {readPolicy} from './client'
import {policyFields,validPolicyFields,validPolicyRevision,type PolicyRequest,type PolicyProposal} from './chat'
const result=(value:unknown)=>({content:[{type:'text',text:JSON.stringify(value)}]})
/** These tools run inside the existing managed chat; they never mutate Policy. */
export function policyChatTools(options:{id?:string;request:()=>PolicyRequest|undefined;afterTurnId:()=>string|undefined;retain:(proposal:PolicyProposal,signal:AbortSignal)=>Promise<void>}):HostTool[]{
 if(!options.id)return []
 const id=options.id
 return [
  {name:'read_policy',effect:'read',delegation:'read',contextPolicy:{reference:true},description:'Read the current saved Policy draft with authenticated access. This is a fresh read, not approval or publication.',inputSchema:{type:'object',properties:{},additionalProperties:false},execute:async(args,signal)=>{
   if(Object.keys(args).length)throw toolInputFailure('Read the Policy linked to this conversation without arguments.')
   const saved=await readPolicy(id,signal);signal.throwIfAborted()
   return result({id,revision:saved.revision,candidate:saved.document.candidate,workflow:saved.document.workflow,reviewState:saved.document.reviewState,requestRevision:options.request()?.revision,guidance:'Saved draft only. Review/publication remain manual. Use the request-time revision for proposals; a changed saved revision requires a fresh request.'})
  }},
  {name:'propose_policy_update',effect:'proposal',contextPolicy:{reference:true},description:'Propose a requested update to the linked saved Policy title, narrative and artifactVersion. Supply all three fields and the exact request-time revision. Retains a diff card only; does not save, approve or publish.',inputSchema:{type:'object',properties:{revision:{type:'string'},title:{type:'string'},narrative:{type:'string'},artifactVersion:{type:'string'}},required:['revision','title','narrative','artifactVersion'],additionalProperties:false},execute:async(args,signal)=>{
   const {revision,...candidate}=args,request=options.request()
   if(Object.keys(args).length!==4||!validPolicyRevision(revision)||!validPolicyFields(candidate)||!request||revision!==request.revision)throw toolInputFailure('Provide title, narrative and artifactVersion against the exact Policy revision included with this request.')
   const proposal:PolicyProposal={version:1,id:crypto.randomUUID(),policyId:id,createdAt:new Date().toISOString(),afterTurnId:options.afterTurnId(),revision,before:policyFields(request.candidate),candidate:policyFields(candidate),operationId:crypto.randomUUID()}
   const check=await checkArtifact({candidate:proposal,fingerprint:p=>JSON.stringify(p.candidate),retain:async(p,check?:PolicyProposal['check'])=>{if(check)p.check=check;await options.retain(p,signal)},check:async(_p,signal)=>{const saved=await readPolicy(id,signal);return {scope:'draft-shape-and-revision' as const,status:saved.revision===revision?'passed' as const:'stale' as const}},passed:check=>check.status==='passed'},signal)
   if(check.status==='stale')throw toolInputFailure('The saved Policy changed during this request. The proposal is retained but cannot be saved. Send a fresh request against the latest saved draft.')
   return result({artifactType:'policy-update',proposalId:proposal.id,policyId:id,revision,status:'proposed',checks:{scope:'draft-shape',status:'passed'},guidance:'Review the exact diff and save through the chat action. Shape checks do not establish policy correctness or authorize review/publication. No Policy was changed.'})
  }}
 ]
}

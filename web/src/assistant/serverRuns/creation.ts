import { copyWireValue, encodeWireValue, retainWireValue, wireJSON } from '../../chat/wire'
import { memberOf, parseJsonText } from '../../research/verify/canon'
import { sha256 } from '@judgment-pack/harness/records'
import { readPackCheck, type PackProposalCheck } from '../../chat/packProposal'
import { jsonIdentity, type AuthoringCase } from '../../research/checkCandidate'
import type { Citation, RunState } from '../../research/run'
import {readinessKey} from '../../research/readiness'

export type CreationAction =
  | {kind:'recover';messageId:string;messageDigest:string}
  | {kind:'check'|'retry-validation'|'establish-cases';candidateDigest:string}
  | {kind:'propose-correction';candidateDigest:string;issueId:string}
  | {kind:'approve-correction';candidateDigest:string;issueId:string;token:string}
  | {kind:'research-choice';choice:'research'|'provided'}
export interface CreationEvidence {
  action:CreationAction['kind'];candidateDigest:string;sourceDigest:string;generation:number;candidate:string
  /** Original typed case bytes remain available separately from display objects. */
  cases:string;held:string;issues:string;check?:PackProposalCheck
  citations:Citation[];unknowns:string[]
  proposal?:{token:string;issueId:string;expectedDisposition:unknown;rationale:string}
}
const object=(value:unknown):value is Record<string,unknown>=>value!==null&&typeof value==='object'&&!Array.isArray(value)
const exact=(value:Record<string,unknown>,keys:readonly string[])=>Object.keys(value).every(k=>keys.includes(k))
const digest=(value:unknown):value is string=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)
const text=(value:unknown):value is string=>typeof value==='string'&&value.length>0&&value.length<=128
const invalid=():never=>{throw Error('Invalid creation evidence.')}
export function readCreationAction(value:unknown):CreationAction {
  if(!object(value))return invalid()
  let keys=['kind','candidateDigest']
  switch(value.kind){
    case 'recover':keys=['kind','messageId','messageDigest'];if(!text(value.messageId)||!digest(value.messageDigest))return invalid();break
    case 'check':case 'retry-validation':case 'establish-cases':break
    case 'propose-correction':keys.push('issueId');if(!text(value.issueId))return invalid();break
    case 'approve-correction':keys.push('issueId','token');if(!text(value.issueId)||!digest(value.token))return invalid();break
    case 'research-choice':keys=['kind','choice'];if(!['research','provided'].includes(String(value.choice)))return invalid();break
    default:return invalid()
  }
  if(!exact(value,keys)||keys.includes('candidateDigest')&&!digest(value.candidateDigest))return invalid()
  return value as unknown as CreationAction
}
function readCases(text:unknown,established=true):AuthoringCase[] {
  if(typeof text!=='string'||text.length>4_000_000)return invalid()
  const value:unknown=JSON.parse(text)
  if(!Array.isArray(value)||value.length>256||value.some(r=>!object(r)||!exact(r,['id','facts','evidenceAvailability','expectedDisposition','expectedHandoffTarget','expectationSource','rationale'])||!textId(r.id)||!object(r.facts)||(established?!object(r.expectedDisposition):!Object.hasOwn(r,'expectedDisposition'))||!textId(r.expectationSource)||typeof r.rationale!=='string'||!r.rationale.trim())||new Set(value.map(r=>r.id)).size!==value.length)return invalid()
  return retainWireValue(value,parseJsonText(text)) as AuthoringCase[]
}
const textId=(v:unknown)=>typeof v==='string'&&v.length>0&&v.length<=1024
interface Issue {position?:number;id:string;original:AuthoringCase;finding:{status:'invalid';message:string;admitted?:boolean;unreachable?:boolean}}
function readIssues(text:unknown):Issue[] {
  if(typeof text!=='string'||text.length>4_000_000)return invalid()
  const rows:unknown=JSON.parse(text)
  if(!Array.isArray(rows)||rows.length>256)return invalid()
  const nodes=parseJsonText(text)
  for(const [index,row] of rows.entries()){
    if(!object(row)||!exact(row,['position','id','original','finding'])||row.position!==undefined&&(!Number.isSafeInteger(row.position)||(row.position as number)<0||(row.position as number)>255)||!textId(row.id)||!object(row.finding)||!exact(row.finding,['status','message','admitted','unreachable'])||row.finding.status!=='invalid'||typeof row.finding.message!=='string'||!row.finding.message.trim()||row.finding.admitted!==undefined&&typeof row.finding.admitted!=='boolean'||row.finding.unreachable!==undefined&&typeof row.finding.unreachable!=='boolean')return invalid()
    const position=nodes.kind==='array'&&memberOf(nodes.items[index]!,'position')
    if(position&&(position.kind!=='number'||position.literal!==String(row.position)&&position.literal!=='-0'))return invalid()
    const original=readCases(JSON.stringify([row.original]),false)[0]!
    if(row.id!==original.id)return invalid()
  }
  if(new Set(rows.map(r=>r.id)).size!==rows.length)return invalid()
  return retainWireValue(rows,parseJsonText(text)) as Issue[]
}
export function readCreation(value:unknown):CreationEvidence {
  if(!object(value)||!exact(value,['action','candidateDigest','sourceDigest','generation','candidate','cases','held','issues','check','proposal','citations','unknowns'])||!['recover','check','retry-validation','establish-cases','propose-correction','approve-correction'].includes(String(value.action))||!digest(value.candidateDigest)||!digest(value.sourceDigest)||!Number.isSafeInteger(value.generation)||(value.generation as number)<0||typeof value.candidate!=='string'||value.candidate.length>2_000_000||sha256(value.candidate)!==value.candidateDigest)return invalid()
  if(!Array.isArray(value.unknowns)||value.unknowns.some(v=>typeof v!=='string'))return invalid()
  if(!Array.isArray(value.citations)||value.citations.length>256||value.citations.some(c=>!object(c)||!exact(c,['sourceId','location','url','quote','traced','excerptId','reason'])||typeof c.sourceId!=='string'||typeof c.location!=='string'||typeof c.url!=='string'||typeof c.quote!=='string'||c.traced!==true||c.excerptId!==null||c.reason!==''))return invalid()
  const cases=readCases(value.cases),held=readCases(value.held,false),issues=readIssues(value.issues)
  if(issues.some(i=>i.position!==undefined&&i.position>=cases.length+issues.length)||new Set(issues.filter(i=>i.position!==undefined).map(i=>i.position)).size!==issues.filter(i=>i.position!==undefined).length)return invalid()
  if(new Set([...cases.map(c=>c.id),...issues.map(i=>i.id)]).size!==cases.length+issues.length||held.length&&(cases.length||issues.length))return invalid()
  if(value.check!==undefined){const check=readPackCheck(value.check);if(!check||check.documentDigest!==value.candidateDigest||check.totalCases!==cases.length||check.valid&&cases.length&&!check.report||held.length||issues.length)return invalid()}
  if(value.proposal!==undefined){const p=value.proposal;if(!object(p)||!exact(p,['token','issueId','expectedDisposition','rationale'])||!digest(p.token)||!issues.some(i=>i.id===p.issueId)||!object(p.expectedDisposition)||typeof p.rationale!=='string'||!p.rationale.trim())return invalid()}
  return value as unknown as CreationEvidence
}

/** Authenticated observations only; the original checkpoint never grants readiness. */
export function withCreation(state:RunState,evidence:CreationEvidence|undefined,current:boolean,proposalCurrent=current):RunState {
  if(!evidence)return state
  const latest=state.candidates.at(-1)
  if(latest?.text!==evidence.candidate||latest.digest!==evidence.candidateDigest)return invalid()
  const cases=readCases(evidence.cases),held=readCases(evidence.held,false),issues=readIssues(evidence.issues)
  if(jsonIdentity(state.cases)!==jsonIdentity(cases)||jsonIdentity(state.expectationIssues.filter(i=>!i.resolved).map(i=>({id:i.id,original:i.original,message:i.message})))!==jsonIdentity(issues.map(i=>({id:i.id,original:i.original,message:i.finding.message}))))return invalid()
  if(jsonIdentity(state.heldProposal?.admitted??[])!==jsonIdentity(held))return invalid()
  const identity=(v:unknown)=>wireJSON(parseJsonText(encodeWireValue(v)))
  if(identity(state.cases)!==wireJSON(parseJsonText(evidence.cases))||identity(state.heldProposal?.admitted??[])!==wireJSON(parseJsonText(evidence.held)))return invalid()
  if(identity(state.expectationIssues.filter(i=>!i.resolved).map(i=>({id:i.id,position:i.position,original:i.original})))!==identity(issues.map(i=>({id:i.id,position:i.position,original:i.original}))))return invalid()
  const c=evidence.check
  const parsed=(text:string)=>{try{return JSON.parse(text) as unknown}catch{return text}}
  const check=c?{documentDigest:c.documentDigest,valid:c.valid,diagnostics:c.diagnostics,cases:c.report?.packs?.[0]?.rows?.map(row=>({id:row.id,passed:row.status==='passed',expected:{disposition:parsed(row.expected),...(cases.find(c=>c.id===row.id)?.expectedHandoffTarget===undefined?{}:{handoffTarget:cases.find(c=>c.id===row.id)!.expectedHandoffTarget})},actual:{disposition:parsed(row.actual),...(cases.find(c=>c.id===row.id)?.expectedHandoffTarget===undefined?{}:{handoffTarget:row.actualHandoffTarget===undefined?null:parsed(row.actualHandoffTarget)})}}))??[]}:undefined
  if(check&&check.cases.some((row,i)=>row.id!==cases[i]?.id))return invalid()
  let result:RunState={...state,cases,heldProposal:held.length?copyWireValue(state.heldProposal,{...state.heldProposal!,candidateDigest:evidence.candidateDigest,admitted:held,dropped:[],unknowns:evidence.unknowns}):null,unknowns:evidence.unknowns,
    expectationIssues:[...state.expectationIssues.filter(i=>i.resolved),...issues.map(i=>copyWireValue(state.expectationIssues.find(old=>!old.resolved&&old.id===i.id),{...(i.position===undefined?{}:{position:i.position}),id:i.id,original:i.original,message:i.finding.message,finding:i.finding,...(proposalCurrent&&evidence.proposal?.issueId===i.id?{proposal:{...evidence.proposal,candidateDigest:evidence.candidateDigest}}:{})}))],
    candidates:state.candidates.map(candidate=>candidate===latest?{...candidate,...(current?{check}:{previousCheck:check,check:undefined})}:candidate),
    citations:evidence.citations.map(c=>({...c,traced:current&&c.traced})),readiness:'',restored:!current,tracedBasis:current?evidence.sourceDigest:undefined,checkRun:c?'complete':undefined}
  if(current&&check?.valid&&cases.length&&check.cases.length===cases.length&&check.cases.every(row=>row.passed)&&issues.length===0){result={...result,status:'ready'};result.readiness=readinessKey(result)}
  return result
}

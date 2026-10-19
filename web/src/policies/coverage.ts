import type {PolicyAuthoringSource} from './authoringSource'
import type {PolicyActor,PolicyRecoveryInfo} from './client'

export type CoverageElement={kind:'rule'|'exception';id:string}|{kind:'applicability'|'fallback'}
export type CoverageStatement=
 |{status:'implemented';consumerElements:CoverageElement[];basis:string}
 |{status:'manual';handoff:string}
 |{status:'awaiting-interpretation';explanation:string}
 |{status:'out-of-scope';rationale:string}
export interface CoveragePackSelector {publicationId:string;artifactId:string;version:string;content:{sha256:string;bytes:number}}
export interface CoverageSelection {coverageId:string;candidateId:string}
export const validCoverageId=(id:unknown):id is string=>typeof id==='string'&&/^coverage_[a-f0-9]{48}$/.test(id)
export const validCoverageCandidateId=(id:unknown):id is string=>typeof id==='string'&&/^coverage_candidate_[a-f0-9]{48}$/.test(id)
export function validCoverageSelection(value:unknown):value is CoverageSelection {
 if(!value||typeof value!=='object'||Array.isArray(value))return false
 const p=value as CoverageSelection
 return Object.keys(p).length===2&&validCoverageId(p.coverageId)&&validCoverageCandidateId(p.candidateId)
}
export const coverageElementKey=(element:CoverageElement)=>JSON.stringify(element)
export interface CoverageCandidate {
 id:string;digest:string;content:{sha256:string;bytes:number};source:PolicyAuthoringSource
 pack:{publicationId:string;document:{kind:'pack';alias:string;id:string;version:string;path:string;content:{sha256:string;bytes:number}}};scope:{kind:'whole-narrative'};statement:CoverageStatement
}
export interface CoverageReview {id:string;sequence:number;candidateId:string;candidateDigest:string;policyRevision:string;decision:'approve'|'request-changes';at:string;actor:PolicyActor;comment?:string;check?:{sha256:string;bytes:number}}
export interface CoverageInventory {rules:string[];exceptions:string[];applicability:boolean;fallback:boolean}
export function coverageElements(inventory?:CoverageInventory):CoverageElement[]{return inventory?[...inventory.rules.map(id=>({kind:'rule' as const,id})),...inventory.exceptions.map(id=>({kind:'exception' as const,id})),...(inventory.applicability?[{kind:'applicability' as const}]:[]),...(inventory.fallback?[{kind:'fallback' as const}]:[])]:[]}
export interface CoverageReply {
 revision:string;policyRevision:string;recovery?:PolicyRecoveryInfo;inventory:CoverageInventory;availability:{packPublication:'present'|'missing'|'unavailable';reason?:string}
 document:{schemaVersion:1;id:string;policyId:string;createdAt:string;updatedAt:string;candidate:CoverageCandidate;reviewState:'unreviewed'|'reviewed'|'changes-requested';currentReviewId?:string;candidates:{id:string;sequence:number;digest:string;content:{sha256:string;bytes:number};policyPublicationId:string;pack:CoveragePackSelector;status:CoverageStatement['status'];at:string;actor:PolicyActor}[];reviews:CoverageReview[]}
}
export interface CoverageSummary {id:string;policyId:string;revision:string;reviewState:CoverageReply['document']['reviewState'];candidateId:string;candidateDigest:string;policyPublicationId:string;policyVersion:string;pack:CoveragePackSelector;status:CoverageStatement['status'];updatedAt:string;recovery?:PolicyRecoveryInfo}
export interface CoverageOptions {policyRevision:string;policyPublications:{publicationId:string;revisionId:string;candidateDigest:string;artifactVersion:string;title:string;content:{sha256:string;bytes:number};at:string}[];packPublications:(CoveragePackSelector&{alias?:string})[];recovery?:PolicyRecoveryInfo;truncated?:boolean}
export interface CoverageResolved {policyRevision:string;source:PolicyAuthoringSource;pack:CoverageCandidate['pack'];inventory:CoverageInventory;recovery?:PolicyRecoveryInfo}
export interface CoverageCheck {coverageRevision:string;candidateDigest:string;policyRevision:string;availability:'available'|'unavailable'|'unsupported';report?:unknown;reason?:string}
export interface CoverageOutcome {outcomeKind:'coverage';delivery:'committed'|'recovered';policyId:string;policyRevision:string;receipt:{operationId:string;kind:string;policyId:string;revision:string;coverageId:string;coverageCandidateId:string;candidateDigest:string;coverageRevision:string;reviewId?:string};coverage:CoverageReply}

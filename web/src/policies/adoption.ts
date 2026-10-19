import type {PolicyAuthoringSource} from './authoringSource'
import type {PolicyActor,PolicyRecoveryInfo,RetainedArtifactRef} from './client'
import type {CoveragePackSelector,CoverageReview,CoverageStatement} from './coverage'

export interface AdoptionSelection {policyId:string;coverageId:string;revision:string;candidateId:string;reviewId:string}
export interface AdoptionChoice extends AdoptionSelection {policyPublicationId:string;policyVersion:string;title:string;scope:{kind:'whole-narrative'};statement:CoverageStatement;pack:CoveragePackSelector}
export interface AdoptionRecoveryBoundary {snapshotId:string;restoredAt:string;sequence:number}
export interface AdoptionDependency {selection:AdoptionSelection;candidateDigest:string;coverageContent:RetainedArtifactRef;coverageReview:CoverageReview;coverageRecovery?:AdoptionRecoveryBoundary;policyRevision:string;lifecycleRevision:string;sourceRevision:string;authorityRevision:string;policyPublicationId:string;source:PolicyAuthoringSource;scope:{kind:'whole-narrative'};statement:CoverageStatement}
export interface AdoptionCandidate {id:string;digest:string;content:RetainedArtifactRef;portableId:string;formatVersion:'1.0.0';basisDigest:string;pack:CoveragePackSelector;dependencies:AdoptionDependency[];document:RetainedArtifactRef;materials:RetainedArtifactRef[];check:RetainedArtifactRef}
export interface AdoptionReview {id:string;sequence:number;candidateId:string;candidateDigest:string;basisDigest:string;decision:'approve'|'request-changes';comment?:string;at:string;actor:PolicyActor;check?:RetainedArtifactRef}
export interface AdoptionReply {revision:string;document:{schemaVersion:1;id:string;createdAt:string;updatedAt:string;candidate:AdoptionCandidate;reviewState:'unreviewed'|'reviewed'|'changes-requested';currentReviewId?:string;reviews:AdoptionReview[]};readiness:{status:'ready'|'stale'|'unavailable';reason?:string};eligibility:{status:'effective'|'not-yet-effective'|'ineligible'|'unavailable';reason?:string};recovery?:PolicyRecoveryInfo;recoveryBoundary?:AdoptionRecoveryBoundary}
export interface AdoptionSummary {id:string;revision:string;candidateDigest:string;pack:CoveragePackSelector;dependencies:AdoptionSelection[];reviewState:AdoptionReply['document']['reviewState'];updatedAt:string}
export interface AdoptionOptions {packPublications:CoveragePackSelector[];coverages:AdoptionChoice[];truncated?:boolean}
export type AdoptionCommand={action:'prepare';operationId:string;pack:CoveragePackSelector;coverages:AdoptionSelection[]}|{action:'review';operationId:string;adoptionId:string;baseRevision:string;candidateDigest:string;basisDigest:string;decision:'approve'|'request-changes';comment?:string}
/** The frozen publication is journal context, never an extra review command field. */
export interface PendingAdoption {pack:CoveragePackSelector;command:AdoptionCommand}
export interface AdoptionOutcome {outcomeKind:'adoption';delivery:'committed'|'recovered';policyId:string;policyRevision:string;adoption:AdoptionReply;receipt:{operationId:string;kind:string;policyId:string;revision:string;adoptionId:string;adoptionRevision:string;adoptionCandidateId:string;candidateDigest:string;reviewId?:string}}
export const adoptionSelection=(row:AdoptionSelection):AdoptionSelection=>({policyId:row.policyId,coverageId:row.coverageId,revision:row.revision,candidateId:row.candidateId,reviewId:row.reviewId})
export const adoptionSelectionKey=(row:AdoptionSelection)=>JSON.stringify(adoptionSelection(row))
export const samePublication=(a:CoveragePackSelector,b:CoveragePackSelector)=>a.publicationId===b.publicationId&&a.artifactId===b.artifactId&&a.version===b.version&&a.content.sha256===b.content.sha256&&a.content.bytes===b.content.bytes
export const sameSelections=(a:AdoptionSelection[],b:AdoptionSelection[])=>a.length===b.length&&a.map(adoptionSelectionKey).sort().join('\n')===b.map(adoptionSelectionKey).sort().join('\n')

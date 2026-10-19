import {msg} from '../i18n'
import {jobsAPI,JobsRequestError,retainedJobMember,type Job,type Release} from './client'
import {parseMappedCase} from './mappedInputs'
import {jobsRequest} from './wire'
import {isExecutableDialect} from './dialectTypes'
import type {JobDraftValues,JobUpgradeBasis} from './drafts'
import type {PackSummary} from '../mcp/types'

export function needsRecordReview(base:JobUpgrade):boolean {
 const mapping=base.release.inputMapping
 return !!mapping&&isExecutableDialect(mapping)&&!!mapping.sources?.some(source=>source.profile==='desk-reviewed-research-v1')
}
export interface JobUpgrade {job:Job;release:Release}
export function upgradePackKey(base:JobUpgrade,packs:PackSummary[]):string {
 const matches=packs.filter(pack=>!!base.release.packId&&pack.packId===base.release.packId&&!pack.detail)
 return matches.length===1?matches[0]!.id:''
}
/** Only declarative text travels into a new candidate. Receipts, selected files,
 * picker grants, preview approvals and trigger admission must be obtained again. */
export function upgradeDraft(base:JobUpgrade,packId:string):JobDraftValues {
 const mapping=base.release.inputMapping??base.release.sample?.source?.mapping
 const mapped=mapping&&isExecutableDialect(mapping)
 let caseText=retainedJobMember(base,['release','sample','source','case'])??'{}'
 if(needsRecordReview(base))caseText='{}'
 else if(mapped){
  const value=parseMappedCase(caseText)
  for(const source of mapping.sources??[]){
   if(source.provider!=='google-drive')continue
   const param=source.arguments?.grant as {$param?:string}|undefined
   const pointer=param?.$param?mapping.case?.parameters?.[param.$param]?.pointer:undefined
   if(!pointer)continue
   const parts=pointer.slice(1).split('/').map(p=>p.replace(/~1/g,'/').replace(/~0/g,'~'))
   let parent:unknown=value
   for(const part of parts.slice(0,-1))parent=parent&&typeof parent==='object'?(parent as Record<string,unknown>)[part]:undefined
   if(parent&&typeof parent==='object')delete (parent as Record<string,unknown>)[parts.at(-1)!]
  }
  caseText=jobsRequest(value)
 }
 const importText=retainedJobMember(base,['release','sample','source','import'])
 return {
  name:base.job.name,packId,inputMode:mapping?'mapped':'manual',
  facts:retainedJobMember(base,['release','sample','facts'])??'{}',
  supplied:retainedJobMember(base,['release','sample','evidence'])!==undefined,
  evidence:retainedJobMember(base,['release','sample','evidence'])??'{}',
  ...(mapped?{mapped:{text:retainedJobMember(base,['release','inputMapping'])??retainedJobMember(base,['release','sample','source','mapping'])!,caseText,...(importText?{importText}:{})}}:{}),
  upgrade:{jobId:base.job.id,baseReleaseId:base.job.releaseId,baseRevision:base.job.revision},
 }
}
export function upgradeDraftState(base:JobUpgrade,values:JobDraftValues):'current'|'completed'|'changed' {
 const prior=values.upgrade
 if(!prior||prior.jobId!==base.job.id)return 'changed'
 if(prior.submittedReleaseId===base.job.releaseId&&base.job.revision===prior.baseRevision+1)return 'completed'
 return prior.baseReleaseId===base.job.releaseId&&prior.baseRevision===base.job.revision?'current':'changed'
}

/** Recovery is a read, never a new approval or write. Only an explicit absent
 * receipt permits fresh candidate admission; unknown/old APIs remain errors. */
export async function readUpgradeReceipt(attempt:JobUpgradeBasis):Promise<Job|null> {
 if(!attempt.submittedReleaseId)return null
 const query=new URLSearchParams({baseRevision:String(attempt.baseRevision),baseReleaseId:attempt.baseReleaseId,releaseId:attempt.submittedReleaseId})
 try{
  const job=await jobsAPI<Job>(`jobs/${attempt.jobId}/upgrade-receipt?${query}`)
  if(job.id!==attempt.jobId||job.releaseId!==attempt.submittedReleaseId||job.revision!==attempt.baseRevision+1)throw Error(msg('The job changed after this draft was saved. Start a new upgrade from its current release.'))
  return job
 }catch(error){if(error instanceof JobsRequestError&&error.status===404&&error.code==='upgrade_not_completed')return null;throw error}
}

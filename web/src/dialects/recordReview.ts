import {msg} from '../i18n'
import type {DialectRecord,RecordReview} from './client'

export function currentRecordReview(record:DialectRecord,recipeVersion:number) {
 return record.review?.recipeVersion===recipeVersion?record.review:undefined
}
export function reviewDecisionLabel(decision:RecordReview['decision']) {
 switch(decision){
  case 'accepted':return msg('Ready for evaluation')
  case 'needs-information':return msg('Needs more information')
  case 'rejected':return msg('Excluded')
 }
}
export function researchReviewLabel(record:DialectRecord,recipeVersion:number) {
 const review=currentRecordReview(record,recipeVersion)
 return review?reviewDecisionLabel(review.decision):record.review?msg('Review again — recipe changed'):msg('Research needs review')
}
export function researchEvaluationLimit(){
 return msg('Mark each selected AI record Ready for evaluation after saving the pack mapping. Evaluation keeps its AI origin and requires a separate confirmation.')
}
export function recordsGuidance(records:DialectRecord[]) {
 const researched=records.some(record=>record.class==='generated')
 const supplied=records.some(record=>record.class!=='generated')
 if(researched&&supplied)return msg('Review AI research and supplied values before qualification. Source quotations support review; they do not independently verify claims.')
 if(researched)return msg('Open a record to review its sources and AI interpretations, then save your review decision.')
 return msg('Provided values have not been independently verified. URLs are saved as research inputs; importing them does not read the websites.')
}

export function dialectEvaluationProblem(records:DialectRecord[],recipeVersion:number) {
 if(records.some(r=>r.class==='generated')&&records.some(r=>r.class!=='generated'))return msg('Evaluate AI research and supplied records in separate selections. No records were skipped.')
 if(records.some(r=>r.class==='generated'&&currentRecordReview(r,recipeVersion)?.decision!=='accepted'))return msg('Mark every selected AI record Ready for evaluation for the current recipe. No records were skipped.')
 return undefined
}

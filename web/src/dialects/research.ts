export type ResearchLimits = ResearchRecipe['limits']
export interface ResearchBudgetRequest {id:string;state:'pending'|'approved'|'declined';createdAt:string;resolvedAt?:string;reviewer?:{kind:string;name?:string};reason:string;current:ResearchLimits;proposed:ResearchLimits;used:ResearchLimits}
export interface ResearchBudgetReview {runId:string;revision:string;recipeVersion:number;recordCount:number;sourceCount:number;request:ResearchBudgetRequest}
export const researchFieldTypes = ['text','integer','decimal','boolean','text-list','range','string','number'] as const
export type ResearchFieldType = typeof researchFieldTypes[number]
export interface ResearchField {name:string;description:string;type:ResearchFieldType}
export interface ResearchRetryPolicy {mode:'off'|'temporary';maxRetries:number;searchTimeouts:boolean}
export const defaultResearchRetries = ():ResearchRetryPolicy=>({mode:'temporary',maxRetries:2,searchTimeouts:false})
export interface ResearchPendingRetry {afterAttempt:string;at:string;number:number;maxAttempts:number;problemCode:string}
export interface ResearchExecutionPolicy {approach:'standard'|'delegated';concurrency:0|1|2}
export interface ResearchExecution {approach:'standard'|'delegated';requestedConcurrency:number;concurrency:number;connectionLimit:number}
export interface ResearchTask {id:string;title:string;objective:string;profile:string;state:'queued'|'running'|'complete'|'failed'|'interrupted';createdAt:string;startedAt?:string;finishedAt?:string;problem?:string;result?:string;modelRequests:number;sourceRequests:number}
/** Legacy targets are read-compatible metadata; new recipes always research records. */
export interface ResearchRecipe {execution?:ResearchExecutionPolicy;retries?:ResearchRetryPolicy;version:1;target:'records'|'companies'|'people'|'people-at-companies';fields:ResearchField[];criteria:{id:string;description:string;qualificationTarget?:string}[];companyIds?:string[];companyInputs?:{preparationId:string;revision:string;entityId:string}[];ai:{connection:string;revision:string;model:string;effort?:string};search?:{connection:string;revision:string};limits:{records:number;searches:number;pages:number;modelCalls:number}}
// Empty Go citation slices are retained as null in existing records and reviews.
export interface ResearchValue {value?:string|number|boolean|string[]|{min:number;max:number}|{min:string;max:string}|null;status:'supported'|'interpretation'|'missing'|'conflicting'|'inaccessible'|'stale';sources?:{sourceId:string;quote:string}[]|null;note?:string}
export interface ResearchEntity {website?:string;id:string;kind:'record'|'company'|'person';name:string;url:string;companyId?:string;fields:Record<string,ResearchValue>}
export interface ResearchSource {id:string;kind:'search'|'page';url?:string;query?:string;at:string;digest:string;text:string;verification:string}
export interface ResearchRun {execution?:ResearchExecution;contractVersion?:number;reprocess?:boolean;selection?:{recordId:string;entity:ResearchEntity}[];budgetRequests?:ResearchBudgetRequest[];phase?:string;continuationOf?:string;inheritedRecords?:number;version:1;id:string;preparationId:string;recipeVersion:number;createdAt:string;updatedAt:string;state:'review'|'queued'|'running'|'paused'|'stopped'|'completed'|'completed-with-gaps'|'partial'|'blocked'|'interrupted';problem?:string;recipe:ResearchRecipe;intent:string;records:ResearchEntity[];companies:ResearchEntity[];sources:ResearchSource[];attempts:{id:string;kind:'model'|'search'|'page';state:string;problem?:string}[];stopRequested:boolean}
export interface ResearchStep {explanation?:string;failure?:import('../assistant/failures').AIFailure;taskId?:string;recovered?:boolean;recordResults?:ResearchRecordOutcome[];actionProblem?:string;continued?:boolean;number?:number;maxAttempts?:number;retryOf?:string;retryAt?:string;retriedBy?:string;retryNote?:string;id:string;kind:'model'|'search'|'page';state:'waiting'|'not-started'|'started'|'completed'|'uncertain';startedAt:string;finishedAt?:string;url?:string;query?:string;problem?:string;problemCode?:string;recovery?:string;result?:string;digest?:string}
export interface ResearchRecordOutcome {key:string;recordId?:string;name:string;state:string;problem?:string;recoveredBy?:string}
export interface ResearchOutcomeCounts {added:number;updated:number;unchanged:number;needsCorrection:number;pending:number}
export interface ResearchSummary {explanation?:string;execution?:ResearchExecution;tasks?:ResearchTask[];reprocess?:boolean;selectionCount?:number;outcomes?:ResearchOutcomeCounts;publicationPending?:boolean;activePageReads?:number;budgetRequest?:ResearchBudgetRequest;canIncrease?:boolean;approvedLimits?:ResearchLimits;phase?:string;canContinue?:boolean;sourceCount?:number;unavailableSources?:number;pendingRetry?:ResearchPendingRetry;id:string;state:ResearchRun['state'];createdAt?:string;updatedAt:string;recipeVersion?:number;recordCount:number;problem?:string;stopRequested:boolean;stepCount?:number;currentStep?:ResearchStep;recovery?:string}
export interface ResearchActivity extends ResearchSummary {approval?:import('./researchApproval').ResearchAllowance;budgetRequests?:ResearchBudgetRequest[];steps:ResearchStep[]}

/** An active run stays visible even if a newer, unstarted review exists. */
export function visibleResearchRun(runs:ResearchSummary[]):ResearchSummary|undefined {
 return runs.find(run=>run.state==='running')??runs.find(run=>run.state==='queued')??runs.find(run=>run.state==='paused')??runs.find(run=>run.state!=='review')
}

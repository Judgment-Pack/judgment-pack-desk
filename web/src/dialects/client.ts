import type { DialectDocumentV2 as DialectV2 } from '../dialect/types'
import { answer, deskFetch } from '../files/client'
export interface RecordReview {recordId:string;recordDigest:string;recipeVersion:number;decision:'accepted'|'needs-information'|'rejected';note:string;at:string;reviewer:{kind:'session'|'local-access';subject?:string;issuer?:string;name?:string}}
export interface RecordReviewEvent {review:RecordReview;record:DialectRecord}
export interface DialectRecord {review?:RecordReview;id:string;name:string;values:Record<string,string>;sourceId:string;row:number;class:'asserted'|'generated';status:'provided'|'needs-research'|'researched';research?:{runId:string;recipeVersion:number;entity:import('./research').ResearchEntity}}
export interface DialectImport {id:string;kind:'csv'|'urls';name:string;digest:string;content:string;at:string;recipeVersion:number}
export interface DialectPackBinding {kind:'pack';key:string;pack:string;digest:string;mapping:DialectV2;conversions?:Record<string,DialectConversion>}
export interface DialectRecipe {template?:import('./templates').TemplateOrigin;intent:string;research?:import('./research').ResearchRecipe;qualification?:DialectPackBinding}
export interface Preparation {chatOrigin?:{chatId:string;draftId:string};reviews?:RecordReviewEvent[];version:1;id:string;name:string;recipe:DialectRecipe;recipeVersion:number;recipes?:{version:number;name:string;recipe:DialectRecipe;at:string}[];createdAt:string;updatedAt:string;records:DialectRecord[];imports:DialectImport[];activity:{kind:'created'|'imported'|'recipe-updated'|'researched';at:string;count?:number;recipeVersion:number}[]}
export interface PreparationReply {sha256:string;document:Preparation}
export interface PreparationSummary {id:string;name:string;records:number;updatedAt:string}
export const dialectAPI = async <T,>(path='',method='GET',body?:unknown,revision?:string,signal?:AbortSignal) => answer<T>(await deskFetch('/api/input-preparations'+path,{method,signal,headers:{'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body===undefined?{}:{body:JSON.stringify(body)})}))
/** A selection export includes only selected rows and their source references.
 * Raw imports may contain unselected rows and are deliberately not copied.
 */
export function exportDialect(doc:Preparation,records:DialectRecord[]) {
 const imports=doc.imports??[]
 const ids=new Set(records.map(record=>record.sourceId))
 const versions=new Set(imports.filter(source=>ids.has(source.id)).map(source=>source.recipeVersion))
 versions.add(doc.recipeVersion)
 records.forEach(record=>{if(record.research)versions.add(record.research.recipeVersion)})
 const reviews=(doc.reviews??[]).filter(event=>records.some(record=>record.id===event.review.recordId))
 reviews.forEach(event=>versions.add(event.review.recipeVersion))
 return {version:1,preparation:{id:doc.id,name:doc.name,recipe:doc.recipe,recipeVersion:doc.recipeVersion},recipes:doc.recipes?.filter(recipe=>versions.has(recipe.version))??[],records,reviews,sources:imports.filter(source=>ids.has(source.id)).map(({content,...reference})=>reference)}
}

export interface DialectConversion {kind:'text'|'decimal'|'number'|'boolean';trueValue?:string;falseValue?:string}
export interface PreparedRow { origin?: 'asserted'|'generated';id:string;original:Record<string,string>;prepared:Record<string,unknown>;issues:{target:string;message:string}[];input:import('../jobs/client').JobInput}
export interface DialectEvaluationRow {recordId:string;runId?:string;state:'pending'|'queued'|'running'|'completed'|'failed'|'interrupted'|'accepted'|'preparing'|'waiting'|'needs-attention'|'submitted'|'expired'|'skipped'|'cancelled'|'superseded';result?:import('../jobs/client').Decision;signing?:import('../jobs/client').Run['signing'];problem?:string}
export interface DialectEvaluation {subscription?:{mode:'new'|'changed';includeExisting:boolean;enabled:boolean;initialized:boolean;triggerId?:string;sourceClass:'asserted'|'generated'};id:string;preparationId:string;revision:string;recipeVersion:number;name:string;release:import('../jobs/client').Release;releaseId:string;jobId?:string;createdAt:string;confirmedAt?:string;state:'review'|'queued'|'running'|'completed'|'partial'|'blocked'|'paused'|'watching';problem?:string;rows:DialectEvaluationRow[];requireTested:boolean;projectReviewed:boolean}
export const prepareRecords=(saved:PreparationReply,records:DialectRecord[])=>dialectAPI<{rows:PreparedRow[]}>('/'+saved.document.id+'/prepare','POST',{recordIds:records.map(r=>r.id)},saved.sha256)

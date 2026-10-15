import type {DialectRecipeFailureCode} from './proposalFailure'
import {defaultResearchRetries} from './research'
import type {ResearchExecutionPolicy,ResearchRecipe,ResearchField} from './research'

/** A chat proposal contains portable intent, never execution authority. */
export interface DialectRecipeProposal {
 version:1;draftId:string;createdAt:string;name:string;intent:string
 execution?:ResearchExecutionPolicy
 target:ResearchRecipe['target'];fields:ResearchField[]
 criteria:ResearchRecipe['criteria'];limits:ResearchRecipe['limits']
 afterTurnId?:string
 attribution?:{connection:string;model:string}
 savedInputId?:string
}
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const keys=(v:Record<string,unknown>,allowed:string[])=>Object.keys(v).every(key=>allowed.includes(key))
const text=(v:unknown,max:number)=>typeof v==='string'&&v.trim().length>0&&v.length<=max
export function dialectRecipeContentProblem(value:unknown):DialectRecipeFailureCode | undefined {
 if(!object(value)||!keys(value,['name','intent','target','fields','criteria','limits','execution']))return 'content'
 if(!text(value.name,160))return 'name'
 if(!text(value.intent,12000))return 'intent'
 if(!['records','companies','people','people-at-companies'].includes(value.target as string))return 'target'
 if(!Array.isArray(value.fields)||value.fields.length<1||value.fields.length>30||!value.fields.every(f=>object(f)&&keys(f,['name','description','type'])&&typeof f.name==='string'&&/^[a-z][a-zA-Z0-9_]{0,63}$/.test(f.name)&&!['constructor','prototype','__proto__'].includes(f.name)&&text(f.description,1000)&&['string','number','boolean'].includes(f.type as string))||new Set(value.fields.map(f=>f.name)).size!==value.fields.length)return 'fields'
 if(!Array.isArray(value.criteria)||value.criteria.length>30||!value.criteria.every(c=>object(c)&&keys(c,['id','description','qualificationTarget'])&&typeof c.id==='string'&&/^[a-z][a-zA-Z0-9_]{0,63}$/.test(c.id)&&text(c.description,2000)&&(c.qualificationTarget===undefined||text(c.qualificationTarget,512)))||new Set(value.criteria.map(c=>c.id)).size!==value.criteria.length)return 'criteria'
 if(value.execution!==undefined&&(!object(value.execution)||!keys(value.execution,['approach','concurrency'])||!['standard','delegated'].includes(String(value.execution.approach))||![0,1,2].includes(value.execution.concurrency as number)))return 'content'
 const limits=value.limits
 if(!(object(limits)&&keys(limits,['records','searches','pages','modelCalls'])&&[[limits.records,1,50],[limits.searches,0,30],[limits.pages,0,100],[limits.modelCalls,1,60]].every(([v,min,max])=>Number.isInteger(v)&&(v as number)>=(min as number)&&(v as number)<=(max as number))))return 'limits'
}
export function validDialectRecipeContent(value:unknown):value is Pick<DialectRecipeProposal,'name'|'intent'|'target'|'fields'|'criteria'|'limits'|'execution'>{ return dialectRecipeContentProblem(value) === undefined }
export function validDialectRecipeProposal(value:unknown):value is DialectRecipeProposal {
 if(!object(value)||!keys(value,['version','draftId','createdAt','name','intent','target','fields','criteria','limits','execution','afterTurnId','attribution','savedInputId']))return false
 const {version,draftId,createdAt,afterTurnId,attribution,savedInputId,...content}=value
 return version===1&&typeof draftId==='string'&&/^[a-zA-Z0-9-]{1,80}$/.test(draftId)&&typeof createdAt==='string'&&Number.isFinite(Date.parse(createdAt))&&(afterTurnId===undefined||text(afterTurnId,160))&&(savedInputId===undefined||typeof savedInputId==='string'&&/^prep_[a-f0-9]{32}$/.test(savedInputId))&&(attribution===undefined||object(attribution)&&keys(attribution,['connection','model'])&&text(attribution.connection,128)&&text(attribution.model,128))&&validDialectRecipeContent(content)
}
export function recipeFromProposal(proposal:DialectRecipeProposal):ResearchRecipe {
 return {...(proposal.execution?{execution:structuredClone(proposal.execution)}:{}),version:1,retries:defaultResearchRetries(),target:proposal.target,fields:structuredClone(proposal.fields),criteria:structuredClone(proposal.criteria),ai:{connection:'',revision:'',model:''},limits:{...proposal.limits,searches:0},companyIds:[]}
}

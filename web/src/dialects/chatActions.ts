import {validDialectProposal,type DialectProposal} from './dialect'
import {defaultResearchRetries} from './research'
import type {DialectRecipe,PreparationReply} from './client'
import {validDialectRecipeContent,type DialectRecipeProposal} from './proposals'

export type DialectRecipePatch={name?:string;intent?:string;research?:Partial<Pick<DialectRecipeProposal,'target'|'fields'|'criteria'|'limits'|'execution'>>}
export interface DialectChatAction {
 version:1;id:string;createdAt:string;afterTurnId?:string
 inputId:string;name:string;revision:string;recipeVersion:number
 kind:'update-dialect'|'update-recipe'|'remove-recipe'|'research'|'reprocess'|'stop-research'|'evaluate'
 dialect?:DialectProposal;patch?:DialectRecipePatch;section?:'research'|'qualification'|'all';recordIds?:string[];runId?:string
 applied?:{at:string;revision?:string;runId?:string}
}
const object=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value)
const keys=(value:Record<string,unknown>,allowed:string[])=>Object.keys(value).every(key=>allowed.includes(key))
const text=(value:unknown,max:number)=>typeof value==='string'&&value.trim().length>0&&value.length<=max
export const validDialectId=(value:unknown):value is string=>typeof value==='string'&&/^prep_[a-f0-9]{32}$/.test(value)
export const validDialectRevision=(value:unknown):value is string=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)
export function validDialectRecipePatch(value:unknown):value is DialectRecipePatch {
 if(!object(value)||!keys(value,['name','intent','research'])||!Object.keys(value).length)return false
 if(value.name!==undefined&&!text(value.name,160)||value.intent!==undefined&&!text(value.intent,12000))return false
 if(value.research===undefined)return true
 if(!object(value.research)||!Object.keys(value.research).length||!keys(value.research,['target','fields','criteria','limits','execution']))return false
 return validDialectRecipeContent({name:'Review',intent:'Review',target:'records',fields:[{name:'description',description:'Description',type:'string'}],criteria:[],limits:{records:3,searches:0,pages:3,modelCalls:10},...value.research})
}
export function validDialectChatAction(value:unknown):value is DialectChatAction {
 if(!object(value)||!keys(value,['version','id','createdAt','afterTurnId','inputId','name','revision','recipeVersion','kind','patch','section','recordIds','runId','applied','dialect']))return false
 if(value.version!==1||!text(value.id,80)||!/^[a-zA-Z0-9-]+$/.test(value.id as string)||!validDialectId(value.inputId)||!validDialectRevision(value.revision)||!text(value.name,160)||!Number.isSafeInteger(value.recipeVersion)||(value.recipeVersion as number)<1||typeof value.createdAt!=='string'||!Number.isFinite(Date.parse(value.createdAt))||value.afterTurnId!==undefined&&!text(value.afterTurnId,160))return false
 if(value.applied!==undefined&&(!object(value.applied)||!keys(value.applied,['at','revision','runId'])||typeof value.applied.at!=='string'||!Number.isFinite(Date.parse(value.applied.at))||value.applied.revision!==undefined&&!validDialectRevision(value.applied.revision)||value.applied.runId!==undefined&&!/^research_[a-f0-9]{32}$/.test(String(value.applied.runId))))return false
 if(value.kind!=='update-dialect'&&value.dialect!==undefined)return false
 switch(value.kind){
  case 'update-dialect':return validDialectProposal(value.dialect)&&value.patch===undefined&&value.section===undefined&&value.recordIds===undefined&&value.runId===undefined
  case 'update-recipe':return validDialectRecipePatch(value.patch)&&value.section===undefined&&value.recordIds===undefined&&value.runId===undefined
  case 'remove-recipe':return ['research','qualification','all'].includes(String(value.section))&&value.patch===undefined&&value.recordIds===undefined&&value.runId===undefined
  case 'research':return value.patch===undefined&&value.section===undefined&&value.recordIds===undefined&&value.runId===undefined
  case 'stop-research':return value.patch===undefined&&value.section===undefined&&value.recordIds===undefined&&typeof value.runId==='string'&&/^research_[a-f0-9]{32}$/.test(value.runId)
  case 'reprocess':
  case 'evaluate':return value.patch===undefined&&value.section===undefined&&value.runId===undefined&&Array.isArray(value.recordIds)&&value.recordIds.length>0&&value.recordIds.length<=1000&&value.recordIds.every(id=>typeof id==='string'&&/^row_[a-f0-9]{64}$/.test(id))&&new Set(value.recordIds).size===value.recordIds.length
  default:return false
 }
}
/** Merge only reviewed portable content. Credentials, mappings and source data
 * are never accepted from a model patch. Existing settings remain user-owned. */
export function applyDialectRecipeAction(saved:PreparationReply,action:DialectChatAction):{name:string;recipe:DialectRecipe} {
 if(saved.document.id!==action.inputId||saved.sha256!==action.revision)throw Error('Dialects changed. Request a new proposal against the current recipe.')
 let name=saved.document.name
 const recipe=structuredClone(saved.document.recipe)
 if(action.kind==='remove-recipe'){
  if(action.section==='all')return {name,recipe:{intent:''}}
  delete recipe[action.section==='research'?'research':'qualification']
 }else if(action.kind==='update-recipe'&&action.patch){
  const patch=action.patch
  if(patch.name!==undefined)name=patch.name
  if(patch.intent!==undefined)recipe.intent=patch.intent
  if(patch.research){
   const prior=recipe.research
   const research={version:1 as const,target:'records' as const,fields:[],criteria:[],limits:{records:3,searches:0,pages:10,modelCalls:15},ai:{connection:'',revision:'',model:''},...(!prior?{retries:defaultResearchRetries()}:{}),...prior,...structuredClone(patch.research)}
   if(prior&&research.target!==prior.target){research.companyIds=[];research.companyInputs=[]}
   recipe.research=research
  }
 }
 return {name,recipe}
}

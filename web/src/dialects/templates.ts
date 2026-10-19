import {msg} from '../i18n'
import {validDialectRecipeContent} from './proposals'
import type {ResearchRecipe} from './research'

/** Desk authoring presets, not the proposed Exchange companion document.
 * Connections, record IDs, approval and pack authority never travel in a preset.
 */
export interface DialectTemplate {
 templateVersion:1; id:string; name:string; description:string; license:string; intent:string
 research?:Pick<ResearchRecipe,'target'|'fields'|'criteria'|'limits'>
}
export interface TemplateOrigin {id:string;name:string;digest:string;license:string}
const limits={records:10,searches:10,pages:20,modelCalls:40}
const field=(name:string,description:string,type:'string'|'number'|'boolean'='string')=>({name,description,type})
export function dialectTemplates():DialectTemplate[]{return [
 {templateVersion:1,id:'blank',name:msg('Start from scratch'),description:msg('Import records or describe a research task. Connect a pack when you are ready.'),license:'Apache-2.0',intent:''},
 {templateVersion:1,id:'research-records',name:msg('Research records'),description:msg('Research any subject with fields you define and retained source references.'),license:'Apache-2.0',intent:msg('Research the requested records using public sources. Keep unsupported information missing and separate interpretation from observations.'),research:{target:'records',fields:[field('description',msg('Source-backed description'))],criteria:[],limits}},
 {templateVersion:1,id:'related-records',name:msg('Related records'),description:msg('Research records related to a selected set of records.'),license:'Apache-2.0',intent:'',research:{target:'records',fields:[field('description',msg('Source-backed description'))],criteria:[],limits}},
 {templateVersion:1,id:'table-records',name:msg('Table records'),description:msg('Import rows from any domain and test their translation into pack facts.'),license:'Apache-2.0',intent:msg('Import table records while preserving their original columns and source references. Choose a pack, map the columns and review conversion previews before evaluation.')}
]}
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const only=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).every(k=>keys.includes(k))
const text=(v:unknown,max:number)=>typeof v==='string'&&v.trim().length>0&&v.length<=max
export function readDialectTemplate(raw:string):DialectTemplate {
 if(new TextEncoder().encode(raw).length>64*1024)throw Error(msg('Choose a template smaller than 64 KB.'))
 let v:unknown;try{v=JSON.parse(raw)}catch{throw Error(msg('The template must contain valid JSON.'))}
 if(!object(v)||!only(v,['templateVersion','id','name','description','license','intent','research'])||v.templateVersion!==1||typeof v.id!=='string'||! /^[a-z][a-z0-9-]{0,79}$/.test(v.id)||!text(v.name,160)||!text(v.description,2000)||!text(v.license,160)||typeof v.intent!=='string'||v.intent.length>12000)throw Error(msg('Choose a Desk template version 1. Connections, credentials, records and executable code are not accepted.'))
 if(v.research!==undefined&&(!object(v.research)||!only(v.research,['target','fields','criteria','limits'])||!validDialectRecipeContent({name:v.name,intent:v.intent||v.description,target:'records',...v.research})))throw Error(msg('The template has invalid research fields or limits.'))
 return v as unknown as DialectTemplate
}
export async function templateOrigin(template:DialectTemplate):Promise<TemplateOrigin>{
 const bytes=new TextEncoder().encode(JSON.stringify(template))
 const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),b=>b.toString(16).padStart(2,'0')).join('')
 return {id:template.id,name:template.name,digest:'sha256:'+digest,license:template.license}
}

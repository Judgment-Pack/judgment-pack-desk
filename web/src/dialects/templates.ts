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
 {templateVersion:1,id:'companies',name:msg('Find companies'),description:msg('Discover companies and prepare evidence for an outreach shortlist.'),license:'Apache-2.0',intent:msg('Find companies matching the scope and criteria we agree. Use public evidence; leave unsupported information missing. Do not infer routing pain or buying intent from company size.'),research:{target:'companies',fields:[field('description',msg('What the company does, with a source reference')),field('country',msg('Country supported by public evidence')),field('employee_count',msg('Employee count only when supported; leave estimates in the evidence note'),'number'),field('fit_observation',msg('Publicly documented observation relevant to the agreed criteria'))],criteria:[],limits}},
 {templateVersion:1,id:'company-people',name:msg('Find people at companies'),description:msg('Research public professional contacts at selected company records.'),license:'Apache-2.0',intent:msg('Find relevant professional contacts at the selected companies. Retain the company relationship, current role and public profile source. Only include business contact details explicitly published for professional use; never guess email addresses.'),research:{target:'people-at-companies',fields:[field('role',msg('Current professional role supported by a public source')),field('profile_url',msg('Public professional profile URL found in a retained source')),field('business_contact',msg('Explicitly published professional contact details; leave missing if unavailable'))],criteria:[],limits}},
 {templateVersion:1,id:'work-items',name:msg('Work item triage'),description:msg('Import work items, then map their fields to a triage pack.'),license:'Apache-2.0',intent:msg('Prepare work item records with their title, description, severity, status and source reference. Preserve missing values. Map reviewed columns to the selected triage pack.')},
 {templateVersion:1,id:'table-records',name:msg('Table records'),description:msg('Import rows from any domain and test their translation into pack facts.'),license:'Apache-2.0',intent:msg('Import table records while preserving their original columns and source references. Choose a pack, map the columns and review conversion previews before evaluation.')}
]}
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const only=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).every(k=>keys.includes(k))
const text=(v:unknown,max:number)=>typeof v==='string'&&v.trim().length>0&&v.length<=max
export function readDialectTemplate(raw:string):DialectTemplate {
 if(new TextEncoder().encode(raw).length>64*1024)throw Error(msg('Choose a template smaller than 64 KB.'))
 let v:unknown;try{v=JSON.parse(raw)}catch{throw Error(msg('The template must contain valid JSON.'))}
 if(!object(v)||!only(v,['templateVersion','id','name','description','license','intent','research'])||v.templateVersion!==1||typeof v.id!=='string'||! /^[a-z][a-z0-9-]{0,79}$/.test(v.id)||!text(v.name,160)||!text(v.description,2000)||!text(v.license,160)||typeof v.intent!=='string'||v.intent.length>12000)throw Error(msg('Choose a Desk template version 1. Connections, credentials, records and executable code are not accepted.'))
 if(v.research!==undefined&&(!object(v.research)||!only(v.research,['target','fields','criteria','limits'])||!validDialectRecipeContent({name:v.name,intent:v.intent||v.description,...v.research})))throw Error(msg('The template has invalid research fields or limits.'))
 return v as unknown as DialectTemplate
}
export async function templateOrigin(template:DialectTemplate):Promise<TemplateOrigin>{
 const bytes=new TextEncoder().encode(JSON.stringify(template))
 const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),b=>b.toString(16).padStart(2,'0')).join('')
 return {id:template.id,name:template.name,digest:'sha256:'+digest,license:template.license}
}

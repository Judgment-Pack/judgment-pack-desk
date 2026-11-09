import {DIALECT_RETAINED_COLUMN_DRAFT,type DialectConversionContract} from '@judgment-pack/harness/dialects'
import {parseJSON} from '../dialect/canon'
import type {RetainedConversion} from '../dialect/types'
import {readFile} from '../files/client'
import {factFields} from '../packs/test-workspace/model'
import {pointerEscape} from '../jobs/sourceInputs'
import type {DialectConversion,DialectPackBinding,PreparationReply} from './client'
import {boundPack,packBinding,recordColumns} from './qualification'

/** Desk authoring proposal. The saved dialect still uses Runner's mapping-v2 wire format. */
export interface DialectProposal {
 packKey:string
 packDigest:string
 facts:{target:string;column:string;conversion:DialectConversion}[]
 evidence:{requirement:string;column:string}[]
 summary:string
}
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const keys=(v:Record<string,unknown>,allowed:string[])=>Object.keys(v).every(key=>allowed.includes(key))
const text=(v:unknown,max:number):v is string=>typeof v==='string'&&v.trim().length>0&&v.length<=max
const safeColumn=(v:unknown):v is string=>text(v,1024)&&!['__proto__','constructor','prototype'].includes(v)
export function validDialectProposal(v:unknown):v is DialectProposal {
 if(!object(v)||!keys(v,['packKey','packDigest','facts','evidence','summary'])||!text(v.packKey,512)||typeof v.packDigest!=='string'||!/^sha256:[a-f0-9]{64}$/.test(v.packDigest)||!text(v.summary,4000))return false
 if(!Array.isArray(v.facts)||v.facts.length>1000||!Array.isArray(v.evidence)||v.evidence.length>1000)return false
 if(!v.facts.every(f=>{
  if(!object(f)||!keys(f,['target','column','conversion'])||!text(f.target,1024)||!safeColumn(f.column)||!object(f.conversion)||!keys(f.conversion,['kind','trueValue','falseValue']))return false
  const c=f.conversion
  return c.kind==='boolean'?text(c.trueValue,160)&&text(c.falseValue,160)&&c.trueValue!==c.falseValue:['text','decimal','number','research-value'].includes(String(c.kind))&&c.trueValue===undefined&&c.falseValue===undefined
 }))return false
 return v.evidence.every(e=>object(e)&&keys(e,['requirement','column'])&&text(e.requirement,1024)&&safeColumn(e.column))&&new Set(v.facts.map(f=>f.target)).size===v.facts.length&&new Set(v.evidence.map(e=>e.requirement)).size===v.evidence.length
}
export function dialectColumns(saved:PreparationReply) {
 return recordColumns(saved.document.records,saved.document.recipe.research).map(c=>c.label)
}
export async function readDialectPack(key:string,signal?:AbortSignal):Promise<DialectPackBinding> {
 if(!text(key,512))throw Error('Choose a saved pack for this dialect.')
 const config=JSON.parse((await readFile('jpack.json',signal)).content)
 if(!config.packs||!Object.hasOwn(config.packs,key)||typeof config.packs[key]?.path!=='string')throw Error('Choose a declared saved pack for this dialect.')
 const file=await readFile(config.packs[key].path,signal)
 signal?.throwIfAborted()
 if(new TextEncoder().encode(file.content).length>1048576)throw Error('The pack is too large for a dialect proposal.')
 return packBinding(key,file.content)
}
export function buildDialect(saved:PreparationReply,proposal:DialectProposal,pack:DialectPackBinding):DialectPackBinding {
 if(pack.conversionContract!==undefined)throw Error('Use explicit retained-column mapping helpers for this selected recipe.')
 if(!validDialectProposal(proposal))throw Error('Provide unique pack facts, source columns and explicit conversions for this dialect.')
 if(pack.key!==proposal.packKey||pack.digest!==proposal.packDigest)throw Error('The pack changed. Read the current dialect context and request a new proposal.')
 const doc=boundPack(pack),fields=factFields(doc),targets=new Set(fields.map(f=>f.path)),requirements=new Set((doc.evidenceRequirements??[]).map(e=>e.id)),columns=new Set(dialectColumns(saved))
 for(const f of proposal.facts){
  if(!targets.has(f.target)||!columns.has(f.column))throw Error('Every dialect fact must use a current pack input and an existing or declared research column.')
  if(!f.target.startsWith('/')||/~(?![01])/.test(f.target)||f.target.split('/').some(s=>['__proto__','constructor','prototype'].includes(s)))throw Error('The dialect contains an unsupported fact path.')
  if(proposal.facts.some(other=>other!==f&&(f.target.startsWith(other.target+'/')||other.target.startsWith(f.target+'/'))))throw Error('Dialect fact paths must not overlap.')
 }
 for(const e of proposal.evidence)if(!requirements.has(e.requirement)||!columns.has(e.column))throw Error('Every evidence binding must use a current requirement and source column.')
 return {...pack,mapping:{version:2,sources:[],case:{facts:proposal.facts.map(f=>({target:f.target,source:'/values/'+pointerEscape(f.column)})),evidence:proposal.evidence.map(e=>({requirement:e.requirement,source:'/values/'+pointerEscape(e.column)}))},unmapped:fields.filter(f=>!proposal.facts.some(row=>row.target===f.path)).map(f=>f.path),unmappedEvidence:[...requirements].filter(id=>!proposal.evidence.some(e=>e.requirement===id))},conversions:Object.fromEntries(proposal.facts.map(f=>[f.target,{...f.conversion}]))}
}


const exact=(value:Record<string,unknown>,required:string[],optional:string[]=[])=>required.every(key=>Object.hasOwn(value,key))&&keys(value,[...required,...optional])
const scalar=(value:unknown):value is string=>typeof value==='string'&&!/[\uD800-\uDFFF]/u.test(value)
const retainedText=(value:unknown,nonblank=false):value is string=>scalar(value)&&value.length>0&&new TextEncoder().encode(value).length<=160&&(!nonblank||!/^[\u0009-\u000D\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]*$/u.test(value))
const retainedPointer=(value:unknown):value is string=>scalar(value)&&value.startsWith('/')&&new TextEncoder().encode(value).length<=1024&&!/~(?![01])/.test(value)&&!value.slice(1).split('/').some(part=>['__proto__','constructor','prototype'].includes(part))
export function validRetainedContract(value:unknown):value is DialectConversionContract {
 if(!object(value)||!exact(value,['contract','spec','profile'])||!object(value.spec)||!exact(value.spec,['version','revision','schemaSha256','corpusSha256']))return false
 const installed=DIALECT_RETAINED_COLUMN_DRAFT,spec=value.spec
 return value.contract===installed.contract&&value.profile===installed.profile&&Object.keys(installed.spec).every(key=>spec[key]===installed.spec[key as keyof typeof installed.spec])
}
function retainedSidecar(value:unknown,leaf:boolean):DialectConversion {
 if(!object(value)||!exact(value,leaf?['contractVersion','column','kind']:['kind'],['trueValue','falseValue']))throw Error('Invalid retained conversion.')
 if(leaf&&(value.contractVersion!=='1'||!retainedText(value.column)))throw Error('Invalid retained conversion.')
 if(value.kind==='boolean'){
  if(!Object.hasOwn(value,'trueValue')||!Object.hasOwn(value,'falseValue')||!retainedText(value.trueValue,true)||!retainedText(value.falseValue,true)||value.trueValue===value.falseValue)throw Error('Invalid retained conversion.')
  return {kind:'boolean',trueValue:value.trueValue,falseValue:value.falseValue}
 }
 if(typeof value.kind!=='string'||!['text','decimal','number','research-value'].includes(value.kind)||Object.hasOwn(value,'trueValue')||Object.hasOwn(value,'falseValue'))throw Error('Invalid retained conversion.')
 return {kind:value.kind as DialectConversion['kind']}
}
export interface RetainedDialectRules {
 facts:{target:string;column:string;conversion:DialectConversion}[]
 evidence:{requirement:string;column:string}[]
}
/** Logical template only. This grants no save, preparation or evaluation admission. */
export function retainedDialectRules(binding:unknown):RetainedDialectRules {
 if(!object(binding)||!exact(binding,['conversionContract','mapping'],['kind','key','pack','digest','conversions'])||!validRetainedContract(binding.conversionContract)||!object(binding.mapping))throw Error('Select the installed retained-column contract explicitly.')
 const m=binding.mapping
 if(!exact(m,['version','profile','case','sources'],['unmapped','unmappedEvidence'])||m.version!==2||m.profile!==binding.conversionContract.profile||!Array.isArray(m.sources)||m.sources.length||!object(m.case)||!exact(m.case,['facts','evidence'])||!Array.isArray(m.case.facts)||m.case.facts.length>256||!Array.isArray(m.case.evidence)||m.case.evidence.length>128)throw Error('Invalid retained logical mapping.')
 const sidecars=binding.conversions===undefined?{}:binding.conversions
 if(!object(sidecars))throw Error('Invalid retained conversion sidecars.')
 const targets=new Set<string>(),requirements=new Set<string>()
 const facts=m.case.facts.map(f=>{
  if(!object(f)||!exact(f,['target','source','conversion'])||!retainedPointer(f.target)||f.source!=='/retained'||targets.has(f.target))throw Error('Invalid retained fact binding.')
  const target=f.target
  if([...targets].some(prior=>prior.startsWith(target+'/')||target.startsWith(prior+'/')))throw Error('Overlapping retained fact targets.')
  const conversion=retainedSidecar(f.conversion,true)
  if(!Object.hasOwn(sidecars,f.target))throw Error('Missing retained conversion sidecar.')
  const sidecar=retainedSidecar(sidecars[f.target],false)
  if(conversion.kind!==sidecar.kind||conversion.trueValue!==sidecar.trueValue||conversion.falseValue!==sidecar.falseValue)throw Error('Retained conversion sidecar disagrees with its rule.')
  targets.add(f.target)
  return {target:f.target,column:(f.conversion as RetainedConversion).column,conversion}
 })
 if(Object.keys(sidecars).length!==facts.length)throw Error('Unexpected retained conversion sidecar.')
 const evidence=m.case.evidence.map(e=>{
  if(!object(e)||!exact(e,['requirement','source','availability'])||!scalar(e.requirement)||!e.requirement||requirements.has(e.requirement)||e.source!=='/retained'||!object(e.availability)||!exact(e.availability,['contractVersion','column'])||e.availability.contractVersion!=='1'||!retainedText(e.availability.column))throw Error('Invalid retained evidence binding.')
  requirements.add(e.requirement)
  return {requirement:e.requirement,column:e.availability.column}
 })
 for(const [value,seen,pointer] of [[m.unmapped,targets,true],[m.unmappedEvidence,requirements,false]] as const){
  if(value===undefined)continue
  if(!Array.isArray(value))throw Error('Invalid retained unmapped targets.')
  for(const target of value){if(!scalar(target)||!target||(pointer&&!retainedPointer(target))||seen.has(target))throw Error('Invalid retained unmapped target.');seen.add(target)}
 }
 return {facts,evidence}
}
/** Duplicate-aware reader for selected controls. Retained frame/value custody is opaque elsewhere. */
export function readRetainedDialectRules(json:string):RetainedDialectRules { return retainedDialectRules(parseJSON(json)) }
/** Explicit reviewed identity is required; no default or migration is inferred. */
export function buildRetainedDialect(pack:DialectPackBinding,rules:RetainedDialectRules,selection:DialectConversionContract,unmapped:string[]=[],unmappedEvidence:string[]=[]):DialectPackBinding {
 if(!validRetainedContract(selection))throw Error('Select the installed retained-column contract explicitly.')
 const conversions=Object.fromEntries(rules.facts.map(f=>[f.target,retainedSidecar(f.conversion,false)]))
 const binding:DialectPackBinding={...pack,conversionContract:{...selection,spec:{...selection.spec}},mapping:{version:2,profile:'base',sources:[],case:{facts:rules.facts.map(f=>({target:f.target,source:'/retained',conversion:{contractVersion:'1',column:f.column,...retainedSidecar(f.conversion,false)} as RetainedConversion})),evidence:rules.evidence.map(e=>({requirement:e.requirement,source:'/retained',availability:{contractVersion:'1',column:e.column}}))},unmapped:[...unmapped],unmappedEvidence:[...unmappedEvidence]},conversions}
 retainedDialectRules(binding)
 return binding
}

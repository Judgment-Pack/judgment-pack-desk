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

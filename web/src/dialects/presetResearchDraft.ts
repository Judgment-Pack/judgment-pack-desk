import {msg} from '../i18n'
import {parseMapping} from '../jobs/mappedInputs'
import type {Candidate,ImportRecord} from '../exchange/client'
import {validDialectRecipeContent} from './proposals'
import type {DialectTemplate,TemplateOrigin} from './templates'

/** Create an editable local authoring fork. This does not bind, execute or
 * claim conformance of the published portable source. Its import remains exact. */
export function researchDraftFromPreset(candidate:Candidate,record:ImportRecord,purpose:string):{template:DialectTemplate;origin:TemplateOrigin}{
 const fail=()=>Error(msg('Choose a research preset with one source and a single text question. Other definitions can be configured in Exchange templates.'))
 if(candidate.entry.kind!=='preset'||record.entryId!==candidate.row.id||! /^[a-f0-9]{64}$/.test(record.id))throw fail()
 const mapping=parseMapping(candidate.document),sources=mapping.sources??[]
 if(sources.length!==1||sources[0].kind!=='research')throw fail()
 const source=sources[0],question=source.question
 let request:string
 if(typeof question==='string')request=question
 else if(question&&typeof question==='object'&&!Array.isArray(question)&&Object.keys(question).length===1&&'$param' in question&&typeof question.$param==='string'){
  const parameter=mapping.case?.parameters?.[question.$param]
  if(!parameter||parameter.type!=='string'||parameter.from||Object.keys(mapping.case?.parameters??{}).length!==1)throw fail()
  if(!purpose.trim())throw fail()
  request=purpose
 }else throw fail()
 // A draft cannot silently reinterpret additional parameter substitutions.
 const hasParameter=(v:unknown):boolean=>!!v&&typeof v==='object'&&(Object.hasOwn(v,'$param')||Object.values(v).some(hasParameter))
 if(hasParameter(source.constraints)||hasParameter(source.evidence))throw fail()
 const instructions=[request,JSON.stringify({constraints:source.constraints,evidence:source.evidence,fields:source.fields},null,2)].filter(Boolean).join('\n\n')
 const research={target:'records' as const,fields:source.fields.map(({name,type,description})=>({name,type,description:description??name})),criteria:[],limits:{records:10,searches:0,pages:20,modelCalls:40}}
 // These are the existing local editor limits, never a partial/truncated import.
 const bytes=(text:string)=>new TextEncoder().encode(text).length
 if(bytes(candidate.entry.title)>160||bytes(candidate.entry.licence)>160||bytes(instructions)>12000||research.fields.some(field=>bytes(field.description)>1000)||!validDialectRecipeContent({name:candidate.entry.title,intent:instructions,...research}))throw Error(msg('This preset exceeds the local recipe editor limits. Configure its portable definition in Exchange templates.'))
 const origin={id:'exchange-'+record.id,name:candidate.entry.title,digest:candidate.digest,license:candidate.entry.licence}
 return {origin,template:{templateVersion:1,id:origin.id,name:origin.name,description:candidate.entry.description??'',license:origin.license,intent:instructions,research}}
}

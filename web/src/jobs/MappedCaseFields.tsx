import { useEffect, useId, useState } from 'react'
import { msg } from '../i18n'
import type { PackDocument } from '../mcp/types'
import { factFields, pointerGet, pointerSet } from '../packs/test-workspace/model'
import { FactInput } from '../packs/test-workspace/CaseEditor'
import { Select } from '../ui/Select'
import {editableJSON} from './inputModel'
import { parseMappedObject,parseMappedCase } from './mappedInputs'
import type { ExecutableDialect } from './dialectTypes'
import { mappingErrorMessage } from './mappingError'
import styles from './JobsView.module.css'

const parseCaseValue=(text:string)=>parseMappedObject('{"value":'+text+'}').value

export function MappedCaseFields({doc,mapping,text,disabled,onChange,onValid}:{doc:PackDocument;mapping:ExecutableDialect;text:string;disabled:boolean;onChange:(text:string)=>void;onValid:(valid:boolean)=>void}){
 const id=useId()
 const [invalid,setInvalid]=useState<Record<string,boolean>>({}),[editError,setEditError]=useState('')
 let value:unknown,error='',rawOnly=false
 try{parseMappedCase(text);try{value=editableJSON(text)}catch{rawOnly=true}}catch(e){error=mappingErrorMessage(e)}
 const valid=!error&&!editError&&!Object.values(invalid).some(Boolean)
 useEffect(()=>onValid(valid),[onValid,valid])
 useEffect(()=>{setInvalid({});setEditError('')},[text,mapping])
 const fields=factFields(doc), copies=mapping.case?.facts??[], evidence=mapping.case?.evidence??[]
 const parameters=Object.entries(mapping.case?.parameters??{}).filter(([name])=>!mapping.sources?.some(s=>s.provider==='google-drive'&&[s.arguments?.fileId,s.arguments?.grant].some(v=>v&&typeof v==='object'&&'$param' in v&&v.$param===name)))
 function patch(path:string,next:unknown){try{onChange(JSON.stringify(pointerSet(value,path,next),null,2));setEditError('')}catch(e){setEditError(mappingErrorMessage(e))}}
 if(rawOnly)return <p className={styles.note}>{msg('Use JSON to preserve these exact numeric values.')}</p>
 return <fieldset className={styles.fields} disabled={disabled||Boolean(error)}>
  {(copies.length>0||evidence.length>0||parameters.length>0)&&<h3>{msg('Case inputs')}</h3>}
  <div className={styles.formGrid}>{copies.map((c,i)=>{const f=fields.find(f=>f.path===c.target),label=f?.label??c.target;return <div className={styles.field} key={`${c.target}-${i}`}><span>{label}</span><FactInput parseJSON={parseCaseValue} label={label} type={f?.type??'json'} value={pointerGet(value,c.source)} onChange={next=>patch(c.source,next)} onValid={ok=>setInvalid(old=>old[c.source]===!ok?old:{...old,[c.source]:!ok})}/></div>})}
  {parameters.map(([name,p])=><div className={styles.field} key={name}><span>{name}</span><FactInput parseJSON={parseCaseValue} label={name} type={p.type==='integer'?'number':'string'} value={pointerGet(value,p.pointer)} onChange={next=>patch(p.pointer,next)} onValid={ok=>setInvalid(old=>old[p.pointer]===!ok?old:{...old,[p.pointer]:!ok})}/></div>)}
  {evidence.map((c,i)=>{const label=doc.evidenceRequirements?.find(e=>e.id===c.requirement)?.description||c.requirement;return <div key={`${c.requirement}-${i}`} className={styles.field}><span>{label}</span><Select id={`${id}-${i}`} aria-label={label} value={String(pointerGet(value,c.source)??'omitted')} disabled={disabled} options={[{value:'omitted',label:msg('Not supplied')},{value:'present',label:msg('Present')},{value:'absent',label:msg('Absent')},{value:'unknown',label:msg('Unknown')}]} onValueChange={next=>patch(c.source,next==='omitted'?undefined:next)}/></div>})}</div>
  {(error||editError)&&<p role="alert" className={styles.problem}>{error||editError}</p>}
 </fieldset>
}

import { useEffect, useId, useMemo, useState } from 'react'
import { msg, useLocale } from '../i18n'
import type { PackDocument } from '../mcp/types'
import { factFields, object, pointerGet, pointerSet } from '../packs/test-workspace/model'
import { FactInput } from '../packs/test-workspace/CaseEditor'
import { Disclosure } from '../ui/Disclosure'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
import { editableJSON, parseInput } from './inputModel'
import styles from './JobsView.module.css'

export interface InputFieldsProps {
 doc?: PackDocument; facts: string; setFacts: (value: string) => void;
 supplied: boolean; setSupplied: (value: boolean) => void; evidence: string; setEvidence: (value: string) => void;
 onUnwritten?: (unwritten: boolean) => void; disabled: boolean; onValid?: (valid: boolean) => void;
}
export function InputFields({doc, facts, setFacts, supplied, setSupplied, evidence, setEvidence, disabled, onValid, onUnwritten}: InputFieldsProps) {
 useLocale()
 const id=useId(), [invalid,setInvalid]=useState<Record<string,boolean>>({}), [editError,setEditError]=useState('')
 const fields=useMemo(()=>factFields(doc).filter(f=>!f.path.split('/').some(k=>/^\d+$/.test(k)||['__proto__','constructor','prototype'].includes(k))),[doc])
 let values: unknown, availability: Record<string,string> = {}, error='', formError=''
 try { parseInput(facts,supplied,evidence) } catch(e) {error=(e as Error).message}
 try {values=editableJSON(facts)} catch(e) {formError=(e as Error).message}
 try { const parsed=JSON.parse(evidence);if(object(parsed))availability=parsed as Record<string,string> } catch { /* JSON editor retains malformed input for correction. */ }
 const valid=!error&&!editError&&!Object.values(invalid).some(Boolean)
 useEffect(()=>{onValid?.(valid)},[valid,onValid])
 const unwritten=!!editError||Object.values(invalid).some(Boolean)
 useEffect(()=>{onUnwritten?.(unwritten);return()=>onUnwritten?.(false)},[unwritten,onUnwritten])
 useEffect(()=>{setInvalid({});setEditError('')},[doc])
 function patch(path: string,value: unknown){
  try {setFacts(JSON.stringify(pointerSet(values,path,value),null,2));setEditError('');setInvalid(v=>({...v,[path]:false}))}catch(e){setEditError((e as Error).message)}
 }
 const evidenceFields=doc?.evidenceRequirements ?? []
 return <fieldset className={styles.fields} disabled={disabled}>
  {fields.length>0&&!formError&&<><p className={styles.note}>{msg('Omitted values remain unknown.')}</p><div className={styles.formGrid}>
   {fields.map(f=><div className={styles.field} key={f.path}><span className={styles.fieldLabel}>{f.label}</span><FactInput parseJSON={editableJSON} label={f.label} type={f.type} value={pointerGet(values,f.path)} onChange={v=>patch(f.path,v)} onValid={ok=>setInvalid(previous=>previous[f.path]===!ok?previous:{...previous,[f.path]:!ok})}/></div>)}
  </div></>}
  {(error||editError)&&<p role="alert" className={styles.problem}>{error||editError}</p>}
  {formError&&!error&&<p className={styles.note}>{formError}</p>}
  <Disclosure title={msg('Facts (JSON)')} open={!fields.length||Boolean(formError)}><div className={styles.field}><label htmlFor={`${id}-facts`}>{msg('Facts (JSON)')}</label><TextArea id={`${id}-facts`} rows={7} value={facts} onChange={e=>{setFacts(e.target.value);setInvalid({});setEditError('')}} spellCheck={false}/><p className={styles.note}>{msg('Use nested values at the paths the pack reads. Omitted values remain unknown.')}</p></div></Disclosure>
  <label className="checkbox"><input type="checkbox" checked={supplied} onChange={e=>setSupplied(e.target.checked)}/>{msg('Supply evidence availability')}</label>
  {supplied&&<><p className={styles.note}>{msg('Attaching a source does not mark an evidence requirement as satisfied.')}</p>
   <div className={styles.formGrid}>{evidenceFields.map((f,index)=><div className={styles.field} key={f.id}><label htmlFor={`${id}-evidence-${index}`}>{f.description||f.id}</label><Select id={`${id}-evidence-${index}`} value={availability[f.id]??'omitted'} disabled={disabled||Boolean(error)} options={[{value:'omitted',label:msg('Not supplied')},{value:'present',label:msg('Present')},{value:'absent',label:msg('Absent')},{value:'unknown',label:msg('Unknown')}]} onValueChange={v=>{const next={...availability};if(v==='omitted')delete next[f.id];else next[f.id]=v;setEvidence(JSON.stringify(next,null,2))}}/></div>)}</div>
   <Disclosure title={msg('Evidence availability (JSON)')} open={!evidenceFields.length}><div className={styles.field}><label htmlFor={`${id}-evidence`}>{msg('Evidence availability (JSON)')}</label><TextArea id={`${id}-evidence`} rows={4} value={evidence} onChange={e=>setEvidence(e.target.value)} spellCheck={false}/></div></Disclosure>
  </>}
 </fieldset>
}

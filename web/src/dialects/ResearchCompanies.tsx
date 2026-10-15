import {dialectListQuery,dialectQuery} from './queries'
import {LoadingState} from '../ui/LoadingState'
import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {type DialectRecord} from './client'
import type {ResearchRecipe} from './research'
import styles from './DialectsView.module.css'

/** Company selection is human-owned. Only the chosen input's records are read. */
export function ResearchCompanies({value,records,disabled,onChange}:{value:ResearchRecipe;records:DialectRecord[];disabled:boolean;onChange:(change:Partial<ResearchRecipe>)=>void}) {
 const [source,setSource]=useState(value.companyInputs?.[0]?.preparationId??'current')
 const inputs=useQuery(dialectListQuery())
 const selected=useQuery({...dialectQuery(source),enabled:source!=='current'})
 const companies=Array.from(new Map((source==='current'?records:selected.data?.document.records??[]).filter(r=>r.research?.entity.kind==='company').map(r=>[r.research!.entity.id,r.research!.entity])).values())
 const count=(value.companyIds?.length??0)+(value.companyInputs?.length??0)
 return <fieldset className={styles.researchCompanies}><legend>{msg('Selected companies')}</legend>
  <Field label={msg('Company records')} hint={msg('Choose the companies to research people at. The selected records are copied into the research review.')}>{props=><Select {...props} value={source} disabled={disabled} options={[{value:'current',label:msg('This dialect')},...(inputs.data?.items??[]).map(input=>({value:input.id,label:input.name}))]} onValueChange={id=>{setSource(id);onChange({companyIds:[],companyInputs:[]})}}/>}</Field>
  {(inputs.error||selected.error)&&<Notice tone="error">{(inputs.error??selected.error)?.message}</Notice>}
  {source!=='current'&&selected.isPending?<LoadingState>{msg('Loading company records…')}</LoadingState>:companies.length?companies.map(company=>{
   const checked=source==='current'?value.companyIds?.includes(company.id)??false:value.companyInputs?.some(ref=>ref.preparationId===source&&ref.entityId===company.id)??false
   return <label className="checkbox" key={company.id}><input type="checkbox" disabled={disabled||!checked&&count>=50} checked={checked} onChange={event=>source==='current'?onChange({companyIds:event.target.checked?[...(value.companyIds??[]),company.id]:(value.companyIds??[]).filter(id=>id!==company.id)}):onChange({companyInputs:event.target.checked?[...(value.companyInputs??[]),{preparationId:source,revision:selected.data!.sha256,entityId:company.id}]:(value.companyInputs??[]).filter(ref=>ref.entityId!==company.id)})}/><span>{company.name}</span></label>
  }):<p className={styles.note}>{msg('No company records in this dialect. Choose another dialect, or research companies first.')}</p>}
 </fieldset>
}

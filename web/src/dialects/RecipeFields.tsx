import {useId,useRef,useState} from 'react'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {CollectionTable} from '../ui/SelectionTable'
import {CollectionToolbar} from '../ui/CollectionToolbar'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {Select} from '../ui/Select'
import {IconPlus,IconTrash} from '../shell/icons'
import type {ResearchRecipe} from './research'
import styles from './RecipeSection.module.css'

type Fields=ResearchRecipe['fields']
/** Every field edits the same recipe buffer; filtering never commits or discards edits. */
export function RecipeFields({value,onChange,disabled}:{expanded?:boolean;value:Fields;onChange:(fields:Fields)=>void;disabled:boolean}){
 const [search,setSearch]=useState(''),[type,setType]=useState('all'),id=useId(),add=useRef<HTMLButtonElement|null>(null)
 const rows=value.map((field,index)=>({field,index})).filter(({field})=>(!search||`${field.name} ${field.description}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()))&&(type==='all'||(field.type==='string'?'text':field.type)===type))
 const types=[{value:'text',label:msg('Text')},{value:'integer',label:msg('Integer')},{value:'decimal',label:msg('Decimal')},{value:'boolean',label:msg('Yes / No')},{value:'text-list',label:msg('Text list')},{value:'range',label:msg('Range')},...(value.some(field=>field.type==='number')?[{value:'number',label:msg('Number')}]:[])]
 const edit=(index:number,change:Partial<Fields[number]>)=>onChange(value.map((field,i)=>i===index?{...field,...change}:field))
 return <section className={styles.section} aria-labelledby={id+'-title'}>
  <header className={styles.heading}><h3 id={id+'-title'}>{msg('Requested fields')} · {value.length}</h3><Button ref={add} variant="quiet" size="small" disabled={disabled||value.length>=30} onClick={()=>{setSearch('');setType('all');onChange([...value,{name:'',description:'',type:'text'}])}}><IconPlus/>{msg('Add field')}</Button></header>
  <CollectionToolbar label={msg('Requested fields')} searchLabel={msg('Search fields')} search={search} onSearch={setSearch} filters={[{label:msg('Type'),value:type,onValueChange:setType,options:[{value:'all',label:msg('All types')},...types]}]} filtered={!!search||type!=='all'} onReset={()=>{setSearch('');setType('all')}}/>
  <CollectionTable className={styles.table} aria-label={msg('Requested fields')}><thead><tr><th scope="col">{msg('Field')}</th><th scope="col">{msg('Type')}</th><th scope="col">{msg('Description')}</th><th scope="col"><span className={styles.srOnly}>{msg('Actions')}</span></th></tr></thead><tbody>{rows.map(({field,index})=><tr key={index}>
   <td><Input aria-label={msg('Field name')} disabled={disabled} maxLength={64} value={field.name} onChange={event=>edit(index,{name:event.target.value})}/></td>
   <td><Select id={id+'-type-'+index} aria-label={msg('Type')} disabled={disabled} value={field.type==='string'?'text':field.type} options={types} onValueChange={type=>edit(index,{type:type as Fields[number]['type']})}/></td>
   <td><TextArea aria-label={msg('Description')} disabled={disabled} rows={3} maxLength={1000} value={field.description} onChange={event=>edit(index,{description:event.target.value})}/></td>
   <td><Button size="small-icon" variant="quiet" aria-label={msg('Remove field {{name}}',{name:field.name})} disabled={disabled||value.length===1} onClick={()=>{onChange(value.filter((_,i)=>i!==index));add.current?.focus()}}><IconTrash/></Button></td>
  </tr>)}</tbody></CollectionTable>
  {!rows.length&&<p className={styles.prose}>{msg('No matching fields')}</p>}
 </section>
}

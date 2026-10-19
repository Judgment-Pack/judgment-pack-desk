import {Fragment,useId,useState,type ReactNode} from 'react'
import {msg} from '../i18n'
import {Button} from './Button'
import {CollectionToolbar} from './CollectionToolbar'
import {IconChevronRight,IconChevronDown} from '../shell/icons'
import styles from './RecordDetailsTable.module.css'

export type RecordDetailField={key:string;label:string;value:string;state:string;attention?:boolean;changed?:boolean;details:ReactNode}
/** Read-only field inspection. Evidence expands across the row; selection and writes belong to the owning workflow. */
export function RecordDetailsTable({fields,hasPrevious=false}:{fields:RecordDetailField[];hasPrevious?:boolean}){
 const [search,setSearch]=useState(''),[filter,setFilter]=useState('all'),[expanded,setExpanded]=useState<Set<string>>(new Set()),[full,setFull]=useState<Set<string>>(new Set())
 const id=useId()
 const rows=fields.filter(field=>(!search||`${field.label} ${field.key} ${field.value} ${field.state}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()))&&(filter==='all'||filter==='attention'&&field.attention||filter==='changed'&&field.changed))
 const toggle=(key:string,set:(update:(current:Set<string>)=>Set<string>)=>void)=>set(current=>{const next=new Set(current);if(next.has(key))next.delete(key);else next.add(key);return next})
 return <div className={styles.root}>
  <CollectionToolbar label={msg('Research fields')} searchLabel={msg('Search fields')} search={search} onSearch={setSearch} filters={[{label:msg('Fields'),value:filter,onValueChange:setFilter,options:[{value:'all',label:msg('All fields')},{value:'attention',label:msg('Needs attention')},...(hasPrevious?[{value:'changed',label:msg('Changed')}]:[])]}]} filtered={!!search||filter!=='all'} onReset={()=>{setSearch('');setFilter('all')}}/>
  <table className={styles.table} aria-label={msg('Research fields')}><colgroup><col className={styles.fieldColumn}/><col/><col className={styles.evidenceColumn}/></colgroup><thead><tr><th scope="col">{msg('Field')}</th><th scope="col">{msg('Value')}</th><th scope="col">{msg('Evidence')}</th></tr></thead><tbody>{rows.map((field,index)=>{
   const open=expanded.has(field.key),long=field.value.length>240,showFull=full.has(field.key),detailsId=`${id}-${index}`
   return <Fragment key={field.key}><tr className={styles.field} data-attention={field.attention||undefined}><th scope="row">{field.label}{field.changed&&<span className={styles.changed}>{msg('Changed')}</span>}</th><td className={styles.value}><span>{long&&!showFull?field.value.slice(0,240):field.value}</span>{long&&<Button size="small" variant="quiet" aria-expanded={showFull} aria-label={showFull?msg('Show less {{field}}',{field:field.label}):msg('Show more {{field}}',{field:field.label})} onClick={()=>toggle(field.key,setFull)}>{showFull?msg('Show less'):msg('Show more')}</Button>}</td><td><Button className={styles.evidence} size="small" variant="quiet" aria-expanded={open} aria-controls={open?detailsId:undefined} aria-label={msg('Details for {{field}}',{field:field.label})} onClick={()=>toggle(field.key,setExpanded)}>{open?<IconChevronDown/>:<IconChevronRight/>}<span>{field.state}</span></Button></td></tr>{open&&<tr className={styles.expansion}><td colSpan={3}><div id={detailsId} className={styles.details}>{field.details}</div></td></tr>}</Fragment>
  })}</tbody></table>
  {!rows.length&&<p className={styles.empty}>{msg('No matching fields')}</p>}
  <p className={styles.count}>{msg('{{shown}} of {{total}} fields',{shown:rows.length,total:fields.length})}</p>
 </div>
}

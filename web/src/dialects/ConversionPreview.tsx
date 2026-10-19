import {checkDialect} from '../dialect/authoring'
import {LoadingState} from '../ui/LoadingState'
import {useState} from 'react'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Disclosure} from '../ui/Disclosure'
import {Notice} from '../ui/Notice'
import {humanId} from '../packs/logicModel'
import {dialectAPI,type DialectPackBinding,type PreparationReply,type PreparedRow} from './client'
import styles from './DialectsView.module.css'
export function ConversionPreview({saved,value,disabled,onChecked,expanded=false}:{saved:PreparationReply;value:DialectPackBinding;disabled:boolean;onChecked?:(identity:string)=>void;expanded?:boolean}){
 const [result,setResult]=useState<{identity:string;rows:PreparedRow[]}>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const identity=JSON.stringify([saved.sha256,value]),rows=result?.identity===identity?result.rows:undefined
 async function check(){setBusy(true);setError('');try{await checkDialect(value.mapping,value.pack);const result=await dialectAPI<{rows:PreparedRow[]}>('/'+saved.document.id+'/conversion-preview','POST',{qualification:value},saved.sha256);setResult({identity,rows:result.rows});onChecked?.(identity)}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 return <Disclosure open={expanded||undefined} title={msg('Conversion rules and examples')}><div className={styles.stack}><p className={styles.note}>{msg('Preview these conversions across your current records before saving. Originals remain unchanged.')}</p><div><Button disabled={busy||disabled||!saved.document.records.length} onClick={()=>void check()}>{busy?<LoadingState inline>{msg('Checking…')}</LoadingState>:msg('Preview conversions')}</Button></div>{error&&<Notice tone="warning">{error}</Notice>}{rows&&<><p className={styles.note}>{msg('{{count}} records · {{issues}} with issues',{count:rows.length,issues:rows.filter(r=>r.issues.length).length})}</p><div className={`${styles.tableWrap} ${styles.reviewTable}`} tabIndex={0} role="region" aria-label={msg('Conversion preview')}><table className={styles.table}><thead><tr><th>{msg('Record')}</th><th>{msg('Dialect')}</th><th>{msg('Used for evaluation')}</th></tr></thead><tbody>{rows.filter((r,index)=>index===0||r.issues.length>0).flatMap(row=>[...Object.entries(row.prepared).filter(([target])=>!row.issues.some(i=>i.target===target)).map(([target,value])=><tr key={row.id+target}><td>{saved.document.records.find(r=>r.id===row.id)?.name}</td><td>{humanId(target,target)}</td><td>{JSON.stringify(value)}</td></tr>),...row.issues.map(issue=><tr key={row.id+issue.target}><td>{saved.document.records.find(r=>r.id===row.id)?.name}</td><td>{humanId(issue.target,issue.target)}</td><td className={styles.problem}>{issue.message}</td></tr>)])}</tbody></table></div></>}</div></Disclosure>
}

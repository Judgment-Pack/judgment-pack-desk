import {msg} from '../i18n'
import {Field,FieldGroup} from '../ui/Field'
import {Select} from '../ui/Select'
import type {ResearchExecution,ResearchExecutionPolicy} from './research'
import styles from './DialectsView.module.css'

export function ResearchExecutionSettings({value,connectionLimit,disabled,onChange}:{value?:ResearchExecutionPolicy;connectionLimit:1|2;disabled:boolean;onChange:(value:ResearchExecutionPolicy)=>void}){
 const approach=value?.approach??'standard',concurrency=value?.concurrency??0
 const effective=approach==='standard'?1:Math.min(concurrency||connectionLimit,connectionLimit)
 return <FieldGroup>
  <Field label={msg('Research approach')} hint={approach==='standard'?msg('One agent manages discovery and record preparation.'):msg('A coordinator delegates independent research tasks. All tasks share the recipe’s limits.')}>{props=><Select {...props} disabled={disabled} value={approach} onValueChange={approach=>onChange({approach:approach as ResearchExecutionPolicy['approach'],concurrency})} options={[{value:'standard',label:msg('Standard')},{value:'delegated',label:msg('Delegated')}]}/>}</Field>
  {approach==='delegated'&&<>
   <Field label={msg('Concurrent AI tasks')} hint={msg('Auto chooses within the connection limit. A number sets the maximum; approval settings stay unchanged.')}>{props=><Select {...props} disabled={disabled} value={String(concurrency)} onValueChange={value=>onChange({approach,concurrency:Number(value) as 0|1|2})} options={[{value:'0',label:msg('Auto')},{value:'1',label:'1'},{value:'2',label:connectionLimit===1?msg('2 · Unavailable with this connection'):'2',disabled:connectionLimit===1}]}/>}</Field>
   <p className={styles.note}>{connectionLimit===1?msg('Currently: 1 AI task at a time · ChatGPT connection limit'):msg('Up to {{count}} concurrent AI tasks',{count:effective})}</p>
  </>}
 </FieldGroup>
}
export function ResearchExecutionDetails({execution}:{execution?:ResearchExecution}){
 if(!execution)return null
 return <><dt>{msg('Research approach')}</dt><dd>{execution.approach==='delegated'?msg('Delegated'):msg('Standard')}</dd>{execution.approach==='delegated'&&<><dt>{msg('Concurrent AI tasks')}</dt><dd>{execution.requestedConcurrency===0?msg('Auto · Up to {{count}}',{count:execution.concurrency}):msg('Up to {{count}}',{count:execution.concurrency})}{execution.connectionLimit===1&&<> · {msg('ChatGPT connection limit')}</>}</dd></>}</>
}
